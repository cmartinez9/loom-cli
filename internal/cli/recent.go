package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newRecentCmd(flags))
	})
}

// recentView is the recent-activity rollup. Every list is non-nil.
type recentView struct {
	Since    string             `json:"since"`
	Until    string             `json:"until"`
	Recorded []loom.RecentVideo `json:"recorded"`
	Watched  []loom.Activity    `json:"watched"`
	Meetings []loom.Meeting     `json:"meetings"`
}

var recentKinds = map[string]bool{"all": true, "recorded": true, "watched": true, "meetings": true}

func newRecentCmd(flags *rootFlags) *cobra.Command {
	var (
		kind   string
		since  string
		ahead  string
		limit  int
		offset int
	)
	cmd := &cobra.Command{
		Use:   "recent",
		Short: "Show what you recently recorded and watched, and your upcoming meetings",
		Long: `Show your recent Loom activity in one view:

  recorded  videos you recorded in the --since window (default 30d),
            with Loom's AI summary
  watched   your video activity history (views, reactions, comments)
            within the --since window
  meetings  upcoming meetings on connected calendars in the next --ahead
            (default 14d)

--kind picks one list; the others are returned empty.`,
		Example: strings.Trim(`
  loom-pp-cli recent
  loom-pp-cli recent --kind recorded --since 90d --json
  loom-pp-cli recent --kind meetings --ahead 7d
  loom-pp-cli recent --kind watched --limit 10 --json --select watched`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "recent")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if len(args) > 0 {
				return usageErr(fmt.Errorf("recent takes no positional arguments; use --kind recorded|watched|meetings|all"))
			}
			kind = strings.ToLower(strings.TrimSpace(kind))
			if !recentKinds[kind] {
				return usageErr(fmt.Errorf("--kind %q must be recorded, watched, meetings or all", kind))
			}
			if strings.TrimSpace(since) == "" {
				return usageErr(fmt.Errorf("--since must be a positive window such as 30d"))
			}
			now := time.Now().UTC()
			bounds, err := timeWindow{Since: since}.bounds(now)
			if err != nil {
				return err
			}
			fwd, err := parseWindow("ahead", ahead)
			if err != nil {
				return err
			}
			if fwd == 0 {
				return usageErr(fmt.Errorf("--ahead must be a positive window such as 14d"))
			}
			if limit < 1 || limit > 100 {
				return usageErr(fmt.Errorf("--limit must be between 1 and 100"))
			}
			if offset < 0 {
				return usageErr(fmt.Errorf("--offset must be 0 or more"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			start := bounds.Start
			view := recentView{
				Since:    start.Format(time.RFC3339),
				Until:    now.Format(time.RFC3339),
				Recorded: make([]loom.RecentVideo, 0),
				Watched:  make([]loom.Activity, 0),
				Meetings: make([]loom.Meeting, 0),
			}
			want := func(k string) bool { return kind == "all" || kind == k }
			if want("recorded") {
				rows, err := lc.RecentUserVideos(ctx, view.Since, view.Until, offset, limit)
				if err != nil {
					return loomErr(err)
				}
				view.Recorded = rows
			}
			if want("watched") {
				rows, err := lc.ActivityFeed(ctx, limit, offset)
				if err != nil {
					return loomErr(err)
				}
				view.Watched = watchedSince(rows, start)
			}
			if want("meetings") {
				rows, err := lc.CalendarMeetings(ctx, now.Format(time.RFC3339), now.Add(fwd).Format(time.RFC3339))
				if err != nil {
					return loomErr(err)
				}
				if len(rows) > limit {
					rows = rows[:limit]
				}
				view.Meetings = rows
			}
			return emit(cmd, flags, view, func(w io.Writer) error {
				printRecent(w, view, want)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "all", "Which activity to show: recorded, watched, meetings or all")
	cmd.Flags().StringVar(&since, "since", "30d", "How far back to look for recorded and watched videos (e.g. 7d, 90d)")
	cmd.Flags().StringVar(&ahead, "ahead", "14d", "How far ahead to look for upcoming meetings (e.g. 7d)")
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum rows per list (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Rows to skip in the recorded and watched lists (for paging)")
	return cmd
}

// watchedSince keeps activity at or after start (unparseable times kept).
func watchedSince(rows []loom.Activity, start time.Time) []loom.Activity {
	out := make([]loom.Activity, 0, len(rows))
	for _, a := range rows {
		if t, ok := loom.ParseTime(a.Time); ok && t.Before(start) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func printRecent(w io.Writer, v recentView, want func(string) bool) {
	if want("recorded") {
		fmt.Fprintf(w, "Recorded (%d)\n", len(v.Recorded))
		tw := newTabWriter(w)
		for _, r := range v.Recorded {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", truncate(r.Name, 50), formatDate(r.CreatedAt), r.Duration, r.ID)
		}
		_ = tw.Flush()
	}
	if want("watched") {
		fmt.Fprintf(w, "Watched (%d)\n", len(v.Watched))
		tw := newTabWriter(w)
		for _, a := range v.Watched {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", formatDate(a.Time), strings.ToLower(a.Type), truncate(a.VideoName, 50), a.VideoID)
		}
		_ = tw.Flush()
	}
	if want("meetings") {
		fmt.Fprintf(w, "Upcoming meetings (%d)\n", len(v.Meetings))
		for _, m := range v.Meetings {
			fmt.Fprintf(w, "  %s  %s (%d min)\n", m.StartsAt, m.Title, m.DurationMinutes)
		}
	}
}
