// Copyright 2026 cmartinez9 and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"loom-pp-cli/internal/loom"
	"loom-pp-cli/internal/store"
)

func intp(v int) *int { return &v }

func deltaStr(p *int) string { return activityFmtDelta(p) }

func TestActivityDeltas(t *testing.T) {
	cur := loom.Video{ID: "v", Views: 10, Comments: 3, Reactions: 1}
	cases := []struct {
		name         string
		prev         *store.ActivitySnapshot
		wantV, wantC string
		wantR        string
		wantComments bool
	}{
		{"no baseline", nil, "-", "-", "-", true},
		{"unchanged", &store.ActivitySnapshot{Views: 10, Comments: 3, Reactions: 1}, "0", "0", "0", false},
		{"grew", &store.ActivitySnapshot{Views: 7, Comments: 1, Reactions: 0}, "+3", "+2", "+1", true},
		{"comment deleted", &store.ActivitySnapshot{Views: 10, Comments: 4, Reactions: 1}, "0", "-1", "0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, c, r := computeActivityDeltas(cur, tc.prev)
			if deltaStr(v) != tc.wantV || deltaStr(c) != tc.wantC || deltaStr(r) != tc.wantR {
				t.Fatalf("deltas = %s %s %s, want %s %s %s", deltaStr(v), deltaStr(c), deltaStr(r), tc.wantV, tc.wantC, tc.wantR)
			}
			if got := activityWantComments(cur, tc.prev); got != tc.wantComments {
				t.Fatalf("activityWantComments = %v, want %v", got, tc.wantComments)
			}
		})
	}
	if activityWantComments(loom.Video{Comments: 0}, nil) {
		t.Fatal("a first-seen video with no comments needs no comment fetch")
	}
}

func cmt(id, author, authorID, at string, replies ...loom.Comment) loom.Comment {
	return loom.Comment{ID: id, Author: author, AuthorID: authorID, Text: "text " + id, CreatedAt: at, Replies: replies}
}

func TestActivityNeedsReply(t *testing.T) {
	const me = "42"
	cases := []struct {
		name        string
		comments    []loom.Comment
		me          string
		want        bool
		wantExcerpt []string // comment ids in the excerpt
	}{
		{"no comments", nil, me, false, nil},
		{"unknown user", []loom.Comment{cmt("1", "Ana", "7", "2026-09-01T10:00:00Z")}, "", false, nil},
		{"someone else commented", []loom.Comment{cmt("1", "Ana", "7", "2026-09-01T10:00:00Z")}, me, true, []string{"1"}},
		{"i commented last on my own thread", []loom.Comment{cmt("1", "Me", me, "2026-09-01T10:00:00Z")}, me, false, nil},
		{"i replied", []loom.Comment{
			cmt("1", "Ana", "7", "2026-09-01T10:00:00Z", cmt("2", "Me", me, "2026-09-01T11:00:00Z")),
		}, me, false, nil},
		{"they replied after me", []loom.Comment{
			cmt("1", "Ana", "7", "2026-09-01T10:00:00Z",
				cmt("2", "Me", me, "2026-09-01T11:00:00Z"),
				cmt("3", "Ana", "7", "2026-09-01T12:00:00Z")),
		}, me, true, []string{"1", "2", "3"}},
		{"excerpt keeps last three", []loom.Comment{
			cmt("1", "Ana", "7", "2026-09-01T10:00:00Z",
				cmt("2", "Me", me, "2026-09-01T11:00:00Z"),
				cmt("3", "Ana", "7", "2026-09-01T12:00:00Z"),
				cmt("4", "Bo", "8", "2026-09-01T13:00:00Z")),
		}, me, true, []string{"2", "3", "4"}},
		{"only the newest thread counts", []loom.Comment{
			cmt("1", "Ana", "7", "2026-09-01T10:00:00Z"),
			cmt("5", "Me", me, "2026-09-02T10:00:00Z"),
		}, me, false, nil},
		{"newest thread unanswered", []loom.Comment{
			cmt("5", "Me", me, "2026-09-02T10:00:00Z"),
			cmt("1", "Ana", "7", "2026-09-03T10:00:00Z"),
		}, me, true, []string{"1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, excerpt := activityNeedsReply(tc.comments, tc.me)
			if got != tc.want {
				t.Fatalf("needs reply = %v, want %v", got, tc.want)
			}
			ids := make([]string, 0, len(excerpt))
			for _, c := range excerpt {
				ids = append(ids, c.ID)
			}
			if strings.Join(ids, ",") != strings.Join(tc.wantExcerpt, ",") {
				t.Fatalf("excerpt = %v, want %v", ids, tc.wantExcerpt)
			}
		})
	}
}

