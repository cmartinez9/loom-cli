package cli

import "github.com/spf13/cobra"

func newVideosUnarchiveCmd(flags *rootFlags) *cobra.Command {
	return newArchiveToggleCmd(flags, archiveToggle{Use: "unarchive [video...]", archive: false})
}
