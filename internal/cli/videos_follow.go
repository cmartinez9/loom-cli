package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newVideosFollowCmd(flags *rootFlags) *cobra.Command {
	var off bool
	cmd := &cobra.Command{
		Use:   "follow [video]",
		Short: "Follow a video to get notified about new comments",
		Long: `Follow a video so Loom notifies you about its activity
(ToggleFollowingVideo). Use --off to stop following.`,
		Example: strings.Trim(`
  loom-pp-cli videos follow latest --dry-run
  loom-pp-cli videos follow 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60
  loom-pp-cli videos follow 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --off`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			verb := "follow"
			if off {
				verb = "unfollow"
			}
			return runSingleVideoWrite(cmd, flags, args, verb, loom.OpToggleFollow, noConfirm, func(id string) map[string]any {
				return map[string]any{"videoId": id, "follow": !off}
			})
		},
	}
	cmd.Flags().BoolVar(&off, "off", false, "Stop following the video")
	return cmd
}
