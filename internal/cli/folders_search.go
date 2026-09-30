package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newFoldersSearchCmd(flags *rootFlags) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Find folders by name with Loom's server-side folder search",
		Long: `Find folders by name with Loom's server-side folder search. Returns each
matching folder's id, name and total video count.

To search videos, folders, spaces and people together, use 'videos search'.`,
		Example: strings.Trim(`
  loom-pp-cli folders search onboarding
  loom-pp-cli folders search "customer calls" --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "query=videos"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "folders search")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("<query> is required, e.g. loom-pp-cli folders search onboarding"))
			}
			if limit < 0 {
				return usageErr(fmt.Errorf("--limit must be 0 or more"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			rows, err := lc.SearchFolders(ctx, query)
			if err != nil {
				return loomErr(err)
			}
			if limit > 0 && len(rows) > limit {
				rows = rows[:limit]
			}
			return printFolders(cmd, flags, rows)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum folders to return (0 = all)")
	return cmd
}
