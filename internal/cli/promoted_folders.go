package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// newFoldersPromotedCmd is the `folders` group.
func newFoldersPromotedCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "folders",
		Example: strings.Trim(`
  loom-pp-cli folders list --agent --select id,name
  loom-pp-cli folders create "Customer demos" --dry-run
`, "\n"),
		Short: "List, find and manage folders in your Loom library",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newFoldersCreateCmd(flags))
	cmd.AddCommand(newFoldersDeleteCmd(flags))
	cmd.AddCommand(newFoldersGetCmd(flags))
	cmd.AddCommand(newFoldersListCmd(flags))
	cmd.AddCommand(newFoldersMoveCmd(flags))
	cmd.AddCommand(newFoldersRenameCmd(flags))
	cmd.AddCommand(newFoldersSearchCmd(flags))
	return cmd
}
