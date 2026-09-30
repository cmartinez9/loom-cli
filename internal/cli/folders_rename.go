package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newFoldersRenameCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "rename [folder-id] [name]",
		Short: "Rename a folder in your Loom library",
		Long:  "Rename a folder (RenameFolder). Find folder ids with 'loom-pp-cli folders list'.",
		Example: strings.Trim(`
  loom-pp-cli folders rename 1111aaaa2222bbbb3333cccc4444dddd "Customer demos 2026" --dry-run
  loom-pp-cli folders rename 1111aaaa2222bbbb3333cccc4444dddd "Archive"
  loom-pp-cli folders rename 1111aaaa2222bbbb3333cccc4444dddd "Team updates" --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "folder=1111aaaa2222bbbb3333cccc4444dddd;name=Customer demos 2026"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runIDTextWrite(cmd, flags, args, "folder", textWrite{textLabel: "name", verb: "rename folder", op: loom.OpRenameFolder, confirm: noConfirm}, func(id, name string) map[string]any {
				return map[string]any{"folderId": id, "name": name}
			})
		},
	}
}
