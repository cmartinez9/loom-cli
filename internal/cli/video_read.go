package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
	"github.com/cmartinez9/loom-cli/internal/store"
)

// Shared plumbing for the per-video read commands (transcript, captions,
// summary, chapters, description, takeaways, comments/tasks/reactions/tags
// list, backlinks). Each command describes its live fetch, its optional
// local-store read and its optional write-through; runVideoRead applies the
// global --data-source policy:
//
//	live  -> Loom only
//	local -> the local store only (missing-mirror guard when it does not exist)
//	auto  -> Loom, falling back to the local store only when the session has
//	         expired (loom.ErrSessionExpired)
//
// A successful live fetch is written through to the local store when the
// store file already exists, so grep and digest see fresh data; the store is
// never created just for write-through.

// videoRead describes one per-video read.
type videoRead[T any] struct {
	// what names the data in hints ("transcript", "AI summary", ...).
	what string
	// live fetches from Loom.
	live func(ctx context.Context, lc *loom.Client, id string) (T, error)
	// local reads the local store; nil means the data is not mirrored.
	// It returns sql.ErrNoRows when the video has no stored row.
	local func(ctx context.Context, db *store.Store, id string) (T, error)
	// empty is the value printed when there is nothing to show.
	empty func(id string) T
	// emptyLive reports whether a live result is empty; when it is, the
	// video's existence is confirmed so unknown ids exit 3, not 0.
	emptyLive func(v T) bool
	// save writes a live result through to the store; nil disables it.
	save func(ctx context.Context, db *store.Store, id string, v T) error
}

// videoReadResult is what runVideoRead produced.
type videoReadResult[T any] struct {
	ID     string
	Value  T
	Source string // sourceLive or sourceLocal
	// Missing is set when --data-source local found no local mirror (or no
	// row for the video); Value is then the empty value and a hint has
	// already been written to stderr.
	Missing bool
}

// addVideoDBFlag declares the --db flag the local-store paths use.
func addVideoDBFlag(cmd *cobra.Command, dbPath *string) {
	cmd.Flags().StringVar(dbPath, "db", "", "Local store path used by --data-source local and write-through (default: the CLI data dir)")
}

// videoReadAnnotations are the annotations every per-video read carries.
// source is the command's data-source policy: sourceAuto when it has a
// local reader, sourceLive when the data is not mirrored locally.
func videoReadAnnotations(source string) map[string]string {
	return map[string]string{
		"mcp:read-only":  "true",
		"pp:data-source": source,
		"pp:happy-args":  "video=latest",
	}
}

