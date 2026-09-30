package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func newSpacesGetCmd(flags *rootFlags) *cobra.Command {
	var members int
	cmd := &cobra.Command{
		Use:   "get <space-id>",
		Short: "Show one space with its counts and first members",
		Long: `Show one space: name, privacy, video, folder and member counts, top tags,
and its first members (20 by default).

<space-id> is the space's UUID from 'spaces list', or 'primary' for your
workspace's primary space.`,
		Example: strings.Trim(`
  loom-pp-cli spaces get primary
  loom-pp-cli spaces get 1111aaaa-2222-4bbb-8ccc-3333dddd4444 --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "space=primary"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "spaces get")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if len(args) != 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("spaces get takes exactly one <space-id>, got %d", len(args)))
			}
			if members < 1 || members > 100 {
				return usageErr(fmt.Errorf("--members must be between 1 and 100"))
			}
			primary := strings.EqualFold(strings.TrimSpace(args[0]), "primary")
			id := ""
			if !primary {
				var err error
				if id, err = spaceIDArg(args[0]); err != nil {
					return err
				}
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			if primary {
				ms, err := lc.SpaceMemberships(ctx, 100)
				if err != nil {
					return loomErr(err)
				}
				for _, m := range ms {
					if m.IsPrimary {
						id = m.ID
						break
					}
				}
				if id == "" {
					return notFoundErr(fmt.Errorf("you are not a member of a primary space; run 'loom-pp-cli spaces list'"))
				}
			}
			sp, err := lc.SpaceDetails(ctx, id, members)
			if err != nil {
				return loomErr(err)
			}
			return emit(cmd, flags, sp, func(w io.Writer) error {
				fmt.Fprintf(w, "Name:     %s\nID:       %s\nPrivacy:  %s\n", sp.Name, sp.ID, sp.Privacy)
				fmt.Fprintf(w, "Videos:   %d\nMembers:  %d\n", sp.TotalVideos, sp.MembersCount)
				if sp.TotalFolders != nil {
					fmt.Fprintf(w, "Folders:  %d\n", *sp.TotalFolders)
				}
				if len(sp.TopTags) > 0 {
					fmt.Fprintf(w, "Top tags: %s\n", strings.Join(sp.TopTags, ", "))
				}
				if len(sp.Members) > 0 {
					fmt.Fprintf(w, "\nMembers (first %d):\n", len(sp.Members))
					for _, m := range sp.Members {
						fmt.Fprintf(w, "  %s  (user %s)\n", m.Name, m.UserID)
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().IntVar(&members, "members", 20, "How many members to include (1-100)")
	return cmd
}
