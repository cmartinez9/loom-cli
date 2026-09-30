// Copyright 2026 cmartinez9 and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
	"github.com/cmartinez9/loom-cli/internal/store"
)

// activityComment is a comment surfaced by activity (new, or part of a
// thread that needs a reply).
type activityComment struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id,omitempty"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	Link      string `json:"link"`
}

// activityVideo is one video's engagement compared with the last check.
type activityVideo struct {
	VideoID        string `json:"video_id"`
	Name           string `json:"name"`
	CreatedAt      string `json:"created_at"`
	Views          int    `json:"views"`
	ViewsDelta     *int   `json:"views_delta"`
	Comments       int    `json:"comments"`
	CommentsDelta  *int   `json:"comments_delta"`
	Reactions      int    `json:"reactions"`
	ReactionsDelta *int   `json:"reactions_delta"`
	// Since is when the snapshot these deltas compare against was taken
	// (null when the video has no earlier snapshot). A video that was
	// outside the window on the previous check compares against an older
	// snapshot, so its deltas can span more than one check.
	Since       *string           `json:"since"`
	NewComments []activityComment `json:"new_comments"`
	NeedsReply  bool              `json:"needs_reply"`
	ReplyThread []activityComment `json:"reply_thread,omitempty"`
	FirstSeen   bool              `json:"first_seen"`
	Link        string            `json:"link"`
}

type activityTotals struct {
	Videos         int `json:"videos"`
	ViewsDelta     int `json:"views_delta"`
	CommentsDelta  int `json:"comments_delta"`
	ReactionsDelta int `json:"reactions_delta"`
	NewComments    int `json:"new_comments"`
	NeedsReply     int `json:"needs_reply"`
}

type activityFailure struct {
	VideoID string `json:"video_id,omitempty"`
	Stage   string `json:"stage"`
	Error   string `json:"error"`
}

// activityResult is the command's JSON envelope.
type activityResult struct {
	CheckedAt     string          `json:"checked_at"`
	PreviousCheck *string         `json:"previous_check"`
	Since         string          `json:"since"`
	ScannedVideos int             `json:"scanned_videos"`
	InWindow      int             `json:"in_window"`
	MaxPages      int             `json:"max_pages"`
	Recorded      bool            `json:"recorded"`
	Videos        []activityVideo `json:"videos"`
	Totals        *activityTotals `json:"totals"` // null on a baseline run

	FetchFailures []activityFailure `json:"fetch_failures"`
	Note          string            `json:"note,omitempty"`
}

// activityDeltaPtr returns cur-prev, or nil when there is no previous snapshot.
func activityDeltaPtr(cur int, prev *store.ActivitySnapshot, pick func(store.ActivitySnapshot) int) *int {
	if prev == nil {
		return nil
	}
	d := cur - pick(*prev)
	return &d
}

// computeActivityDeltas compares a live listing row with its previous snapshot.
func computeActivityDeltas(v loom.Video, prev *store.ActivitySnapshot) (views, comments, reactions *int) {
	views = activityDeltaPtr(v.Views, prev, func(s store.ActivitySnapshot) int { return s.Views })
	comments = activityDeltaPtr(v.Comments, prev, func(s store.ActivitySnapshot) int { return s.Comments })
	reactions = activityDeltaPtr(v.Reactions, prev, func(s store.ActivitySnapshot) int { return s.Reactions })
	return
}

// activityWantComments reports whether activity should fetch a video's comments:
// its comment count rose, or it has comments and no baseline yet.
func activityWantComments(v loom.Video, prev *store.ActivitySnapshot) bool {
	if prev == nil {
		return v.Comments > 0
	}
	return v.Comments > prev.Comments
}

func activityPositive(p *int) bool { return p != nil && *p > 0 }

