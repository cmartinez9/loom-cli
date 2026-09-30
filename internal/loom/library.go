package loom

import (
	"context"
	"encoding/json"
	"fmt"
)

// Library sources accepted by GetLoomsForLibrary.
const (
	SourceMine     = "MINE"
	SourceArchived = "ARCHIVED"
)

// ListOptions controls one GetLoomsForLibrary page.
type ListOptions struct {
	Limit     int    // page size (1-100)
	Cursor    string // Relay cursor from a previous page
	FolderID  string // restrict to one folder
	Source    string // MINE (default) or ARCHIVED
	SortOrder string // DESC (default) or ASC
}

// Page is one page of the library listing.
type Page struct {
	Videos      []Video
	EndCursor   string
	HasNextPage bool
}

// ListVideos fetches one page of the signed-in user's library, newest first.
func (l *Client) ListVideos(ctx context.Context, opts ListOptions) (*Page, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	source := opts.Source
	if source == "" {
		source = SourceMine
	}
	order := opts.SortOrder
	if order == "" {
		order = "DESC"
	}
	vars := map[string]any{
		"limit":             limit,
		"source":            source,
		"sortType":          "RECENT",
		"sortOrder":         order,
		"filters":           []any{},
		"includeTotalCount": false,
	}
	if opts.Cursor != "" {
		vars["cursor"] = opts.Cursor
	}
	if opts.FolderID != "" {
		vars["folderId"] = opts.FolderID
	}
	data, err := l.Query(ctx, OpLibrary, vars)
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return &Page{}, nil
	}
	payload, err := l.field(ctx, OpLibrary, data, "getLooms")
	if err != nil {
		return nil, err
	}
	var out struct {
		Videos *struct {
			Edges []struct {
				Node json.RawMessage `json:"node"`
			} `json:"edges"`
			PageInfo struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"videos"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", OpLibrary, err)
	}
	page := &Page{Videos: make([]Video, 0)}
	if out.Videos == nil {
		return page, nil
	}
	for _, e := range out.Videos.Edges {
		var head struct {
			Typename string `json:"__typename"`
		}
		_ = json.Unmarshal(e.Node, &head)
		if head.Typename != "" && head.Typename != "RegularUserVideo" {
			continue
		}
		v, err := NormalizeVideo(e.Node)
		if err != nil {
			continue
		}
		page.Videos = append(page.Videos, v)
	}
	page.EndCursor = out.Videos.PageInfo.EndCursor
	page.HasNextPage = out.Videos.PageInfo.HasNextPage
	return page, nil
}

// GetVideo fetches one video's full metadata (GetVideoCardDetails).
func (l *Client) GetVideo(ctx context.Context, id string) (*Video, error) {
	data, err := l.Query(ctx, OpVideoDetails, map[string]any{"videoId": id})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return &Video{ID: id, ShareURL: ShareURL(id), Tags: []string{}}, nil
	}
	raw, err := l.field(ctx, OpVideoDetails, data, "video")
	if err != nil {
		return nil, err
	}
	v, err := NormalizeVideo(raw)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
