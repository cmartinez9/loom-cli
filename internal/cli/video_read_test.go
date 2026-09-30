package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cmartinez9/loom-cli/internal/loom"
	"github.com/cmartinez9/loom-cli/internal/store"
)

func TestNormalizeTranscriptStatus(t *testing.T) {
	cases := []struct {
		status string
		segs   int
		want   string
	}{
		{"success", 10, "success"},
		{"SUCCESS", 0, "success"},
		{"in_progress", 0, "pending"},
		{"processing", 0, "pending"},
		{"", 0, "pending"},
		{"", 3, "success"},
		{"no_audio", 0, "no_audio"},
		{"in_progress", 4, "in_progress"},
	}
	for _, c := range cases {
		if got := normalizeTranscriptStatus(c.status, c.segs); got != c.want {
			t.Errorf("normalizeTranscriptStatus(%q,%d) = %q, want %q", c.status, c.segs, got, c.want)
		}
	}
}

func TestRenderTranscriptSpeakers(t *testing.T) {
	one := []loom.Segment{{Start: 0, End: 2, Speaker: 0, Text: "hello"}, {Start: 2, End: 4, Speaker: 0, Text: "again"}}
	two := []loom.Segment{{Start: 0, End: 2, Speaker: 0, Text: "hello"}, {Start: 65, End: 70, Speaker: 1, Text: "hi"}}
	cases := []struct {
		name     string
		format   string
		segs     []loom.Segment
		speakers bool
		want     string
	}{
		{"text single speaker ignores flag", "text", one, true, "hello\nagain\n"},
		{"text two speakers", "text", two, true, "Speaker 1: hello\nSpeaker 2: hi\n"},
		{"text two speakers flag off", "text", two, false, "hello\nhi\n"},
		{"timestamped", "timestamped", two, true, "[0:00] Speaker 1: hello\n[1:05] Speaker 2: hi\n"},
		{"srt", "srt", one, false, "1\n00:00:00,000 --> 00:00:02,000\nhello\n\n2\n00:00:02,000 --> 00:00:04,000\nagain\n\n"},
		{"vtt header", "vtt", one[:1], false, "WEBVTT\n\n1\n00:00:00.000 --> 00:00:02.000\nhello\n\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderTranscript(c.format, c.segs, c.speakers); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
	// Prefixing must not mutate the caller's segments.
	if two[0].Text != "hello" {
		t.Fatalf("speakerLines mutated input: %q", two[0].Text)
	}
}

func TestBuildTranscriptView(t *testing.T) {
	tr := &loom.Transcript{VideoID: exampleVideoID, Version: 3, Language: "en", Status: "success", Segments: []loom.Segment{
		{Start: 0, End: 4.5, Text: "first"}, {Start: 4.5, End: 9, Text: "second"},
	}}
	v := buildTranscriptView(exampleVideoID, tr, false)
	if v.Status != "success" || len(v.Segments) != 2 || v.Text != "first\nsecond" {
		t.Fatalf("unexpected view: %+v", v)
	}
	if v.Segments[1].Link != loom.ShareURL(exampleVideoID)+"?t=4" || v.Segments[1].Timestamp != "0:04" {
		t.Fatalf("bad link/timestamp: %+v", v.Segments[1])
	}
	empty := buildTranscriptView(exampleVideoID, &loom.Transcript{Status: "unavailable"}, false)
	if empty.Status != "unavailable" || empty.Segments == nil {
		t.Fatalf("empty view: %+v", empty)
	}
	b, _ := json.Marshal(empty)
	if !strings.Contains(string(b), `"segments":[]`) {
		t.Fatalf("segments must marshal as []: %s", b)
	}
}

func TestMergeArtifact(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	existing := &store.ArtifactRow{
		VideoID: exampleVideoID, Summary: "old summary", SummaryStatus: "success",
		Chapters: json.RawMessage(`[{"start":0,"title":"Intro"}]`), ChaptersStatus: "success",
		Tasks: json.RawMessage(`[{"id":"1"}]`), Takeaways: json.RawMessage(`["a"]`),
	}
	cases := []struct {
		name     string
		existing *store.ArtifactRow
		patch    artifactPatch
		check    func(t *testing.T, r store.ArtifactRow)
	}{
		{"new row from summary", nil, artifactPatch{Summary: "s", SummaryStatus: "success"}, func(t *testing.T, r store.ArtifactRow) {
			if r.VideoID != exampleVideoID || r.Summary != "s" || len(r.Chapters) != 0 {
				t.Fatalf("got %+v", r)
			}
		}},
		{"summary keeps chapters and tasks", existing, artifactPatch{Summary: "new", SummaryStatus: "success"}, func(t *testing.T, r store.ArtifactRow) {
			if r.Summary != "new" || string(r.Chapters) != string(existing.Chapters) || string(r.Tasks) != `[{"id":"1"}]` || string(r.Takeaways) != `["a"]` {
				t.Fatalf("got %+v", r)
			}
		}},
		{"empty summary never blanks", existing, artifactPatch{Summary: "  ", SummaryStatus: "pending"}, func(t *testing.T, r store.ArtifactRow) {
			if r.Summary != "old summary" || r.SummaryStatus != "pending" {
				t.Fatalf("got %+v", r)
			}
		}},
		{"empty chapters never blank", existing, artifactPatch{Chapters: json.RawMessage(`[]`), ChaptersStatus: "in_progress"}, func(t *testing.T, r store.ArtifactRow) {
			if string(r.Chapters) != string(existing.Chapters) || r.ChaptersStatus != "in_progress" || r.Summary != "old summary" {
				t.Fatalf("got %+v", r)
			}
		}},
		{"chapters replace", existing, artifactPatch{Chapters: json.RawMessage(`[{"start":5}]`)}, func(t *testing.T, r store.ArtifactRow) {
			if string(r.Chapters) != `[{"start":5}]` || r.ChaptersStatus != "success" {
				t.Fatalf("got %+v", r)
			}
		}},
		{"fetched_at stamped", existing, artifactPatch{}, func(t *testing.T, r store.ArtifactRow) {
			if r.FetchedAt != "2026-09-29T12:00:00Z" {
				t.Fatalf("got %q", r.FetchedAt)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.check(t, mergeArtifact(c.existing, exampleVideoID, c.patch, now))
		})
	}
	if existing.Summary != "old summary" {
		t.Fatal("mergeArtifact mutated the existing row")
	}
}

func TestSaveArtifactPatchRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenWithContext(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.EnsureLoomSchema(ctx); err != nil {
		t.Fatal(err)
	}
	chs := []loom.Chapter{{Start: 0, Timestamp: "0:00", Title: "Intro", Link: loom.ShareURL(exampleVideoID)}}
	if err := saveArtifactPatch(ctx, db, exampleVideoID, artifactPatch{Chapters: mustJSON(chs), ChaptersStatus: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := saveArtifactPatch(ctx, db, exampleVideoID, artifactPatch{Summary: "A summary.", SummaryStatus: "success"}); err != nil {
		t.Fatal(err)
	}
	got, err := localChapters(ctx, db, exampleVideoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Chapters) != 1 || got.Chapters[0].Title != "Intro" || got.Status != "success" {
		t.Fatalf("chapters lost after summary write-through: %+v", got)
	}
	a, err := db.GetArtifacts(ctx, exampleVideoID)
	if err != nil || a.Summary != "A summary." {
		t.Fatalf("summary not saved: %+v %v", a, err)
	}
}

func TestIsEmptyJSONArray(t *testing.T) {
	for in, want := range map[string]bool{"": true, "null": true, "[]": true, " [ ] ": true, `["x"]`: false, `{}`: false} {
		if got := isEmptyJSONArray(json.RawMessage(in)); got != want {
			t.Errorf("isEmptyJSONArray(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFlattenCommentThreads(t *testing.T) {
	threads := []loom.Comment{
		{ID: "1", Text: "top", Replies: []loom.Comment{
			{ID: "2", ParentID: "1", Text: "reply", Replies: []loom.Comment{{ID: "3", Text: "nested"}}},
		}},
		{ID: "4", Text: "second"},
	}
	rows := flattenCommentThreads(threads)
	type pd struct {
		ID, Parent string
		Depth      int
	}
	got := make([]pd, 0, len(rows))
	for _, r := range rows {
		got = append(got, pd{r.ID, r.ParentID, r.Depth})
	}
	want := []pd{{"1", "", 0}, {"2", "1", 1}, {"3", "2", 2}, {"4", "", 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if empty := flattenCommentThreads(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("empty input must give [] not nil")
	}
}

func TestBuildCommentThreads(t *testing.T) {
	flat := []loom.Comment{
		{ID: "1", Text: "top"},
		{ID: "2", ParentID: "1", Text: "reply"},
		{ID: "3", ParentID: "missing", Text: "orphan"},
		{ID: "4", ParentID: "2", Text: "nested"},
		{ID: "5", ParentID: "5", Text: "self"},
	}
	threads := buildCommentThreads(flat)
	ids := []string{}
	for _, c := range threads {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []string{"1", "3", "5"}) {
		t.Fatalf("roots = %v", ids)
	}
	if len(threads[0].Replies) != 1 || threads[0].Replies[0].ID != "2" || len(threads[0].Replies[0].Replies) != 1 {
		t.Fatalf("nesting wrong: %+v", threads[0])
	}
	if threads[1].Replies == nil {
		t.Fatal("replies must be [] not nil")
	}
	out := renderCommentThreads(threads[:1])
	if !strings.Contains(out, "\n  Anonymous  ") || !strings.Contains(out, "\n    Anonymous  ") {
		t.Fatalf("indentation missing:\n%s", out)
	}
}

func TestSummarizeReactions(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []reactionCount
	}{
		{"empty", nil, []reactionCount{}},
		{"counts and order", []string{"👍", "🎉", "👍", "❤️", "🎉", "👍"}, []reactionCount{{"👍", 3}, {"🎉", 2}, {"❤️", 1}}},
		{"ties sorted by emoji", []string{"b", "a"}, []reactionCount{{"a", 1}, {"b", 1}}},
		{"blank emoji", []string{" "}, []reactionCount{{"(unknown)", 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rs := make([]loom.Reaction, 0, len(c.in))
			for _, e := range c.in {
				rs = append(rs, loom.Reaction{Emoji: e})
			}
			got := summarizeReactions(rs)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestRenderChapters(t *testing.T) {
	base := loom.ShareURL(exampleVideoID)
	chs := []chapterView{
		{Start: 0, Timestamp: "0:00", Title: "Intro", Link: base},
		{Start: 97, Timestamp: "1:37", Title: "Setup [part_1]", Link: base + "?t=97"},
		{Start: 3725, Timestamp: "1:02:05", Title: "Wrap", Link: base + "?t=3725"},
	}
	md := renderChaptersMarkdown(chs)
	wantMD := "- [0:00](" + base + ") Intro\n- [1:37](" + base + "?t=97) Setup \\[part\\_1\\]\n- [1:02:05](" + base + "?t=3725) Wrap\n"
	if md != wantMD {
		t.Fatalf("markdown:\n%s\nwant:\n%s", md, wantMD)
	}
	text := renderChaptersText(chs)
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "   0:00  Intro           ") || !strings.HasSuffix(lines[1], "?t=97") {
		t.Fatalf("text:\n%s", text)
	}
	if renderChaptersMarkdown(nil) != "" {
		t.Fatal("no chapters should render nothing")
	}
}

func TestBuildChaptersView(t *testing.T) {
	v := buildChaptersView(exampleVideoID, &loom.Chapters{Chapters: []loom.Chapter{{Start: 1, Title: "x"}}})
	if v.Status != "success" {
		t.Fatalf("status = %q", v.Status)
	}
	v = buildChaptersView(exampleVideoID, &loom.Chapters{})
	if v.Status != "none" || v.Chapters == nil {
		t.Fatalf("empty: %+v", v)
	}
}

func TestFilterTasks(t *testing.T) {
	tasks := []loom.Task{{ID: "1"}, {ID: "2", ResolvedAt: "2026-01-01T00:00:00Z"}, {ID: "3"}}
	if got := filterTasks(tasks, false); len(got) != 3 {
		t.Fatalf("all: %d", len(got))
	}
	got := filterTasks(tasks, true)
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "3" {
		t.Fatalf("open: %+v", got)
	}
	if got := filterTasks(nil, true); got == nil {
		t.Fatal("must be [] not nil")
	}
}

func TestNormalizeSummaryStatus(t *testing.T) {
	cases := []struct{ status, summary, want string }{
		{"AUTO", "text", "success"},
		{"PENDING", "", "pending"},
		{"FAILED", "", "failed"},
		{"AUTO", "", "none"},
		{"", "", "none"},
		{"weird_new_state", "", "weird_new_state"},
		{"PENDING", "already have text", "success"},
	}
	for _, c := range cases {
		if got := normalizeSummaryStatus(c.status, c.summary); got != c.want {
			t.Errorf("normalizeSummaryStatus(%q,%q) = %q want %q", c.status, c.summary, got, c.want)
		}
	}
}

func TestBuildBacklinkRows(t *testing.T) {
	rows := buildBacklinkRows(backlinkData{
		Backlinks: []loom.Backlink{{ID: "b1", Source: "CONFLUENCE", SourceLink: "https://example.atlassian.net/wiki/x", Title: "Spec", IsSynced: true}},
		Links:     []loom.VideoLink{{ID: "l1", URL: "https://example.com", Title: "Docs", Start: 5, End: 9, Timestamp: "0:05"}},
	})
	if len(rows) != 2 || rows[0].Kind != "backlink" || rows[0].Source != "confluence" || rows[0].IsSynced == nil || !*rows[0].IsSynced {
		t.Fatalf("backlink row: %+v", rows)
	}
	if rows[1].Kind != "link" || rows[1].URL != "https://example.com" || rows[1].Start != 5 {
		t.Fatalf("link row: %+v", rows[1])
	}
	if got := buildBacklinkRows(backlinkData{}); got == nil || len(got) != 0 {
		t.Fatal("empty must be []")
	}
}
