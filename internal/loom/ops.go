// Package loom is the hand-built layer over Loom's web-app GraphQL endpoint.
//
// Loom has no public API for a user's own library. Every call the web app
// makes is POST https://www.loom.com/graphql with an operationName, the full
// query document and variables. The documents in ops/ were captured from the
// live web app (or taken from karbassi/mcp-loom and corrected against the live
// schema) and each was confirmed with one read-only live call; mutation
// documents were validated without executing by omitting required variables.
package loom

import (
	"embed"
	"fmt"
	"path"
)

//go:embed ops/*.graphql
var opsFS embed.FS

// Operation names. Keep these in sync with the files in ops/.
const (
	OpMe                 = "Me"
	OpLibrary            = "GetLoomsForLibrary"
	OpVideoDetails       = "GetVideoCardDetails"
	OpSearch             = "GlobalSearchQuery"
	OpTranscript         = "FetchVideoTranscript"
	OpChapters           = "FetchChapters"
	OpSummary            = "GetAutoSummaryStatus"
	OpTakeaways          = "GetMeetingTakeaways"
	OpComments           = "FetchPagedComments"
	OpTasks              = "GetVideoTasks"
	OpReactions          = "FetchVideoReactions"
	OpTags               = "GetTagsByVideoId"
	OpBacklinks          = "GetVideoBacklinks"
	OpLinks              = "VideoLinksAndSettings"
	OpDownloadURL        = "GetVideoTranscodedUrl"
	OpFolders            = "GetPublishedFolders"
	OpMyVideosFolder     = "GetMyVideosFolder"
	OpFolder             = "GetFolder"
	OpSearchFolders      = "SearchFolders"
	OpSpaceMemberships   = "GetMySpaceMemberships"
	OpOpenSpaces         = "GetOpenSpaces"
	OpSpace              = "GetSpaceNameAndMembers"
	OpPlaylists          = "GetAvailablePlaylists"
	OpPlaylist           = "GetPlaylistDetails"
	OpWatchLaterCount    = "GetUserWatchLaterListCount"
	OpRecentVideos       = "RecentUserVideos"
	OpActivityFeed       = "GetCurrentUserVideoActivityFeed"
	OpCalendarMeetings   = "GetCalendarMeetings"
	OpUser               = "GetUserByIdWithProfile"
	OpUserVideoCount     = "GetTotalVideosCountByUser"
	OpIsPinned           = "GetIsVideoPinned"
	OpFollowsVideo       = "GetUserFollowsVideo"
	OpSelectedWorkspace  = "SelectedWorkspaceMembership"
	OpUpdateName         = "UpdateVideoName"
	OpUpdateDescription  = "UpdateVideoDescription"
	OpUpdateSettings     = "UpdateVideoSettings"
	OpArchive            = "ArchiveVideos"
	OpMoveVideos         = "BulkMoveVideos"
	OpMoveFolders        = "BulkMoveFolders"
	OpDeleteVideo        = "DeleteVideo"
	OpRecoverVideo       = "RecoverVideo"
	OpDuplicateVideo     = "DuplicateVideo"
	OpPin                = "UpdateVideoPinStatus"
	OpCreateFolder       = "CreateFolder"
	OpRenameFolder       = "RenameFolder"
	OpDeleteFolders      = "BulkDeleteFolders"
	OpShareToSpaces      = "BatchShareVideosToSpaces"
	OpCreateComment      = "CreateVideoComment"
	OpEditComment        = "EditComment"
	OpDeleteComment      = "DeleteComment"
	OpAddCommentReaction = "AddCommentReaction"
	OpCreateTask         = "CreateVideoTask"
	OpUpdateTask         = "UpdateVideoTask"
	OpDeleteTask         = "DeleteVideoTask"
	OpApproveTask        = "ApproveVideoTask"
	OpRespondTask        = "RespondToVideoTask"
	OpAddReaction        = "AddVideoReaction"
	OpDeleteReaction     = "DeleteVideoReaction"
	OpToggleFollow       = "ToggleFollowingVideo"
	OpAddWatchLater      = "AddVideoToWatchLaterList"
	OpRemoveWatchLater   = "RemoveVideoFromWatchLaterList"
)

// Document returns the GraphQL document for an operation name.
func Document(op string) (string, error) {
	b, err := opsFS.ReadFile(path.Join("ops", op+".graphql"))
	if err != nil {
		return "", fmt.Errorf("unknown loom operation %q", op)
	}
	return string(b), nil
}

// ObjectKind names the kind of object an operation reads or changes
// ("video", "folder", "space", "playlist", "user", "comment", "task"), so
// not-found errors can name the right thing.
func ObjectKind(op string) string {
	switch op {
	case OpFolder, OpFolders, OpSearchFolders, OpMyVideosFolder, OpCreateFolder, OpRenameFolder, OpDeleteFolders, OpMoveFolders:
		return "folder"
	case OpSpace, OpSpaceMemberships, OpOpenSpaces:
		return "space"
	case OpPlaylist, OpPlaylists, OpWatchLaterCount:
		return "playlist"
	case OpUser, OpUserVideoCount:
		return "user"
	case OpEditComment, OpDeleteComment, OpAddCommentReaction:
		return "comment"
	case OpUpdateTask, OpDeleteTask, OpApproveTask, OpRespondTask:
		return "task"
	}
	return "video"
}
