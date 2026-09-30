package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newTasksEditCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "edit [task-id] [text]",
		Short: "Replace the text of an action item",
		Long: `Edit an action item's text (UpdateVideoTask). Find task ids with
'loom-pp-cli tasks list <video>'.

Viewers of the video see the action item, so editing it needs --yes;
--dry-run previews it without --yes.`,
		Example: strings.Trim(`
  loom-pp-cli tasks edit 7731 "Send the updated pricing deck" --dry-run
  loom-pp-cli tasks edit 7731 "File the login bug in Jira" --yes
  loom-pp-cli tasks edit 7731 "Follow up with design on Friday" --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "task=7731;text=Send the updated pricing deck"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runIDTextWrite(cmd, flags, args, "task", textWrite{textLabel: "text", verb: "edit task", op: loom.OpUpdateTask, confirm: confirmVisible}, func(id, text string) map[string]any {
				return map[string]any{"id": id, "content": text}
			})
		},
	}
}