func TestActivityUnseenComments(t *testing.T) {
	comments := []loom.Comment{
		cmt("1", "Ana", "7", "2026-09-01T10:00:00Z", cmt("3", "Bo", "8", "2026-09-01T12:00:00Z")),
		cmt("2", "Cy", "9", "2026-09-01T11:00:00Z"),
	}
	cases := []struct {
		seen map[string]bool
		want string
	}{
		{map[string]bool{}, "1,2,3"},
		{map[string]bool{"1": true}, "2,3"},
		{map[string]bool{"1": true, "2": true, "3": true}, ""},
	}
	for _, tc := range cases {
		got := make([]string, 0)
		for _, c := range unseenComments(comments, tc.seen) {
			got = append(got, c.ID)
		}
		if strings.Join(got, ",") != tc.want {
			t.Fatalf("unseen(%v) = %v, want %s", tc.seen, got, tc.want)
		}
	}
}

func TestSelectActivityRows(t *testing.T) {
	rows := []activityVideo{
		{VideoID: "quiet", ViewsDelta: intp(0), CommentsDelta: intp(0), ReactionsDelta: intp(0)},
		{VideoID: "views", ViewsDelta: intp(2), CommentsDelta: intp(0), ReactionsDelta: intp(0)},
		{VideoID: "reply", ViewsDelta: intp(0), CommentsDelta: intp(0), ReactionsDelta: intp(0), NeedsReply: true},
		{VideoID: "new", FirstSeen: true},
		{VideoID: "fewer", ViewsDelta: intp(0), CommentsDelta: intp(-1), ReactionsDelta: intp(0)},
	}
	ids := func(rs []activityVideo) string {
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			out = append(out, r.VideoID)
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		name     string
		baseline bool
		all      bool
		limit    int
		want     string
	}{
		{"changes only", false, false, 0, "views,reply,new"},
		{"all", false, true, 0, "quiet,views,reply,new,fewer"},
		{"baseline shows all", true, false, 0, "quiet,views,reply,new,fewer"},
		{"limit", false, true, 2, "quiet,views"},
	}
	for _, tc := range cases {
		if got := ids(selectActivityRows(rows, tc.baseline, tc.all, tc.limit)); got != tc.want {
			t.Fatalf("%s: rows = %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestActivityNote(t *testing.T) {
	cases := []struct {
		name                  string
		baseline, noSave, cap bool
		inWindow, shown       int
		want                  string
	}{
		{"baseline", true, false, false, 5, 5, "baseline recorded for 5 videos"},
		{"baseline no-save", true, true, false, 5, 5, "--no-save"},
		{"quiet later run", false, false, false, 5, 0, "use --all"},
		{"cap empty", false, false, true, 0, 0, "raise --max-pages"},
		{"empty window", true, false, false, 0, 0, "no videos in the window; nothing recorded"},
	}
	for _, tc := range cases {
		got := activityNote(tc.baseline, tc.noSave, tc.cap, tc.inWindow, tc.shown, 50, 5, "2026-09-01T00:00:00.000000Z")
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%s: note %q missing %q", tc.name, got, tc.want)
		}
	}
	if got := activityNote(false, false, false, 5, 2, 5, 5, "x"); got != "" {
		t.Fatalf("a later run with changes needs no note, got %q", got)
	}
}

// TestActivitySnapshotStore checks the baseline round trip: the latest prior
// snapshot per video, the previous check time, and seen-comment tracking.
func TestActivitySnapshotStore(t *testing.T) {
	ctx := context.Background()
	db, _ := newTestStore(t)
	t1 := "2026-09-01T10:00:00.000000Z"
	t2 := "2026-09-02T10:00:00.000000Z"
	t3 := "2026-09-03T10:00:00.000000Z"
	if last, err := db.LastActivityCheck(ctx, t3); err != nil || last != "" {
		t.Fatalf("empty LastActivityCheck = %q, %v", last, err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.SaveActivitySnapshots(ctx, []store.ActivitySnapshot{
		{TakenAt: t1, VideoID: "a", Views: 1},
		{TakenAt: t1, VideoID: "b", Views: 5},
	}))
	must(db.SaveActivitySnapshots(ctx, []store.ActivitySnapshot{{TakenAt: t2, VideoID: "a", Views: 4, Comments: 1}}))
	prev, err := db.LatestActivitySnapshots(ctx, []string{"a", "b", "c"}, t3)
	must(err)
	if prev["a"].Views != 4 || prev["a"].TakenAt != t2 || prev["b"].Views != 5 {
		t.Fatalf("latest snapshots = %+v", prev)
	}
	if _, ok := prev["c"]; ok {
		t.Fatal("video c has no snapshot")
	}
	before, err := db.LatestActivitySnapshots(ctx, []string{"a"}, t2)
	must(err)
	if before["a"].TakenAt != t1 {
		t.Fatalf("snapshot before t2 = %+v, want t1", before["a"])
	}
	last, err := db.LastActivityCheck(ctx, t3)
	must(err)
	if last != t2 {
		t.Fatalf("LastActivityCheck = %q, want %q", last, t2)
	}
	must(db.RecordSeenComments(ctx, []store.SeenComment{{CommentID: "c1", VideoID: "a"}, {CommentID: "c2", VideoID: "a"}}))
	must(db.RecordSeenComments(ctx, []store.SeenComment{{CommentID: "c1", VideoID: "a"}}))
	seen, err := db.SeenCommentIDs(ctx, "a")
	must(err)
	if len(seen) != 2 || !seen["c1"] || !seen["c2"] {
		t.Fatalf("seen = %v", seen)
	}
}

func TestActivityUsageErrors(t *testing.T) {
	cases := [][]string{
		{"activity", "--since", "whenever", "--json"},
		{"activity", "--since", "0d", "--json"},
		{"activity", "--max-scan-pages", "0", "--json"},
		{"activity", "stray", "--json"},
	}
	for _, args := range cases {
		_, _, err := runCLI(t, args...)
		var ce *cliError
		if !errors.As(err, &ce) || ce.code != 2 {
			t.Fatalf("%v: err = %v, want exit 2", args, err)
		}
	}
}

func TestActivitySnapshotsKeepPreviousCommentCount(t *testing.T) {
	videos := []loom.Video{
		{ID: "ok", Views: 5, Comments: 4},
		{ID: "failed", Views: 9, Comments: 7},
		{ID: "failed-new", Views: 1, Comments: 2},
	}
	prevs := map[string]store.ActivitySnapshot{
		"ok":     {VideoID: "ok", Comments: 1},
		"failed": {VideoID: "failed", Comments: 3},
	}
	snaps := activitySnapshots(videos, prevs, map[string]bool{"failed": true, "failed-new": true}, "T")
	got := map[string]store.ActivitySnapshot{}
	for _, s := range snaps {
		got[s.VideoID] = s
	}
	if got["ok"].Comments != 4 {
		t.Errorf("ok comments = %d, want the new count 4", got["ok"].Comments)
	}
	if s := got["failed"]; s.Comments != 3 || s.Views != 9 {
		t.Errorf("failed video snapshot = %+v, want previous comment count 3 and new views 9", s)
	}
	if _, ok := got["failed-new"]; ok {
		t.Error("a failed first-sight video must not be recorded, so its comments are fetched next run")
	}
}

func TestActivityTotalsNullOnBaseline(t *testing.T) {
	rows := []activityVideo{{VideoID: "a", ViewsDelta: intp(2), NewComments: []activityComment{{ID: "c"}}}, {VideoID: "b", ViewsDelta: intp(1), NeedsReply: true}}
	if activityTotalsFor(rows, true) != nil {
		t.Fatal("baseline totals must be null")
	}
	tot := activityTotalsFor(rows, false)
	if tot == nil || tot.Videos != 2 || tot.ViewsDelta != 3 || tot.NewComments != 1 || tot.NeedsReply != 1 {
		t.Fatalf("totals = %+v", tot)
	}
	b, _ := json.Marshal(activityResult{Videos: []activityVideo{}})
	if !strings.Contains(string(b), `"totals":null`) || !strings.Contains(string(b), `"recorded":false`) {
		t.Fatalf("empty result JSON = %s", b)
	}
}
