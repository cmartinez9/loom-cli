// Copyright 2026 cmartinez9 and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/cliutil"
	"loom-pp-cli/internal/loom"
	"loom-pp-cli/internal/store"
)

// hydrateVideo reports what one run did for one video.
type hydrateVideo struct {
	VideoID    string `json:"video_id"`
	Name       string `json:"name"`
	Transcript string `json:"transcript"` // saved | pending | none | error | kept | skipped
	Artifacts  string `json:"artifacts"`  // saved | partial | error | kept | skipped
	Error      string `json:"error,omitempty"`
}

// hydrateFailure is one failed fetch, kept out of the saved counts.
type hydrateFailure struct {
	VideoID string `json:"video_id"`
	Stage   string `json:"stage"`
	Error   string `json:"error"`
	err     error  // the underlying failure, for the exit code
}

// hydrateResult is the command's JSON envelope.
type hydrateResult struct {
	Processed        int              `json:"processed"`
	TranscriptsSaved int              `json:"transcripts_saved"`
	ArtifactsSaved   int              `json:"artifacts_saved"`
	Pending          int              `json:"pending"`
	Skipped          int              `json:"skipped"`
	Failed           int              `json:"failed"`
	Remaining        int              `json:"remaining"`
	Videos           []hydrateVideo   `json:"videos"`
	FetchFailures    []hydrateFailure `json:"fetch_failures"`
	Note             string           `json:"note,omitempty"`
}

func newHydrateResult() hydrateResult {
	return hydrateResult{Videos: make([]hydrateVideo, 0), FetchFailures: make([]hydrateFailure, 0)}
}

// hydrateOptions selects which videos a run works on.
type hydrateOptions struct {
	Only      string // transcripts | ai | all
	Force     bool
	Takeaways bool
	Limit     int // 0 = no cap
}

// hydratePlan is the work planned for one video.
type hydratePlan struct {
	Video      store.LocalVideo
	Transcript bool
	Artifacts  bool
	Takeaways  bool
	Prev       store.HydrateState
}

// Transcript states that need no retry: saved, or Loom has none for the video.
func hydrateTranscriptSettled(status string) bool { return status == "done" || status == "none" }

// summaryStaleAfter is how long after recording an AI summary Loom still
// reports as pending is treated as never coming. Loom normally finishes
// within minutes; past this age hydrate stops counting it as pending or
// retrying it, and summary/digest say "no AI summary available".
const summaryStaleAfter = 7 * 24 * time.Hour

// summaryPendingStale reports an empty summary Loom still calls pending for
// a video recorded more than summaryStaleAfter ago.
func summaryPendingStale(status, summary, createdAt string, now time.Time) bool {
	if !hydrateSummaryPending(status, summary) {
		return false
	}
	t, ok := loom.ParseTime(createdAt)
	return ok && now.Sub(t) > summaryStaleAfter
}

// hydrateArtifactsSettled reports whether stored artifacts need no retry:
// fully saved, or saved without errors while the summary has stayed pending
// long past the video's recording.
func hydrateArtifactsSettled(st store.HydrateState, createdAt string, now time.Time) bool {
	if st.ArtifactsStatus == "done" {
		return true
	}
	if st.ArtifactsStatus != "partial" || st.LastError != "" {
		return false
	}
	t, ok := loom.ParseTime(createdAt)
	return ok && now.Sub(t) > summaryStaleAfter
}

