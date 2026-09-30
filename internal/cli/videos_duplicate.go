package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newVideosDuplicateCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "duplicate [video]",
		Short: "Make a copy of a video in your library",
		Long: `Duplicate a video (DuplicateVideo). The result includes the new video's id
and name.`,
		Example: strings.Trim(`
  loom-pp-cli videos duplicate latest --dry-run
  loom-pp-cli videos duplicate 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json
  loom-pp-cli videos duplicate https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runSingleVideoWrite(cmd, flags, args, "duplicate", loom.OpDuplicateVideo, noConfirm, func(id string) map[string]any {
				return map[string]any{"videoId": id}
			})
		},
	}
}
