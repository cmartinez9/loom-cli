package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newVideosPinCmd(flags *rootFlags) *cobra.Command {
	var off bool
	cmd := &cobra.Command{
		Use:   "pin [video]",
		Short: "Pin a video to the top of your library, or unpin it",
		Long: `Pin a video in your library (UpdateVideoPinStatus with context
my-library-context). Use --off to unpin.`,
		Example: strings.Trim(`
  loom-pp-cli videos pin latest --dry-run
  loom-pp-cli videos pin 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60
  loom-pp-cli videos pin 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --off`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			verb := "pin"
			if off {
				verb = "unpin"
			}
			return runSingleVideoWrite(cmd, flags, args, verb, loom.OpPin, noConfirm, func(id string) map[string]any {
				return map[string]any{"input": map[string]any{"videoId": id, "context": loom.PinContextLibrary, "newStatus": !off}}
			})
		},
	}
	cmd.Flags().BoolVar(&off, "off", false, "Unpin the video instead of pinning it")
	return cmd
}
