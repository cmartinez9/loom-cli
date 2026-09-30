package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Playlist is one playlist summary.
type Playlist struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	VideoCount    int     `json:"video_count"`
	Privacy       string  `json:"privacy,omitempty"`
	Owner         string  `json:"owner,omitempty"`
	OwnerID       string  `json:"owner_id,omitempty"`
	TotalDuration float64 `json:"total_duration_seconds,omitempty"`
	CreatedAt     string  `json:"created_at,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
	WatchLater    bool    `json:"watch_later"`
}

// WatchLaterID is the pseudo id the CLI uses for the Watch Later list.
const WatchLaterID = "watch-later"

// AvailablePlaylists returns up to first playlists the user can see, most
// recently updated first.
func (l *Client) AvailablePlaylists(ctx context.Context, first int) ([]Playlist, error) {
	out := make([]Playlist, 0)
	if first <= 0 || first > 100 {
		first = 50
	}
	data, err := l.Query(ctx, OpPlaylists, map[string]any{"first": first, "sortField": "UPDATED_AT", "sortDirection": "DESC"})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return out, nil
	}
	raw, err := l.field(ctx, OpPlaylists, data, "getAvailablePlaylists")
	if err != nil {
		return nil, err
	}
	var r struct {
		Playlists *struct {
			Nodes []struct {
				Name          string      `json:"name"`
				PlaylistID    string      `json:"playlistId"`
				OwnerID       json.Number `json:"ownerId"`
				CreatedAt     string      `json:"createdAt"`
				UpdatedAt     string      `json:"updatedAt"`
				VideoCount    int         `json:"videoCount"`
				Privacy       string      `json:"privacy"`
				TotalDuration float64     `json:"totalDuration"`
				Owner         *struct {
					DisplayName string `json:"display_name"`
				} `json:"owner"`
			} `json:"nodes"`
		} `json:"playlists"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpPlaylists, err)
	}
	if r.Playlists == nil {
		return out, nil
	}
	for _, n := range r.Playlists.Nodes {
		p := Playlist{ID: n.PlaylistID, Name: n.Name, VideoCount: n.VideoCount, Privacy: n.Privacy, OwnerID: n.OwnerID.String(),
			TotalDuration: n.TotalDuration, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt}
		if n.Owner != nil {
			p.Owner = TitleName(n.Owner.DisplayName)
		}
		out = append(out, p)
	}
	return out, nil
}

// WatchLaterCount returns how many videos are on the Watch Later list.
func (l *Client) WatchLaterCount(ctx context.Context) (int, error) {
	data, err := l.Query(ctx, OpWatchLaterCount, nil)
	if err != nil {
		return 0, err
	}
	if l.IsDryRun() {
		return 0, nil
	}
	raw, err := l.field(ctx, OpWatchLaterCount, data, "getUserWatchLaterListCount")
	if err != nil {
		return 0, err
	}
	var r struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return 0, fmt.Errorf("%s: %w", OpWatchLaterCount, err)
	}
	return r.Count, nil
}

// PlaylistVideo is one video on a playlist.
type PlaylistVideo struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Sort            int     `json:"sort"`
	DurationSeconds float64 `json:"duration_seconds"`
	Duration        string  `json:"duration"`
	Owner           string  `json:"owner,omitempty"`
	AddedAt         string  `json:"added_at,omitempty"`
	CanAccess       bool    `json:"can_access"`
	Watched         bool    `json:"watched"`
	ShareURL        string  `json:"share_url"`
}

// PlaylistDetail is one playlist with its videos.
type PlaylistDetail struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	VideoCount    int             `json:"video_count"`
	TotalViews    int             `json:"total_views"`
	TotalDuration float64         `json:"total_duration_seconds"`
	Owner         string          `json:"owner,omitempty"`
	OwnerID       string          `json:"owner_id,omitempty"`
	CanEdit       bool            `json:"can_edit"`
	Collaborators int             `json:"collaborators"`
	Videos        []PlaylistVideo `json:"videos"`
}

// PlaylistDetails returns one playlist with its videos.
func (l *Client) PlaylistDetails(ctx context.Context, id string) (*PlaylistDetail, error) {
	data, err := l.Query(ctx, OpPlaylist, map[string]any{"playlistId": id})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return &PlaylistDetail{ID: id, Videos: make([]PlaylistVideo, 0)}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("%s: %w", OpPlaylist, err)
	}
	raw := obj["getPlaylistDetails"]
	if len(raw) == 0 || string(raw) == "null" {
		return nil, &NotFoundError{Kind: "playlist", ID: id}
	}
	if err := CheckUnion(OpPlaylist, raw); err != nil {
		var ue *UnionError
		if errors.As(err, &ue) {
			if serr := l.normalizeAuth(ctx, err); errors.Is(serr, ErrSessionExpired) {
				return nil, serr
			}
			return nil, &NotFoundError{Kind: "playlist", ID: id, Msg: ue.Message}
		}
		return nil, err
	}
	var r struct {
		PlaylistID         string  `json:"playlistId"`
		Name               string  `json:"name"`
		Description        *string `json:"description"`
		CanEdit            bool    `json:"canEdit"`
		VideoCount         int     `json:"videoCount"`
		PlaylistTotalViews int     `json:"playlistTotalViews"`
		TotalDuration      float64 `json:"totalDuration"`
		Owner              *struct {
			ID          json.Number `json:"id"`
			DisplayName string      `json:"display_name"`
		} `json:"owner"`
		Collaborators []json.RawMessage `json:"collaborators"`
		Videos        []struct {
			VideoID          string  `json:"videoId"`
			Sort             int     `json:"sort"`
			CreatedAt        string  `json:"createdAt"`
			Name             string  `json:"name"`
			PlayableDuration float64 `json:"playableDuration"`
			ViewerCanAccess  bool    `json:"viewerCanAccess"`
			ViewerHasWatched bool    `json:"viewerHasWatched"`
			Owner            *struct {
				DisplayName string `json:"display_name"`
			} `json:"owner"`
		} `json:"videos"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpPlaylist, err)
	}
	out := &PlaylistDetail{ID: r.PlaylistID, Name: r.Name, CanEdit: r.CanEdit, VideoCount: r.VideoCount, TotalViews: r.PlaylistTotalViews,
		TotalDuration: r.TotalDuration, Collaborators: len(r.Collaborators), Videos: make([]PlaylistVideo, 0, len(r.Videos))}
	if out.ID == "" {
		out.ID = id
	}
	if r.Description != nil {
		out.Description = *r.Description
	}
	if r.Owner != nil {
		out.Owner, out.OwnerID = TitleName(r.Owner.DisplayName), r.Owner.ID.String()
	}
	for _, v := range r.Videos {
		pv := PlaylistVideo{ID: v.VideoID, Name: v.Name, Sort: v.Sort, DurationSeconds: round3(v.PlayableDuration), Duration: FormatClock(v.PlayableDuration),
			AddedAt: v.CreatedAt, CanAccess: v.ViewerCanAccess, Watched: v.ViewerHasWatched, ShareURL: ShareURL(v.VideoID)}
		if v.Owner != nil {
			pv.Owner = TitleName(v.Owner.DisplayName)
		}
		out.Videos = append(out.Videos, pv)
	}
	return out, nil
}