// planHydrate picks the videos this run must fetch. Videos whose requested
// parts are already done are counted as skipped unless Force is set; pending
// and failed parts are retried. Limit caps the videos worked on (skips do not
// count against it).
func planHydrate(videos []store.LocalVideo, states map[string]store.HydrateState, takeawaysDone map[string]bool, opts hydrateOptions, now time.Time) ([]hydratePlan, int, int) {
	plans := make([]hydratePlan, 0)
	skipped, remaining := 0, 0
	for _, v := range videos {
		st := states[v.ID]
		p := hydratePlan{Video: v, Prev: st}
		if opts.Only != "ai" {
			p.Transcript = opts.Force || !hydrateTranscriptSettled(st.TranscriptStatus)
		}
		if opts.Only != "transcripts" {
			needTakeaways := opts.Takeaways && !takeawaysDone[v.ID]
			p.Artifacts = opts.Force || !hydrateArtifactsSettled(st, v.CreatedAt, now) || needTakeaways
			p.Takeaways = opts.Takeaways && p.Artifacts
		}
		if !p.Transcript && !p.Artifacts {
			skipped++
			continue
		}
		if opts.Limit > 0 && len(plans) >= opts.Limit {
			remaining++
			continue
		}
		plans = append(plans, p)
	}
	return plans, skipped, remaining
}

// hydrateSummaryPending reports whether Loom is still generating the AI summary.
func hydrateSummaryPending(status, summary string) bool {
	if strings.TrimSpace(summary) != "" {
		return false
	}
	return hydrateStillGenerating(status)
}

