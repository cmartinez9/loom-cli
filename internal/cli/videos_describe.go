package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newVideosDescribeCmd(flags *rootFlags) *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "describe [video] [text]",
		Short: "Set a video's description from text or a file",
		Long: `Set a video's description (UpdateVideoDescription).

<video> is an id, share URL or "latest". Pass the text inline, or use
--from-file <path> (or --from-file - for stdin) for longer descriptions.
The description is replaced, not appended.`,
		Example: strings.Trim(`
  loom-pp-cli videos describe latest "Walkthrough of the new billing page" --dry-run
  loom-pp-cli videos describe 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --from-file notes.md
  loom-pp-cli videos describe latest --from-file - < summary.txt`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest;text=Walkthrough of the new billing page"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runVideoTextWrite(cmd, flags, args, textWrite{textLabel: "text", verb: "set description of video", op: loom.OpUpdateDescription, confirm: noConfirm, fromFile: &fromFile}, func(id, text string, _ int) map[string]any {
				return map[string]any{"id": id, "description": text}
			})
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Read the description from this file ('-' for stdin)")
	return cmd
}
