package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newCommentsDeleteCmd(flags *rootFlags) *cobra.Command {
	var reply bool
	cmd := &cobra.Command{
		Use:   "delete [comment-id]",
		Short: "Delete a comment or reply (requires --yes)",
		Long: `Delete a comment (DeleteComment). Pass --reply when the id is a reply.

Everyone who can open the video sees the comment disappear, so deleting
requires --yes; --dry-run previews it.`,
		Example: strings.Trim(`
  loom-pp-cli comments delete 51234 --dry-run
  loom-pp-cli comments delete 51234 --yes
  loom-pp-cli comments delete 51240 --reply --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "comment=51234"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runIDWrite(cmd, flags, args, "comment", "delete comment", loom.OpDeleteComment, confirmVisible, func(id string) map[string]any {
				return map[string]any{"id": id, "type": commentType(reply)}
			})
		},
	}
	cmd.Flags().BoolVar(&reply, "reply", false, "The id is a reply (REPLY) rather than a top-level comment (COMMENT)")
	return cmd
}