// hydrateStillGenerating reports whether a Loom processing status means the
// result is not ready yet (pending, in_progress, queued, ...), as opposed to
// finished or permanently unavailable (success, failed, no_audio, ...).
func hydrateStillGenerating(status string) bool {
	s := strings.ToLower(status)
	for _, k := range []string{"pend", "process", "progress", "queue", "running", "start"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// hydrateArtifactsStatus classifies an artifacts fetch: done when summary, chapters
// and action items all came back and the summary is not still generating;
// partial when something is missing or pending; error when nothing arrived.
func hydrateArtifactsStatus(okParts, totalParts int, pending bool) string {
	switch {
	case okParts == 0:
		return "error"
	case okParts < totalParts || pending:
		return "partial"
	default:
		return "done"
	}
}

func newNovelHydrateCmd(flags *rootFlags) *cobra.Command {
	var (
		since       string
		limit       int
		force       bool
		only        string
		takeaways   bool
		concurrency int
		dbPath      string
	)
	cmd := &cobra.Command{
		Use:   "hydrate",
		Short: "Pull transcripts plus Loom's own summaries, chapters and action items for your whole library in one resumable pass.",
		Long: strings.TrimSpace(`
Backfill transcripts and Loom's AI summary, chapters and action items into the
local store for videos already cached by 'sync', so 'grep' and 'digest' work
offline.

Use this command to backfill transcripts and Loom's AI summaries, chapters and
action items for videos already in the local store. Do NOT use this command to
refresh the video list or pick up new recordings; use 'sync' instead. For one
video's transcript right now, use 'transcript'.

Progress is saved per video as soon as it finishes, so an interrupted run
resumes where it stopped. Videos already hydrated are skipped unless --force;
transcripts or summaries Loom was still generating, and failed fetches, are
retried on the next run.

Each video gets its own API budget (--timeout, 60s by default), so a large
hydrate is never cut short by one command-wide deadline; a video that runs
past its budget is a per-video failure retried next run. Set --timeout
explicitly to also bound the whole run.

Exit codes: 0 when at least one video was hydrated (failures are listed in
fetch_failures), the failures' own code when every video failed, 7 when
Loom's throttling stops the run (including a rate-limit wait that runs out
the time budget), 4 when the session expired, and 5 when an explicit
--timeout ran out. Progress is saved before any early stop.`),
		Example: strings.Trim(`
  loom-pp-cli hydrate --since 90d --agent
  loom-pp-cli hydrate --since 30d
  loom-pp-cli hydrate --only transcripts --limit 50
  loom-pp-cli hydrate --since 7d --takeaways --force`, "\n"),
		// mcp:read-only means "never mutates the Loom account": hydrate only reads
		// Loom, though it caches what it reads in the local store. Live checks
		// rely on this to sample it safely.
		Annotations: map[string]string{
			"mcp:read-only":  "true",
			"pp:data-source": "live",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "hydrate")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if len(args) > 0 {
				return usageErr(fmt.Errorf("hydrate takes no arguments (got %q); use --since or --limit to choose videos", strings.Join(args, " ")))
			}
			window, err := timeWindow{Since: since}.bounds(time.Now())
			if err != nil {
				return err
			}
			switch only {
			case "all", "transcripts", "ai":
			default:
				return usageErr(fmt.Errorf("--only must be transcripts, ai or all (got %q)", only))
			}
			if limit < 0 {
				return usageErr(fmt.Errorf("--limit must not be negative (got %d)", limit))
			}
			if concurrency < 1 || concurrency > 8 {
				return usageErr(fmt.Errorf("--concurrency must be between 1 and 8 (got %d)", concurrency))
			}
			if cliutil.IsDogfoodEnv() {
				limit = 1
				concurrency = 1
			}
			dbPath, ok := localStoreExists(dbPath)
			if !ok {
				missingMirrorHint(cmd.ErrOrStderr(), dbPath, "loom-pp-cli sync --resources videos --db "+dbPath)
				return emitHydrate(cmd, flags, newHydrateResult())
			}
			// The whole run is bounded only by an explicit --timeout; each
			// video's fetches get their own budget inside runHydrate.
			ctx, cancel := runContext(cmd, flags)
			defer cancel()
			db, err := openLoomStore(ctx, dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			videos, err := db.LocalVideos(ctx, window.startRFC3339())
			if err != nil {
				return fmt.Errorf("reading local videos: %w", err)
			}
			if len(videos) == 0 {
				if since != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "no synced videos created in the last %s; widen --since, or pick up new recordings with\nrun: loom-pp-cli sync --resources videos\n", since)
				} else {
					fmt.Fprintln(cmd.ErrOrStderr(), "no synced videos in the local store\nrun: loom-pp-cli sync --resources videos")
				}
				return emitHydrate(cmd, flags, newHydrateResult())
			}
			states, err := db.HydrateStates(ctx)
			if err != nil {
				return fmt.Errorf("reading hydrate state: %w", err)
			}
			tkDone, err := db.TakeawaysAttempted(ctx)
			if err != nil {
				return fmt.Errorf("reading takeaways state: %w", err)
			}
			plans, skipped, remaining := planHydrate(videos, states, tkDone, hydrateOptions{Only: only, Force: force, Takeaways: takeaways, Limit: limit}, time.Now())
			res := newHydrateResult()
			res.Skipped = skipped
			res.Remaining = remaining
			if len(plans) == 0 {
				res.Note = fmt.Sprintf("all %d videos in the window are already hydrated; use --force to refetch", skipped)
				return emitHydrate(cmd, flags, res)
			}
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			outcome := bulkOutcome{what: "hydrate"}
			abortErr := runHydrate(ctx, flags.timeout, lc, db, plans, concurrency, &res, &outcome)
			if res.Failed > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d of %d videos had fetch failures; counts cover the %d that succeeded; re-run to retry\n",
					res.Failed, res.Processed, res.Processed-res.Failed)
			}
			if abortErr != nil {
				outcome.halt(abortErr)
				res.Remaining += len(plans) - res.Processed
				res.Note = hydrateStopNote(abortErr, res.Remaining)
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: "+res.Note)
			} else if res.Remaining > 0 {
				res.Note = fmt.Sprintf("%d more videos need hydrating; re-run to continue", res.Remaining)
			}
			if err := emitHydrate(cmd, flags, res); err != nil {
				return err
			}
			return outcome.exitErr()
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "Only hydrate videos created within this window (e.g. 30d, 2w); default is every synced video")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of videos to fetch this run (0 means no cap)")
	cmd.Flags().BoolVar(&force, "force", false, "Refetch videos that are already hydrated")
	cmd.Flags().StringVar(&only, "only", "all", "What to fetch: transcripts, ai (summary, chapters, action items) or all")
	cmd.Flags().BoolVar(&takeaways, "takeaways", false, "Also fetch Loom's meeting key takeaways")
	cmd.Flags().IntVar(&concurrency, "concurrency", 2, "Videos fetched in parallel (1-8)")
	cmd.Flags().StringVar(&dbPath, "db", "", "Path to the local SQLite store (default: the CLI data directory)")
	return cmd
}

