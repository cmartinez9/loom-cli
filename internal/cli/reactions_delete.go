package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newReactionsDeleteCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "delete [reaction-id]",
		Short: "Remove one of your emoji reactions (requires --yes)",
		Long: `Delete a video reaction (DeleteVideoReaction). Find reaction ids with
'loom-pp-cli reactions list <video>'.

Viewers of the video see the reaction removed, so deleting it requires
--yes; --dry-run previews it.`,
		Example: strings.Trim(`
  loom-pp-cli reactions delete 90412 --dry-run
  loom-pp-cli reactions delete 90412 --yes
  loom-pp-cli reactions delete 90412 --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "reaction=90412"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runIDWrite(cmd, flags, args, "reaction", "delete reaction", loom.OpDeleteReaction, confirmVisible, func(id string) map[string]any {
				return map[string]any{"reactionId": id}
			})
		},
	}
}
