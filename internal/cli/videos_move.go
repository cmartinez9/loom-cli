package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newVideosMoveCmd(flags *rootFlags) *cobra.Command {
	var sel videoSelector
	var to string
	cmd := &cobra.Command{
		Use:   "move [video...] --to <folder-id>",
		Short: "Move videos into a folder, by id or by library filters",
		Long: `Move videos into a folder (BulkMoveVideos).

Targets are positional video ids, share URLs or "latest", ids read with
--from-file, and/or the videos selected from your library by --all, --since,
--before, --folder and --match (newest first, capped by --limit and
--max-pages). Moving changes your
library, so it requires --yes; --dry-run lists the targets first. Find folder
ids with 'loom-pp-cli folders list'.`,
		Example: strings.Trim(`
  loom-pp-cli videos move latest --to 1111aaaa2222bbbb3333cccc4444dddd --dry-run
  loom-pp-cli videos move --match "(?i)standup" --to 1111aaaa2222bbbb3333cccc4444dddd --dry-run --json
  loom-pp-cli videos move 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --to 1111aaaa2222bbbb3333cccc4444dddd --yes`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest;--to=1111aaaa2222bbbb3333cccc4444dddd"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			folder := placeholder("", "folder-id")
			if to != "" {
				id, err := opaqueIDArg("folder", to)
				if err != nil {
					return err
				}
				folder = id
			} else if !flags.dryRun {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--to <folder-id> is required"))
			}
			return runBulkVideoWrite(cmd, flags, args, sel, loom.SourceMine, "move videos to folder "+folder, loom.OpMoveVideos, confirmLibrary, func(ids []string) map[string]any {
				return map[string]any{"videoIds": ids, "newParentFolderId": folder}
			})
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "Destination folder id")
	registerVideoSelector(cmd, &sel, selectorOpts{})
	return cmd
}
