package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newFoldersCreateCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new folder in your Loom library",
		Long:  "Create a folder in your library (CreateFolder). The result includes the new folder's id.",
		Example: strings.Trim(`
  loom-pp-cli folders create "Customer demos" --dry-run
  loom-pp-cli folders create "Q3 planning"
  loom-pp-cli folders create "Onboarding" --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "name=Customer demos"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if len(args) > 1 {
				return usageErr(fmt.Errorf("expected one <name>; quote a name that contains spaces"))
			}
			name := ""
			if len(args) > 0 {
				name = strings.TrimSpace(args[0])
			}
			if name == "" && !flags.dryRun {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("<name> is required: folders create <name>"))
			}
			name = placeholder(name, "name")
			return runWrite(cmd, flags, fmt.Sprintf("create folder %q", name), loom.OpCreateFolder, map[string]any{"name": name}, nil, noConfirm)
		},
	}
}
