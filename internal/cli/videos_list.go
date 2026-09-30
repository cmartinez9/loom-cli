package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newVideosListCmd(flags *rootFlags) *cobra.Command {
	var (
		limit    int
		all      bool
		maxPages int
		folder   string
		archived bool
		window   timeWindow
		match    string
		dbPath   string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the videos in your Loom library, newest first",
		Long: `List the videos in your Loom library, newest first.

Filters: --folder (one folder), --archived (the archive instead of the
library), --since/--before (recording-time window, e.g. 7d or 2w) and
--match (case-insensitive regular expression on the title). The listing
stops paging once it passes the --since window.

--data-source live calls Loom, local reads the videos synced by
'loom-pp-cli sync', and auto (the default) calls Loom and falls back to the
local store only when the Loom session has expired.`,
		Example: strings.Trim(`
  loom-pp-cli videos list
  loom-pp-cli videos list --since 7d --json
  loom-pp-cli videos list --match "standup|demo" --limit 10
  loom-pp-cli videos list --all --json --select id,name,created_at
  loom-pp-cli videos list --folder 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60
  loom-pp-cli videos list --data-source local`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "videos list")
			}
			if len(args) > 0 {
				return usageErr(fmt.Errorf("videos list takes no positional arguments (got %q); use --match to filter by title", strings.Join(args, " ")))
			}
			if limit < 0 {
				return usageErr(fmt.Errorf("--limit must be 0 or more"))
			}
			filter, err := buildSelectFilter(time.Now(), window, folder, match)
			if err != nil {
				return err
			}
			maxPages, err := effectiveMaxPages(maxPages)
			if err != nil {
				return err
			}
			rowLimit := limit
			if all {
				rowLimit = 0
			}
			db := resolveDBPath(dbPath)
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()

			local := func() error {
				if done, err := missingLocalMirror(cmd, flags, db, "videos", make([]loom.Video, 0)); done {
					return err
				}
				vs, err := readSyncedVideos(ctx, db)
				if err != nil {
					return err
				}
				rows := make([]loom.Video, 0)
				for _, v := range vs {
					if v.Archived != archived {
						continue
					}
					if filter.Keep(v) {
						rows = append(rows, v)
						if rowLimit > 0 && len(rows) >= rowLimit {
							break
						}
					}
				}
				return printVideos(cmd, flags, rows)
			}
			live := func() error {
				lc, err := newLoomClient(flags)
				if err != nil {
					return err
				}
				source := loom.SourceMine
				if archived {
					source = loom.SourceArchived
				}
				res, err := scanLibrary(ctx, lc, scanOpts{Source: source, Filter: filter, Limit: rowLimit, MaxPages: maxPages})
				if err != nil {
					return err
				}
				if res.Capped {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", capNote(maxPages, "videos"))
				}
				return printVideos(cmd, flags, res.Videos)
			}
			return withDataSource(cmd, flags, db, "videos", live, local)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum videos to return")
	cmd.Flags().BoolVar(&all, "all", false, "Return every matching video (pages through the whole library, up to --max-pages)")
	registerMaxPages(cmd, &maxPages, "videos", false)
	cmd.Flags().StringVar(&folder, "folder", "", "Only videos in this folder (32-hex id or folder URL)")
	cmd.Flags().BoolVar(&archived, "archived", false, "List archived videos instead of the active library")
	registerWindowFlags(cmd, &window, "before", "Only videos recorded within this window (e.g. 7d, 36h, 2w)", "Only videos recorded before this long ago (e.g. 30d)")
	cmd.Flags().StringVar(&match, "match", "", "Case-insensitive regular expression matched against the title")
	cmd.Flags().StringVar(&dbPath, "db", "", "Local store path for --data-source local (default: the CLI's data directory)")
	return cmd
}

// printVideos renders videos as JSON (machine output) or a table.
func printVideos(cmd *cobra.Command, flags *rootFlags, rows []loom.Video) error {
	return emit(cmd, flags, rows, func(w io.Writer) error {
		if len(rows) == 0 {
			fmt.Fprintln(w, "No matching videos.")
			return nil
		}
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "NAME\tCREATED\tDURATION\tVIEWS\tCOMMENTS\tFOLDER\tID")
		for _, v := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n", truncate(v.Name, 50), formatDate(v.CreatedAt), v.Duration, v.Views, v.Comments, truncate(v.FolderName, 24), v.ID)
		}
		return tw.Flush()
	})
}
