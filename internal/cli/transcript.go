package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
	"loom-pp-cli/internal/store"
)

// transcriptSegmentView is one phrase as the transcript command prints it.
type transcriptSegmentView struct {
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Speaker   int     `json:"speaker"`
	Text      string  `json:"text"`
	Timestamp string  `json:"timestamp"`
	Link      string  `json:"link"`
}

// transcriptView is the machine output of `transcript`.
type transcriptView struct {
	VideoID  string                  `json:"video_id"`
	Version  int                     `json:"version"`
	Language string                  `json:"language"`
	Status   string                  `json:"status"`
	Segments []transcriptSegmentView `json:"segments"`
	Text     string                  `json:"text"`
}

var transcriptFormats = map[string]bool{"text": true, "timestamped": true, "json": true, "srt": true, "vtt": true}

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newTranscriptCmd(flags))
	})
}

func newTranscriptCmd(flags *rootFlags) *cobra.Command {
	var format string
	var speakers bool
	var dbPath string
	cmd := &cobra.Command{
		Use:   "transcript [video]",
		Short: "Print a video's transcript as text, timestamps, JSON, SRT or VTT",
		Long: `Print a video's transcript. The video is a 32-hex id, a loom.com share or
embed URL, or "latest" for your newest recording.

Formats: timestamped (default in a terminal: "[m:ss] text" lines), text,
json, srt and vtt. An explicit --format other than json always prints that
format, even when stdout is piped; without --format, piped or --json output
is a JSON object with segments, deep links and the joined text.

When Loom is still transcribing, the status is "pending" and the command
exits 0 with a hint to retry. --data-source local reads a transcript saved by
'hydrate'; a live fetch refreshes that copy when the local store exists.`,
		Example: strings.Trim(`
  loom-pp-cli transcript latest
  loom-pp-cli transcript 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --format text
  loom-pp-cli transcript https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --format srt > talk.srt
  loom-pp-cli transcript latest --json | jq '.segments[0]'
  loom-pp-cli transcript latest --speakers`, "\n"),
		Annotations: videoReadAnnotations(sourceAuto),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "transcript")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			format = strings.ToLower(strings.TrimSpace(format))
			if format == "" {
				format = "timestamped"
			}
			if !transcriptFormats[format] {
				return usageErr(fmt.Errorf("--format must be one of text, timestamped, json, srt, vtt (got %q)", format))
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[*loom.Transcript]{
				what: "transcript",
				live: func(ctx context.Context, lc *loom.Client, id string) (*loom.Transcript, error) {
					return lc.FetchTranscript(ctx, id)
				},
				local: localTranscript,
				empty: func(id string) *loom.Transcript {
					return &loom.Transcript{VideoID: id, Status: "unavailable", Segments: make([]loom.Segment, 0)}
				},
				save: saveTranscriptThrough,
			})
			if err != nil {
				return err
			}
			view := buildTranscriptView(res.ID, res.Value, speakers)
			pending := len(view.Segments) == 0 && view.Status == "pending"
			out := cmd.OutOrStdout()

			explicitRaw := cmd.Flags().Changed("format") && format != "json"
			if explicitRaw {
				if res.Missing {
					return nil
				}
				if pending {
					fmt.Fprintln(cmd.ErrOrStderr(), "transcript pending: Loom is still transcribing this video; retry in a minute")
					return nil
				}
				if len(view.Segments) == 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "no transcript text (status: %s)\n", view.Status)
					return nil
				}
				// Raw formats are human output too: transcript text is
				// untrusted, so it goes through the terminal-safe writer.
				return writeHuman(cmd, func(w io.Writer) error {
					_, err := io.WriteString(w, renderTranscript(format, res.Value.Segments, speakers))
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
				if pending {
					fmt.Fprintln(w, "Transcript pending: Loom is still transcribing this video. Retry in a minute.")
					return nil
				}
				if len(view.Segments) == 0 {
					fmt.Fprintf(w, "No transcript text (status: %s).\n", view.Status)
					return nil
				}
				_, err := io.WriteString(w, renderTranscript("timestamped", res.Value.Segments, speakers))
				return err
			})
		},
	}
	cmd.Flags().StringVar(&format, "format", "timestamped", "Output format: text, timestamped, json, srt or vtt")
	cmd.Flags().BoolVar(&speakers, "speakers", false, "Prefix lines with \"Speaker N:\" when more than one speaker appears")
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}

// pendingTranscriptStatuses are Loom transcription states that mean "not
// ready yet".
var pendingTranscriptStatuses = map[string]bool{
	"pending": true, "in_progress": true, "processing": true, "queued": true,
	"started": true, "transcribing": true, "not_started": true, "waiting": true,
}

