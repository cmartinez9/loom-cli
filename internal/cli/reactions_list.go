package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

// reactionCount is one row of `reactions list --summary`.
type reactionCount struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
}

func newReactionsListCmd(flags *rootFlags) *cobra.Command {
	var summary bool
	var limit int
	var dbPath string
	cmd := &cobra.Command{
		Use:   "list [video]",
		Short: "List the emoji reactions on a video with who and when",
		Long: `List the emoji reactions on a video: emoji, who reacted and the playback
time. --summary aggregates counts per emoji, most used first. Reactions
are not kept in the local store, so --data-source local is a usage error
(exit 2).`,
		Example: strings.Trim(`
  loom-pp-cli reactions list latest
  loom-pp-cli reactions list 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --summary
  loom-pp-cli reactions list https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json`, "\n"),
		Annotations: map[string]string{
			"mcp:read-only":  "true",
			"pp:data-source": sourceLive,
			"pp:happy-args":  "video=latest",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "reactions list")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			if limit < 0 {
				return usageErr(fmt.Errorf("--limit must be 0 (all) or more"))
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[[]loom.Reaction]{
				what: "reactions",
				live: func(ctx context.Context, lc *loom.Client, id string) ([]loom.Reaction, error) {
					return lc.FetchReactions(ctx, id)
				},
				empty: func(string) []loom.Reaction { return make([]loom.Reaction, 0) },
			})
			if err != nil {
				return err
			}
			reactions := res.Value
			if reactions == nil {
				reactions = make([]loom.Reaction, 0)
			}
			if summary {
				counts := summarizeReactions(reactions)
				if limit > 0 && len(counts) > limit {
					counts = counts[:limit]
				}
				return emit(cmd, flags, counts, func(w io.Writer) error {
					if len(counts) == 0 {
						if !res.Missing {
							fmt.Fprintln(w, "No reactions on this video.")
						}
						return nil
					}
					for _, c := range counts {
						fmt.Fprintf(w, "%s  %d\n", c.Emoji, c.Count)
					}
					return nil
				})
			}
			if limit > 0 && len(reactions) > limit {
				reactions = reactions[:limit]
			}
			return emit(cmd, flags, reactions, func(w io.Writer) error {
				if len(reactions) == 0 {
					if !res.Missing {
						fmt.Fprintln(w, "No reactions on this video.")
					}
					return nil
				}
				for _, r := range reactions {
					user := r.User
					if user == "" {
						user = "(anonymous)"
					}
					fmt.Fprintf(w, "%s  %s  %s\n", r.Timestamp, r.Emoji, user)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&summary, "summary", false, "Aggregate reaction counts per emoji instead of listing each one")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum rows to return (0 = all)")
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}

// summarizeReactions counts reactions per emoji, most used first, ties by
// emoji so the order is stable.
func summarizeReactions(reactions []loom.Reaction) []reactionCount {
	counts := map[string]int{}
	for _, r := range reactions {
		e := strings.TrimSpace(r.Emoji)
		if e == "" {
			e = "(unknown)"
		}
		counts[e]++
	}
	out := make([]reactionCount, 0, len(counts))
	for e, n := range counts {
		out = append(out, reactionCount{Emoji: e, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Emoji < out[j].Emoji
	})
	return out
}
