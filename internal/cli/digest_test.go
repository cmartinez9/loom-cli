// Copyright 2026 cmartinez9 and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"loom-pp-cli/internal/loom"
	"loom-pp-cli/internal/store"
)

const digestTestID = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"

func digestTestArtifacts() *store.ArtifactRow {
	chapters, _ := json.Marshal(loom.ParseChapters(digestTestID, "0:00 Intro\n1:30 Pricing [draft]"))
	tasks, _ := json.Marshal([]loom.Task{
		{Content: "Send the deck", Start: 95, Timestamp: "1:35", Link: loom.DeepLink(digestTestID, 95)},
		{Content: "Book a follow-up", ResolvedAt: "2026-09-02T00:00:00Z"},
	})
	return &store.ArtifactRow{VideoID: digestTestID, Summary: "  The team reviewed pricing.  ", SummaryStatus: "success", Chapters: chapters, Tasks: tasks, Takeaways: json.RawMessage(`[]`)}
}

func TestDigestVideoFrom(t *testing.T) {
	v := store.LocalVideo{ID: digestTestID, Name: "Pricing review", CreatedAt: "2026-09-01T10:00:00.000Z", DurationSeconds: 125}
	missing := digestVideoFrom(v, nil, "7d")
	if missing.Hydrated || !strings.Contains(missing.Note, "hydrate --since 7d") || missing.ShareURL != loom.ShareURL(digestTestID) {
		t.Fatalf("missing artifacts entry = %+v", missing)
	}
	if missing.Chapters == nil || missing.ActionItems == nil {
		t.Fatal("empty lists must marshal as []")
	}
	d := digestVideoFrom(v, digestTestArtifacts(), "7d")
	if !d.Hydrated || d.Summary != "The team reviewed pricing." || d.Duration != "2:05" {
		t.Fatalf("hydrated entry = %+v", d)
	}
	if len(d.Chapters) != 2 || d.Chapters[1].Link != loom.ShareURL(digestTestID)+"?t=90" {
		t.Fatalf("chapters = %+v", d.Chapters)
	}
	if len(d.ActionItems) != 2 || d.Takeaways != nil {
		t.Fatalf("action items = %+v takeaways = %v", d.ActionItems, d.Takeaways)
	}
}

