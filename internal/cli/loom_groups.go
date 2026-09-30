package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newCommentsGroupCmd(flags))
		addNovelCommandIfAbsent(root, newTasksGroupCmd(flags))
		addNovelCommandIfAbsent(root, newReactionsGroupCmd(flags))
		addNovelCommandIfAbsent(root, newTagsGroupCmd(flags))
		addNovelCommandIfAbsent(root, newPlaylistsGroupCmd(flags))
		addNovelCommandIfAbsent(root, newUsersGroupCmd(flags))
	})
}

// newCommentsGroupCmd is the `comments` group.
func newCommentsGroupCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "comments",
		Example: strings.Trim(`
  loom-pp-cli comments list latest
  loom-pp-cli comments add latest "Looks good" --at 1:23 --dry-run
`, "\n"),
		Short: "Read and write comments on a video",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newCommentsAddCmd(flags))
	cmd.AddCommand(newCommentsDeleteCmd(flags))
	cmd.AddCommand(newCommentsEditCmd(flags))
	cmd.AddCommand(newCommentsListCmd(flags))
	cmd.AddCommand(newCommentsReactCmd(flags))
	return cmd
}

// newTasksGroupCmd is the `tasks` group.
func newTasksGroupCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "tasks",
		Example: strings.Trim(`
  loom-pp-cli tasks list latest --open
  loom-pp-cli tasks add latest "Send the recap" --at 2:10 --dry-run
`, "\n"),
		Short: "Read and manage Loom's AI action items on a video",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newTasksAddCmd(flags))
	cmd.AddCommand(newTasksApproveCmd(flags))
	cmd.AddCommand(newTasksDeleteCmd(flags))
	cmd.AddCommand(newTasksDoneCmd(flags))
	cmd.AddCommand(newTasksEditCmd(flags))
	cmd.AddCommand(newTasksListCmd(flags))
	return cmd
}

// newReactionsGroupCmd is the `reactions` group.
func newReactionsGroupCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "reactions",
		Example: strings.Trim(`
  loom-pp-cli reactions list latest --summary
  loom-pp-cli reactions add latest 🎉 --at 0:30 --dry-run
`, "\n"),
		Short: "Read, add and remove emoji reactions on a video",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newReactionsAddCmd(flags))
	cmd.AddCommand(newReactionsDeleteCmd(flags))
	cmd.AddCommand(newReactionsListCmd(flags))
	return cmd
}

// newTagsGroupCmd is the `tags` group.
func newTagsGroupCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "tags",
		Example: strings.Trim(`
  loom-pp-cli tags list latest --json
`, "\n"),
		Short: "Tags on a video",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newTagsListCmd(flags))
	return cmd
}

// newPlaylistsGroupCmd is the `playlists` group.
func newPlaylistsGroupCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "playlists",
		Example: strings.Trim(`
  loom-pp-cli playlists list
  loom-pp-cli playlists get watch-later --json
`, "\n"),
		Short: "Playlists you can see, including Watch Later",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newPlaylistsGetCmd(flags))
	cmd.AddCommand(newPlaylistsListCmd(flags))
	return cmd
}

// newUsersGroupCmd is the `users` group.
func newUsersGroupCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "users",
		Example: strings.Trim(`
  loom-pp-cli users get --json --select id,videos_count
`, "\n"),
		Short: "Loom user profiles and video counts",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newUsersGetCmd(flags))
	return cmd
}
