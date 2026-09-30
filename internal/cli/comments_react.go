package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newCommentsReactCmd(flags *rootFlags) *cobra.Command {
	var reply bool
	cmd := &cobra.Command{
		Use:   "react [comment-id] [emoji]",
		Short: "Add an emoji reaction to a comment or reply",
		Long: `React to a comment with an emoji (AddCommentReaction). Pass --reply when the
id is a reply. The emoji is sent as Loom's extendedReaction value.

The comment's author and the video's viewers see the reaction, so reacting
needs --yes; --dry-run previews it without --yes.`,
		Example: strings.Trim(`
  loom-pp-cli comments react 51234 "👍" --dry-run
  loom-pp-cli comments react 51234 "🎉" --yes
  loom-pp-cli comments react 51240 "✅" --reply --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "comment=51234;emoji=👍"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runIDTextWrite(cmd, flags, args, "comment", textWrite{textLabel: "emoji", verb: "react to comment", op: loom.OpAddCommentReaction, confirm: confirmVisible}, func(id, emoji string) map[string]any {
				return map[string]any{"input": map[string]any{
					"commentGuid":      id,
					"commentType":      commentType(reply),
					"extendedReaction": emoji,
				}}
			})
		},
	}
	cmd.Flags().BoolVar(&reply, "reply", false, "The id is a reply (REPLY) rather than a top-level comment (COMMENT)")
	return cmd
}
