// Copyright 2026 cmartinez9 and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"loom-pp-cli/internal/loom"
	"loom-pp-cli/internal/store"
)

func TestPlanHydrate(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-48 * time.Hour).Format(time.RFC3339)
	old := now.Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	videos := []store.LocalVideo{
		{ID: "new", CreatedAt: recent},
		{ID: "done", CreatedAt: old},
		{ID: "tpending", CreatedAt: recent},
		{ID: "terror", CreatedAt: old},
		{ID: "tnone", CreatedAt: old},
		{ID: "apartial-recent", CreatedAt: recent},
		{ID: "apartial-old", CreatedAt: old},
		{ID: "apartial-err", CreatedAt: old},
	}
	states := map[string]store.HydrateState{
		"done":            {TranscriptStatus: "done", ArtifactsStatus: "done"},
		"tpending":        {TranscriptStatus: "pending", ArtifactsStatus: "done"},
		"terror":          {TranscriptStatus: "error", ArtifactsStatus: "done", LastError: "transcript: boom"},
		"tnone":           {TranscriptStatus: "none", ArtifactsStatus: "done"},
		"apartial-recent": {TranscriptStatus: "done", ArtifactsStatus: "partial"},
		"apartial-old":    {TranscriptStatus: "done", ArtifactsStatus: "partial"},
		"apartial-err":    {TranscriptStatus: "done", ArtifactsStatus: "partial", LastError: "summary: boom"},
	}
	ids := func(ps []hydratePlan) string {
		out := make([]string, 0, len(ps))
		for _, p := range ps {
			out = append(out, fmt.Sprintf("%s:%v%v%v", p.Video.ID, b2i(p.Transcript), b2i(p.Artifacts), b2i(p.Takeaways)))
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		name          string
		opts          hydrateOptions
		tkDone        map[string]bool
		want          string
		wantSkipped   int
		wantRemaining int
	}{
		{"default all", hydrateOptions{Only: "all"}, nil,
			"new:110,tpending:100,terror:100,apartial-recent:010,apartial-err:010", 3, 0},
		{"transcripts only", hydrateOptions{Only: "transcripts"}, nil,
			"new:100,tpending:100,terror:100", 5, 0},
		{"ai only", hydrateOptions{Only: "ai"}, nil,
			"new:010,apartial-recent:010,apartial-err:010", 5, 0},
		{"force", hydrateOptions{Only: "all", Force: true, Limit: 2}, nil,
			"new:110,done:110", 0, 6},
		{"limit", hydrateOptions{Only: "all", Limit: 1}, nil, "new:110", 3, 4},
		{"takeaways needed", hydrateOptions{Only: "ai", Takeaways: true}, map[string]bool{"done": true},
			"new:011,tpending:011,terror:011,tnone:011,apartial-recent:011,apartial-old:011,apartial-err:011", 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tk := tc.tkDone
			if tk == nil {
				tk = map[string]bool{}
			}
			plans, skipped, remaining := planHydrate(videos, states, tk, tc.opts, now)
			if got := ids(plans); got != tc.want {
				t.Fatalf("plans = %s\nwant    %s", got, tc.want)
			}
			if skipped != tc.wantSkipped || remaining != tc.wantRemaining {
				t.Fatalf("skipped, remaining = %d, %d; want %d, %d", skipped, remaining, tc.wantSkipped, tc.wantRemaining)
			}
		})
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestHydrateStillGenerating(t *testing.T) {
	cases := map[string]bool{
		"pending": true, "in_progress": true, "PROCESSING": true, "queued": true, "not_started": true,
		"success": false, "failed": false, "no_audio": false, "unsupported": false, "": false, "auto": false,
	}
	for status, want := range cases {
		if got := hydrateStillGenerating(status); got != want {
			t.Errorf("hydrateStillGenerating(%q) = %v, want %v", status, got, want)
		}
	}
	if hydrateSummaryPending("pending", "A summary exists") {
		t.Error("a summary with text is never pending")
	}
}

func TestHydrateArtifactsStatus(t *testing.T) {
	cases := []struct {
		ok, total int
		pending   bool
		want      string
	}{
		{3, 3, false, "done"},
		{3, 3, true, "partial"},
		{2, 3, false, "partial"},
		{0, 3, false, "error"},
	}
	for _, tc := range cases {
		if got := hydrateArtifactsStatus(tc.ok, tc.total, tc.pending); got != tc.want {
			t.Errorf("hydrateArtifactsStatus(%d,%d,%v) = %q, want %q", tc.ok, tc.total, tc.pending, got, tc.want)
		}
	}
}

// TestHydrateStateResume checks the store round trip hydrate relies on to
// resume: recorded states feed the next plan.
func TestHydrateStateResume(t *testing.T) {
	ctx := context.Background()
	db, _ := newTestStore(t)
	now := time.Now().UTC()
	for _, id := range []string{"a", "b", "c"} {
		insertTestVideo(t, db, id, "video "+id, now.Add(-time.Hour).Format(time.RFC3339), 10)
	}
	if err := db.SaveHydrateState(ctx, store.HydrateState{VideoID: "a", TranscriptStatus: "done", ArtifactsStatus: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveHydrateState(ctx, store.HydrateState{VideoID: "b", TranscriptStatus: "pending", ArtifactsStatus: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkTakeawaysAttempted(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	videos, err := db.LocalVideos(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	states, err := db.HydrateStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := db.TakeawaysAttempted(ctx)
	if err != nil || !tk["a"] || tk["b"] {
		t.Fatalf("TakeawaysAttempted = %v, %v", tk, err)
	}
	plans, skipped, _ := planHydrate(videos, states, tk, hydrateOptions{Only: "all"}, now)
	got := map[string]bool{}
	for _, p := range plans {
		got[p.Video.ID] = true
	}
	if skipped != 1 || got["a"] || !got["b"] || !got["c"] {
		t.Fatalf("resume plan = %v skipped %d; want b and c planned, a skipped", got, skipped)
	}
}

func TestHydrateEmptyStore(t *testing.T) {
	_, path := newTestStore(t)
	out, errOut, err := runCLI(t, "hydrate", "--since", "30d", "--db", path, "--json")
	if err != nil {
		t.Fatalf("hydrate on empty store: %v", err)
	}
	var res hydrateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if res.Processed != 0 || res.Videos == nil || res.FetchFailures == nil {
		t.Fatalf("unexpected empty result: %+v", res)
	}
	if !strings.Contains(errOut, "sync --resources videos") {
		t.Fatalf("stderr hint missing: %q", errOut)
	}
}

func TestHydrateUsageErrors(t *testing.T) {
	cases := [][]string{
		{"hydrate", "--since", "later", "--json"},
		{"hydrate", "--only", "videos", "--json"},
		{"hydrate", "--concurrency", "0", "--json"},
		{"hydrate", "--limit", "-1", "--json"},
		{"hydrate", "extra-arg", "--json"},
	}
	for _, args := range cases {
		_, _, err := runCLI(t, args...)
		var ce *cliError
		if !errors.As(err, &ce) || ce.code != 2 {
			t.Fatalf("%v: err = %v, want exit 2", args, err)
		}
	}
}

func TestHydrateDryRun(t *testing.T) {
	out, _, err := runCLI(t, "hydrate", "--since", "30d", "--dry-run", "--json")
	if err != nil || !strings.Contains(out, `"dry_run":true`) {
		t.Fatalf("dry-run: %q, %v", out, err)
	}
}

// TestHydrateAbortKeepsState: a run stopped by an expired session must not
// overwrite the video's recorded state (or its last error) and must not
// count the video as processed.
func TestHydrateAbortKeepsState(t *testing.T) {
	ctx := context.Background()
	db, _ := newTestStore(t)
	const id = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"
	prev := store.HydrateState{VideoID: id, TranscriptStatus: "error", ArtifactsStatus: "partial", LastError: "transcript: earlier failure"}
	if err := db.SaveHydrateState(ctx, prev); err != nil {
		t.Fatal(err)
	}
	lc := fakeLoomClient(t, func(op string) string {
		return `{"errors":[{"message":"No logged in viewer found","extensions":{"code":"UNAUTHENTICATED"}}],"data":null}`
	})
	plan := hydratePlan{Video: store.LocalVideo{ID: id, Name: "Demo"}, Transcript: true, Artifacts: true, Prev: prev}
	o := hydrateOne(ctx, 0, lc, db, plan)
	if !errors.Is(o.abort, loom.ErrSessionExpired) || o.completed {
		t.Fatalf("outcome = %+v, want an uncompleted session-expired abort", o)
	}
	states, err := db.HydrateStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := states[id]; got.LastError != prev.LastError || got.TranscriptStatus != prev.TranscriptStatus || got.ArtifactsStatus != prev.ArtifactsStatus {
		t.Fatalf("state after abort = %+v, want unchanged %+v", got, prev)
	}
	res := newHydrateResult()
	if err := runHydrate(ctx, 0, lc, db, []hydratePlan{plan}, 1, &res, &bulkOutcome{what: "hydrate"}); !errors.Is(err, loom.ErrSessionExpired) || res.Processed != 0 {
		t.Fatalf("runHydrate err=%v processed=%d, want session expired and 0 processed", err, res.Processed)
	}
}

// TestHydrateArtifactsNeverBlank: a summary Loom reports empty (still
// processing) must not erase the summary saved earlier, and one pending for
// weeks after recording is not counted as pending.
func TestHydrateArtifactsNeverBlank(t *testing.T) {
	ctx := context.Background()
	db, _ := newTestStore(t)
	const id = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"
	if err := db.SaveArtifacts(ctx, store.ArtifactRow{VideoID: id, Summary: "Saved summary.", SummaryStatus: "success",
		Chapters: json.RawMessage(`[{"start":0,"title":"Intro"}]`)}); err != nil {
		t.Fatal(err)
	}
	lc := fakeLoomClient(t, func(op string) string {
		switch op {
		case loom.OpSummary:
			return `{"data":{"getAutoFeatureStatuses":{"autoSummary":"","autoSummaryStatus":"PENDING"}}}`
		case loom.OpChapters:
			return `{"data":{"fetchVideoChapters":{"content":"","auto_chapter_status":"pending"}}}`
		case loom.OpTasks:
			return `{"data":{"getVideoTasks":{"tasks":[]}}}`
		}
		return `{"data":{}}`
	})
	fail := func(stage string, err error) { t.Errorf("unexpected %s failure: %v", stage, err) }
	recent := store.LocalVideo{ID: id, CreatedAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}
	status, pending, abort := hydrateArtifacts(ctx, ctx, lc, db, hydratePlan{Video: recent, Artifacts: true}, fail)
	if abort != nil || !pending || status != "partial" {
		t.Fatalf("recent video: status=%s pending=%v abort=%v, want partial and pending", status, pending, abort)
	}
	a, err := db.GetArtifacts(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Summary != "Saved summary." || !strings.Contains(string(a.Chapters), "Intro") {
		t.Fatalf("stored artifacts blanked: %+v", a)
	}
	old := store.LocalVideo{ID: id, CreatedAt: time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)}
	status, pending, _ = hydrateArtifacts(ctx, ctx, lc, db, hydratePlan{Video: old, Artifacts: true}, fail)
	if pending || status != "done" {
		t.Fatalf("stale pending summary: status=%s pending=%v, want done and not pending", status, pending)
	}
}

func TestSummaryPendingStale(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	old := now.Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	fresh := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	if !summaryPendingStale("pending", "", old, now) {
		t.Error("pending for 8 days should be stale")
	}
	if summaryPendingStale("pending", "", fresh, now) || summaryPendingStale("pending", "text", old, now) || summaryPendingStale("success", "", old, now) {
		t.Error("fresh, filled or finished summaries are not stale")
	}
}

// TestHydratePerVideoBudget: a video that runs past its own budget is a
// per-video failure (recorded, run continues); only the end of the run
// context stops the run.
func TestHydratePerVideoBudget(t *testing.T) {
	ctx := context.Background()
	db, _ := newTestStore(t)
	const id = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"
	lc := fakeLoomClient(t, func(op string) string {
		if op == loom.OpMe {
			return `{"data":{"me":{"id":"1"}}}`
		}
		time.Sleep(300 * time.Millisecond)
		return `{"data":{}}`
	})
	plan := hydratePlan{Video: store.LocalVideo{ID: id, Name: "Slow"}, Transcript: true, Artifacts: true}
	o := hydrateOne(ctx, 50*time.Millisecond, lc, db, plan)
	if o.abort != nil || !o.completed || len(o.failures) == 0 {
		t.Fatalf("outcome = %+v, want a completed video with failures and no abort", o)
	}
	res := newHydrateResult()
	outcome := bulkOutcome{what: "hydrate"}
	if err := runHydrate(ctx, 50*time.Millisecond, lc, db, []hydratePlan{plan}, 1, &res, &outcome); err != nil {
		t.Fatalf("runHydrate = %v, want no run-stopping error", err)
	}
	if res.Processed != 1 || res.Failed != 1 || exitCode(outcome.exitErr()) != 5 {
		t.Fatalf("res = %+v exit %d, want 1 processed, 1 failed, exit 5 (every video failed)", res, exitCode(outcome.exitErr()))
	}

	runCtx, cancel := context.WithCancel(ctx)
	cancel()
	o = hydrateOne(runCtx, 0, lc, db, plan)
	if !errors.Is(o.abort, context.Canceled) || o.completed {
		t.Fatalf("ended run: outcome = %+v, want an abort", o)
	}
}

// TestHydrateStopNote: each way a hydrate run stops early gets its own
// note; a Ctrl-C never blames --timeout.
func TestHydrateStopNote(t *testing.T) {
	cases := []struct {
		err          error
		want, absent string
	}{
		{context.Canceled, "interrupted with 3 videos left", "--timeout"},
		{fmt.Errorf("fetching: %w", context.Canceled), "interrupted", "--timeout"},
		{context.DeadlineExceeded, "stopped at the --timeout limit with 3 videos left", "interrupted"},
		{loom.ErrSessionExpired, "stopped early with 3 videos left", "--timeout"},
	}
	for _, tc := range cases {
		got := hydrateStopNote(tc.err, 3)
		if !strings.Contains(got, tc.want) || strings.Contains(got, tc.absent) {
			t.Errorf("hydrateStopNote(%v) = %q, want %q and not %q", tc.err, got, tc.want, tc.absent)
		}
	}
}
