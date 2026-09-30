package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newPlaylistsGetCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <playlist-id>",
		Short: "Show one playlist with its videos, views and collaborators",
		Long: `Show one playlist: name, description, owner, total views and duration,
collaborator count, and its videos in playlist order (with whether you have
watched each).

<playlist-id> comes from 'playlists list'. 'watch-later' shows your Watch
Later list's video count.`,
		Example: strings.Trim(`
  loom-pp-cli playlists get watch-later
  loom-pp-cli playlists get 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "playlist=watch-later"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "playlists get")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if len(args) != 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("playlists get takes exactly one <playlist-id>, got %d", len(args)))
			}
			id, err := playlistIDArg(args[0])
			if err != nil {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			if strings.EqualFold(id, loom.WatchLaterID) {
				count, err := lc.WatchLaterCount(ctx)
				if err != nil {
					return loomErr(err)
				}
				row := watchLaterRow(count)
				return emit(cmd, flags, row, func(w io.Writer) error {
					fmt.Fprintf(w, "Watch Later: %s\n", countNoun(count, "video"))
					return nil
				})
			}
			p, err := lc.PlaylistDetails(ctx, id)
			if err != nil {
				return loomErr(err)
			}
			return emit(cmd, flags, p, func(w io.Writer) error {
				fmt.Fprintf(w, "Name:    %s\nID:      %s\n", p.Name, p.ID)
				if p.Owner != "" {
					fmt.Fprintf(w, "Owner:   %s\n", p.Owner)
				}
				fmt.Fprintf(w, "Videos:  %d\nViews:   %d\n", p.VideoCount, p.TotalViews)
				if p.Description != "" {
					fmt.Fprintf(w, "\n%s\n", p.Description)
				}
				if len(p.Videos) > 0 {
					fmt.Fprintln(w)
					tw := newTabWriter(w)
					for _, v := range p.Videos {
						fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", truncate(v.Name, 50), v.Duration, map[bool]string{true: "watched", false: ""}[v.Watched], v.ID)
					}
					return tw.Flush()
				}
				return nil
			})
		},
	}
	return cmd
}
