// Copyright 2026 cmartinez9 and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source local

package cli

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
	"loom-pp-cli/internal/store"
)

// grepRow is one spoken-word match.
type grepRow struct {
	VideoID   string  `json:"video_id"`
	VideoName string  `json:"video_name"`
	CreatedAt string  `json:"created_at"`
	Timestamp string  `json:"timestamp"`
	Start     float64 `json:"start"`
	Text      string  `json:"text"`
	Context   string  `json:"context"`
	Link      string  `json:"link"`
}

func newNovelGrepCmd(flags *rootFlags) *cobra.Command {
	var (
		anyWord  bool
		limit    int
		contextN int
		videoArg string
		since    string
		dbPath   string
	)
	cmd := &cobra.Command{
		Use:   "grep [phrase]",
		Short: "Find the exact moment you said something in any of your videos, with a link that jumps straight to it.",
		Long: strings.TrimSpace(`
Search every hydrated transcript for a spoken phrase and return the video,
the h:mm:ss position and a share link that starts playback at that moment.

Use this command to find the moment a phrase was spoken across your library,
with timestamped deep links. Do NOT use this command to find videos, folders,
spaces or people by title or name; use 'videos search' instead, or 'search'
for title/description matches in the local store.

A multi-word argument matches as an exact phrase; --any matches any of the
words. Matching is case-insensitive and stemmed ("pricing" also finds
"priced"). Search operators in the input are treated as plain words.
Transcripts come from the local store: run 'hydrate' first.`),
		Example: strings.Trim(`
  loom-pp-cli grep "pricing experiment" --agent
  loom-pp-cli grep "funnel" --agent --select video_name,timestamp,link
  loom-pp-cli grep onboarding checklist --any --since 90d
  loom-pp-cli grep "roadmap" --video 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --context 2`, "\n"),
		Annotations: map[string]string{
			"mcp:read-only":          "true",
			"pp:data-source":         "local",
			"pp:happy-args":          "phrase=the",
			"pp:no-error-path-probe": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "grep")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			phrase := strings.TrimSpace(strings.Join(args, " "))
			if phrase == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a phrase to search for is required, e.g. loom-pp-cli grep \"pricing experiment\""))
			}
			match, err := grepFTSQuery(phrase, anyWord)
			if err != nil {
				return usageErr(err)
			}
			if limit <= 0 {
				return usageErr(fmt.Errorf("--limit must be at least 1 (got %d)", limit))
			}
			if contextN < 0 || contextN > 20 {
				return usageErr(fmt.Errorf("--context must be between 0 and 20 (got %d)", contextN))
			}
			bounds, err := timeWindow{Since: since}.bounds(time.Now())
			if err != nil {
				return err
			}
			videoID := ""
			if strings.TrimSpace(videoArg) != "" {
				if videoID, err = videoIDArg(videoArg); err != nil {
					return err
				}
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			dbPath, ok := localStoreExists(dbPath)
			if !ok {
				missingMirrorHint(cmd.ErrOrStderr(), dbPath, "loom-pp-cli sync --resources videos --db "+dbPath, "loom-pp-cli hydrate --db "+dbPath)
				return emitGrepRows(cmd, flags, make([]grepRow, 0))
			}
			db, err := openLoomStore(ctx, dbPath)
			if err != nil {
				return err
			}
			defer db.Close()
			n, err := db.HydratedTranscriptCount(ctx)
			if err != nil {
				return fmt.Errorf("counting hydrated transcripts: %w", err)
			}
			if n == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "no hydrated transcripts in the local store\nrun: loom-pp-cli hydrate --since 90d")
				return emitGrepRows(cmd, flags, make([]grepRow, 0))
			}
			hits, err := db.SearchPhrases(ctx, store.PhraseSearch{
				Match:   match,
				VideoID: videoID,
				Since:   bounds.startRFC3339(),
				Limit:   limit,
			})
			if err != nil {
				return fmt.Errorf("searching transcripts: %w", err)
			}
			rows := make([]grepRow, 0, len(hits))
			for _, h := range hits {
				ctxText := h.Text
				if contextN > 0 {
					window, err := db.PhraseWindow(ctx, h.VideoID, h.Seq, contextN, contextN)
					if err != nil {
						return fmt.Errorf("reading transcript context: %w", err)
					}
					ctxText = grepJoinContext(window)
				}
				rows = append(rows, grepRow{
					VideoID:   h.VideoID,
					VideoName: h.VideoName,
					CreatedAt: h.CreatedAt,
					Timestamp: loom.FormatClock(h.Start),
					Start:     h.Start,
					Text:      h.Text,
					Context:   ctxText,
					Link:      loom.DeepLink(h.VideoID, h.Start),
				})
			}
			return emitGrepRows(cmd, flags, rows, grepFTSTokens(phrase)...)
		},
	}
	cmd.Flags().BoolVar(&anyWord, "any", false, "Match phrases containing any of the words instead of the exact phrase")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of matches to return, best match first")
	cmd.Flags().IntVar(&contextN, "context", 1, "Number of neighbouring transcript phrases to include before and after each match")
	cmd.Flags().StringVar(&videoArg, "video", "", "Only search one video (32-hex id or loom.com share URL)")
	cmd.Flags().StringVar(&since, "since", "", "Only search videos created within this window (e.g. 30d, 2w, 12h)")
	cmd.Flags().StringVar(&dbPath, "db", "", "Path to the local SQLite store (default: the CLI data directory)")
	return cmd
}

