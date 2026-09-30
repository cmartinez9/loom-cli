package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// newVideosPromotedCmd is the `videos` group.
func newVideosPromotedCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "videos",
		Example: strings.Trim(`
  loom-pp-cli videos list --since 30d
  loom-pp-cli videos get latest --json
  loom-pp-cli videos rename latest "Q3 demo" --dry-run
`, "\n"),
		Short: "Browse, search, export and organize the videos in your Loom library",
		Long: `Browse, search, export and organize the videos in your Loom library.

Read commands (list, get, search, export) call Loom live. Write commands
(rename, describe, settings, archive, move, delete, pin, share, ...) print a
preview with --dry-run and require --yes when they change or remove videos.`,
		RunE: parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newVideosArchiveCmd(flags))
	cmd.AddCommand(newVideosDeleteCmd(flags))
	cmd.AddCommand(newVideosDescribeCmd(flags))
	cmd.AddCommand(newVideosDuplicateCmd(flags))
	cmd.AddCommand(newVideosExportCmd(flags))
	cmd.AddCommand(newVideosFollowCmd(flags))
	cmd.AddCommand(newVideosGetCmd(flags))
	cmd.AddCommand(newVideosListCmd(flags))
	cmd.AddCommand(newVideosMoveCmd(flags))
	cmd.AddCommand(newVideosPinCmd(flags))
	cmd.AddCommand(newVideosRecoverCmd(flags))
	cmd.AddCommand(newVideosRenameCmd(flags))
	cmd.AddCommand(newVideosSearchCmd(flags))
	cmd.AddCommand(newVideosSettingsCmd(flags))
	cmd.AddCommand(newVideosShareCmd(flags))
	cmd.AddCommand(newVideosUnarchiveCmd(flags))
	cmd.AddCommand(newVideosWatchLaterCmd(flags))
	return cmd
}
