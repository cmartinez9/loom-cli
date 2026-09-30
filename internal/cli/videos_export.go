package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/cliutil"
	"loom-pp-cli/internal/loom"
)

// exportBundle is everything exported for one video. The Markdown file is
// rendered from it; --format json writes it as-is.
type exportBundle struct {
	Video            loom.Video     `json:"video"`
	Summary          string         `json:"summary"`
	SummaryStatus    string         `json:"summary_status,omitempty"`
	Chapters         []loom.Chapter `json:"chapters"`
	ActionItems      []loom.Task    `json:"action_items"`
	Takeaways        []string       `json:"takeaways"`
	Comments         []loom.Comment `json:"comments"`
	TranscriptStatus string         `json:"transcript_status,omitempty"`
	Transcript       []loom.Segment `json:"transcript"`
	Missing          []string       `json:"missing,omitempty"`
}

type exportItem struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Bytes  int    `json:"bytes,omitempty"`
	Error  string `json:"error,omitempty"`
}

type exportReport struct {
	OutDir  string       `json:"out_dir"`
	Format  string       `json:"format"`
	Results []exportItem `json:"results"`
	Written int          `json:"written"`
	Skipped int          `json:"skipped"`
	Failed  int          `json:"failed"`
	// NotAttempted counts videos left untouched because the run stopped
	// early (expired session, rate limit, timeout); each is also listed in
	// Results with status "not_attempted".
	NotAttempted  int           `json:"not_attempted"`
	FetchFailures []itemFailure `json:"fetch_failures,omitempty"`
	// Capped is set when --all/--since/--folder stopped at --max-pages
	// with older videos left unselected.
	Capped bool   `json:"capped,omitempty"`
	Note   string `json:"note,omitempty"`
}

func newVideosExportCmd(flags *rootFlags) *cobra.Command {
	var (
		out    string
		format string
		force  bool
		sel    videoSelector
	)
	cmd := &cobra.Command{
		Use:   "export [video]...",
		Short: "Save a Markdown bundle per video: metadata, summary, chapters, comments and transcript",
		Long: `Write one file per video into --out named <created-date>-<slug>-<id8>.md
with the video's metadata, Loom's AI summary, chapters (with ?t= deep links),
action items, key takeaways (when Loom has them), comments and the
timestamped transcript. --format json writes the same content as .json.

Select videos by id, share URL or 'latest', from a file (--from-file), or in
bulk with --all, --since, --before, --folder and --match (--limit caps the
scan). Re-running is idempotent: a file whose content is unchanged is
skipped (use --force to rewrite it). Each video reports written, skipped,
failed or not_attempted; one failure (including one video running past its
API budget) does not stop the others.

Exit codes: 0 when at least one video was exported (failures are listed),
the failures' own code when every video failed (3 when every video was
missing, 5 otherwise), and 4 or 7 when an expired session or rate limiting
stopped the run. An explicit --timeout bounds the whole run (exit 5 when it
runs out); without it each video gets its own API budget.

For a rollup of many videos' summaries in one document, use 'loom-pp-cli digest'.`,
		Example: strings.Trim(`
  loom-pp-cli videos export latest
  loom-pp-cli videos export 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --out ./notes
  loom-pp-cli videos export --since 30d --format json --json
  loom-pp-cli videos export --all --dry-run`, "\n"),
		Annotations: map[string]string{"pp:data-source": "live", "pp:happy-args": "video=latest"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			format = strings.ToLower(strings.TrimSpace(format))
			if format == "markdown" {
				format = "md"
			}
			// Input validation needs no network, so bad input exits 2 even
			// under --dry-run.
			if format != "md" && format != "json" {
				return usageErr(fmt.Errorf("--format %q must be md or json", format))
			}
			if err := sel.validate(args); err != nil {
				return err
			}
			if dryRunOK(flags) {
				plan := transferPlan{DryRun: true, Action: "videos export", Targets: sel.describe(args), OutDir: out, Format: format, Force: force,
					Would: "fetch each video's metadata, summary, chapters, action items, takeaways, comments and transcript and write one file per video; nothing was fetched or written"}
				return emit(cmd, flags, plan, func(w io.Writer) error {
					fmt.Fprintf(w, "dry-run: would export %s to %s as %s\n", strings.Join(plan.Targets, ", "), out, format)
					return nil
				})
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if !sel.hasInput(args) {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("name at least one <video> (id, share URL or 'latest'), or use --from-file, --all, --since, --before, --folder or --match"))
			}
			if cliutil.IsAnyHarness() {
				return writeHarnessRefusal(cmd.OutOrStdout(), flags, "videos export (writes files)")
			}
			// The selection runs under the run context; each lookup gets
			// its own API budget inside selectVideos.
			runCtx, cancelRun := runContext(cmd, flags)
			defer cancelRun()
			found, err := selectVideos(runCtx, flags, args, sel, selectOpts{details: true})
			if err != nil {
				return loomErr(err)
			}
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(out, 0o700); err != nil {
				return fmt.Errorf("creating --out %s: %w", out, err)
			}
			report := exportReport{OutDir: out, Format: format, Results: make([]exportItem, 0, len(found.Videos)+len(found.Failures))}
			if found.Capped {
				report.Capped, report.Note = true, capNote(found.MaxPages, "videos")
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", report.Note)
			}
			outcome := bulkOutcome{what: "export"}
			for _, f := range found.Failures {
				report.Results = append(report.Results, exportItem{ID: f.ID, Status: "failed", Error: f.Error})
				report.FetchFailures = append(report.FetchFailures, f)
				report.Failed++
				outcome.fail(f.err)
			}
			for _, v := range found.Videos {
				item := exportItem{ID: v.ID, Name: v.Name}
				if outcome.stopped() {
					// The run stopped: list what was never tried.
					item.Status, item.Error = statusNotAttempted, outcome.notAttemptedMsg()
					report.Results = append(report.Results, item)
					report.NotAttempted++
					continue
				}
				// Each video gets its own API budget so a long bulk export is
				// not cut off by one command-wide deadline.
				vctx, vcancel := boundCtx(runCtx, flags)
				bundle, err := buildExportBundle(vctx, lc, v)
				vcancel()
				if err == nil {
					item.Path, item.Status, item.Bytes, err = writeExportBundle(out, format, bundle, force)
				}
				switch {
				case err != nil:
					item.Status, item.Error = "failed", err.Error()
					report.FetchFailures = append(report.FetchFailures, itemFailure{ID: v.ID, Error: err.Error(), err: err})
					report.Failed++
					outcome.fail(err)
					if stop := stopCause(runCtx, err); stop != nil {
						outcome.halt(stop)
					}
				case item.Status == "skipped":
					report.Skipped++
					outcome.ok()
				default:
					report.Written++
					outcome.ok()
				}
				report.Results = append(report.Results, item)
			}
			total := len(report.Results)
			if report.Failed > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d of %d exports failed; %d written, %d skipped\n", report.Failed, total, report.Written, report.Skipped)
			}
			if report.NotAttempted > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: stopped early; %d videos were not attempted (re-run to continue)\n", report.NotAttempted)
			}
			if err := emit(cmd, flags, report, func(w io.Writer) error {
				for _, r := range report.Results {
					switch r.Status {
					case "failed":
						fmt.Fprintf(w, "failed   %s  %s\n", r.ID, r.Error)
					case statusNotAttempted:
						fmt.Fprintf(w, "not attempted %s  %s\n", r.ID, r.Name)
					default:
						fmt.Fprintf(w, "%-8s %s\n", r.Status, r.Path)
					}
				}
				fmt.Fprintf(w, "%d written, %d skipped, %d failed, %d not attempted\n", report.Written, report.Skipped, report.Failed, report.NotAttempted)
				return nil
			}); err != nil {
				return err
			}
			return outcome.exitErr()
		},
	}
	cmd.Flags().StringVar(&out, "out", "./loom-export", "Directory to write export files into")
	cmd.Flags().StringVar(&format, "format", "md", "File format: md (Markdown) or json")
	cmd.Flags().BoolVar(&force, "force", false, "Rewrite files even when their content is unchanged")
	registerVideoSelector(cmd, &sel, selectorOpts{})
	return cmd
}

