package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// newSpacesPromotedCmd is the `spaces` group.
func newSpacesPromotedCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "spaces",
		Example: strings.Trim(`
  loom-pp-cli spaces list --open
  loom-pp-cli spaces get primary --json
`, "\n"),
		Short: "List and inspect the Loom spaces you belong to",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newSpacesGetCmd(flags))
	cmd.AddCommand(newSpacesListCmd(flags))
	return cmd
}
