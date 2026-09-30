package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newFoldersMoveCmd(flags *rootFlags) *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "move [folder-id...] --to <folder-id>",
		Short: "Move folders inside another folder (requires --yes)",
		Long: `Move one or more folders under a new parent folder (BulkMoveFolders).
Moving requires --yes; --dry-run previews it.`,
		Example: strings.Trim(`
  loom-pp-cli folders move 1111aaaa2222bbbb3333cccc4444dddd --to 5555eeee6666ffff7777aaaa8888bbbb --dry-run
  loom-pp-cli folders move 1111aaaa2222bbbb3333cccc4444dddd --to 5555eeee6666ffff7777aaaa8888bbbb --yes
  loom-pp-cli folders move 1111aaaa2222bbbb3333cccc4444dddd,9999cccc0000dddd1111eeee2222ffff --to 5555eeee6666ffff7777aaaa8888bbbb --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "folder=1111aaaa2222bbbb3333cccc4444dddd;--to=5555eeee6666ffff7777aaaa8888bbbb"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			ids, err := opaqueIDList("folder", args)
			if err != nil {
				return err
			}
			parent := ""
			if to != "" {
				if parent, err = opaqueIDArg("folder", to); err != nil {
					return err
				}
			}
			if (len(ids) == 0 || parent == "") && !flags.dryRun {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("at least one <folder-id> and --to <folder-id> are required"))
			}
			if len(ids) == 0 {
				ids = []string{"<folder-id>"}
			}
			parent = placeholder(parent, "folder-id")
			action := fmt.Sprintf("move %d folder(s) into %s", len(ids), parent)
			if err := requireConfirm(flags, action, confirmLibrary); err != nil {
				return err
			}
			return runWrite(cmd, flags, action, loom.OpMoveFolders, map[string]any{"folderIds": ids, "newParentFolderId": parent}, ids, confirmLibrary)
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "Destination parent folder id")
	return cmd
}
