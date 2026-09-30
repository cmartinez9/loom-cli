package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newVideosDeleteCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete [video]",
		Short: "Delete a video (moves it to Loom's trash; requires --yes)",
		Long: `Delete a video (DeleteVideo).

Loom moves deleted videos to its trash, where 'videos recover' can restore
them for a limited time. Deleting requires --yes; --dry-run previews it.`,
		Example: strings.Trim(`
  loom-pp-cli videos delete latest --dry-run
  loom-pp-cli videos delete 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --yes
  loom-pp-cli videos delete https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runSingleVideoWrite(cmd, flags, args, "delete", loom.OpDeleteVideo, confirmLibrary, func(id string) map[string]any {
				return map[string]any{"id": id}
			})
		},
	}
}