func emitGrepRows(cmd *cobra.Command, flags *rootFlags, rows []grepRow, terms ...string) error {
	return emit(cmd, flags, rows, func(w io.Writer) error {
		if len(rows) == 0 {
			fmt.Fprintln(w, "No matches found.")
			return nil
		}
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "TIME\tVIDEO\tTEXT\tLINK")
		for _, r := range rows {
			text := strings.Join(strings.Fields(r.Text), " ")
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Timestamp, truncate(r.VideoName, 40), grepSnippet(text, terms, 70), r.Link)
		}
		return tw.Flush()
	})
}

// grepSnippet returns at most width runes of text, centred on the first
// occurrence of any search term (case-insensitive, falling back to the
// term's stem-like prefix since matching is stemmed), with an ellipsis on
// each side that was cut.
func grepSnippet(text string, terms []string, width int) string {
	runes := []rune(text)
	if width <= 0 || len(runes) <= width {
		return text
	}
	lower := make([]rune, len(runes))
	for i, r := range runes {
		lower[i] = unicode.ToLower(r)
	}
	find := func(needle []rune) int {
		if len(needle) == 0 || len(needle) > len(lower) {
			return -1
		}
		for i := 0; i+len(needle) <= len(lower); i++ {
			match := true
			for j, r := range needle {
				if lower[i+j] != r {
					match = false
					break
				}
			}
			if match {
				return i
			}
		}
		return -1
	}
	at, atLen := -1, 0
	for _, t := range terms {
		needle := []rune(strings.ToLower(t))
		idx := find(needle)
		if idx < 0 && len(needle) > 4 {
			needle = needle[:4]
			idx = find(needle)
		}
		if idx >= 0 && (at < 0 || idx < at) {
			at, atLen = idx, len(needle)
		}
	}
	if at < 0 {
		at = 0
	}
	inner := width - 2 // room for the two ellipses
	if inner < 1 {
		inner = 1
	}
	start := at + atLen/2 - inner/2
	if start < 0 {
		start = 0
	}
	if start+inner > len(runes) {
		start = len(runes) - inner
	}
	end := start + inner
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

// grepJoinContext joins neighbouring phrases into one readable excerpt.
func grepJoinContext(phrases []string) string {
	parts := make([]string, 0, len(phrases))
	for _, p := range phrases {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

// grepFTSTokens splits user input into the word tokens SQLite's unicode61
// tokenizer would produce: runs of letters and digits. Every other rune
// (quotes, *, -, :, parentheses, ^, +) is a separator, so no FTS5 syntax
// survives.
func grepFTSTokens(input string) []string {
	return strings.FieldsFunc(input, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.Is(unicode.Mn, r)
	})
}

// grepFTSQuery turns free text into a safe FTS5 MATCH expression. Every
// token is wrapped in double quotes so words like AND, OR, NOT and NEAR are
// searched literally. Multi-word input becomes one quoted phrase unless
// anyWord is set, in which case the words are OR-ed.
func grepFTSQuery(input string, anyWord bool) (string, error) {
	tokens := grepFTSTokens(input)
	if len(tokens) == 0 {
		return "", fmt.Errorf("phrase %q has no searchable words (letters or digits)", input)
	}
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	if anyWord {
		seen := map[string]bool{}
		parts := make([]string, 0, len(tokens))
		for _, t := range tokens {
			k := strings.ToLower(t)
			if seen[k] {
				continue
			}
			seen[k] = true
			parts = append(parts, quote(t))
		}
		return strings.Join(parts, " OR "), nil
	}
	return quote(strings.Join(tokens, " ")), nil
}