// runVideoRead enforces the command's declared data-source policy
// (checkDataSource), resolves arg to a video id and reads it according to
// --data-source.
func runVideoRead[T any](cmd *cobra.Command, flags *rootFlags, dbPath, arg string, r videoRead[T]) (*videoReadResult[T], error) {
	if err := checkDataSource(cmd, flags); err != nil {
		return nil, err
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()

	latest := isLatestArg(arg)
	id := ""
	if !latest {
		parsed, err := videoIDArg(arg)
		if err != nil {
			return nil, err
		}
		id = parsed
	}

	mode := dataSourceMode(flags)
	if mode == sourceLocal {
		return readVideoLocal(ctx, cmd, dbPath, id, latest, r, nil)
	}

	lc, err := newLoomClient(flags)
	if err != nil {
		return nil, err
	}
	fallback := func(liveErr error) (*videoReadResult[T], error) {
		res, err := readVideoLocal(ctx, cmd, dbPath, id, latest, r, liveErr)
		if err == nil {
			path, _ := localStoreExists(dbPath)
			localFallbackNote(cmd, path, r.what)
		}
		return res, err
	}
	if latest {
		resolved, err := resolveVideoArg(ctx, lc, arg)
		if err != nil {
			if mode == sourceAuto && errors.Is(err, loom.ErrSessionExpired) {
				return fallback(err)
			}
			return nil, err
		}
		id = resolved
	}
	v, err := r.live(ctx, lc, id)
	if err != nil {
		if mode == sourceAuto && errors.Is(err, loom.ErrSessionExpired) {
			return fallback(loomErr(err))
		}
		return nil, videoReadErr(ctx, lc, id, err)
	}
	if r.emptyLive != nil && r.emptyLive(v) {
		// Some operations answer an unknown id with an empty list; make
		// sure the video exists before reporting "nothing here".
		if ok, perr := lc.VideoExists(ctx, id); perr == nil && !ok {
			return nil, videoNotFoundErr(id)
		}
	}
	if r.save != nil && !lc.IsDryRun() {
		writeThroughVideo(ctx, cmd, dbPath, id, v, r)
	}
	return &videoReadResult[T]{ID: id, Value: v, Source: sourceLive}, nil
}

// videoReadErr maps a live per-video failure onto a typed exit code. Loom's
// per-video operations report unknown ids inconsistently, so a failure that
// is not already an auth, rate-limit or typed error is disambiguated with
// one existence probe: a missing video exits 3 with a clear message.
func videoReadErr(ctx context.Context, lc *loom.Client, id string, err error) error {
	var typed *cliError
	if errors.As(err, &typed) || errors.Is(err, loom.ErrSessionExpired) || errors.Is(err, loom.ErrNoTranscript) {
		return loomErr(err)
	}
	if isVideoMissing(err) {
		return videoNotFoundErr(id)
	}
	if ok, perr := lc.VideoExists(ctx, id); perr == nil && !ok {
		return videoNotFoundErr(id)
	} else if errors.Is(perr, loom.ErrSessionExpired) {
		return loomErr(perr)
	}
	return loomErr(err)
}

// videoNotFoundMsg is the one wording for an unknown or inaccessible video.
func videoNotFoundMsg(id string) string {
	return fmt.Sprintf("video %s was not found, or your Loom account cannot open it", id)
}

// videoNotFoundErr is the exit-3 error for an unknown or inaccessible video.
// Every video-not-found path goes through it.
func videoNotFoundErr(id string) error {
	return notFoundErr(fmt.Errorf("%s\nhint: check the id or share URL; 'latest' picks your newest recording", videoNotFoundMsg(id)))
}

// isVideoMissing reports whether err is Loom's answer for a video that does
// not exist or that the signed-in user cannot open (after normalizeAuth has
// ruled out a signed-out session).
func isVideoMissing(err error) bool {
	var ue *loom.UnionError
	if !errors.As(err, &ue) || loom.ObjectKind(ue.Op) != "video" {
		return false
	}
	switch ue.Typename {
	case "NotFoundError", "PrivateVideo", "UserNotAuthorizedError":
		return true
	}
	return false
}

// videoLookupErr maps a failure to fetch one video's metadata.
func videoLookupErr(id string, err error) error {
	if isVideoMissing(err) {
		return videoNotFoundErr(id)
	}
	return loomErr(err)
}

// videoFailureMsg is the per-item error text of a bulk command.
func videoFailureMsg(id string, err error) string {
	if isVideoMissing(err) {
		return videoNotFoundMsg(id)
	}
	return err.Error()
}

// readVideoLocal serves a read from the local store, applying the one rule
// for local misses: no store at all is an empty result with a stderr hint
// (exit 0); a store without the video, including "latest" when the store
// has no active (unarchived) videos, is exit 3. When liveErr is set this is
// an auto-mode fallback: any local miss returns liveErr instead, so an
// expired session is never reported as "no data".
func readVideoLocal[T any](ctx context.Context, cmd *cobra.Command, dbPath, id string, latest bool, r videoRead[T], liveErr error) (*videoReadResult[T], error) {
	miss := func(err error) (*videoReadResult[T], error) {
		if liveErr != nil {
			return nil, liveErr
		}
		return nil, err
	}
	path, ok := localStoreExists(dbPath)
	if !ok {
		if liveErr != nil {
			return nil, liveErr
		}
		missingMirrorHint(cmd.ErrOrStderr(), path, "loom-pp-cli sync && loom-pp-cli hydrate (or use --data-source live)")
		return &videoReadResult[T]{ID: id, Value: r.empty(id), Source: sourceLocal, Missing: true}, nil
	}
	if r.local == nil {
		// checkDataSource rejects --data-source local for reads without a
		// local copy, so only the auto fallback gets here.
		return miss(usageErr(fmt.Errorf("the local store does not keep %s; use --data-source live", r.what)))
	}
	db, err := openLoomStore(ctx, path)
	if err != nil {
		return miss(err)
	}
	defer db.Close()
	if latest {
		// Like the live "latest", the newest video that is not archived.
		latestID, err := db.LatestActiveVideoID(ctx)
		if err != nil {
			return miss(err)
		}
		if latestID == "" {
			return miss(notFoundErr(fmt.Errorf("the local store has no active videos to resolve latest\nhint: run 'loom-pp-cli sync' or use --data-source live")))
		}
		id = latestID
	} else {
		known, err := db.HasLocalVideo(ctx, id)
		if err != nil {
			return miss(err)
		}
		if !known {
			return miss(localVideoMissingErr(id))
		}
	}
	v, err := r.local(ctx, db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return miss(notFoundErr(fmt.Errorf("no local %s for video %s\nhint: run 'loom-pp-cli hydrate' to backfill it, or use --data-source live", r.what, id)))
		}
		return miss(err)
	}
	return &videoReadResult[T]{ID: id, Value: v, Source: sourceLocal}, nil
}

