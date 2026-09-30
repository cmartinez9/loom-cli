package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Folder is a library folder.
type Folder struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Visibility string `json:"visibility,omitempty"`
	SpecialID  string `json:"special_id,omitempty"`
	VideoCount *int   `json:"video_count,omitempty"`
	ParentID   string `json:"parent_id,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

// MyVideosFolder returns the user's root "My Videos" folder.
func (l *Client) MyVideosFolder(ctx context.Context) (*Folder, error) {
	data, err := l.Query(ctx, OpMyVideosFolder, map[string]any{})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return nil, nil
	}
	raw, err := l.field(ctx, OpMyVideosFolder, data, "myVideosFolder")
	if err != nil {
		return nil, err
	}
	var f Folder
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", OpMyVideosFolder, err)
	}
	return &f, nil
}

// GetFolder returns one folder's details.
// An unknown folder, or one the user cannot open, is a *NotFoundError with
// Kind "folder" however Loom phrased it (a "Folder Not Found" GraphQL
// error, a NotFoundError union member or a null folder).
func (l *Client) GetFolder(ctx context.Context, id string) (*Folder, error) {
	data, err := l.Query(ctx, OpFolder, map[string]any{"folderId": id})
	if err != nil {
		return nil, folderLookupErr(id, err)
	}
	if l.IsDryRun() {
		return &Folder{ID: id}, nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) == nil {
		if v := obj["folder"]; len(v) == 0 || string(v) == "null" {
			return nil, &NotFoundError{Kind: "folder", ID: id}
		}
	}
	raw, err := l.field(ctx, OpFolder, data, "folder")
	if err != nil {
		return nil, folderLookupErr(id, err)
	}
	var r struct {
		ID                string  `json:"id"`
		Name              string  `json:"name"`
		Visibility        string  `json:"visibility"`
		CreatedAt         string  `json:"createdAt"`
		UpdatedAt         string  `json:"updatedAt"`
		TotalNestedVideos *int    `json:"totalNestedVideos"`
		ParentFolderID    *string `json:"parent_folder_id"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpFolder, err)
	}
	f := &Folder{ID: r.ID, Name: r.Name, Visibility: r.Visibility, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, VideoCount: r.TotalNestedVideos}
	if r.ParentFolderID != nil {
		f.ParentID = *r.ParentFolderID
	}
	return f, nil
}

// folderLookupErr turns Loom's "no such folder" answers into a typed
// *NotFoundError; other errors (an expired session included) pass through.
func folderLookupErr(id string, err error) error {
	var ge *GraphQLError
	if errors.As(err, &ge) {
		for _, m := range ge.Messages {
			if strings.Contains(strings.ToLower(m), "not found") {
				return &NotFoundError{Kind: "folder", ID: id}
			}
		}
	}
	var ue *UnionError
	if errors.As(err, &ue) && ue.Typename == "NotFoundError" {
		return &NotFoundError{Kind: "folder", ID: id, Msg: ue.Message}
	}
	return err
}

// PublishedFolders returns one page of the user's active folders, most
// recently used first.
func (l *Client) PublishedFolders(ctx context.Context, first int, after string) ([]Folder, PageInfo, error) {
	out := make([]Folder, 0)
	if first <= 0 || first > 100 {
		first = 50
	}
	vars := map[string]any{"first": first, "source": "ACTIVE", "sortType": "RECENT", "sortOrder": "DESC", "filters": []any{}}
	if after != "" {
		vars["after"] = after
	}
	data, err := l.Query(ctx, OpFolders, vars)
	if err != nil {
		return nil, PageInfo{}, err
	}
	if l.IsDryRun() {
		return out, PageInfo{}, nil
	}
	payload, err := l.field(ctx, OpFolders, data, "getPublishedFolders")
	if err != nil {
		return nil, PageInfo{}, err
	}
	var r struct {
		Folders *struct {
			Edges []struct {
				Node Folder `json:"node"`
			} `json:"edges"`
			PageInfo struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"folders"`
	}
	if err := json.Unmarshal(payload, &r); err != nil {
		return nil, PageInfo{}, fmt.Errorf("%s: %w", OpFolders, err)
	}
	if r.Folders == nil {
		return out, PageInfo{}, nil
	}
	for _, e := range r.Folders.Edges {
		if e.Node.ID != "" {
			out = append(out, e.Node)
		}
	}
	return out, PageInfo{EndCursor: r.Folders.PageInfo.EndCursor, HasNextPage: r.Folders.PageInfo.HasNextPage}, nil
}

// SearchFolders finds folders by name.
func (l *Client) SearchFolders(ctx context.Context, query string) ([]Folder, error) {
	out := make([]Folder, 0)
	data, err := l.Query(ctx, OpSearchFolders, map[string]any{"searchQuery": query})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return out, nil
	}
	raw, err := l.field(ctx, OpSearchFolders, data, "searchFolders")
	if err != nil {
		return nil, err
	}
	var r struct {
		Folders []struct {
			ID                string `json:"id"`
			Name              string `json:"name"`
			TotalNestedVideos *int   `json:"totalNestedVideos"`
		} `json:"folders"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpSearchFolders, err)
	}
	for _, f := range r.Folders {
		out = append(out, Folder{ID: f.ID, Name: f.Name, VideoCount: f.TotalNestedVideos})
	}
	return out, nil
}