// normalizeTranscriptStatus lowercases Loom's status and folds the
// still-working states into "pending".
func normalizeTranscriptStatus(status string, segments int) string {
	s := strings.ToLower(strings.TrimSpace(status))
	if pendingTranscriptStatuses[s] {
		if segments > 0 {
			return s
		}
		return "pending"
	}
	if s == "" {
		if segments > 0 {
			return "success"
		}
		return "pending"
	}
	return s
}

// hasMultipleSpeakers reports whether more than one speaker index appears.
func hasMultipleSpeakers(segs []loom.Segment) bool {
	for i := 1; i < len(segs); i++ {
		if segs[i].Speaker != segs[0].Speaker {
			return true
		}
	}
	return false
}

// speakerLines returns the segments with "Speaker N: " prefixed to the text
// when speakers is set and more than one speaker appears. Speaker indexes
// are zero-based in Loom's data and printed one-based.
func speakerLines(segs []loom.Segment, speakers bool) []loom.Segment {
	if !speakers || !hasMultipleSpeakers(segs) {
		return segs
	}
	out := make([]loom.Segment, len(segs))
	for i, s := range segs {
		s.Text = fmt.Sprintf("Speaker %d: %s", s.Speaker+1, s.Text)
		out[i] = s
	}
	return out
}

// renderTranscript renders segments in one of the raw formats.
func renderTranscript(format string, segs []loom.Segment, speakers bool) string {
	segs = speakerLines(segs, speakers)
	switch format {
	case "srt":
		return loom.FormatSRT(segs)
	case "vtt":
		return loom.FormatVTT(segs)
	case "text":
		var b strings.Builder
		for _, s := range segs {
			b.WriteString(s.Text)
			b.WriteByte('\n')
		}
		return b.String()
	default:
		return loom.FormatTimestamped(segs)
	}
}

// buildTranscriptView shapes a transcript for JSON output.
func buildTranscriptView(id string, t *loom.Transcript, speakers bool) transcriptView {
	view := transcriptView{VideoID: id, Segments: make([]transcriptSegmentView, 0)}
	if t == nil {
		view.Status = "unavailable"
		return view
	}
	if t.VideoID != "" {
		view.VideoID = t.VideoID
	}
	view.Version, view.Language = t.Version, t.Language
	view.Status = t.Status
	if view.Status != "unavailable" {
		view.Status = normalizeTranscriptStatus(t.Status, len(t.Segments))
	}
	for _, s := range t.Segments {
		view.Segments = append(view.Segments, transcriptSegmentView{
			Start: s.Start, End: s.End, Speaker: s.Speaker, Text: s.Text,
			Timestamp: loom.FormatClock(s.Start), Link: loom.DeepLink(view.VideoID, s.Start),
		})
	}
	lines := speakerLines(t.Segments, speakers)
	parts := make([]string, 0, len(lines))
	for _, s := range lines {
		parts = append(parts, s.Text)
	}
	view.Text = strings.Join(parts, "\n")
	return view
}

// localTranscript reads a hydrated transcript from the store.
func localTranscript(ctx context.Context, db *store.Store, id string) (*loom.Transcript, error) {
	row, phrases, err := db.GetTranscript(ctx, id)
	if err != nil {
		return nil, err
	}
	t := &loom.Transcript{VideoID: id, Version: row.Version, Language: row.Language, Status: row.Status, Segments: make([]loom.Segment, 0, len(phrases))}
	for _, p := range phrases {
		t.Segments = append(t.Segments, loom.Segment{Start: p.Start, End: p.End, Speaker: p.Speaker, Text: p.Text})
	}
	return t, nil
}

// saveTranscriptThrough stores a live transcript. Pending transcripts are
// skipped so they never replace a complete stored copy.
func saveTranscriptThrough(ctx context.Context, db *store.Store, id string, t *loom.Transcript) error {
	if t == nil || len(t.Segments) == 0 {
		return nil
	}
	phrases := make([]store.PhraseRow, 0, len(t.Segments))
	for _, s := range t.Segments {
		phrases = append(phrases, store.PhraseRow{Start: s.Start, End: s.End, Speaker: s.Speaker, Text: s.Text})
	}
	status := t.Status
	if status == "" {
		status = "success"
	}
	return db.SaveTranscript(ctx, store.TranscriptRow{VideoID: id, Version: t.Version, Language: t.Language, Status: status, Text: t.Text()}, phrases)
}
