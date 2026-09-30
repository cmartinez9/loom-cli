package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

func newVideosSettingsCmd(flags *rootFlags) *cobra.Command {
	var downloads, comments string
	cmd := &cobra.Command{
		Use:   "settings [video]",
		Short: "Turn downloads and comments on or off for a video",
		Long: `Change a video's viewer settings (UpdateVideoSettings).

--downloads on|off controls whether viewers can download the video;
--comments on|off controls whether viewers can comment. Give at least one;
settings you leave out are not changed. Viewers are affected, so applying
the change needs --yes; --dry-run previews it without --yes.`,
		Example: strings.Trim(`
  loom-pp-cli videos settings latest --downloads off --dry-run
  loom-pp-cli videos settings 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --downloads on --comments off --yes
  loom-pp-cli videos settings latest --comments on --yes --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest;--downloads=off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if len(args) > 1 {
				return usageErr(fmt.Errorf("expected one <video>"))
			}
			var videoArg string
			if len(args) > 0 {
				if err := checkVideoArg(args[0]); err != nil {
					return err
				}
				videoArg = args[0]
			}
			settings := map[string]any{}
			if cmd.Flags().Changed("downloads") {
				v, err := parseOnOff("downloads", downloads)
				if err != nil {
					return err
				}
				settings["download_enabled"] = v
			}
			if cmd.Flags().Changed("comments") {
				v, err := parseOnOff("comments", comments)
				if err != nil {
					return err
				}
				settings["comments_enabled"] = v
			}
			if len(settings) == 0 && !flags.dryRun {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("give at least one of --downloads on|off or --comments on|off"))
			}
			if videoArg == "" && !flags.dryRun {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("<video> is required: settings <video> --downloads on|off"))
			}
			if err := requireConfirm(flags, "changing viewer settings of video "+placeholder(videoArg, "video"), confirmVisible); err != nil {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			id, err := resolveVideoForWrite(ctx, flags, videoArg)
			if err != nil {
				return err
			}
			vars := map[string]any{"videoId": id, "settings": settings}
			return runWrite(cmd, flags, "update settings of video "+id, loom.OpUpdateSettings, vars, []string{id}, confirmVisible)
		},
	}
	cmd.Flags().StringVar(&downloads, "downloads", "", "Allow viewers to download: on or off")
	cmd.Flags().StringVar(&comments, "comments", "", "Allow viewers to comment: on or off")
	return cmd
}
