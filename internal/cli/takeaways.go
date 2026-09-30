package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
	"loom-pp-cli/internal/store"
)

// takeawaysView is the machine output of `takeaways`.
type takeawaysView struct {
	VideoID   string   `json:"video_id"`
	Available bool     `json:"available"`
	Takeaways []string `json:"takeaways"`
	Note      string   `json:"note"`
}

// noTakeawaysNote explains an empty result.
const noTakeawaysNote = "Loom has no key takeaways for this video; they are generated for meeting recordings"

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newTakeawaysCmd(flags))
	})
}

func newTakeawaysCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "takeaways [video]",
		Short: "Print Loom's AI key takeaways for a meeting recording",
		Long: `Print Loom's AI key takeaways for a video. Loom only generates takeaways for
meeting recordings; for other videos the command exits 0 with
{available: false, takeaways: [], note}. --data-source local reads takeaways
saved by 'hydrate'.`,
		Example: strings.Trim(`
  loom-pp-cli takeaways latest
  loom-pp-cli takeaways 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json
  loom-pp-cli takeaways https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --agent`, "\n"),
		Annotations: videoReadAnnotations(sourceAuto),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "takeaways")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[*loom.Takeaways]{
				what: "key takeaways",
				live: func(ctx context.Context, lc *loom.Client, id string) (*loom.Takeaways, error) {
					t, err := lc.FetchTakeaways(ctx, id)
					if err == nil && t != nil && !t.Available && strings.Contains(strings.ToLower(t.Note), "not found") {
						// Loom answers an unknown id with GenericError "Video not found.".
						return nil, videoNotFoundErr(id)
					}
					return t, err
				},
				local: func(ctx context.Context, db *store.Store, id string) (*loom.Takeaways, error) {
					a, err := localArtifacts(ctx, db, id)
					if err != nil {
						return nil, err
					}
					t := &loom.Takeaways{VideoID: id, Takeaways: make([]string, 0)}
					if len(a.Takeaways) > 0 {
						if err := json.Unmarshal(a.Takeaways, &t.Takeaways); err != nil {
							return nil, fmt.Errorf("decoding stored takeaways: %w", err)
						}
					}
					t.Available = len(t.Takeaways) > 0
					return t, nil
				},
				empty: func(id string) *loom.Takeaways { return &loom.Takeaways{VideoID: id, Takeaways: make([]string, 0)} },
				save: func(ctx context.Context, db *store.Store, id string, t *loom.Takeaways) error {
					if !t.Available {
						return nil
					}
					return saveArtifactPatch(ctx, db, id, artifactPatch{Takeaways: mustJSON(t.Takeaways)})
				},
			})
			if err != nil {
				return err
			}
			view := takeawaysView{VideoID: res.ID, Takeaways: make([]string, 0)}
			if res.Value != nil {
				view.Available = res.Value.Available
				view.Note = res.Value.Note
				if res.Value.Takeaways != nil {
					view.Takeaways = res.Value.Takeaways
				}
			}
			if !view.Available && view.Note == "" {
				view.Note = noTakeawaysNote
			}
			return emit(cmd, flags, view, func(w io.Writer) error {
				if res.Missing {
					return nil
				}
				if !view.Available {
					fmt.Fprintln(w, view.Note)
					return nil
				}
				for _, t := range view.Takeaways {
					fmt.Fprintf(w, "- %s\n", t)
				}
				return nil
			})
		},
	}
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}
