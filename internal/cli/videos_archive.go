package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newVideosArchiveCmd(flags *rootFlags) *cobra.Command {
	return newArchiveToggleCmd(flags, archiveToggle{Use: "archive [video...]", archive: true})
}

// archiveToggle carries the literal Use string for each wrapper so the
// command path stays visible to static doc checks.
type archiveToggle struct {
	Use     string
	archive bool
}

// newArchiveToggleCmd builds `videos archive` (archive=true) and
// `videos unarchive` (archive=false); both call ArchiveVideos.
func newArchiveToggleCmd(flags *rootFlags, t archiveToggle) *cobra.Command {
	archive := t.archive
	var sel videoSelector
	verb, source := "archive", loom.SourceMine
	short := "Archive videos by id, share URL, latest, or library filters"
	example := strings.Trim(`
  loom-pp-cli videos archive latest --dry-run
  loom-pp-cli videos archive --before 180d --dry-run --json
  loom-pp-cli videos archive 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --yes`, "\n")
	if !archive {
		verb, source = "unarchive", loom.SourceArchived
		short = "Restore archived videos by id, share URL, or filters over the archive"
		example = strings.Trim(`
  loom-pp-cli videos unarchive 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --dry-run
  loom-pp-cli videos unarchive --match "(?i)demo" --dry-run --json
  loom-pp-cli videos unarchive --since 30d --yes`, "\n")
	}
	cmd := &cobra.Command{
		Use:   t.Use,
		Short: short,
		Long: short + `.

Targets are positional video ids, share URLs or "latest", ids read with
--from-file, and/or the videos selected from your library by --all, --since,
--before, --folder and --match (newest first, capped by --limit and at most
--max-pages pages).` + map[bool]string{true: "", false: " unarchive selects from your archived videos."}[archive] + `
This changes your library, so it requires --yes; use --dry-run to list the
selected targets and the exact ArchiveVideos variables first.`,
		Example:     example,
		Annotations: map[string]string{"pp:happy-args": "video=latest"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runBulkVideoWrite(cmd, flags, args, sel, source, verb+" videos", loom.OpArchive, confirmLibrary, func(ids []string) map[string]any {
				return map[string]any{"videoIds": ids, "isArchived": archive}
			})
		},
	}
	registerVideoSelector(cmd, &sel, selectorOpts{})
	return cmd
}