// firstLine returns the first line of s (errors carry hints on later lines).
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// buildExportBundle fetches every section for one video. A section Loom
// does not have is recorded in Missing; a signed-out session or rate limit
// fails the video.
func buildExportBundle(ctx context.Context, lc *loom.Client, v loom.Video) (*exportBundle, error) {
	b := &exportBundle{Video: v, Chapters: make([]loom.Chapter, 0), ActionItems: make([]loom.Task, 0), Takeaways: make([]string, 0),
		Comments: make([]loom.Comment, 0), Transcript: make([]loom.Segment, 0)}
	a, missing, err := lc.FetchArtifacts(ctx, v.ID, loom.ArtifactOptions{Summary: true, Chapters: true, ActionItems: true, Takeaways: true, Comments: true, Transcript: true})
	if err != nil {
		return nil, err
	}
	for _, m := range missing {
		b.Missing = append(b.Missing, strings.ReplaceAll(m.Section, "_", " ")+": "+m.Err.Error())
	}
	if a.Summary != nil {
		b.Summary, b.SummaryStatus = a.Summary.Summary, a.Summary.Status
	}
	if a.Chapters != nil {
		b.Chapters = a.Chapters.Chapters
	}
	if a.ActionItems != nil {
		b.ActionItems = a.ActionItems
	}
	if a.Takeaways != nil {
		b.Takeaways = a.Takeaways.Takeaways
	}
	if a.Comments != nil {
		b.Comments = a.Comments
	}
	if a.Transcript != nil {
		b.Transcript, b.TranscriptStatus = a.Transcript.Segments, a.Transcript.Status
	}
	return b, nil
}