// activitySnapshots builds the snapshot rows to record. A video whose new
// comments could not be read (comment fetch failed, or the signed-in user
// was unknown so needs_reply could not be judged) keeps its previous comment
// count, so the next run still sees the rise and fetches them; with no
// previous snapshot it is left unrecorded.
func activitySnapshots(videos []loom.Video, prevs map[string]store.ActivitySnapshot, keepPrevComments map[string]bool, takenAt string) []store.ActivitySnapshot {
	snaps := make([]store.ActivitySnapshot, 0, len(videos))
	for _, v := range videos {
		comments := v.Comments
		if keepPrevComments[v.ID] {
			prev, ok := prevs[v.ID]
			if !ok {
				continue
			}
			comments = prev.Comments
		}
		snaps = append(snaps, store.ActivitySnapshot{TakenAt: takenAt, VideoID: v.ID, Name: v.Name, Views: v.Views, UniqueViews: v.UniqueViews, Comments: comments, Reactions: v.Reactions})
	}
	return snaps
}

// activityTotalsFor sums the deltas over every in-window video. A baseline
// run has nothing to compare with, so its totals are null.
func activityTotalsFor(rows []activityVideo, baseline bool) *activityTotals {
	if baseline {
		return nil
	}
	t := &activityTotals{}
	for _, r := range rows {
		t.Videos++
		if r.ViewsDelta != nil {
			t.ViewsDelta += *r.ViewsDelta
		}
		if r.CommentsDelta != nil {
			t.CommentsDelta += *r.CommentsDelta
		}
		if r.ReactionsDelta != nil {
			t.ReactionsDelta += *r.ReactionsDelta
		}
		t.NewComments += len(r.NewComments)
		if r.NeedsReply {
			t.NeedsReply++
		}
	}
	return t
}

// activityHasChanges reports whether a video changed since the last check.
func activityHasChanges(v activityVideo) bool {
	return activityPositive(v.ViewsDelta) || activityPositive(v.CommentsDelta) || activityPositive(v.ReactionsDelta) ||
		v.NeedsReply || len(v.NewComments) > 0 || v.FirstSeen
}

// commentTime orders comments by created_at, falling back to string order.
func activityCommentBefore(a, b loom.Comment) bool {
	ta, oka := loom.ParseTime(a.CreatedAt)
	tb, okb := loom.ParseTime(b.CreatedAt)
	if oka && okb {
		return ta.Before(tb)
	}
	return a.CreatedAt < b.CreatedAt
}

// activityFlatten returns every comment and reply, depth first.
func activityFlatten(cs []loom.Comment) []loom.Comment {
	out := make([]loom.Comment, 0, len(cs))
	loom.WalkComments(cs, func(c loom.Comment, _ int) { out = append(out, c) })
	return out
}

func toActivityComment(c loom.Comment) activityComment {
	return activityComment{ID: c.ID, ParentID: c.ParentID, Author: c.Author, Text: truncate(strings.TrimSpace(c.Text), 280), CreatedAt: c.CreatedAt, Link: c.Link}
}

// activityNeedsReply looks at the newest top-level comment thread and reports
// whether its last message (considering replies) was written by someone
// other than meID. It also returns up to the last three messages of that
// thread, oldest first, as an excerpt.
func activityNeedsReply(comments []loom.Comment, meID string) (bool, []activityComment) {
	if len(comments) == 0 || meID == "" {
		return false, nil
	}
	newest := comments[0]
	for _, c := range comments[1:] {
		if activityCommentBefore(newest, c) {
			newest = c
		}
	}
	thread := activityFlatten([]loom.Comment{newest})
	sort.SliceStable(thread, func(i, j int) bool { return activityCommentBefore(thread[i], thread[j]) })
	last := thread[len(thread)-1]
	if last.AuthorID == meID {
		return false, nil
	}
	start := len(thread) - 3
	if start < 0 {
		start = 0
	}
	excerpt := make([]activityComment, 0, len(thread)-start)
	for _, c := range thread[start:] {
		excerpt = append(excerpt, toActivityComment(c))
	}
	return true, excerpt
}

// unseenComments returns the comments (including replies) whose ids are
// not in seen, oldest first.
func unseenComments(comments []loom.Comment, seen map[string]bool) []loom.Comment {
	out := make([]loom.Comment, 0)
	for _, c := range activityFlatten(comments) {
		if !seen[c.ID] {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return activityCommentBefore(out[i], out[j]) })
	return out
}

