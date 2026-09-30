package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newTasksDoneCmd(flags *rootFlags) *cobra.Command {
	var undo bool
	cmd := &cobra.Command{
		Use:   "done [task-id]",
		Short: "Mark an action item done, or reopen it with --undo",
		Long: `Mark an action item as done (RespondToVideoTask responded=true); --undo
sets responded=false.

Viewers of the video see the action item's state, so marking it done (or
reopening it) needs --yes; --dry-run previews it without --yes.`,
		Example: strings.Trim(`
  loom-pp-cli tasks done 7731 --dry-run
  loom-pp-cli tasks done 7731 --yes
  loom-pp-cli tasks done 7731 --undo --yes`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "task=7731"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			verb := "mark done task"
			if undo {
				verb = "reopen task"
			}
			return runIDWrite(cmd, flags, args, "task", verb, loom.OpRespondTask, confirmVisible, func(id string) map[string]any {
				return map[string]any{"id": id, "responded": !undo}
			})
		},
	}
	cmd.Flags().BoolVar(&undo, "undo", false, "Reopen the task (responded=false)")
	return cmd
}