// hydrateStopNote explains why a hydrate run stopped with remaining videos
// left: a run-stopping Loom error, Ctrl-C, or the explicit --timeout.
func hydrateStopNote(abortErr error, remaining int) string {
	switch {
	case loom.IsRunStopping(abortErr):
		return fmt.Sprintf("stopped early with %d videos left: %v; progress is saved, re-run to continue", remaining, firstLine(abortErr.Error()))
	case errors.Is(abortErr, context.Canceled):
		return fmt.Sprintf("interrupted with %d videos left; progress is saved, re-run to continue", remaining)
	case errors.Is(abortErr, context.DeadlineExceeded):
		return fmt.Sprintf("stopped at the --timeout limit with %d videos left; progress is saved, re-run (or raise --timeout) to continue", remaining)
	default:
		return fmt.Sprintf("stopped early with %d videos left: %v; progress is saved, re-run to continue", remaining, firstLine(abortErr.Error()))
	}
}

// runHydrate works through plans with a small worker pool, saving state per
// video. Each video's fetches get their own budget (perVideo, from
// --timeout) under runCtx. It returns the first run-stopping error
// (throttling, expired session, the end of runCtx) after the in-flight
// videos finish, and records each processed video in outcome.
func runHydrate(runCtx context.Context, perVideo time.Duration, lc *loom.Client, db *store.Store, plans []hydratePlan, concurrency int, res *hydrateResult, outcome *bulkOutcome) error {
	workCtx, stop := context.WithCancel(runCtx)
	defer stop()
	jobs := make(chan int)
	outcomes := make([]hydrateOutcome, len(plans))
	var (
		mu       sync.Mutex
		abortErr error
		wg       sync.WaitGroup
	)
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if workCtx.Err() != nil {
					continue
				}
				o := hydrateOne(workCtx, perVideo, lc, db, plans[idx])
				if o.abort != nil {
					mu.Lock()
					if abortErr == nil {
						abortErr = o.abort
					}
					mu.Unlock()
					stop()
				}
				outcomes[idx] = o
			}
		}()
	}
	for i := range plans {
		if workCtx.Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, o := range outcomes {
		if !o.completed {
			continue
		}
		res.Processed++
		res.Videos = append(res.Videos, o.video)
		if o.savedTranscript {
			res.TranscriptsSaved++
		}
		if o.savedArtifacts {
			res.ArtifactsSaved++
		}
		if o.pending {
			res.Pending++
		}
		if len(o.failures) > 0 {
			res.Failed++
			res.FetchFailures = append(res.FetchFailures, o.failures...)
		}
		// A video counts as hydrated when it saved something or had
		// nothing to fail; one that only failed counts as failed.
		if len(o.failures) > 0 && !o.savedTranscript && !o.savedArtifacts {
			outcome.fail(o.failures[0].err)
		} else {
			outcome.ok()
		}
	}
	if abortErr == nil && runCtx.Err() != nil {
		abortErr = runCtx.Err()
	}
	return abortErr
}

type hydrateOutcome struct {
	video           hydrateVideo
	failures        []hydrateFailure
	savedTranscript bool
	savedArtifacts  bool
	pending         bool
	completed       bool  // state was recorded; the video counts as processed
	abort           error // run-stopping error hit while working on this video
}

