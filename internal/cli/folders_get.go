package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func newFoldersGetCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <folder-id>",
		Short: "Show one folder's name, visibility, video count and parent",
		Long: `Show one folder's details: name, visibility, total videos (including
nested folders), parent folder and timestamps.

<folder-id> is the 32-character folder id from 'folders list', or
'my-videos' for your root My Videos folder.`,
		Example: strings.Trim(`
  loom-pp-cli folders get my-videos
  loom-pp-cli folders get 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "folder=my-videos"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "folders get")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if len(args) != 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("folders get takes exactly one <folder-id>, got %d", len(args)))
			}
			root := isRootFolderAlias(args[0])
			id := ""
			if !root {
				var err error
				if id, err = folderIDArg(args[0]); err != nil {
					return err
				}
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			if root {
				f, err := lc.MyVideosFolder(ctx)
				if err != nil {
					return loomErr(err)
				}
				if f == nil {
					return notFoundErr(fmt.Errorf("your account has no My Videos folder"))
				}
				id = f.ID
			}
			f, err := lc.GetFolder(ctx, id)
			if err != nil {
				return loomErr(err)
			}
			return emit(cmd, flags, f, func(w io.Writer) error {
				fmt.Fprintf(w, "Name:       %s\nID:         %s\n", f.Name, f.ID)
				if f.Visibility != "" {
					fmt.Fprintf(w, "Visibility: %s\n", f.Visibility)
				}
				if f.VideoCount != nil {
					fmt.Fprintf(w, "Videos:     %d\n", *f.VideoCount)
				}
				if f.ParentID != "" {
					fmt.Fprintf(w, "Parent:     %s\n", f.ParentID)
				}
				if f.CreatedAt != "" {
					fmt.Fprintf(w, "Created:    %s\n", f.CreatedAt)
				}
				return nil
			})
		},
	}
	return cmd
}

func isRootFolderAlias(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "my-videos", "root", "my_videos", "myvideos":
		return true
	}
	return false
}
