package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newReactionsAddCmd(flags *rootFlags) *cobra.Command {
	var at string
	cmd := &cobra.Command{
		Use:   "add [video] [emoji]",
		Short: "React to a video with an emoji at a playback offset",
		Long: `Add an emoji reaction to a video (AddVideoReaction). --at sets the playback
offset (m:ss, h:mm:ss or seconds), sent as integer milliseconds; default 0:00.

Viewers of the video see the reaction, so adding it needs --yes; --dry-run
previews it without --yes.`,
		Example: strings.Trim(`
  loom-pp-cli reactions add latest "👍" --at 0:30 --dry-run
  loom-pp-cli reactions add 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 "🎉" --at 1:05 --yes
  loom-pp-cli reactions add latest "🔥" --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest;emoji=👍;--at=0:05"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runVideoTextWrite(cmd, flags, args, textWrite{textLabel: "emoji", verb: "react to video", op: loom.OpAddReaction, confirm: confirmVisible, at: &at}, func(id, emoji string, ms int) map[string]any {
				return map[string]any{"videoId": id, "time": ms, "type": emoji}
			})
		},
	}
	cmd.Flags().StringVar(&at, "at", "", "Playback offset for the reaction (m:ss, h:mm:ss or seconds)")
	return cmd
}
