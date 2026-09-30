// Copyright 2026 cmartinez9 and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source local

package cli

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
	"github.com/cmartinez9/loom-cli/internal/store"
)

// digestVideo is one video in a window digest.
type digestVideo struct {
	VideoID       string         `json:"video_id"`
	Name          string         `json:"name"`
	CreatedAt     string         `json:"created_at"`
	Duration      string         `json:"duration"`
	ShareURL      string         `json:"share_url"`
	Hydrated      bool           `json:"hydrated"`
	Summary       string         `json:"summary"`
	SummaryStatus string         `json:"summary_status,omitempty"`
	Chapters      []loom.Chapter `json:"chapters"`
	ActionItems   []loom.Task    `json:"action_items"`
	Takeaways     []string       `json:"takeaways,omitempty"`
	Note          string         `json:"note,omitempty"`
}

// digestResult is the JSON form of a digest.
type digestResult struct {
	Since            string        `json:"since"`
	Until            string        `json:"until"`
	Window           string        `json:"window"`
	Videos           []digestVideo `json:"videos"`
	MissingArtifacts int           `json:"missing_artifacts"`
}

// digestVideoFrom builds a digest entry from a synced video and its stored
// artifacts (nil when the video was never hydrated).
func digestVideoFrom(v store.LocalVideo, a *store.ArtifactRow, window string) digestVideo {
	d := digestVideo{
		VideoID:     v.ID,
		Name:        v.Name,
		CreatedAt:   v.CreatedAt,
		Duration:    loom.FormatClock(v.DurationSeconds),
		ShareURL:    v.ShareURL,
		Chapters:    make([]loom.Chapter, 0),
		ActionItems: make([]loom.Task, 0),
	}
	if d.ShareURL == "" {
		d.ShareURL = loom.ShareURL(v.ID)
	}
	if a == nil {
		d.Note = "not hydrated yet: run loom-pp-cli hydrate --since " + window
		return d
	}
	d.Hydrated = true
	d.Summary = strings.TrimSpace(a.Summary)
	d.SummaryStatus = a.SummaryStatus
	if summaryPendingStale(a.SummaryStatus, a.Summary, v.CreatedAt, time.Now()) {
		// Loom never finished this summary; it is not coming.
		d.SummaryStatus = summaryUnavailable
	}
	if len(a.Chapters) > 0 {
		_ = json.Unmarshal(a.Chapters, &d.Chapters)
	}
	for i := range d.Chapters {
		if d.Chapters[i].Link == "" {
			d.Chapters[i].Link = loom.DeepLink(v.ID, d.Chapters[i].Start)
		}
		if d.Chapters[i].Timestamp == "" {
			d.Chapters[i].Timestamp = loom.FormatClock(d.Chapters[i].Start)
		}
	}
	if len(a.Tasks) > 0 {
		_ = json.Unmarshal(a.Tasks, &d.ActionItems)
	}
	if d.Chapters == nil {
		d.Chapters = make([]loom.Chapter, 0)
	}
	if d.ActionItems == nil {
		d.ActionItems = make([]loom.Task, 0)
	}
	if len(a.Takeaways) > 0 {
		var tk []string
		if json.Unmarshal(a.Takeaways, &tk) == nil && len(tk) > 0 {
			d.Takeaways = tk
		}
	}
	return d
}

// renderDigestMarkdown renders the digest as Markdown.
func renderDigestMarkdown(res digestResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Loom digest: %s – %s\n\n", formatDate(res.Since), formatDate(res.Until))
	if len(res.Videos) == 0 {
		b.WriteString("No videos in this window.\n")
		return b.String()
	}
	b.WriteString(countNoun(len(res.Videos), "video"))
	if res.MissingArtifacts > 0 {
		fmt.Fprintf(&b, " · %d not hydrated yet", res.MissingArtifacts)
	}
	b.WriteString("\n")
	for _, v := range res.Videos {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			name = "Untitled video"
		}
		fmt.Fprintf(&b, "\n## [%s](%s)\n\n", mdInline(name), v.ShareURL)
		fmt.Fprintf(&b, "%s · %s\n", formatDate(v.CreatedAt), v.Duration)
		if !v.Hydrated {
			fmt.Fprintf(&b, "\n_%s_\n", v.Note)
			continue
		}
		if v.Summary != "" {
			fmt.Fprintf(&b, "\n%s\n", v.Summary)
		} else if v.SummaryStatus == summaryUnavailable {
			b.WriteString("\n_No AI summary available._\n")
		} else if hydrateStillGenerating(v.SummaryStatus) {
			b.WriteString("\n_Loom is still generating the AI summary._\n")
		} else if v.SummaryStatus != "" {
			fmt.Fprintf(&b, "\n_No AI summary from Loom (status: %s)._\n", v.SummaryStatus)
		} else {
			b.WriteString("\n_No AI summary from Loom._\n")
		}
		if len(v.Chapters) > 0 {
			b.WriteString("\n**Chapters**\n\n")
			for _, c := range v.Chapters {
				b.WriteString(mdChapterLine(c.Timestamp, c.Link, c.Title))
			}
		}
		if len(v.ActionItems) > 0 {
			b.WriteString("\n**Action items**\n\n")
			for _, t := range v.ActionItems {
				b.WriteString(mdTaskLine(t))
			}
		}
		if len(v.Takeaways) > 0 {
			b.WriteString("\n**Key takeaways**\n\n")
			for _, t := range v.Takeaways {
				b.WriteString(mdBullet(t))
			}
		}
	}
	return b.String()
}