func newNovelActivityCmd(flags *rootFlags) *cobra.Command {
	var (
		since    string
		maxPages int
		noSave   bool
		showAll  bool
		limit    int
		dbPath   string
	)
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "See which recent videos got new views, comments or reactions since you last checked",
		Long: strings.TrimSpace(`
Compare your recent videos' views, comments and reactions with the last time
you ran this command, list new comments, and flag threads whose last message
is from someone else (needs_reply).

Use this command to see what changed across your recent videos (new views,
comments, reactions) since the last check. Do NOT use this command to read the
full comment thread on one video; use 'comments list' instead.

The first run records a baseline (deltas and totals are null, every video is
first_seen). Later runs show only the videos that changed, unless --all.
Each video's "since" is the snapshot its deltas compare against. Each run
records a new snapshot in the local store; pass --no-save to look without
moving the baseline.`),
		Example: strings.Trim(`
  loom-pp-cli activity --since 30d --agent
  loom-pp-cli activity --since 14d --agent
  loom-pp-cli activity --since 7d --all
  loom-pp-cli activity --since 90d --no-save --max-pages 10`, "\n"),
		// mcp:read-only means "never mutates the Loom account": activity only reads
		// Loom, though it saves local snapshots that move the "since last check"
		// baseline (skip with --no-save). Live checks rely on this to sample it.
		Annotations: map[string]string{
			"mcp:read-only":  "true",
			"pp:data-source": "live",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "activity")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if len(args) > 0 {
				return usageErr(fmt.Errorf("activity takes no arguments (got %q); use --since to choose the window", strings.Join(args, " ")))
			}
			if strings.TrimSpace(since) == "" {
				return usageErr(fmt.Errorf("--since must be a positive window such as 30d"))
			}
			bounds, err := timeWindow{Since: since}.bounds(time.Now())
			if err != nil {
				return err
			}
			maxPages, err := effectiveMaxPages(maxPages)
			if err != nil {
				return err
			}
			if limit < 0 {
				return usageErr(fmt.Errorf("--limit must not be negative (got %d)", limit))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			sinceTime := bounds.Start.UTC()
			res := activityResult{
				CheckedAt:     now.Format(store.SnapshotTimeLayout),
				Since:         sinceTime.Format(time.RFC3339),
				MaxPages:      maxPages,
				Videos:        make([]activityVideo, 0),
				FetchFailures: make([]activityFailure, 0),
			}

			// 1. Scan the library newest first until the window ends.
			scan, err := scanLibrary(ctx, lc, scanOpts{Filter: selectFilter{Window: windowBounds{Start: sinceTime}}, MaxPages: maxPages})
			if err != nil {
				return loomErr(err)
			}
			inWindow, capHit := scan.Videos, scan.Capped
			res.ScannedVideos = scan.Scanned
			res.InWindow = len(inWindow)

			// 2. Compare with the last snapshot.
			db, err := openLoomStore(ctx, dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			ids := make([]string, 0, len(inWindow))
			for _, v := range inWindow {
				ids = append(ids, v.ID)
			}
			prevs, err := db.LatestActivitySnapshots(ctx, ids, res.CheckedAt)
			if err != nil {
				return fmt.Errorf("reading activity snapshots: %w", err)
			}
			lastCheck, err := db.LastActivityCheck(ctx, res.CheckedAt)
			if err != nil {
				return fmt.Errorf("reading activity snapshots: %w", err)
			}
			baseline := lastCheck == ""
			if !baseline {
				res.PreviousCheck = &lastCheck
			}

			rows := make([]activityVideo, len(inWindow))
			needComments := make([]int, 0)
			for i, v := range inWindow {
				var prev *store.ActivitySnapshot
				if p, ok := prevs[v.ID]; ok {
					prev = &p
				}
				vd, cd, rd := computeActivityDeltas(v, prev)
				rows[i] = activityVideo{
					VideoID: v.ID, Name: v.Name, CreatedAt: v.CreatedAt,
					Views: v.Views, ViewsDelta: vd,
					Comments: v.Comments, CommentsDelta: cd,
					Reactions: v.Reactions, ReactionsDelta: rd,
					NewComments: make([]activityComment, 0),
					FirstSeen:   prev == nil,
					Link:        loom.ShareURL(v.ID),
				}
				if prev != nil {
					taken := prev.TakenAt
					rows[i].Since = &taken
				}
				if activityWantComments(v, prev) {
					needComments = append(needComments, i)
				}
			}

			// 3. Fetch comments where the count rose (or on first sight).
			seenToRecord := make([]store.SeenComment, 0)
			// Videos whose comment count must not advance in this snapshot,
			// so the next run looks at their comments again.
			keepPrevComments := map[string]bool{}
			if len(needComments) > 0 {
				meID := ""
				// Comment lookups stop starting once one hits an error no
				// later lookup can get past (expired session, rate limit).
				var stopMu sync.Mutex
				var stopErr error
				me, err := lc.CurrentUser(ctx)
				switch {
				case err == nil:
					meID = me.ID
				case loom.IsRunStopping(err):
					return loomErr(err)
				default:
					res.FetchFailures = append(res.FetchFailures, activityFailure{Stage: "current_user", Error: err.Error()})
					// needs_reply cannot be judged without knowing who "me" is.
					for _, idx := range needComments {
						keepPrevComments[inWindow[idx].ID] = true
					}
				}
				type commentResult struct {
					comments []loom.Comment
					err      error
				}
				results := make([]commentResult, len(needComments))
				var wg sync.WaitGroup
				sem := make(chan struct{}, 2)
				for j, idx := range needComments {
					wg.Add(1)
					go func(j int, id string) {
						defer wg.Done()
						sem <- struct{}{}
						defer func() { <-sem }()
						stopMu.Lock()
						stop := stopErr
						stopMu.Unlock()
						if stop != nil {
							results[j] = commentResult{err: stop}
							return
						}
						cs, err := lc.FetchComments(ctx, id, false)
						if err != nil && loom.IsRunStopping(err) {
							stopMu.Lock()
							if stopErr == nil {
								stopErr = err
							}
							stopMu.Unlock()
						}
						results[j] = commentResult{comments: cs, err: err}
					}(j, inWindow[idx].ID)
				}
				wg.Wait()
				for j, idx := range needComments {
					r := results[j]
					row := &rows[idx]
					if r.err != nil {
						if loom.IsRunStopping(r.err) {
							return loomErr(r.err)
						}
						res.FetchFailures = append(res.FetchFailures, activityFailure{VideoID: row.VideoID, Stage: "comments", Error: r.err.Error()})
						keepPrevComments[row.VideoID] = true
						continue
					}
					seen, err := db.SeenCommentIDs(ctx, row.VideoID)
					if err != nil {
						return fmt.Errorf("reading seen comments: %w", err)
					}
					for _, c := range unseenComments(r.comments, seen) {
						row.NewComments = append(row.NewComments, toActivityComment(c))
						seenToRecord = append(seenToRecord, store.SeenComment{
							CommentID: c.ID, VideoID: row.VideoID, ParentID: c.ParentID, Author: c.Author,
							AuthorID: c.AuthorID, Text: c.Text, CreatedAt: c.CreatedAt,
						})
					}
					row.NeedsReply, row.ReplyThread = activityNeedsReply(r.comments, meID)
				}
				if n := activityCountStage(res.FetchFailures, "comments"); n > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d of %d comment lookups failed; new-comment totals cover the remaining %d videos\n",
						n, len(needComments), len(needComments)-n)
				}
			}

			// 4. Record the new snapshot unless --no-save.
			if !noSave && len(inWindow) > 0 {
				saveCtx := context.WithoutCancel(ctx)
				snaps := activitySnapshots(inWindow, prevs, keepPrevComments, res.CheckedAt)
				if err := db.SaveActivitySnapshots(saveCtx, snaps); err != nil {
					return fmt.Errorf("recording activity snapshot: %w", err)
				}
				if err := db.RecordSeenComments(saveCtx, seenToRecord); err != nil {
					return fmt.Errorf("recording seen comments: %w", err)
				}
				res.Recorded = len(snaps) > 0
			}

			// 5. Totals over every in-window video; choose rows to show.
			res.Totals = activityTotalsFor(rows, baseline)
			res.Videos = selectActivityRows(rows, baseline, showAll, limit)
			res.Note = activityNote(baseline, noSave, capHit, len(inWindow), len(res.Videos), res.ScannedVideos, maxPages, lastCheck)
			return emit(cmd, flags, res, func(w io.Writer) error { return renderActivity(w, res) })
		},
	}
	cmd.Flags().StringVar(&since, "since", "30d", "Only check videos created within this window (e.g. 30d, 2w)")
	registerMaxPages(cmd, &maxPages, "videos", true)
	cmd.Flags().BoolVar(&noSave, "no-save", false, "Do not record this check as the new baseline")
	cmd.Flags().BoolVar(&showAll, "all", false, "List every scanned video, not only the ones that changed")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of videos to list (0 means no cap)")
	cmd.Flags().StringVar(&dbPath, "db", "", "Path to the local SQLite store (default: the CLI data directory)")
	return cmd
}