// hydrateOne fetches and stores one video's transcript and AI artifacts
// under its own budget (perVideo; 0 = unbounded), then records its hydrate
// state. A failure that stops the run (stopCause against runCtx) aborts the
// video without recording it; any other failure, including the video
// running past its own budget, is recorded as a per-video failure. Store
// writes use a context that survives the run being stopped so finished work
// is never lost.
func hydrateOne(runCtx context.Context, perVideo time.Duration, lc *loom.Client, db *store.Store, p hydratePlan) hydrateOutcome {
	ctx, cancel := runCtx, context.CancelFunc(func() {})
	if perVideo > 0 {
		ctx, cancel = context.WithTimeout(runCtx, perVideo)
	}
	defer cancel()
	id := p.Video.ID
	o := hydrateOutcome{video: hydrateVideo{VideoID: id, Name: p.Video.Name, Transcript: "kept", Artifacts: "kept"}}
	if !p.Transcript && p.Prev.TranscriptStatus == "" {
		o.video.Transcript = "skipped"
	}
	if !p.Artifacts && p.Prev.ArtifactsStatus == "" {
		o.video.Artifacts = "skipped"
	}
	saveCtx := context.WithoutCancel(runCtx)
	state := store.HydrateState{VideoID: id, TranscriptStatus: p.Prev.TranscriptStatus, ArtifactsStatus: p.Prev.ArtifactsStatus}
	errs := make([]string, 0)
	fail := func(stage string, err error) {
		msg := err.Error()
		o.failures = append(o.failures, hydrateFailure{VideoID: id, Stage: stage, Error: msg, err: err})
		errs = append(errs, stage+": "+msg)
	}
	finish := func() hydrateOutcome {
		state.LastError = strings.Join(errs, "; ")
		o.video.Error = state.LastError
		if o.abort != nil && o.video.Error == "" {
			o.video.Error = "stopped: " + o.abort.Error()
		}
		if err := db.SaveHydrateState(saveCtx, state); err != nil {
			fail("state", err)
			o.video.Error = strings.Join(errs, "; ")
		}
		o.completed = true
		return o
	}

	if p.Transcript {
		t, err := lc.FetchTranscript(ctx, id)
		switch {
		case err == nil && len(t.Segments) > 0:
			if err := saveTranscriptThrough(saveCtx, db, id, t); err != nil {
				fail("transcript", fmt.Errorf("saving transcript: %w", err))
				state.TranscriptStatus, o.video.Transcript = "error", "error"
			} else {
				state.TranscriptStatus, o.video.Transcript = "done", "saved"
				o.savedTranscript = true
			}
		case err == nil && hydrateStillGenerating(t.Status):
			state.TranscriptStatus, o.video.Transcript = "pending", "pending"
			o.pending = true
		case err == nil:
			// Loom finished without phrases (e.g. no_audio): nothing to retry.
			state.TranscriptStatus, o.video.Transcript = "none", "none"
		case errors.Is(err, loom.ErrNoTranscript):
			state.TranscriptStatus, o.video.Transcript = "none", "none"
		case stopCause(runCtx, err) != nil:
			// Stopped mid-video: leave its previous state (and last error)
			// untouched and do not count it as processed.
			o.abort = stopCause(runCtx, err)
			return o
		default:
			fail("transcript", err)
			state.TranscriptStatus, o.video.Transcript = "error", "error"
		}
	}

	if p.Artifacts {
		status, pending, abort := hydrateArtifacts(ctx, saveCtx, lc, db, p, fail)
		if abort != nil && stopCause(runCtx, abort) == nil {
			// Only this video ran out of budget (or hit its own
			// failure): record it and move on.
			fail("artifacts", abort)
			status, pending, abort = "error", false, nil
		}
		if abort != nil {
			o.abort = stopCause(runCtx, abort)
			if o.savedTranscript {
				// Record the transcript finished before the stop, keeping
				// the previous artifacts status and last error; the video
				// still does not count as processed.
				keep := store.HydrateState{VideoID: id, TranscriptStatus: state.TranscriptStatus, ArtifactsStatus: p.Prev.ArtifactsStatus, LastError: p.Prev.LastError}
				_ = db.SaveHydrateState(saveCtx, keep)
			}
			return o
		}
		state.ArtifactsStatus = status
		switch status {
		case "done":
			o.video.Artifacts = "saved"
			o.savedArtifacts = true
		case "partial":
			o.video.Artifacts = "partial"
			o.savedArtifacts = true
		default:
			o.video.Artifacts = "error"
		}
		if pending {
			o.pending = true
		}
	}
	return finish()
}

