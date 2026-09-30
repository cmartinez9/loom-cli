package cli

import (
	"context"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
	"github.com/cmartinez9/loom-cli/internal/store"
)

// captionsView is the machine output of `captions`.
type captionsView struct {
	VideoID string `json:"video_id"`
	Format  string `json:"format"`
	VTT     string `json:"vtt"`
}

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newCaptionsCmd(flags))
	})
}

func newCaptionsCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "captions [video]",
		Short: "Print a video's WebVTT captions exactly as Loom serves them",
		Long: `Print a video's WebVTT captions exactly as Loom serves them, ready to
redirect into a .vtt file. With --json (or --agent) the output is
{video_id, format, vtt}. --data-source local renders captions from a
transcript saved by 'hydrate'.`,
		Example: strings.Trim(`
  loom-pp-cli captions latest
  loom-pp-cli captions 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 > talk.vtt
  loom-pp-cli captions https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json`, "\n"),
		Annotations: videoReadAnnotations(sourceAuto),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "captions")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[string]{
				what: "captions",
				live: func(ctx context.Context, lc *loom.Client, id string) (string, error) {
					return lc.FetchCaptions(ctx, id)
				},
				local: func(ctx context.Context, db *store.Store, id string) (string, error) {
					t, err := localTranscript(ctx, db, id)
					if err != nil {
						return "", err
					}
					return loom.FormatVTT(t.Segments), nil
				},
				empty: func(string) string { return "" },
			})
			if err != nil {
				return err
			}
			view := captionsView{VideoID: res.ID, Format: "vtt", VTT: res.Value}
			out := cmd.OutOrStdout()
			// Captions are a file format: print them raw even when piped,
			// unless machine output was asked for explicitly.
			if wantsMachineOutput(flags) {
				return printJSONFiltered(out, view, flags)
			}
			if res.Missing {
				return nil
			}
			if !isTerminal(out) {
				// Redirected to a file: the VTT exactly as Loom serves it.
				_, err = io.WriteString(out, res.Value)
				return err
			}
			// On a terminal, caption text is untrusted human output.
			return writeHuman(cmd, func(w io.Writer) error {
				_, err := io.WriteString(w, res.Value)
				return err
			})
		},
	}
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}
