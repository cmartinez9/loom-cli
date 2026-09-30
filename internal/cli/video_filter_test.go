package cli

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func TestBuildSelectFilterErrors(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name                         string
		since, before, folder, match string
		wantErr                      bool
	}{
		{name: "empty", wantErr: false},
		{name: "since days", since: "7d"},
		{name: "before weeks", before: "4w"},
		{name: "bad since", since: "soon", wantErr: true},
		{name: "negative since", since: "-5d", wantErr: true},
		{name: "bad before", before: "x", wantErr: true},
		{name: "empty window", since: "7d", before: "30d", wantErr: true},
		{name: "valid window", since: "30d", before: "7d"},
		{name: "bad regex", match: "(", wantErr: true},
		{name: "good regex", match: "(?i)demo"},
		{name: "bad folder", folder: "not a folder", wantErr: true},
		{name: "good folder", folder: "1111aaaa2222bbbb3333cccc4444dddd"},
		{name: "folder url", folder: "https://www.loom.com/looms/folders/team-1111aaaa2222bbbb3333cccc4444dddd"},
		{name: "opaque folder id", folder: "abc_def"},
		{name: "overlong folder", folder: "x" + strings.Repeat("a", 128), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := testFilter(now, tc.since, tc.before, tc.folder, tc.match)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestSelectFilterKeep(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	vid := func(name, created, folder string) loom.Video {
		return loom.Video{ID: "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60", Name: name, CreatedAt: created, FolderID: folder}
	}
	const fA, fB = "1111aaaa2222bbbb3333cccc4444dddd", "5555eeee6666ffff7777aaaa8888bbbb"
	recent := "2026-09-27T10:00:00.000Z" // 2 days old
	old := "2026-05-01T10:00:00.000Z"    // ~5 months old
	tests := []struct {
		name                         string
		since, before, folder, match string
		v                            loom.Video
		want                         bool
	}{
		{name: "no filters keeps all", v: vid("Demo", old, ""), want: true},
		{name: "since keeps recent", since: "7d", v: vid("Demo", recent, ""), want: true},
		{name: "since drops old", since: "7d", v: vid("Demo", old, ""), want: false},
		{name: "before keeps old", before: "90d", v: vid("Demo", old, ""), want: true},
		{name: "before drops recent", before: "90d", v: vid("Demo", recent, ""), want: false},
		{name: "unparseable time fails time filter", since: "7d", v: vid("Demo", "yesterday", ""), want: false},
		{name: "unparseable time ok without time filter", v: vid("Demo", "yesterday", ""), want: true},
		{name: "match hit", match: "^Demo", v: vid("Demo day", recent, ""), want: true},
		{name: "match miss", match: "zzzzqqq", v: vid("Demo day", recent, ""), want: false},
		{name: "match is case-insensitive", match: "demo", v: vid("Demo day", recent, ""), want: true},
		{name: "match case-insensitive flag", match: "(?i)demo", v: vid("Demo day", recent, ""), want: true},
		{name: "folder mismatch", folder: fA, v: vid("Demo", recent, fB), want: false},
		{name: "folder match", folder: fA, v: vid("Demo", recent, fA), want: true},
		{name: "folder unknown on video trusts server filter", folder: fA, v: vid("Demo", recent, ""), want: true},
		{name: "all filters", since: "30d", match: "Demo", folder: fA, v: vid("Demo", recent, fA), want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := testFilter(now, tc.since, tc.before, tc.folder, tc.match)
			if err != nil {
				t.Fatal(err)
			}
			if got := f.Keep(tc.v); got != tc.want {
				t.Fatalf("Keep = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilterPageStopsPastSinceWindow(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	f, err := testFilter(now, "7d", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	page := []loom.Video{
		{ID: "a", Name: "one", CreatedAt: "2026-09-28T10:00:00Z"},
		{ID: "b", Name: "two", CreatedAt: "2026-09-25T10:00:00Z"},
		{ID: "c", Name: "three", CreatedAt: "2026-09-01T10:00:00Z"},
		{ID: "d", Name: "four", CreatedAt: "2026-09-26T10:00:00Z"}, // out of order: never reached
	}
	kept, done := filterPage(f, page, 0, nil)
	if !done {
		t.Fatal("expected scan to stop at the first video older than --since")
	}
	if len(kept) != 2 || kept[0].ID != "a" || kept[1].ID != "b" {
		t.Fatalf("kept = %+v", kept)
	}

	none, _ := testFilter(now, "", "", "", "zzzzqqq")
	kept, done = filterPage(none, page, 0, nil)
	if done || len(kept) != 0 || kept == nil {
		t.Fatalf("no-match filter: kept=%v (nil=%v) done=%v", kept, kept == nil, done)
	}
}

func TestPlanRenames(t *testing.T) {
	targets := []selectedVideo{
		{ID: "a", Name: "Untitled recording"},
		{ID: "b", Name: "Standup 2026-09-01"},
		{ID: "c", Name: "Untitled"},
		{ID: "d", Name: "Demo"},
	}
	tests := []struct {
		name, match, tmpl string
		want              map[string]string
	}{
		{name: "prefix replace", match: "^Untitled", tmpl: "Draft", want: map[string]string{"a": "Draft recording", "c": "Draft"}},
		{name: "capture group", match: `^Standup (\d{4})-(\d{2})-(\d{2})$`, tmpl: "Standup $2/$3/$1", want: map[string]string{"b": "Standup 09/01/2026"}},
		{name: "unchanged dropped", match: "^Demo$", tmpl: "Demo", want: map[string]string{}},
		{name: "empty result dropped", match: "^Untitled$", tmpl: "", want: map[string]string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := planRenames(targets, regexp.MustCompile(tc.match), tc.tmpl)
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v, want %v", got, tc.want)
			}
			for _, g := range got {
				if tc.want[g.ID] != g.NewName {
					t.Fatalf("%s: new name %q, want %q", g.ID, g.NewName, tc.want[g.ID])
				}
			}
		})
	}
}

func TestNewBulkPlanEmptyTargetsIsArray(t *testing.T) {
	flags := &rootFlags{dryRun: true}
	p := newBulkPlan(flags, "archive videos", loom.OpArchive, map[string]any{"videoIds": []string{}}, selection{Filtered: true, Scanned: 5, MaxPages: 10}, nil)
	if p.Targets == nil || p.Count != 0 || p.Note == "" {
		t.Fatalf("plan = %+v", p)
	}
}

func TestScanLibraryCapAndUndated(t *testing.T) {
	now := time.Now().UTC()
	page := `{"data":{"getLooms":{"__typename":"GetLoomsForLibraryPayload","videos":{"edges":[
		{"node":{"__typename":"RegularUserVideo","id":"8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60","name":"Weekly Demo","createdAt":"` + now.Add(-time.Hour).Format(time.RFC3339) + `"}},
		{"node":{"__typename":"RegularUserVideo","id":"8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f61","name":"Undated","createdAt":"not a date"}}],
		"pageInfo":{"endCursor":"next","hasNextPage":true}}}}}`
	lc := fakeLoomClient(t, func(op string) string { return page })
	f, err := testFilter(now, "7d", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := scanLibrary(context.Background(), lc, scanOpts{Filter: f, MaxPages: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Capped || res.Pages != 2 || res.Scanned != 4 {
		t.Fatalf("scan = %+v, want capped after 2 pages", res)
	}
	for _, v := range res.Videos {
		if v.Name == "Undated" {
			t.Fatal("an undated video must not pass a --since window")
		}
	}
	if len(res.Videos) != 2 {
		t.Fatalf("kept %d videos, want 2 (one per page)", len(res.Videos))
	}
}

func testVideo(id, name, created string) loom.Video {
	return loom.Video{ID: id, Name: name, CreatedAt: created, FolderID: "f1"}
}

func TestSelectFilterAndPage(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	vs := []loom.Video{
		testVideo("a", "Standup notes", "2026-06-29T10:00:00Z"),
		testVideo("b", "Demo day", "2026-06-20T10:00:00Z"),
		testVideo("c", "Old standup", "2026-01-01T10:00:00Z"),
		testVideo("d", "Undated standup", "not a date"),
	}
	cases := []struct {
		name          string
		since, before string
		match         string
		limit         int
		want          string
		wantDone      bool
	}{
		{"no filter", "", "", "", 0, "a,b,c,d", false},
		{"limit", "", "", "", 2, "a,b", true},
		{"since stops paging", "7d", "", "", 0, "a", true},
		{"before excludes undated", "", "5d", "", 0, "b,c", false},
		{"window", "30d", "5d", "", 0, "b", true},
		{"match case-insensitive", "", "", "STANDUP", 0, "a,c,d", false},
		{"no match", "", "", "zzzzqqq", 0, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := testFilter(now, tc.since, tc.before, "", tc.match)
			if err != nil {
				t.Fatal(err)
			}
			kept, done := filterPage(f, vs, tc.limit, nil)
			ids := make([]string, 0)
			for _, v := range kept {
				ids = append(ids, v.ID)
			}
			if got := strings.Join(ids, ","); got != tc.want || done != tc.wantDone {
				t.Fatalf("got %q done=%v, want %q done=%v", got, done, tc.want, tc.wantDone)
			}
		})
	}
}

func TestSelectFilterUsageErrors(t *testing.T) {
	now := time.Now()
	for _, tc := range [][3]string{{"xyz", "", ""}, {"", "-3d", ""}, {"1d", "7d", ""}, {"", "", "("}} {
		_, err := testFilter(now, tc[0], tc[1], "", tc[2])
		var ce *cliError
		if !errors.As(err, &ce) || ce.code != 2 {
			t.Fatalf("%v: err = %v, want usage error", tc, err)
		}
	}
}

// testFilter builds a filter from raw --since/--before values.
func testFilter(now time.Time, since, before, folder, match string) (selectFilter, error) {
	return buildSelectFilter(now, timeWindow{Since: since, Before: before}, folder, match)
}

func TestTimeWindowZeroHandling(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	b, err := timeWindow{}.bounds(now)
	if err != nil || !b.Start.IsZero() || !b.End.IsZero() {
		t.Fatalf("unset window = %+v, %v, want unbounded", b, err)
	}
	for _, w := range []timeWindow{{Since: "0d"}, {Before: "0"}, {Since: "-1d"}, {Since: "soon"}} {
		var ce *cliError
		if _, err := w.bounds(now); !errors.As(err, &ce) || ce.code != 2 {
			t.Errorf("%+v: err = %v, want a usage error", w, err)
		}
	}
	_, err = timeWindow{Since: "7d", Before: "30d", beforeFlag: "until"}.bounds(now)
	if err == nil || !strings.Contains(err.Error(), "--until 30d") {
		t.Fatalf("empty window err = %v, want it to name --until", err)
	}
	b, err = timeWindow{Since: "30d", Before: "7d"}.bounds(now)
	if err != nil || !b.Start.Equal(now.Add(-30*24*time.Hour)) || !b.End.Equal(now.Add(-7*24*time.Hour)) {
		t.Fatalf("window = %+v, %v", b, err)
	}
}
