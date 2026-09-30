package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

// userView is a user profile plus their video count in the workspace.
type userView struct {
	loom.UserProfile
	VideosCount *int   `json:"videos_count"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

func newUsersGetCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [user-id]",
		Short: "Show a Loom user's profile and video count (you, by default)",
		Long: `Show a Loom user's profile: name, profile link, role and location, plus
how many videos they have in your current workspace.

With no [user-id], shows the signed-in user.`,
		Example: strings.Trim(`
  loom-pp-cli users get
  loom-pp-cli users get --json --select id,videos_count
  loom-pp-cli users get 12345678 --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "users get")
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if len(args) > 1 {
				return usageErr(fmt.Errorf("users get takes at most one [user-id], got %d", len(args)))
			}
			id := ""
			if len(args) == 1 {
				var err error
				if id, err = userIDArg(args[0]); err != nil {
					return err
				}
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			if id == "" {
				me, err := lc.CurrentUser(ctx)
				if err != nil {
					return loomErr(err)
				}
				id = me.ID
			}
			prof, err := lc.UserByID(ctx, id)
			if err != nil {
				return loomErr(err)
			}
			view := userView{UserProfile: *prof}
			if ws, err := lc.SelectedWorkspace(ctx); err == nil {
				view.WorkspaceID = ws.ID
				if n, err := lc.UserVideoCount(ctx, prof.ID, ws.ID); err == nil {
					view.VideosCount = &n
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not count videos: %v\n", err)
				}
			} else if n, err := lc.UserVideoCount(ctx, prof.ID, ""); err == nil {
				view.VideosCount = &n
			}
			return emit(cmd, flags, view, func(w io.Writer) error {
				fmt.Fprintf(w, "Name:     %s\nID:       %s\n", view.Name, view.ID)
				if view.ProfileURL != "" {
					fmt.Fprintf(w, "Profile:  %s\n", view.ProfileURL)
				}
				if view.Role != "" {
					fmt.Fprintf(w, "Role:     %s\n", view.Role)
				}
				if view.Location != "" {
					fmt.Fprintf(w, "Location: %s\n", view.Location)
				}
				if view.VideosCount != nil {
					fmt.Fprintf(w, "Videos:   %d\n", *view.VideosCount)
				}
				return nil
			})
		},
	}
	return cmd
}
