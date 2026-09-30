package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newVideosRecoverCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "recover [video]",
		Short: "Restore a deleted video from Loom's trash",
		Long: `Restore a deleted video from Loom's trash (RecoverVideo, force=false).

Pass the deleted video's id or share URL. Loom refuses when the video is no
longer recoverable.`,
		Example: strings.Trim(`
  loom-pp-cli videos recover 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --dry-run
  loom-pp-cli videos recover 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60
  loom-pp-cli videos recover https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runSingleVideoWrite(cmd, flags, args, "recover", loom.OpRecoverVideo, noConfirm, func(id string) map[string]any {
				return map[string]any{"videoId": id, "force": false}
			})
		},
	}
}
