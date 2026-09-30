package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newVideosShareCmd(flags *rootFlags) *cobra.Command {
	var sel videoSelector
	var spaces []string
	cmd := &cobra.Command{
		Use:   "share [video...] --space <space-id>...",
		Short: "Share videos to one or more Loom spaces in one call",
		Long: `Share videos to spaces (BatchShareVideosToSpaces).

Targets are positional video ids, share URLs or "latest", ids read with
--from-file, and/or the videos selected from your library by --all, --since,
--before, --folder and --match (newest first, capped by --limit and
--max-pages). Sharing makes the videos
visible to every member of the spaces, so it requires --yes; --dry-run lists
the targets first. Find space ids with 'loom-pp-cli spaces list'.`,
		Example: strings.Trim(`
  loom-pp-cli videos share latest --space 3333aaaa4444bbbb5555cccc6666dddd --dry-run
  loom-pp-cli videos share --since 7d --space 3333aaaa4444bbbb5555cccc6666dddd --dry-run --json
  loom-pp-cli videos share 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --space 3333aaaa4444bbbb5555cccc6666dddd --yes`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest;--space=3333aaaa4444bbbb5555cccc6666dddd"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			spaceIDs, err := opaqueIDList("space", spaces)
			if err != nil {
				return err
			}
			if len(spaceIDs) == 0 {
				if !flags.dryRun {
					_ = cmd.Usage()
					return usageErr(fmt.Errorf("--space <space-id> is required (repeat or comma-separate for several)"))
				}
				spaceIDs = []string{"<space-id>"}
			}
			return runBulkVideoWrite(cmd, flags, args, sel, loom.SourceMine, fmt.Sprintf("share videos to %d space(s)", len(spaceIDs)), loom.OpShareToSpaces, confirmVisible, func(ids []string) map[string]any {
				return map[string]any{"videoIds": ids, "spaceIds": spaceIDs}
			})
		},
	}
	cmd.Flags().StringSliceVar(&spaces, "space", nil, "Space id to share to (repeatable or comma-separated)")
	registerVideoSelector(cmd, &sel, selectorOpts{})
	return cmd
}
