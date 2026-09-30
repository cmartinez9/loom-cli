package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newSpacesListCmd(flags *rootFlags) *cobra.Command {
	var (
		open   bool
		limit  int
		dbPath string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the Loom spaces you belong to, or every open space",
		Long: `List the Loom spaces you belong to (id, name, privacy, primary flag).

--open lists the workspace's open spaces instead, with video and member
counts and whether you are a member. --data-source local reads the spaces
synced by 'loom-pp-cli sync'.`,
		Example: strings.Trim(`
  loom-pp-cli spaces list
  loom-pp-cli spaces list --open --json
  loom-pp-cli spaces list --json --select id,name`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "spaces list")
			}
			if len(args) > 0 {
				return usageErr(fmt.Errorf("spaces list takes no positional arguments; use 'spaces get <space-id>' for one space"))
			}
			if limit < 1 || limit > 100 {
				return usageErr(fmt.Errorf("--limit must be between 1 and 100"))
			}
			if open && dataSourceMode(flags) == sourceLocal {
				return usageErr(fmt.Errorf("--open needs live data; drop --data-source local"))
			}
			db := resolveDBPath(dbPath)
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			local := func() error {
				if done, err := missingLocalMirror(cmd, flags, db, "spaces", make([]loom.SpaceMembership, 0)); done {
					return err
				}
				spaces, err := readSyncedSpaces(ctx, db)
				if err != nil {
					return err
				}
				rows := make([]loom.SpaceMembership, 0, len(spaces))
				for _, sp := range spaces {
					rows = append(rows, loom.SpaceMembership{Space: sp})
				}
				if len(rows) > limit {
					rows = rows[:limit]
				}
				return printMemberships(cmd, flags, rows)
			}
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			if open {
				rows, err := lc.OpenSpaces(ctx, limit)
				if err != nil {
					return loomErr(err)
				}
				return emit(cmd, flags, rows, func(w io.Writer) error {
					if len(rows) == 0 {
						fmt.Fprintln(w, "No open spaces.")
						return nil
					}
					tw := newTabWriter(w)
					fmt.Fprintln(tw, "NAME\tPRIVACY\tVIDEOS\tMEMBERS\tMEMBER\tID")
					for _, s := range rows {
						fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\t%s\n", truncate(s.Name, 40), s.Privacy, s.TotalVideos, s.MembersCount, yesNo(s.IsMember), s.ID)
					}
					return tw.Flush()
				})
			}
			live := func() error {
				rows, err := lc.SpaceMemberships(ctx, limit)
				if err != nil {
					return err
				}
				return printMemberships(cmd, flags, rows)
			}
			return withDataSource(cmd, flags, db, "spaces", live, local)
		},
	}
	cmd.Flags().BoolVar(&open, "open", false, "List the workspace's open spaces with counts instead of your memberships")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum spaces to return (1-100)")
	cmd.Flags().StringVar(&dbPath, "db", "", "Local store path for --data-source local (default: the CLI's data directory)")
	return cmd
}

func printMemberships(cmd *cobra.Command, flags *rootFlags, rows []loom.SpaceMembership) error {
	return emit(cmd, flags, rows, func(w io.Writer) error {
		if len(rows) == 0 {
			fmt.Fprintln(w, "You are not a member of any space.")
			return nil
		}
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "NAME\tPRIVACY\tPRIMARY\tID")
		for _, s := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", truncate(s.Name, 40), s.Privacy, yesNo(s.IsPrimary), s.ID)
		}
		return tw.Flush()
	})
}
