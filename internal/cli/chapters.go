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

// chapterView is one chapter as `chapters` prints it.
type chapterView struct {
	Start     float64 `json:"start"`
	Timestamp string  `json:"timestamp"`
	Title     string  `json:"title"`
	Link      string  `json:"link"`
}

// chaptersView is the machine output of `chapters`.
type chaptersView struct {
	VideoID  string        `json:"video_id"`
	Status   string        `json:"status"`
	Edited   bool          `json:"edited"`
	Chapters []chapterView `json:"chapters"`
}

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newChaptersCmd(flags))
	})
}

func newChaptersCmd(flags *rootFlags) *cobra.Command {
	var format string
	var dbPath string
	cmd := &cobra.Command{
		Use:   "chapters [video]",
		Short: "List Loom's AI chapters for a video with timestamped deep links",
		Long: `List Loom's AI chapters for a video. Each chapter has its start time and a
share link that starts playback there (?t=<seconds>).

Human output is "m:ss  Title  link" lines; --format md prints a markdown
bullet list (always, even when piped); --json prints
{video_id, status, edited, chapters}. --data-source local reads chapters
saved by 'hydrate'.`,
		Example: strings.Trim(`
  loom-pp-cli chapters latest
  loom-pp-cli chapters 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --format md
  loom-pp-cli chapters https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json`, "\n"),
		Annotations: videoReadAnnotations(sourceAuto),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "chapters")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			format = strings.ToLower(strings.TrimSpace(format))
			switch format {
			case "", "text":
				format = "text"
			case "md", "markdown":
				format = "md"
			case "json":
			default:
				return usageErr(fmt.Errorf("--format must be text, md or json (got %q)", format))
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[*loom.Chapters]{
				what: "chapters",
				live: func(ctx context.Context, lc *loom.Client, id string) (*loom.Chapters, error) {
					return lc.FetchChapters(ctx, id)
				},
				local: localChapters,
				empty: func(id string) *loom.Chapters {
					return &loom.Chapters{VideoID: id, Status: "unavailable", Chapters: make([]loom.Chapter, 0)}
				},
				save: func(ctx context.Context, db *store.Store, id string, c *loom.Chapters) error {
					return saveArtifactPatch(ctx, db, id, artifactPatch{Chapters: mustJSON(c.Chapters), ChaptersStatus: c.Status})
				},
			})
			if err != nil {
				return err
			}
			view := buildChaptersView(res.ID, res.Value)
			out := cmd.OutOrStdout()
			if format == "md" {
				if res.Missing {
					return nil
				}
				return writeHuman(cmd, func(w io.Writer) error {
					_, err := io.WriteString(w, renderChaptersMarkdown(view.Chapters))
					return err
				})
			}
			if format == "json" || !wantsHumanTable(out, flags) {
				return printJSONFiltered(out, view, flags)
			}
			if res.Missing {
				return nil
			}
			return writeHuman(cmd, func(w io.Writer) error {
				if len(view.Chapters) == 0 {
					fmt.Fprintf(w, "No chapters for this video (status: %s).\n", view.Status)
					return nil
				}
				_, err := io.WriteString(w, renderChaptersText(view.Chapters))
				return err
			})
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text, md (markdown bullet list) or json")
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}

// buildChaptersView shapes chapters for output.
func buildChaptersView(id string, c *loom.Chapters) chaptersView {
	view := chaptersView{VideoID: id, Chapters: make([]chapterView, 0)}
	if c == nil {
		view.Status = "unavailable"
		return view
	}
	view.Status, view.Edited = c.Status, c.Edited
	for _, ch := range c.Chapters {
		view.Chapters = append(view.Chapters, chapterView{Start: ch.Start, Timestamp: ch.Timestamp, Title: ch.Title, Link: ch.Link})
	}
	if view.Status == "" {
		view.Status = "none"
		if len(view.Chapters) > 0 {
			view.Status = "success"
		}
	}
	return view
}

// renderChaptersText prints "m:ss  Title  link" lines with aligned columns.
func renderChaptersText(chs []chapterView) string {
	stampW, titleW := 0, 0
	for _, c := range chs {
		stampW = max(stampW, len(c.Timestamp))
		titleW = max(titleW, len([]rune(c.Title)))
	}
	var b strings.Builder
	for _, c := range chs {
		pad := titleW - len([]rune(c.Title))
		fmt.Fprintf(&b, "%*s  %s%s  %s\n", stampW, c.Timestamp, c.Title, strings.Repeat(" ", pad), c.Link)
	}
	return b.String()
}

// renderChaptersMarkdown prints a markdown bullet list with deep links.
func renderChaptersMarkdown(chs []chapterView) string {
	var b strings.Builder
	for _, c := range chs {
		b.WriteString(mdChapterLine(c.Timestamp, c.Link, c.Title))
	}
	return b.String()
}

// localChapters reads chapters saved by hydrate or a previous live fetch.
func localChapters(ctx context.Context, db *store.Store, id string) (*loom.Chapters, error) {
	a, err := localArtifacts(ctx, db, id)
	if err != nil {
		return nil, err
	}
	out := &loom.Chapters{VideoID: id, Status: a.ChaptersStatus, Chapters: make([]loom.Chapter, 0)}
	if len(a.Chapters) > 0 {
		if err := json.Unmarshal(a.Chapters, &out.Chapters); err != nil {
			return nil, fmt.Errorf("decoding stored chapters: %w", err)
		}
	}
	for i := range out.Chapters {
		if out.Chapters[i].Timestamp == "" {
			out.Chapters[i].Timestamp = loom.FormatClock(out.Chapters[i].Start)
		}
		if out.Chapters[i].Link == "" {
			out.Chapters[i].Link = loom.DeepLink(id, out.Chapters[i].Start)
		}
	}
	return out, nil
}
