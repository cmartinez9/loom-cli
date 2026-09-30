package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newTasksAddCmd(flags *rootFlags) *cobra.Command {
	var at string
	cmd := &cobra.Command{
		Use:   "add [video] [text]",
		Short: "Add an action item to a video, optionally at a timestamp",
		Long: `Add an action item to a video (CreateVideoTask). --at pins it to a playback
offset (m:ss, h:mm:ss or seconds), sent as integer milliseconds.

Viewers of the video see the action item, so adding it needs --yes;
--dry-run previews it without --yes.`,
		Example: strings.Trim(`
  loom-pp-cli tasks add latest "Send the pricing deck to the customer" --at 2:10 --dry-run
  loom-pp-cli tasks add 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 "File a bug for the login error" --yes
  loom-pp-cli tasks add latest "Follow up with design" --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest;text=Follow up with design;--at=0:05"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runVideoTextWrite(cmd, flags, args, textWrite{textLabel: "text", verb: "add task to video", op: loom.OpCreateTask, confirm: confirmVisible, at: &at}, func(id, text string, ms int) map[string]any {
				return map[string]any{"videoId": id, "content": text, "timestamp": ms}
			})
		},
	}
	cmd.Flags().StringVar(&at, "at", "", "Playback offset for the task (m:ss, h:mm:ss or seconds)")
	return cmd
}
