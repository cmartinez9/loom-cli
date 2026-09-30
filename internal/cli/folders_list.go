package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newFoldersListCmd(flags *rootFlags) *cobra.Command {
	var (
		limit    int
		maxPages int
		dbPath   string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your Loom folders, starting with the root My Videos folder",
		Long: `List your Loom folders: the root "My Videos" folder first, then your active
folders, most recently used first. At most --max-pages pages of 50 folders
are read; a warning says when more were left.

--data-source local reads the folders synced by 'loom-pp-cli sync'; auto
(the default) falls back to them only when the Loom session has expired.`,
		Example: strings.Trim(`
  loom-pp-cli folders list
  loom-pp-cli folders list --json --select id,name
  loom-pp-cli folders list --data-source local`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "folders list")
			}
			if len(args) > 0 {
				return usageErr(fmt.Errorf("folders list takes no positional arguments; use 'folders search <query>' to find a folder by name"))
			}
			if limit < 0 {
				return usageErr(fmt.Errorf("--limit must be 0 or more"))
			}
			maxPages, err := effectiveMaxPages(maxPages)
			if err != nil {
				return err
			}
			db := resolveDBPath(dbPath)
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			local := func() error {
				if done, err := missingLocalMirror(cmd, flags, db, "folders", make([]loom.Folder, 0)); done {
					return err
				}
				rows, err := readSyncedFolders(ctx, db)
				if err != nil {
					return err
				}
				return printFolders(cmd, flags, rootFolderFirst(rows, limit))
			}
			live := func() error {
				lc, err := newLoomClient(flags)
				if err != nil {
					return err
				}
				rows, capped, err := liveFolders(ctx, lc, limit, maxPages)
				if err != nil {
					return err
				}
				if capped {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", capNote(maxPages, "folders"))
				}
				return printFolders(cmd, flags, rows)
			}
			return withDataSource(cmd, flags, db, "folders", live, local)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum folders to return (0 = all)")
	registerMaxPages(cmd, &maxPages, "folders", false)
	cmd.Flags().StringVar(&dbPath, "db", "", "Local store path for --data-source local (default: the CLI's data directory)")
	return cmd
}

// liveFolders reads the root folder and up to maxPages pages of active
// folders. capped reports that more pages were left unread.
func liveFolders(ctx context.Context, lc *loom.Client, limit, maxPages int) ([]loom.Folder, bool, error) {
	rows := make([]loom.Folder, 0)
	root, err := lc.MyVideosFolder(ctx)
	if err != nil {
		return nil, false, err
	}
	if root != nil {
		rows = append(rows, *root)
	}
	after := ""
	for page := 0; ; page++ {
		if limit > 0 && len(rows) >= limit {
			break
		}
		if page >= maxPages {
			return rootFolderFirst(rows, limit), true, nil
		}
		fs, info, err := lc.PublishedFolders(ctx, selectPageSize, after)
		if err != nil {
			return nil, false, err
		}
		for _, f := range fs {
			if root != nil && f.ID == root.ID {
				continue
			}
			rows = append(rows, f)
		}
		if !info.HasNextPage || info.EndCursor == "" {
			break
		}
		after = info.EndCursor
	}
	return rootFolderFirst(rows, limit), false, nil
}

// rootFolderFirst puts the My Videos root folder first and applies limit.
func rootFolderFirst(rows []loom.Folder, limit int) []loom.Folder {
	out := make([]loom.Folder, 0, len(rows))
	for _, f := range rows {
		if f.SpecialID != "" {
			out = append(out, f)
		}
	}
	for _, f := range rows {
		if f.SpecialID == "" {
			out = append(out, f)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func printFolders(cmd *cobra.Command, flags *rootFlags, rows []loom.Folder) error {
	return emit(cmd, flags, rows, func(w io.Writer) error {
		if len(rows) == 0 {
			fmt.Fprintln(w, "No folders.")
			return nil
		}
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "NAME\tVISIBILITY\tVIDEOS\tID")
		for _, f := range rows {
			count := ""
			if f.VideoCount != nil {
				count = fmt.Sprint(*f.VideoCount)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", truncate(f.Name, 50), f.Visibility, count, f.ID)
		}
		return tw.Flush()
	})
}
