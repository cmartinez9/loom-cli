package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

var searchTypes = []string{"all", "videos", "folders", "spaces", "tags", "users", "playlists"}

func newVideosSearchCmd(flags *rootFlags) *cobra.Command {
	var (
		kind  string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search Loom server-side for videos, folders, spaces, tags, people and playlists",
		Long: `Search Loom server-side in one call. Video matches cover titles,
descriptions and transcripts (matched_fields says which); the same call also
returns matching folders, spaces, tags, workspace members and playlists.

Use --type to keep one kind of result. For spoken-word matches with
timestamps across hydrated transcripts, use 'loom-pp-cli grep' instead.`,
		Example: strings.Trim(`
  loom-pp-cli videos search onboarding
  loom-pp-cli videos search "quarterly review" --type videos --json
  loom-pp-cli videos search design --limit 5 --json --select videos.id,videos.name`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "query=demo"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "videos search")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("<query> is required, e.g. loom-pp-cli videos search onboarding"))
			}
			kind = strings.ToLower(strings.TrimSpace(kind))
			if !validSearchType(kind) {
				return usageErr(fmt.Errorf("--type %q is not one of %s", kind, strings.Join(searchTypes, ", ")))
			}
			if limit < 0 {
				return usageErr(fmt.Errorf("--limit must be 0 or more"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			ws, err := lc.SelectedWorkspace(ctx)
			if err != nil {
				return loomErr(err)
			}
			res, err := lc.GlobalSearch(ctx, query, ws.ID)
			if err != nil {
				return loomErr(err)
			}
			shapeSearch(res, kind, limit)
			for _, w := range res.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			return emit(cmd, flags, res, func(w io.Writer) error {
				printSearch(w, res)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&kind, "type", "all", "Result type to keep: all, videos, folders, spaces, tags, users or playlists")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum results per type (Loom returns at most 10 videos and 10 spaces per query)")
	return cmd
}

func validSearchType(kind string) bool {
	for _, t := range searchTypes {
		if t == kind {
			return true
		}
	}
	return false
}

// shapeSearch empties every type except kind (unless kind is all) and
// truncates each list to limit (0 = no limit).
func shapeSearch(r *loom.SearchResults, kind string, limit int) {
	keep := func(t string) bool { return kind == "all" || kind == "" || kind == t }
	if !keep("videos") {
		r.Videos = r.Videos[:0]
	}
	if !keep("users") {
		r.Users = r.Users[:0]
	}
	if !keep("tags") {
		r.Tags = r.Tags[:0]
	}
	if !keep("folders") {
		r.Folders = r.Folders[:0]
	}
	if !keep("playlists") {
		r.Playlists = r.Playlists[:0]
	}
	if !keep("spaces") {
		r.Spaces = r.Spaces[:0]
	}
	if limit > 0 {
		if len(r.Videos) > limit {
			r.Videos = r.Videos[:limit]
		}
		if len(r.Users) > limit {
			r.Users = r.Users[:limit]
		}
		if len(r.Tags) > limit {
			r.Tags = r.Tags[:limit]
		}
		if len(r.Folders) > limit {
			r.Folders = r.Folders[:limit]
		}
		if len(r.Playlists) > limit {
			r.Playlists = r.Playlists[:limit]
		}
		if len(r.Spaces) > limit {
			r.Spaces = r.Spaces[:limit]
		}
	}
}

func printSearch(w io.Writer, r *loom.SearchResults) {
	total := len(r.Videos) + len(r.Users) + len(r.Tags) + len(r.Folders) + len(r.Playlists) + len(r.Spaces)
	if total == 0 {
		fmt.Fprintf(w, "No results for %q.\n", r.Query)
		return
	}
	if len(r.Videos) > 0 {
		fmt.Fprintf(w, "Videos (%d)\n", len(r.Videos))
		tw := newTabWriter(w)
		for _, v := range r.Videos {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\tmatched: %s\n", truncate(v.Name, 50), formatDate(v.CreatedAt), v.Duration, v.ID, strings.Join(v.MatchedFields, ","))
		}
		_ = tw.Flush()
	}
	if len(r.Folders) > 0 {
		fmt.Fprintf(w, "Folders (%d)\n", len(r.Folders))
		for _, f := range r.Folders {
			fmt.Fprintf(w, "  %s  %s\n", f.Name, f.ID)
		}
	}
	if len(r.Spaces) > 0 {
		fmt.Fprintf(w, "Spaces (%d)\n", len(r.Spaces))
		for _, s := range r.Spaces {
			fmt.Fprintf(w, "  %s  %s  (%d members, %d videos)\n", s.Name, s.ID, s.MembersCount, s.TotalVideos)
		}
	}
	if len(r.Tags) > 0 {
		fmt.Fprintf(w, "Tags (%d)\n", len(r.Tags))
		for _, t := range r.Tags {
			fmt.Fprintf(w, "  %s (%d)\n", t.Tag, t.Count)
		}
	}
	if len(r.Users) > 0 {
		fmt.Fprintf(w, "People (%d)\n", len(r.Users))
		for _, u := range r.Users {
			fmt.Fprintf(w, "  %s  %s\n", u.Name, u.ID)
		}
	}
	if len(r.Playlists) > 0 {
		fmt.Fprintf(w, "Playlists (%d)\n", len(r.Playlists))
		for _, p := range r.Playlists {
			fmt.Fprintf(w, "  %s  %s  (%d videos)\n", p.Name, p.ID, p.VideoCount)
		}
	}
}
