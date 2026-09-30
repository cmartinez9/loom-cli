package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

// videoDetail is a video plus the signed-in user's pin and follow state.
// Pinned/Following are omitted when Loom did not answer them.
type videoDetail struct {
	loom.Video
	Pinned    *bool `json:"pinned,omitempty"`
	Following *bool `json:"following,omitempty"`
}

func newVideosGetCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <video>",
		Short: "Show one video's metadata, pin and follow state",
		Long: `Show one video's metadata: title, description, duration, views, comments,
reactions, folder, owner, privacy and share URL, plus whether you pinned it
in My Library and whether you follow its activity.

<video> is a 32-character video id, a loom.com share URL, or 'latest' for
your newest recording.`,
		Example: strings.Trim(`
  loom-pp-cli videos get latest
  loom-pp-cli videos get 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json
  loom-pp-cli videos get https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json --select id,name,duration`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "video=latest"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "videos get")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if len(args) != 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("videos get takes exactly one <video> (id, share URL or 'latest'), got %d", len(args)))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			id, err := resolveVideoArg(ctx, lc, args[0])
			if err != nil {
				return err
			}
			v, err := lc.GetVideo(ctx, id)
			if err != nil {
				return videoLookupErr(id, err)
			}
			out := videoDetail{Video: *v}
			if pinned, err := lc.IsVideoPinned(ctx, id, loom.PinContextLibrary); err == nil {
				out.Pinned = &pinned
			}
			if following, err := lc.FollowsVideo(ctx, id); err == nil {
				out.Following = &following
			}
			return emit(cmd, flags, out, func(w io.Writer) error {
				printVideoDetail(w, out)
				return nil
			})
		},
	}
	return cmd
}

func printVideoDetail(w io.Writer, v videoDetail) {
	line := func(k, val string) {
		if val != "" {
			fmt.Fprintf(w, "%-12s %s\n", k+":", val)
		}
	}
	line("Name", v.Name)
	line("ID", v.ID)
	line("URL", v.ShareURL)
	line("Created", v.CreatedAt)
	line("Duration", v.Duration)
	line("Views", fmt.Sprintf("%d (%d unique)", v.Views, v.UniqueViews))
	line("Comments", fmt.Sprint(v.Comments))
	line("Reactions", fmt.Sprint(v.Reactions))
	line("Owner", v.OwnerName)
	line("Folder", v.FolderName)
	line("Privacy", v.Privacy)
	if v.Archived {
		line("Archived", "yes")
	}
	if len(v.Tags) > 0 {
		line("Tags", strings.Join(v.Tags, ", "))
	}
	if v.Pinned != nil {
		line("Pinned", yesNo(*v.Pinned))
	}
	if v.Following != nil {
		line("Following", yesNo(*v.Following))
	}
	line("Downloads", yesNo(v.DownloadEnabled))
	if v.Description != "" {
		fmt.Fprintf(w, "\n%s\n", v.Description)
	}
}
