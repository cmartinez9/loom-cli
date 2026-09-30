package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
	"github.com/cmartinez9/loom-cli/internal/store"
)

// tagView is one tag on a video.
type tagView struct {
	VideoID string `json:"video_id"`
	Tag     string `json:"tag"`
}

func newTagsListCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "list [video]",
		Short: "List the tags attached to a video",
		Long: `List the tags attached to a video, one row per tag. --data-source local
reads the tags stored with the videos synced by 'sync'.`,
		Example: strings.Trim(`
  loom-pp-cli tags list latest
  loom-pp-cli tags list 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json
  loom-pp-cli tags list https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --data-source local`, "\n"),
		Annotations: map[string]string{
			"mcp:read-only":  "true",
			"pp:data-source": sourceAuto,
			"pp:happy-args":  "video=latest",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "tags list")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[[]string]{
				what: "tags",
				live: func(ctx context.Context, lc *loom.Client, id string) ([]string, error) {
					return lc.FetchTags(ctx, id)
				},
				local:     localTags,
				empty:     func(string) []string { return make([]string, 0) },
				emptyLive: func(tags []string) bool { return len(tags) == 0 },
			})
			if err != nil {
				return err
			}
			rows := make([]tagView, 0, len(res.Value))
			for _, t := range res.Value {
				rows = append(rows, tagView{VideoID: res.ID, Tag: t})
			}
			return emit(cmd, flags, rows, func(w io.Writer) error {
				if len(rows) == 0 {
					if !res.Missing {
						fmt.Fprintln(w, "No tags on this video.")
					}
					return nil
				}
				for _, r := range rows {
					fmt.Fprintln(w, r.Tag)
				}
				return nil
			})
		},
	}
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}

// localTags reads the tags array saved with a synced video.
func localTags(ctx context.Context, db *store.Store, id string) ([]string, error) {
	return db.SyncedVideoTags(ctx, id)
}
