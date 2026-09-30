package cli

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newVideosWatchLaterCmd(flags *rootFlags) *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   "watch-later [video]",
		Short: "Add a video to your Watch Later list, or remove it",
		Long: `Add a video to your Watch Later playlist (AddVideoToWatchLaterList, sending
your local UTC offset in minutes as minutesFromUTC). Use --remove to take it
off the list (RemoveVideoFromWatchLaterList).`,
		Example: strings.Trim(`
  loom-pp-cli videos watch-later https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --dry-run
  loom-pp-cli videos watch-later 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60
  loom-pp-cli videos watch-later 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --remove`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if remove {
				return runSingleVideoWrite(cmd, flags, args, "remove from Watch Later", loom.OpRemoveWatchLater, noConfirm, func(id string) map[string]any {
					return map[string]any{"videoId": id}
				})
			}
			return runSingleVideoWrite(cmd, flags, args, "add to Watch Later", loom.OpAddWatchLater, noConfirm, func(id string) map[string]any {
				return map[string]any{"videoId": id, "minutesFromUTC": utcOffsetMinutes(time.Now())}
			})
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove the video from Watch Later instead of adding it")
	return cmd
}
