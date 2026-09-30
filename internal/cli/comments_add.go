package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newCommentsAddCmd(flags *rootFlags) *cobra.Command {
	var at string
	cmd := &cobra.Command{
		Use:   "add [video] [text]",
		Short: "Post a comment on a video, optionally at a timestamp",
		Long: `Post a comment on a video (CreateVideoComment).

--at pins the comment to a playback offset (1:23, 1:02:03 or seconds); it is
sent to Loom as integer milliseconds. Without --at the comment is at 0:00.

Viewers of the video see the comment, so posting needs --yes; --dry-run
previews it without --yes.`,
		Example: strings.Trim(`
  loom-pp-cli comments add latest "Great walkthrough, one question at 1:23" --at 1:23 --dry-run
  loom-pp-cli comments add 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 "Looks good to ship" --yes
  loom-pp-cli comments add https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 "See the error here" --at 0:42 --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest;text=Looks good to ship;--at=0:05"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runVideoTextWrite(cmd, flags, args, textWrite{textLabel: "text", verb: "comment on video", op: loom.OpCreateComment, confirm: confirmVisible, at: &at}, func(id, text string, ms int) map[string]any {
				return map[string]any{"videoId": id, "content": text, "timestamp": ms}
			})
		},
	}
	cmd.Flags().StringVar(&at, "at", "", "Playback offset for the comment (m:ss, h:mm:ss or seconds)")
	return cmd
}