func newNovelDigestCmd(flags *rootFlags) *cobra.Command {
	var (
		window = timeWindow{Since: "7d"}
		format string
		dbPath string
	)
	cmd := &cobra.Command{
		Use:   "digest",
		Short: "One markdown recap of a week's videos with Loom's summaries, chapter links and action items.",
		Long: strings.TrimSpace(`
Roll up the videos recorded in a time window into one Markdown (or JSON)
document with Loom's AI summary, chapters with ?t= deep links, and action
items, read from the local store.

Use this command to roll up many videos from a time window into one digest of
Loom's summaries, chapters and action items. Do NOT use this command for one
video's full bundle with transcript and files; use 'videos export' instead.

Reads only the local store: run 'sync' for the video list and 'hydrate' for
the AI artifacts. Videos without artifacts are listed with a hint to hydrate.`),
		Example: strings.Trim(`
  loom-pp-cli digest --since 7d
  loom-pp-cli digest --since 14d --until 7d
  loom-pp-cli digest --since 30d --format json
  loom-pp-cli digest --since 7d --agent`, "\n"),
		Annotations: map[string]string{
			"mcp:read-only":  "true",
			"pp:data-source": "local",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "digest")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if len(args) > 0 {
				return usageErr(fmt.Errorf("digest takes no arguments (got %q); use --since and --until to choose the window", strings.Join(args, " ")))
			}
			since := window.Since
			if strings.TrimSpace(since) == "" {
				return usageErr(fmt.Errorf("--since must be a positive window such as 7d"))
			}
			now := time.Now().UTC()
			bounds, err := window.bounds(now)
			if err != nil {
				return err
			}
			if bounds.End.IsZero() {
				bounds.End = now
			}
			asJSON := wantsMachineOutput(flags)
			if cmd.Flags().Changed("format") {
				switch strings.ToLower(format) {
				case "md", "markdown":
					asJSON = false
				case "json":
					asJSON = true
				default:
					return usageErr(fmt.Errorf("--format must be md or json (got %q)", format))
				}
			}
			res := digestResult{
				Since:  bounds.Start.UTC().Format(time.RFC3339),
				Until:  bounds.End.UTC().Format(time.RFC3339),
				Window: since,
				Videos: make([]digestVideo, 0),
			}
			output := func() error {
				if asJSON {
					return printJSONFiltered(cmd.OutOrStdout(), res, flags)
				}
				return writeHuman(cmd, func(w io.Writer) error {
					_, err := io.WriteString(w, renderDigestMarkdown(res))
					return err
				})
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			dbPath, ok := localStoreExists(dbPath)
			if !ok {
				missingMirrorHint(cmd.ErrOrStderr(), dbPath, "loom-pp-cli sync --resources videos --db "+dbPath, "loom-pp-cli hydrate --since "+since+" --db "+dbPath)
				return output()
			}
			db, err := openLoomStore(ctx, dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			videos, err := db.LocalVideos(ctx, res.Since)
			if err != nil {
				return fmt.Errorf("reading local videos: %w", err)
			}
			// LocalVideos is newest first; a recap reads oldest first.
			for i := len(videos) - 1; i >= 0; i-- {
				v := videos[i]
				// The store query already applied the lower bound; the
				// window check also drops rows with unparseable times.
				if t, ok := loom.ParseTime(v.CreatedAt); !ok || !bounds.contains(t) {
					continue
				}
				a, err := db.GetArtifacts(ctx, v.ID)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("reading artifacts for a video: %w", err)
				}
				if err != nil {
					a = nil
				}
				d := digestVideoFrom(v, a, since)
				if !d.Hydrated {
					res.MissingArtifacts++
				}
				res.Videos = append(res.Videos, d)
			}
			if len(res.Videos) == 0 {
				// Match hydrate: an empty store gets a sync hint, not just an
				// empty recap that looks like a quiet week.
				if any, err := db.HasLocalVideos(ctx); err == nil && !any {
					fmt.Fprintln(cmd.ErrOrStderr(), "no synced videos in the local store\nrun: loom-pp-cli sync --resources videos --db "+dbPath)
				}
			}
			if res.MissingArtifacts > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "%d of %d videos are not hydrated yet\nrun: loom-pp-cli hydrate --since %s\n", res.MissingArtifacts, len(res.Videos), since)
			}
			return output()
		},
	}
	registerWindowFlags(cmd, &window, "until", "Start of the window, as a look-back from now (e.g. 7d, 2w, 30d)", "End of the window, as a look-back from now (default: now)")
	cmd.Flags().StringVar(&format, "format", "md", "Output format: md (Markdown) or json; --json/--agent also select JSON")
	cmd.Flags().StringVar(&dbPath, "db", "", "Path to the local SQLite store (default: the CLI data directory)")
	return cmd
}