func activityCountStage(fs []activityFailure, stage string) int {
	n := 0
	for _, f := range fs {
		if f.Stage == stage {
			n++
		}
	}
	return n
}

// selectActivityRows keeps every row on the baseline run or with --all, and
// otherwise only rows with activity; limit caps the list (0 = no cap).
func selectActivityRows(rows []activityVideo, baseline, showAll bool, limit int) []activityVideo {
	out := make([]activityVideo, 0, len(rows))
	for _, r := range rows {
		if baseline || showAll || activityHasChanges(r) {
			out = append(out, r)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// activityNote explains baseline runs, quiet runs and scan-cap hits.
func activityNote(baseline, noSave, capHit bool, inWindow, shown, scanned, maxPages int, lastCheck string) string {
	var parts []string
	switch {
	case baseline && noSave:
		parts = append(parts, "no previous check to compare with; --no-save left the baseline unrecorded")
	case baseline:
		parts = append(parts, "first check: baseline recorded for "+strconv.Itoa(inWindow)+" videos; deltas appear on the next run")
	case shown == 0 && inWindow > 0:
		parts = append(parts, fmt.Sprintf("no new views, comments or reactions since %s across %d videos; use --all to list them", lastCheck, inWindow))
	}
	if inWindow == 0 {
		// Replaces any baseline wording: an empty window records nothing.
		parts = []string{fmt.Sprintf("no videos in the window; nothing recorded (%d scanned)", scanned)}
	}
	if capHit {
		parts = append(parts, capNote(maxPages, "videos"))
	}
	return strings.Join(parts, "; ")
}

func activityFmtDelta(p *int) string {
	if p == nil {
		return "-"
	}
	if *p > 0 {
		return "+" + strconv.Itoa(*p)
	}
	return strconv.Itoa(*p)
}

func renderActivity(w io.Writer, res activityResult) error {
	if len(res.Videos) > 0 {
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "VIDEO\tVIEWS\tCOMMENTS\tREACTIONS\tNEW\tREPLY")
		for _, v := range res.Videos {
			reply := ""
			if v.NeedsReply {
				reply = "needs reply"
			}
			fmt.Fprintf(tw, "%s\t%d (%s)\t%d (%s)\t%d (%s)\t%d\t%s\n", truncate(v.Name, 44),
				v.Views, activityFmtDelta(v.ViewsDelta), v.Comments, activityFmtDelta(v.CommentsDelta), v.Reactions, activityFmtDelta(v.ReactionsDelta),
				len(v.NewComments), reply)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		for _, v := range res.Videos {
			if !v.NeedsReply || len(v.ReplyThread) == 0 {
				continue
			}
			last := v.ReplyThread[len(v.ReplyThread)-1]
			fmt.Fprintf(w, "\n%s: %s wrote %q\n  %s\n", truncate(v.Name, 44), last.Author, truncate(last.Text, 120), last.Link)
		}
	}
	prev := "none"
	if res.PreviousCheck != nil {
		prev = *res.PreviousCheck
	}
	fmt.Fprintf(w, "checked %d videos (scanned %d); previous check: %s\n", res.InWindow, res.ScannedVideos, prev)
	if res.Note != "" {
		fmt.Fprintln(w, res.Note)
	}
	return nil
}
