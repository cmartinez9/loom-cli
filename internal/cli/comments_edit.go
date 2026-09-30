package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newCommentsEditCmd(flags *rootFlags) *cobra.Command {
	var reply bool
	cmd := &cobra.Command{
		Use:   "edit [video] [comment-id] [text]",
		Short: "Replace the text of one of your comments or replies",
		Long: `Edit a comment (EditComment). Loom needs the video the comment belongs to
and the comment's type: top-level comments are COMMENT, replies are REPLY
(pass --reply). Find comment ids with 'loom-pp-cli comments list <video>'.

Viewers of the video see the edited text, so editing needs --yes; --dry-run
previews it without --yes.`,
		Example: strings.Trim(`
  loom-pp-cli comments edit latest 51234 "Fixed typo in my question" --dry-run
  loom-pp-cli comments edit 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 51234 "Updated: shipped in v2" --yes
  loom-pp-cli comments edit 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 51240 "Thanks, resolved" --reply --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest;comment=51234;text=Updated wording"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if len(args) > 3 {
				return usageErr(fmt.Errorf("expected <video> <comment-id> <text>; quote text that contains spaces"))
			}
			var videoArg, commentID, text string
			if len(args) > 0 {
				if err := checkVideoArg(args[0]); err != nil {
					return err
				}
				videoArg = args[0]
			}
			if len(args) > 1 {
				id, err := opaqueIDArg("comment", args[1])
				if err != nil {
					return err
				}
				commentID = id
			}
			if len(args) > 2 {
				text = strings.TrimSpace(args[2])
			}
			if (videoArg == "" || commentID == "" || text == "") && !flags.dryRun {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("<video>, <comment-id> and <text> are all required"))
			}
			if err := requireConfirm(flags, "edit comment "+placeholder(commentID, "comment-id"), confirmVisible); err != nil {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			id, err := resolveVideoForWrite(ctx, flags, videoArg)
			if err != nil {
				return err
			}
			commentID = placeholder(commentID, "comment-id")
			vars := map[string]any{"id": commentID, "videoId": id, "content": placeholder(text, "text"), "type": commentType(reply)}
			return runWrite(cmd, flags, "edit comment "+commentID, loom.OpEditComment, vars, []string{commentID}, confirmVisible)
		},
	}
	cmd.Flags().BoolVar(&reply, "reply", false, "The id is a reply (REPLY) rather than a top-level comment (COMMENT)")
	return cmd
}

// commentType maps --reply to Loom's PublicVideoCommentType / CommentType.
func commentType(reply bool) string {
	if reply {
		return commentTypeReply
	}
	return commentTypeComment
}
