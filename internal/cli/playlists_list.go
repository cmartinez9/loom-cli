package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newPlaylistsListCmd(flags *rootFlags) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the playlists you can see plus your Watch Later list",
		Long: `List the playlists you can see, most recently updated first, followed by
your Watch Later list as its own row (id 'watch-later', watch_later: true)
with its video count.`,
		Example: strings.Trim(`
  loom-pp-cli playlists list
  loom-pp-cli playlists list --json --select id,name,video_count`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "playlists list")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if len(args) > 0 {
				return usageErr(fmt.Errorf("playlists list takes no positional arguments; use 'playlists get <playlist-id>' for one playlist"))
			}
			if limit < 1 || limit > 100 {
				return usageErr(fmt.Errorf("--limit must be between 1 and 100"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			rows, err := lc.AvailablePlaylists(ctx, limit)
			if err != nil {
				return loomErr(err)
			}
			count, err := lc.WatchLaterCount(ctx)
			if err != nil {
				return loomErr(err)
			}
			rows = append(rows, watchLaterRow(count))
			return emit(cmd, flags, rows, func(w io.Writer) error {
				tw := newTabWriter(w)
				fmt.Fprintln(tw, "NAME\tVIDEOS\tPRIVACY\tUPDATED\tID")
				for _, p := range rows {
					fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n", truncate(p.Name, 40), p.VideoCount, p.Privacy, formatDate(p.UpdatedAt), p.ID)
				}
				return tw.Flush()
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum playlists to return (1-100), not counting Watch Later")
	return cmd
}

func watchLaterRow(count int) loom.Playlist {
	return loom.Playlist{ID: loom.WatchLaterID, Name: "Watch Later", VideoCount: count, Privacy: "private", WatchLater: true}
}
