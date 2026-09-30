package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
	"loom-pp-cli/internal/store"
)

// descriptionView is the machine output of `description`.
type descriptionView struct {
	VideoID     string `json:"video_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newDescriptionCmd(flags))
	})
}

func newDescriptionCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "description [video]",
		Short: "Print a video's description (Loom's AI-written one on AI plans)",
		Long: `Print a video's description field. On Loom plans with AI features this is
the AI-written description unless someone edited it. JSON output is
{video_id, name, description}. --data-source local reads the description
from the videos synced by 'sync'.`,
		Example: strings.Trim(`
  loom-pp-cli description latest
  loom-pp-cli description 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json
  loom-pp-cli description https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --data-source local`, "\n"),
		Annotations: videoReadAnnotations(sourceAuto),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "description")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[descriptionView]{
				what: "description",
				live: func(ctx context.Context, lc *loom.Client, id string) (descriptionView, error) {
					v, err := lc.GetVideo(ctx, id)
					if err != nil {
						return descriptionView{}, err
					}
					return descriptionView{VideoID: v.ID, Name: v.Name, Description: v.Description}, nil
				},
				local: func(ctx context.Context, db *store.Store, id string) (descriptionView, error) {
					view := descriptionView{VideoID: id}
					var err error
					view.Name, view.Description, err = db.SyncedVideoText(ctx, id)
					return view, err
				},
				empty: func(id string) descriptionView { return descriptionView{VideoID: id} },
			})
			if err != nil {
				return err
			}
			view := res.Value
			if view.VideoID == "" {
				view.VideoID = res.ID
			}
			return emit(cmd, flags, view, func(w io.Writer) error {
				if res.Missing {
					return nil
				}
				if strings.TrimSpace(view.Description) == "" {
					fmt.Fprintln(w, "This video has no description.")
					return nil
				}
				fmt.Fprintln(w, strings.TrimSpace(view.Description))
				return nil
			})
		},
	}
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}