func TestRenderDigestMarkdown(t *testing.T) {
	v := store.LocalVideo{ID: digestTestID, Name: "Pricing review", CreatedAt: "2026-09-01T10:00:00.000Z", DurationSeconds: 125}
	cases := []struct {
		name   string
		res    digestResult
		want   []string
		absent []string
	}{
		{
			name: "empty window",
			res:  digestResult{Since: "2026-09-22T00:00:00Z", Until: "2026-09-29T00:00:00Z"},
			want: []string{"# Loom digest: ", "No videos in this window."},
		},
		{
			name: "hydrated video",
			res: digestResult{Since: "2026-08-29T00:00:00Z", Until: "2026-09-29T00:00:00Z",
				Videos: []digestVideo{digestVideoFrom(v, digestTestArtifacts(), "30d")}},
			want: []string{
				"1 video\n",
				"## [Pricing review](https://www.loom.com/share/" + digestTestID + ")",
				"· 2:05",
				"The team reviewed pricing.",
				"**Chapters**",
				"- [0:00](https://www.loom.com/share/" + digestTestID + ") Intro",
				"- [1:30](https://www.loom.com/share/" + digestTestID + "?t=90) Pricing \\[draft\\]",
				"**Action items**",
				"- [ ] Send the deck ([1:35](https://www.loom.com/share/" + digestTestID + "?t=95))",
				"- [x] Book a follow-up",
			},
			absent: []string{"No videos in this window", "not hydrated"},
		},
		{
			name: "not hydrated",
			res: digestResult{Since: "2026-08-29T00:00:00Z", Until: "2026-09-29T00:00:00Z", MissingArtifacts: 1,
				Videos: []digestVideo{digestVideoFrom(v, nil, "30d")}},
			want:   []string{"1 not hydrated yet", "_not hydrated yet: run loom-pp-cli hydrate --since 30d_"},
			absent: []string{"**Chapters**"},
		},
		{
			name: "summary failed",
			res: digestResult{Since: "2026-08-29T00:00:00Z", Until: "2026-09-29T00:00:00Z",
				Videos: []digestVideo{digestVideoFrom(v, &store.ArtifactRow{VideoID: digestTestID, SummaryStatus: "failed"}, "30d")}},
			want: []string{"No AI summary from Loom (status: failed)"},
		},
		{
			name: "brackets escaped in title",
			res: digestResult{Since: "2026-08-29T00:00:00Z", Until: "2026-09-29T00:00:00Z",
				Videos: []digestVideo{digestVideoFrom(store.LocalVideo{ID: digestTestID, Name: "Demo [v2]", CreatedAt: "2026-09-01T10:00:00Z"}, nil, "30d")}},
			want: []string{`## [Demo \[v2\]](`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			md := renderDigestMarkdown(tc.res)
			for _, w := range tc.want {
				if !strings.Contains(md, w) {
					t.Fatalf("markdown missing %q:\n%s", w, md)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(md, a) {
					t.Fatalf("markdown unexpectedly contains %q:\n%s", a, md)
				}
			}
		})
	}
}

func TestWindowBoundsContains(t *testing.T) {
	start := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	cases := map[string]bool{
		"2026-09-21T23:59:59Z":     false,
		"2026-09-22T00:00:00Z":     true,
		"2026-09-28T23:59:59.000Z": true,
		"2026-09-29T00:00:00Z":     false,
		"2026-09-30T10:00:00Z":     false,
	}
	b := windowBounds{Start: start, End: end}
	for in, want := range cases {
		tm, ok := loom.ParseTime(in)
		if !ok {
			t.Fatalf("ParseTime(%q) failed", in)
		}
		if got := b.contains(tm); got != want {
			t.Errorf("contains(%q) = %v, want %v", in, got, want)
		}
	}
	if !(windowBounds{}).contains(start) || (windowBounds{}).bounded() {
		t.Error("an unbounded window must contain everything")
	}
	if got := (windowBounds{}).startRFC3339(); got != "" {
		t.Errorf("open start = %q", got)
	}
	if got := b.startRFC3339(); got != "2026-09-22T00:00:00Z" {
		t.Errorf("startRFC3339 = %q", got)
	}
}

// TestDigestCommandJSON runs the command against a temp store with one
// hydrated and one unhydrated video in the window and one outside it.
func TestDigestCommandJSON(t *testing.T) {
	ctx := context.Background()
	db, path := newTestStore(t)
	now := time.Now().UTC()
	insertTestVideo(t, db, digestTestID, "Pricing review", now.Add(-48*time.Hour).Format(time.RFC3339), 125)
	insertTestVideo(t, db, "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f61", "Standup", now.Add(-24*time.Hour).Format(time.RFC3339), 60)
	insertTestVideo(t, db, "ffffffffffffffffffffffffffffffff", "Old", now.Add(-40*24*time.Hour).Format(time.RFC3339), 60)
	if err := db.SaveArtifacts(ctx, *digestTestArtifacts()); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	out, errOut, err := runCLI(t, "digest", "--since", "7d", "--db", path, "--json")
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	var res digestResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(res.Videos) != 2 || res.MissingArtifacts != 1 || res.Videos[0].VideoID != digestTestID {
		t.Fatalf("digest = %+v", res)
	}
	if !strings.Contains(errOut, "hydrate --since 7d") {
		t.Fatalf("stderr hint missing: %q", errOut)
	}
	out, _, err = runCLI(t, "digest", "--since", "7d", "--until", "36h", "--db", path, "--format", "md")
	if err != nil || !strings.Contains(out, "Pricing review") || strings.Contains(out, "Standup") {
		t.Fatalf("--until window markdown: %v\n%s", err, out)
	}
	out, errOut, err = runCLI(t, "digest", "--since", "12h", "--db", path, "--json")
	if err != nil || !strings.Contains(out, `"videos": []`) {
		t.Fatalf("empty window: %v\n%s", err, out)
	}
	if strings.Contains(errOut, "no synced videos") {
		t.Fatalf("empty window over a filled store should not ask for sync: %q", errOut)
	}
}

func TestDigestEmptyStoreHintsSync(t *testing.T) {
	db, path := newTestStore(t)
	_ = db.Close()
	out, errOut, err := runCLI(t, "digest", "--since", "400d", "--db", path, "--json")
	if err != nil || !strings.Contains(out, `"videos": []`) {
		t.Fatalf("digest on empty store: %v\n%s", err, out)
	}
	if !strings.Contains(errOut, "no synced videos in the local store") || !strings.Contains(errOut, "sync --resources videos") {
		t.Fatalf("sync hint missing: %q", errOut)
	}
}

func TestDigestUsageErrors(t *testing.T) {
	cases := [][]string{
		{"digest", "--since", "eventually", "--json"},
		{"digest", "--since", "7d", "--until", "14d", "--json"},
		{"digest", "--since", "7d", "--format", "html"},
		{"digest", "stray", "--json"},
	}
	for _, args := range cases {
		_, _, err := runCLI(t, args...)
		var ce *cliError
		if !errors.As(err, &ce) || ce.code != 2 {
			t.Fatalf("%v: err = %v, want exit 2", args, err)
		}
	}
}
