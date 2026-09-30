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

func newTasksListCmd(flags *rootFlags) *cobra.Command {
	var open bool
	var limit int
	var dbPath string
	cmd := &cobra.Command{
		Use:   "list [video]",
		Short: "List Loom's AI action items on a video with deep links",
		Long: `List the action items (tasks) on a video, each with its playback time and a
share link that starts there. --open hides resolved items. --data-source
local reads action items saved by 'hydrate'.`,
		Example: strings.Trim(`
  loom-pp-cli tasks list latest
  loom-pp-cli tasks list 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --open --json
  loom-pp-cli tasks list https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --data-source local`, "\n"),
		Annotations: map[string]string{
			"mcp:read-only":  "true",
			"pp:data-source": sourceAuto,
			"pp:happy-args":  "video=latest",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "tasks list")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			if limit < 0 {
				return usageErr(fmt.Errorf("--limit must be 0 (all) or more"))
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[[]loom.Task]{
				what: "action items",
				live: func(ctx context.Context, lc *loom.Client, id string) ([]loom.Task, error) {
					return lc.FetchTasks(ctx, id)
				},
				local: func(ctx context.Context, db *store.Store, id string) ([]loom.Task, error) {
					a, err := localArtifacts(ctx, db, id)
					if err != nil {
						return nil, err
					}
					tasks := make([]loom.Task, 0)
					if len(a.Tasks) > 0 {
						if err := json.Unmarshal(a.Tasks, &tasks); err != nil {
							return nil, fmt.Errorf("decoding stored tasks: %w", err)
						}
					}
					return tasks, nil
				},
				empty: func(string) []loom.Task { return make([]loom.Task, 0) },
				save: func(ctx context.Context, db *store.Store, id string, tasks []loom.Task) error {
					return saveArtifactPatch(ctx, db, id, artifactPatch{Tasks: mustJSON(tasks)})
				},
			})
			if err != nil {
				return err
			}
			tasks := filterTasks(res.Value, open)
			if limit > 0 && len(tasks) > limit {
				tasks = tasks[:limit]
			}
			return emit(cmd, flags, tasks, func(w io.Writer) error {
				if len(tasks) == 0 {
					if !res.Missing {
						fmt.Fprintln(w, "No action items on this video.")
					}
					return nil
				}
				for _, t := range tasks {
					mark := "[ ]"
					if t.ResolvedAt != "" {
						mark = "[x]"
					}
					owner := ""
					if t.Owner != "" {
						owner = "  (" + t.Owner + ")"
					}
					fmt.Fprintf(w, "%s %s  %s%s  %s\n", mark, t.Timestamp, strings.TrimSpace(t.Content), owner, t.Link)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&open, "open", false, "Hide resolved action items")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum action items to return (0 = all)")
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}

// filterTasks drops resolved tasks when openOnly is set.
func filterTasks(tasks []loom.Task, openOnly bool) []loom.Task {
	out := make([]loom.Task, 0, len(tasks))
	for _, t := range tasks {
		if openOnly && t.ResolvedAt != "" {
			continue
		}
		out = append(out, t)
	}
	return out
}