// writeThroughVideo saves a live result into an existing local store.
// Failures only warn: the live answer is already in hand.
func writeThroughVideo[T any](ctx context.Context, cmd *cobra.Command, dbPath, id string, v T, r videoRead[T]) {
	path, ok := localStoreExists(dbPath)
	if !ok {
		return
	}
	db, err := openLoomStore(ctx, path)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not update the local store: %v\n", err)
		return
	}
	defer db.Close()
	if err := r.save(ctx, db, id, v); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not update the local store: %v\n", err)
	}
}

// artifactPatch is a partial update to a video's stored AI artifacts. Nil
// or empty fields leave the stored value alone.
type artifactPatch struct {
	Summary        string
	SummaryStatus  string
	Chapters       json.RawMessage
	ChaptersStatus string
	Tasks          json.RawMessage
	Takeaways      json.RawMessage
}

// mergeArtifact applies p on top of existing (nil when the video has no row
// yet). A patch never blanks out a stored value: empty strings and empty
// JSON arrays are ignored, so a still-processing summary cannot erase the
// one hydrate saved earlier. Statuses are replaced whenever given.
func mergeArtifact(existing *store.ArtifactRow, id string, p artifactPatch, now time.Time) store.ArtifactRow {
	row := store.ArtifactRow{VideoID: id}
	if existing != nil {
		row = *existing
		row.VideoID = id
	}
	if strings.TrimSpace(p.Summary) != "" {
		row.Summary = p.Summary
	}
	if p.SummaryStatus != "" {
		row.SummaryStatus = p.SummaryStatus
	}
	if !isEmptyJSONArray(p.Chapters) {
		row.Chapters = p.Chapters
	}
	if p.ChaptersStatus != "" {
		row.ChaptersStatus = p.ChaptersStatus
	}
	if !isEmptyJSONArray(p.Tasks) {
		row.Tasks = p.Tasks
	}
	if !isEmptyJSONArray(p.Takeaways) {
		row.Takeaways = p.Takeaways
	}
	row.FetchedAt = now.UTC().Format(time.RFC3339)
	return row
}

// isEmptyJSONArray reports whether raw is absent, null or [].
func isEmptyJSONArray(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return true
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		return len(arr) == 0
	}
	return false
}

// saveArtifactPatch merges p into the video's artifact row and saves it.
func saveArtifactPatch(ctx context.Context, db *store.Store, id string, p artifactPatch) error {
	existing, err := db.GetArtifacts(ctx, id)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		existing = nil
	}
	return db.SaveArtifacts(ctx, mergeArtifact(existing, id, p, time.Now()))
}

// localArtifacts reads a video's artifact row (sql.ErrNoRows when absent).
func localArtifacts(ctx context.Context, db *store.Store, id string) (*store.ArtifactRow, error) {
	return db.GetArtifacts(ctx, id)
}

// mustJSON marshals v, falling back to an empty array.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}
