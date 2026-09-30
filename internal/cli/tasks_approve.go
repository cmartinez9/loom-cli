package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newTasksApproveCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "approve [task-id]",
		Short: "Approve an AI-suggested action item so it stays on the video",
		Long: `Approve a suggested action item (ApproveVideoTask).

Viewers of the video see approved action items, so approving needs --yes;
--dry-run previews it without --yes.`,
		Example: strings.Trim(`
  loom-pp-cli tasks approve 7731 --dry-run
  loom-pp-cli tasks approve 7731 --yes
  loom-pp-cli tasks approve 7731 --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "task=7731"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runIDWrite(cmd, flags, args, "task", "approve task", loom.OpApproveTask, confirmVisible, func(id string) map[string]any {
				return map[string]any{"id": id}
			})
		},
	}
}
