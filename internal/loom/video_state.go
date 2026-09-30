package loom

import (
	"context"
	"encoding/json"
	"fmt"
)

// The signed-in user's per-video state: pinned and followed.

// PinContextLibrary is the pin context the web app uses in My Library.
const PinContextLibrary = "my-library-context"

// IsVideoPinned reports whether a video is pinned in a pin context.
func (l *Client) IsVideoPinned(ctx context.Context, id, pinContext string) (bool, error) {
	data, err := l.Query(ctx, OpIsPinned, map[string]any{"videoId": id, "context": pinContext})
	if err != nil {
		return false, err
	}
	if l.IsDryRun() {
		return false, nil
	}
	raw, err := l.field(ctx, OpIsPinned, data, "getIsVideoPinned")
	if err != nil {
		return false, err
	}
	var r struct {
		IsPinned bool `json:"isPinned"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return false, fmt.Errorf("%s: %w", OpIsPinned, err)
	}
	return r.IsPinned, nil
}

// FollowsVideo reports whether the user follows a video's activity.
func (l *Client) FollowsVideo(ctx context.Context, id string) (bool, error) {
	data, err := l.Query(ctx, OpFollowsVideo, map[string]any{"videoId": id})
	if err != nil {
		return false, err
	}
	if l.IsDryRun() {
		return false, nil
	}
	raw, err := l.field(ctx, OpFollowsVideo, data, "result")
	if err != nil {
		return false, err
	}
	var r struct {
		Follow bool `json:"follow"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return false, fmt.Errorf("%s: %w", OpFollowsVideo, err)
	}
	return r.Follow, nil
}
