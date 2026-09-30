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

// flatComment is one comment in --flat output: replies are separate rows
// linked by parent_id.
type flatComment struct {
	ID        string  `json:"id"`
	VideoID   string  `json:"video_id"`
	ParentID  string  `json:"parent_id"`
	Author    string  `json:"author"`
	AuthorID  string  `json:"author_id,omitempty"`
	Text      string  `json:"text"`
	Start     float64 `json:"start"`
	Timestamp string  `json:"timestamp"`
	Link      string  `json:"link"`
	CreatedAt string  `json:"created_at"`
	Edited    bool    `json:"edited"`
	Deleted   bool    `json:"deleted"`
	Reactions int     `json:"reactions"`
	Depth     int     `json:"depth"`
}

func newCommentsListCmd(flags *rootFlags) *cobra.Command {
	var flat, includeDeleted bool
	var limit int
	var dbPath string
	cmd := &cobra.Command{
		Use:   "list [video]",
		Short: "List a video's comments as threads with replies nested",
		Long: `List a video's comments. JSON output is threaded: each top-level comment
carries its replies in "replies". --flat prints one row per comment with
parent_id instead. Deleted comments are hidden unless --include-deleted.
--data-source local reads comments recorded by 'activity'.`,
		Example: strings.Trim(`
  loom-pp-cli comments list latest
  loom-pp-cli comments list 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json
  loom-pp-cli comments list https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --flat --json`, "\n"),
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
				return writeDryRun(cmd.OutOrStdout(), flags, "comments list")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			if limit < 0 {
				return usageErr(fmt.Errorf("--limit must be 0 (all) or more"))
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[[]loom.Comment]{
				what: "comments",
				live: func(ctx context.Context, lc *loom.Client, id string) ([]loom.Comment, error) {
					return lc.FetchComments(ctx, id, includeDeleted)
				},
				local: localComments,
				empty: func(string) []loom.Comment { return make([]loom.Comment, 0) },
			})
			if err != nil {
				return err
			}
			threads := res.Value
			if threads == nil {
				threads = make([]loom.Comment, 0)
			}
			if limit > 0 && len(threads) > limit {
				threads = threads[:limit]
			}
			out := cmd.OutOrStdout()
			if flat {
				rows := flattenCommentThreads(threads)
				if !wantsHumanTable(out, flags) {
					return printJSONFiltered(out, rows, flags)
				}
				return writeHuman(cmd, func(w io.Writer) error {
					if len(rows) == 0 {
						if !res.Missing {
							fmt.Fprintln(w, "No comments on this video.")
						}
						return nil
					}
					_, err := io.WriteString(w, renderCommentThreads(threads))
					return err
				})
			}
			if !wantsHumanTable(out, flags) {
				return printJSONFiltered(out, threads, flags)
			}
			return writeHuman(cmd, func(w io.Writer) error {
				if len(threads) == 0 {
					if !res.Missing {
						fmt.Fprintln(w, "No comments on this video.")
					}
					return nil
				}
				_, err := io.WriteString(w, renderCommentThreads(threads))
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&flat, "flat", false, "Print one row per comment with parent_id instead of nested threads")
	cmd.Flags().BoolVar(&includeDeleted, "include-deleted", false, "Include comments that were deleted")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum top-level threads to return (0 = all)")
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}

// flattenCommentThreads walks threads depth-first into parent_id-linked rows.
func flattenCommentThreads(threads []loom.Comment) []flatComment {
	out := make([]flatComment, 0)
	// path[d] is the id of the comment currently open at depth d, so a reply
	// without a ParentID still links to the comment it is nested under.
	path := make([]string, 0, 4)
	loom.WalkComments(threads, func(c loom.Comment, depth int) {
		path = append(path[:depth], c.ID)
		pid := c.ParentID
		if pid == "" && depth > 0 {
			pid = path[depth-1]
		}
		out = append(out, flatComment{
			ID: c.ID, VideoID: c.VideoID, ParentID: pid, Author: c.Author, AuthorID: c.AuthorID,
			Text: c.Text, Start: c.Start, Timestamp: c.Timestamp, Link: c.Link, CreatedAt: c.CreatedAt,
			Edited: c.Edited, Deleted: c.Deleted, Reactions: c.Reactions, Depth: depth,
		})
	})
	return out
}

// buildCommentThreads nests parent_id-linked comments under their parents,
// keeping input order. Replies whose parent is missing become top-level.
func buildCommentThreads(flat []loom.Comment) []loom.Comment {
	byID := make(map[string]int, len(flat))
	for i, c := range flat {
		byID[c.ID] = i
	}
	children := map[string][]int{}
	roots := make([]int, 0)
	for i, c := range flat {
		if _, ok := byID[c.ParentID]; c.ParentID != "" && ok && c.ParentID != c.ID {
			children[c.ParentID] = append(children[c.ParentID], i)
			continue
		}
		roots = append(roots, i)
	}
	var build func(i int, seen map[string]bool) loom.Comment
	build = func(i int, seen map[string]bool) loom.Comment {
		c := flat[i]
		c.Replies = make([]loom.Comment, 0)
		seen[c.ID] = true
		for _, ci := range children[c.ID] {
			if seen[flat[ci].ID] {
				continue
			}
			c.Replies = append(c.Replies, build(ci, seen))
		}
		return c
	}
	out := make([]loom.Comment, 0, len(roots))
	seen := map[string]bool{}
	for _, i := range roots {
		out = append(out, build(i, seen))
	}
	return out
}

// renderCommentThreads prints an indented thread: author, m:ss, text.
func renderCommentThreads(threads []loom.Comment) string {
	var b strings.Builder
	loom.WalkComments(threads, func(c loom.Comment, depth int) {
		text := strings.ReplaceAll(strings.TrimSpace(c.Text), "\n", " ")
		if c.Deleted {
			text = "[deleted] " + text
		}
		fmt.Fprintf(&b, "%s%s  %s  %s\n", strings.Repeat("  ", depth), c.AuthorLabel(), c.Timestamp, text)
	})
	return b.String()
}

// localComments rebuilds threads from comments recorded by `activity`.
func localComments(ctx context.Context, db *store.Store, id string) ([]loom.Comment, error) {
	seen, err := db.SeenComments(ctx, id)
	if err != nil {
		return nil, err
	}
	flat := make([]loom.Comment, 0, len(seen))
	for _, s := range seen {
		flat = append(flat, loom.Comment{
			ID: s.CommentID, VideoID: id, ParentID: s.ParentID, Author: s.Author, AuthorID: s.AuthorID,
			Text: s.Text, CreatedAt: s.CreatedAt, Timestamp: loom.FormatClock(0), Link: loom.ShareURL(id),
			Replies: make([]loom.Comment, 0),
		})
	}
	return buildCommentThreads(flat), nil
}