// writeExportBundle renders and writes one bundle, skipping an unchanged file
// unless force. It returns the path, status (written or skipped) and size.
func writeExportBundle(dir, format string, b *exportBundle, force bool) (string, string, int, error) {
	var content []byte
	base, err := videoFileBase(b.Video)
	if err != nil {
		return "", "", 0, err
	}
	path := filepath.Join(dir, base+"."+format)
	if format == "json" {
		buf, err := json.MarshalIndent(b, "", "  ")
		if err != nil {
			return "", "", 0, err
		}
		content = append(buf, '\n')
	} else {
		content = []byte(renderExportMarkdown(b))
	}
	status, err := writeIfChanged(path, content, force)
	return path, status, len(content), err
}

// writeIfChanged writes content atomically unless an identical file
// (same SHA-256) exists and force is false.
func writeIfChanged(path string, content []byte, force bool) (string, error) {
	if !force {
		if existing, err := os.ReadFile(filepath.Clean(path)); err == nil && sha256.Sum256(existing) == sha256.Sum256(content) {
			return "skipped", nil
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".export-*")
	if err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return "written", nil
}

// renderExportMarkdown renders a bundle as a Markdown document. The output is
// deterministic for the same data so re-exports can be skipped by hash.
func renderExportMarkdown(b *exportBundle) string {
	var w bytes.Buffer
	v := b.Video
	id := v.ID
	title := strings.TrimSpace(v.Name)
	if title == "" {
		title = "Untitled video"
	}
	fmt.Fprintf(&w, "# %s\n\n", mdInline(title))
	fmt.Fprintf(&w, "- **Video:** %s\n", v.ShareURL)
	if v.CreatedAt != "" {
		fmt.Fprintf(&w, "- **Recorded:** %s\n", v.CreatedAt)
	}
	fmt.Fprintf(&w, "- **Duration:** %s\n", v.Duration)
	if v.OwnerName != "" {
		fmt.Fprintf(&w, "- **Owner:** %s\n", mdInline(v.OwnerName))
	}
	if v.FolderName != "" {
		fmt.Fprintf(&w, "- **Folder:** %s\n", mdInline(v.FolderName))
	}
	fmt.Fprintf(&w, "- **Views:** %d · **Comments:** %d · **Reactions:** %d\n", v.Views, v.Comments, v.Reactions)
	if len(v.Tags) > 0 {
		tags := make([]string, 0, len(v.Tags))
		for _, t := range v.Tags {
			tags = append(tags, mdInline(t))
		}
		fmt.Fprintf(&w, "- **Tags:** %s\n", strings.Join(tags, ", "))
	}
	fmt.Fprintf(&w, "- **ID:** %s\n", id)
	if d := strings.TrimSpace(v.Description); d != "" {
		fmt.Fprintf(&w, "\n## Description\n\n%s\n", d)
	}

	w.WriteString("\n## Summary\n\n")
	if s := strings.TrimSpace(b.Summary); s != "" {
		fmt.Fprintf(&w, "%s\n", s)
	} else if b.SummaryStatus != "" {
		fmt.Fprintf(&w, "_No AI summary available (status: %s)._\n", b.SummaryStatus)
	} else {
		w.WriteString("_No AI summary available._\n")
	}

	w.WriteString("\n## Chapters\n\n")
	if len(b.Chapters) == 0 {
		w.WriteString("_No chapters._\n")
	}
	for _, c := range b.Chapters {
		w.WriteString(mdChapterLine(c.Timestamp, c.Link, c.Title))
	}

	w.WriteString("\n## Action items\n\n")
	if len(b.ActionItems) == 0 {
		w.WriteString("_No action items._\n")
	}
	for _, t := range b.ActionItems {
		w.WriteString(mdTaskLine(t))
	}

	if len(b.Takeaways) > 0 {
		w.WriteString("\n## Key takeaways\n\n")
		for _, t := range b.Takeaways {
			w.WriteString(mdBullet(t))
		}
	}

	w.WriteString("\n## Comments\n\n")
	if len(b.Comments) == 0 {
		w.WriteString("_No comments._\n")
	}
	loom.WalkComments(b.Comments, func(c loom.Comment, depth int) {
		fmt.Fprintf(&w, "%s- **%s** at [%s](%s) (%s): %s\n", strings.Repeat("  ", depth), mdInline(c.AuthorLabel()), c.Timestamp, c.Link, formatDate(c.CreatedAt), mdInline(c.Text))
	})

	w.WriteString("\n## Transcript\n\n")
	if len(b.Transcript) == 0 {
		if b.TranscriptStatus != "" {
			fmt.Fprintf(&w, "_No transcript available (status: %s)._\n", b.TranscriptStatus)
		} else {
			w.WriteString("_No transcript available._\n")
		}
	}
	for _, s := range b.Transcript {
		fmt.Fprintf(&w, "- [%s](%s) %s\n", loom.FormatClock(s.Start), loom.DeepLink(id, s.Start), mdInline(s.Text))
	}

	if len(b.Missing) > 0 {
		w.WriteString("\n## Not available\n\n")
		for _, m := range b.Missing {
			w.WriteString(mdBullet(m))
		}
	}
	return w.String()
}
