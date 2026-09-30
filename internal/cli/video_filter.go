package cli

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/cliutil"
	"github.com/cmartinez9/loom-cli/internal/loom"
)

// The one video filter (selectFilter) and the one newest-first library scan
// (scanLibrary, bounded by --max-pages) that videos list, activity, the bulk
// writes, videos export and download all use.

const selectPageSize = 50

// defaultMaxPages is the one default for --max-pages (50 items per page).
const defaultMaxPages = 10

// registerMaxPages declares --max-pages with the shared default; noun names
// what a page holds ("videos", "folders"). withAlias also accepts the older
// --max-scan-pages spelling as a hidden alias.
func registerMaxPages(cmd *cobra.Command, p *int, noun string, withAlias bool) {
	cmd.Flags().IntVar(p, "max-pages", defaultMaxPages, "Maximum pages ("+fmt.Sprint(selectPageSize)+" "+noun+" each) to read, newest first")
	if withAlias {
		cmd.Flags().IntVar(p, "max-scan-pages", defaultMaxPages, "Alias of --max-pages")
		_ = cmd.Flags().MarkHidden("max-scan-pages")
	}
}

// effectiveMaxPages validates --max-pages and applies the dogfood cap (1).
func effectiveMaxPages(n int) (int, error) {
	if n < 1 {
		return 0, usageErr(fmt.Errorf("--max-pages must be at least 1 (got %d)", n))
	}
	if cliutil.IsDogfoodEnv() && n > 1 {
		n = 1
	}
	return n, nil
}

// capNote is the one wording for a listing that stopped at --max-pages.
func capNote(maxPages int, noun string) string {
	return fmt.Sprintf("scan stopped at --max-pages %d; older %s were not considered, raise --max-pages to scan further", maxPages, noun)
}

// selectFilter is the compiled, pure form of the selection flags.
type selectFilter struct {
	// Window keeps videos created at or after Window.Start and before
	// Window.End (a zero bound is open).
	Window   windowBounds
	FolderID string
	Match    *regexp.Regexp // case-insensitive
}

// buildSelectFilter compiles selection flags relative to now. Bad windows,
// regexes or folder ids are usage errors. --match is always
// case-insensitive; --folder accepts a 32-hex id or a folder URL.
func buildSelectFilter(now time.Time, w timeWindow, folder, match string) (selectFilter, error) {
	var f selectFilter
	b, err := w.bounds(now)
	if err != nil {
		return f, err
	}
	f.Window = b
	if folder != "" {
		id, err := folderIDArg(folder)
		if err != nil {
			return f, err
		}
		f.FolderID = id
	}
	if match != "" {
		re, err := regexp.Compile("(?i)" + match)
		if err != nil {
			return f, usageErr(fmt.Errorf("--match %q is not a valid regular expression: %v", match, err))
		}
		f.Match = re
	}
	return f, nil
}

// active reports whether the filter narrows the library at all.
func (f selectFilter) active() bool {
	return f.Window.bounded() || f.FolderID != "" || f.Match != nil
}

// Keep reports whether a video passes every filter. Videos whose creation
// time cannot be parsed never pass a time filter.
func (f selectFilter) Keep(v loom.Video) bool {
	if f.Window.bounded() {
		t, ok := loom.ParseTime(v.CreatedAt)
		if !ok || !f.Window.contains(t) {
			return false
		}
	}
	if f.FolderID != "" && v.FolderID != "" && v.FolderID != f.FolderID {
		return false
	}
	if f.Match != nil && !f.Match.MatchString(v.Name) {
		return false
	}
	return true
}

// PastWindow reports whether a newest-first scan can stop: the video is
// older than --since, so every later video is too.
func (f selectFilter) PastWindow(v loom.Video) bool {
	if f.Window.Start.IsZero() {
		return false
	}
	t, ok := loom.ParseTime(v.CreatedAt)
	return ok && t.Before(f.Window.Start)
}

// filterPage applies a filter to one newest-first page, appending matches to
// kept until limit (0 = no limit). done reports that the scan can stop: the
// --since window was passed or the limit was reached.
func filterPage(f selectFilter, videos []loom.Video, limit int, kept []loom.Video) ([]loom.Video, bool) {
	if kept == nil {
		kept = make([]loom.Video, 0)
	}
	for _, v := range videos {
		if f.PastWindow(v) {
			return kept, true
		}
		if !f.Keep(v) {
			continue
		}
		kept = append(kept, v)
		if limit > 0 && len(kept) >= limit {
			return kept, true
		}
	}
	return kept, false
}

// scanOpts controls one library scan.
type scanOpts struct {
	Source   string // loom.SourceMine (default) or loom.SourceArchived
	Filter   selectFilter
	Limit    int // stop after this many matches; 0 = no limit
	MaxPages int // pages of 50 fetched at most
}

// scanResult is a library scan's outcome.
type scanResult struct {
	Videos  []loom.Video
	Pages   int
	Scanned int
	Capped  bool // stopped by MaxPages while more pages existed
}

// scanLibrary walks the library newest first, keeping filter matches and
// stopping at the limit, the --since boundary, the last page or MaxPages.
// Errors are returned unmapped (callers use loomErr).
func scanLibrary(ctx context.Context, lc *loom.Client, o scanOpts) (*scanResult, error) {
	res := &scanResult{Videos: make([]loom.Video, 0)}
	maxPages := o.MaxPages
	if maxPages <= 0 {
		maxPages = defaultMaxPages
	}
	pageSize := selectPageSize
	if o.Limit > 0 && o.Limit < pageSize && !o.Filter.active() {
		pageSize = o.Limit
	}
	cursor := ""
	for page := 0; page < maxPages; page++ {
		p, err := lc.ListVideos(ctx, loom.ListOptions{Limit: pageSize, Cursor: cursor, FolderID: o.Filter.FolderID, Source: o.Source})
		if err != nil {
			return nil, err
		}
		res.Pages++
		res.Scanned += len(p.Videos)
		var done bool
		res.Videos, done = filterPage(o.Filter, p.Videos, o.Limit, res.Videos)
		if done || !p.HasNextPage || p.EndCursor == "" || len(p.Videos) == 0 {
			return res, nil
		}
		cursor = p.EndCursor
	}
	res.Capped = true
	return res, nil
}
