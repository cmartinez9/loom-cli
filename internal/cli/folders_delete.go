package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newFoldersDeleteCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete [folder-id...]",
		Short: "Delete one or more folders (requires --yes)",
		Long: `Delete folders (BulkDeleteFolders). Pass one or more folder ids, separated
by spaces or commas. Deleting requires --yes; --dry-run previews it.`,
		Example: strings.Trim(`
  loom-pp-cli folders delete 1111aaaa2222bbbb3333cccc4444dddd --dry-run
  loom-pp-cli folders delete 1111aaaa2222bbbb3333cccc4444dddd --yes
  loom-pp-cli folders delete 1111aaaa2222bbbb3333cccc4444dddd,5555eeee6666ffff7777aaaa8888bbbb --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "folder=1111aaaa2222bbbb3333cccc4444dddd"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			ids, err := opaqueIDList("folder", args)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				if !flags.dryRun {
					_ = cmd.Usage()
					return usageErr(fmt.Errorf("at least one <folder-id> is required"))
				}
				ids = []string{"<folder-id>"}
			}
			action := fmt.Sprintf("delete %d folder(s)", len(ids))
			if err := requireConfirm(flags, action, confirmLibrary); err != nil {
				return err
			}
			return runWrite(cmd, flags, action, loom.OpDeleteFolders, map[string]any{"folderIds": ids}, ids, confirmLibrary)
		},
	}
}
