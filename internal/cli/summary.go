package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
	"loom-pp-cli/internal/store"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newSummaryCmd(flags))
	})
}

func newSummaryCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "summary [video]",
		Short: "Print Loom's AI-generated summary paragraph for a video",
		Long: `Print Loom's AI-generated summary for a video. JSON output is
{video_id, status, summary}; status is Loom's generation state (success,
pending, ...). --data-source local reads the summary saved by 'hydrate'.`,
		Example: strings.Trim(`
  loom-pp-cli summary latest
  loom-pp-cli summary 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json
  loom-pp-cli summary https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --data-source local`, "\n"),
		Annotations: videoReadAnnotations(sourceAuto),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "summary")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[*loom.Summary]{
				what: "AI summary",
				live: func(ctx context.Context, lc *loom.Client, id string) (*loom.Summary, error) {
					s, err := lc.FetchSummary(ctx, id)
					if err == nil && s != nil {
						s.Status = normalizeSummaryStatus(s.Status, s.Summary)
						if s.Status == "pending" && !lc.IsDryRun() {
							// Pending long after recording means it is not coming.
							if v, verr := lc.GetVideo(ctx, id); verr == nil && summaryPendingStale(s.Status, s.Summary, v.CreatedAt, time.Now()) {
								s.Status = summaryUnavailable
							}
						}
					}
					return s, err
				},
				local: func(ctx context.Context, db *store.Store, id string) (*loom.Summary, error) {
					a, err := localArtifacts(ctx, db, id)
					if err != nil {
						return nil, err
					}
					status := normalizeSummaryStatus(a.SummaryStatus, a.Summary)
					if status == "pending" && summaryPendingStale(status, a.Summary, db.SyncedVideoCreatedAt(ctx, id), time.Now()) {
						status = summaryUnavailable
					}
					return &loom.Summary{VideoID: id, Status: status, Summary: a.Summary}, nil
				},
				empty: func(id string) *loom.Summary { return &loom.Summary{VideoID: id, Status: "unavailable"} },
				save: func(ctx context.Context, db *store.Store, id string, s *loom.Summary) error {
					return saveArtifactPatch(ctx, db, id, artifactPatch{Summary: s.Summary, SummaryStatus: s.Status})
				},
			})
			if err != nil {
				return err
			}
			view := res.Value
			view.VideoID = res.ID
			return emit(cmd, flags, view, func(w io.Writer) error {
				if res.Missing {
					return nil
				}
				if strings.TrimSpace(view.Summary) == "" {
					if view.Status == summaryUnavailable {
						fmt.Fprintln(w, "No AI summary available.")
						return nil
					}
					fmt.Fprintf(w, "No AI summary yet (status: %s).\n", view.Status)
					return nil
				}
				fmt.Fprintln(w, strings.TrimSpace(view.Summary))
				return nil
			})
		},
	}
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}

// summaryUnavailable is the status for a summary Loom reported as pending
// long after recording (see summaryPendingStale): it is not coming.
const summaryUnavailable = "unavailable"

// normalizeSummaryStatus maps Loom's autoDescriptionStatus (AUTO when the AI
// summary was generated, PENDING, FAILED, ...) onto success / pending /
// failed / none. A summary that has text is a success whatever Loom calls it.
func normalizeSummaryStatus(status, summary string) string {
	if strings.TrimSpace(summary) != "" {
		return "success"
	}
	switch s := strings.ToLower(strings.TrimSpace(status)); s {
	case "pending", "in_progress", "processing", "queued", "started":
		return "pending"
	case "failed", "failure", "error":
		return "failed"
	case "", "auto", "success", "succeeded", "done", "complete", "completed", "edited", "manual":
		return "none"
	default:
		return s
	}
}