// hydrateArtifacts fetches summary, chapters and action items (and
// optionally key takeaways) and merges them into the stored row through
// saveArtifactPatch, so a part that failed, or came back empty while Loom
// is still processing, never blanks a value saved earlier. It returns the
// artifacts status, whether Loom is still generating the summary, and any
// run-stopping error. A summary still "pending" more than summaryStaleAfter
// after recording is treated as unavailable, not pending.
func hydrateArtifacts(ctx, saveCtx context.Context, lc *loom.Client, db *store.Store, p hydratePlan, fail func(string, error)) (string, bool, error) {
	id := p.Video.ID
	a, failed, abort := lc.FetchArtifacts(ctx, id, loom.ArtifactOptions{Summary: true, Chapters: true, ActionItems: true, Takeaways: p.Takeaways})
	if abort != nil {
		return "", false, abort
	}
	var row artifactPatch
	ok := 0
	pending := false
	for _, f := range failed {
		fail(f.Section, f.Err)
	}
	if a.Summary != nil {
		ok++
		row.Summary, row.SummaryStatus = a.Summary.Summary, a.Summary.Status
		pending = hydrateSummaryPending(a.Summary.Status, a.Summary.Summary) && !summaryPendingStale(a.Summary.Status, a.Summary.Summary, p.Video.CreatedAt, time.Now())
	}
	if a.Chapters != nil {
		ok++
		b, _ := json.Marshal(a.Chapters.Chapters)
		row.Chapters, row.ChaptersStatus = b, a.Chapters.Status
	}
	if !sectionFailed(failed, loom.SectionActionItems) {
		ok++
		b, _ := json.Marshal(a.ActionItems)
		row.Tasks = b
	}
	if a.Takeaways != nil {
		b, _ := json.Marshal(a.Takeaways.Takeaways)
		row.Takeaways = b
		if err := db.MarkTakeawaysAttempted(saveCtx, id); err != nil {
			fail("takeaways", fmt.Errorf("recording takeaways state: %w", err))
		}
	}

	status := hydrateArtifactsStatus(ok, 3, pending)
	if ok > 0 {
		if err := saveArtifactPatch(saveCtx, db, id, row); err != nil {
			fail("artifacts", fmt.Errorf("saving artifacts: %w", err))
			return "error", pending, nil
		}
	}
	return status, pending, nil
}

// sectionFailed reports whether FetchArtifacts reported section as failed.
func sectionFailed(failed []loom.SectionError, section string) bool {
	for _, f := range failed {
		if f.Section == section {
			return true
		}
	}
	return false
}

func emitHydrate(cmd *cobra.Command, flags *rootFlags, res hydrateResult) error {
	return emit(cmd, flags, res, func(w io.Writer) error {
		if len(res.Videos) > 0 {
			tw := newTabWriter(w)
			fmt.Fprintln(tw, "VIDEO\tTRANSCRIPT\tAI\tERROR")
			for _, v := range res.Videos {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", truncate(v.Name, 44), v.Transcript, v.Artifacts, truncate(v.Error, 60))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
		}
		fmt.Fprintf(w, "processed %d, transcripts saved %d, AI artifacts saved %d, pending %d, skipped %d, failed %d\n",
			res.Processed, res.TranscriptsSaved, res.ArtifactsSaved, res.Pending, res.Skipped, res.Failed)
		if res.Note != "" {
			fmt.Fprintln(w, res.Note)
		}
		return nil
	})
}
