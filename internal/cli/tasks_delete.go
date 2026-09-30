package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newTasksDeleteCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete [task-id]",
		Short: "Delete an action item from a video (requires --yes)",
		Long:  "Delete an action item (DeleteVideoTask). Requires --yes; --dry-run previews it.",
		Example: strings.Trim(`
  loom-pp-cli tasks delete 7731 --dry-run
  loom-pp-cli tasks delete 7731 --yes
  loom-pp-cli tasks delete 7731 --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "task=7731"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runIDWrite(cmd, flags, args, "task", "delete task", loom.OpDeleteTask, confirmLibrary, func(id string) map[string]any {
				return map[string]any{"id": id}
			})
		},
	}
}
