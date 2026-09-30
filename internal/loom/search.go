package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// SearchVideoHit is one video match from GlobalSearchQuery.
type SearchVideoHit struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	ShareURL        string   `json:"share_url"`
	CreatedAt       string   `json:"created_at"`
	DurationSeconds float64  `json:"duration_seconds"`
	Duration        string   `json:"duration"`
	Owner           string   `json:"owner,omitempty"`
	MatchedFields   []string `json:"matched_fields"`
}

// SearchUserHit is one workspace member match.
type SearchUserHit struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email,omitempty"`
	ProfileURL string `json:"profile_url,omitempty"`
	VideoCount *int   `json:"video_count,omitempty"`
}

// SearchTagHit is one tag match.
type SearchTagHit struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
	URL   string `json:"url,omitempty"`
}

// SearchFolderHit is one folder match.
type SearchFolderHit struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	VideoCount *int   `json:"video_count,omitempty"`
	Owner      string `json:"owner,omitempty"`
	SpaceID    string `json:"space_id,omitempty"`
	SpaceName  string `json:"space_name,omitempty"`
}

// SearchPlaylistHit is one playlist match.
type SearchPlaylistHit struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	VideoCount int    `json:"video_count"`
	OwnerID    string `json:"owner_id,omitempty"`
}

// SearchSpaceHit is one space match.
type SearchSpaceHit struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MembersCount int    `json:"members_count"`
	TotalVideos  int    `json:"total_videos"`
	Archived     bool   `json:"archived"`
}

// SearchResults groups every result type of one GlobalSearchQuery call.
// Every slice is non-nil so JSON renders [] for no matches.
type SearchResults struct {
	Query     string              `json:"query"`
	Videos    []SearchVideoHit    `json:"videos"`
	Users     []SearchUserHit     `json:"users"`
	Tags      []SearchTagHit      `json:"tags"`
	Folders   []SearchFolderHit   `json:"folders"`
	Playlists []SearchPlaylistHit `json:"playlists"`
	Spaces    []SearchSpaceHit    `json:"spaces"`
	Warnings  []string            `json:"warnings,omitempty"`
}

// NewSearchResults returns an empty result set with non-nil slices.
func NewSearchResults(query string) *SearchResults {
	return &SearchResults{
		Query:     query,
		Videos:    make([]SearchVideoHit, 0),
		Users:     make([]SearchUserHit, 0),
		Tags:      make([]SearchTagHit, 0),
		Folders:   make([]SearchFolderHit, 0),
		Playlists: make([]SearchPlaylistHit, 0),
		Spaces:    make([]SearchSpaceHit, 0),
	}
}

// GlobalSearch runs Loom's server-side search across videos, users, tags,
// folders, playlists and spaces in one call.
func (l *Client) GlobalSearch(ctx context.Context, query, workspaceID string) (*SearchResults, error) {
	data, err := l.Query(ctx, OpSearch, map[string]any{"searchQuery": query, "workspaceId": workspaceID})
	if err != nil {
		return nil, err
	}
	out := NewSearchResults(query)
	if l.IsDryRun() {
		return out, nil
	}
	res, err := ParseGlobalSearch(query, data)
	if err != nil {
		return nil, l.normalizeAuth(ctx, err)
	}
	return res, nil
}

// ParseGlobalSearch decodes a GlobalSearchQuery data object. A section typed
// as an error union member becomes a warning instead of failing the call,
// except UserNotAuthorizedError on the videos section (the user's own
// videos), which is returned as a typed *UnionError for normalizeAuth to
// report as a signed-out session.
func ParseGlobalSearch(query string, data json.RawMessage) (*SearchResults, error) {
	out := NewSearchResults(query)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("%s: %w", OpSearch, err)
	}
	var authErr error
	section := func(name string) (json.RawMessage, bool) {
		raw, ok := obj[name]
		if !ok || len(raw) == 0 || string(raw) == "null" {
			return nil, false
		}
		if err := CheckUnion(OpSearch, raw); err != nil {
			var ue *UnionError
			if name == "videos" && errors.As(err, &ue) && ue.Typename == "UserNotAuthorizedError" {
				authErr = err
			}
			out.Warnings = append(out.Warnings, name+": "+err.Error())
			return nil, false
		}
		return raw, true
	}
	if raw, ok := section("videos"); ok {
		var r struct {
			Results *struct {
				Nodes []struct {
					Video *struct {
						ID               string  `json:"id"`
						Name             string  `json:"name"`
						SharePageURI     string  `json:"sharePageUri"`
						PlayableDuration float64 `json:"playable_duration"`
						CreatedAt        string  `json:"createdAt"`
						Owner            *struct {
							DisplayName string `json:"display_name"`
						} `json:"owner"`
					} `json:"video"`
					MatchedFields []struct {
						FieldName string `json:"fieldName"`
					} `json:"matchedFields"`
				} `json:"nodes"`
			} `json:"videoResults"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("%s videos: %w", OpSearch, err)
		}
		if r.Results != nil {
			for _, n := range r.Results.Nodes {
				if n.Video == nil || n.Video.ID == "" {
					continue
				}
				hit := SearchVideoHit{
					ID:              n.Video.ID,
					Name:            n.Video.Name,
					ShareURL:        ShareURL(n.Video.ID),
					CreatedAt:       n.Video.CreatedAt,
					DurationSeconds: round3(n.Video.PlayableDuration),
					Duration:        FormatClock(n.Video.PlayableDuration),
					MatchedFields:   make([]string, 0, len(n.MatchedFields)),
				}
				if n.Video.Owner != nil {
					hit.Owner = TitleName(n.Video.Owner.DisplayName)
				}
				for _, f := range n.MatchedFields {
					hit.MatchedFields = append(hit.MatchedFields, f.FieldName)
				}
				out.Videos = append(out.Videos, hit)
			}
		}
	}
	if raw, ok := section("users"); ok {
		var r struct {
			Users []struct {
				ID          json.Number `json:"id"`
				DisplayName string      `json:"display_name"`
				Email       string      `json:"email"`
				ProfileURI  string      `json:"profileUri"`
				Profile     *struct {
					Count *int `json:"profileVideoCount"`
				} `json:"profile"`
			} `json:"users"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("%s users: %w", OpSearch, err)
		}
		for _, u := range r.Users {
			hit := SearchUserHit{ID: u.ID.String(), Name: TitleName(u.DisplayName), Email: u.Email, ProfileURL: u.ProfileURI}
			if u.Profile != nil {
				hit.VideoCount = u.Profile.Count
			}
			out.Users = append(out.Users, hit)
		}
	}
	if raw, ok := section("tags"); ok {
		var r struct {
			Tags []struct {
				Tag   string `json:"tag"`
				Count int    `json:"count"`
				URI   string `json:"uri"`
			} `json:"tags"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("%s tags: %w", OpSearch, err)
		}
		for _, t := range r.Tags {
			out.Tags = append(out.Tags, SearchTagHit{Tag: t.Tag, Count: t.Count, URL: t.URI})
		}
	}
	if raw, ok := section("folders"); ok {
		var r struct {
			Folders []struct {
				ID                string `json:"id"`
				Name              string `json:"name"`
				TotalNestedVideos *int   `json:"totalNestedVideos"`
				Owner             *struct {
					DisplayName string `json:"display_name"`
				} `json:"owner"`
				Space *struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"space"`
			} `json:"folders"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("%s folders: %w", OpSearch, err)
		}
		for _, f := range r.Folders {
			hit := SearchFolderHit{ID: f.ID, Name: f.Name, VideoCount: f.TotalNestedVideos}
			if f.Owner != nil {
				hit.Owner = TitleName(f.Owner.DisplayName)
			}
			if f.Space != nil {
				hit.SpaceID, hit.SpaceName = f.Space.ID, f.Space.Name
			}
			out.Folders = append(out.Folders, hit)
		}
	}
	if raw, ok := section("playlists"); ok {
		var r struct {
			Playlists []struct {
				PlaylistID string      `json:"playlistId"`
				Name       string      `json:"name"`
				VideoCount int         `json:"videoCount"`
				OwnerID    json.Number `json:"ownerId"`
			} `json:"playlists"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("%s playlists: %w", OpSearch, err)
		}
		for _, p := range r.Playlists {
			out.Playlists = append(out.Playlists, SearchPlaylistHit{ID: p.PlaylistID, Name: p.Name, VideoCount: p.VideoCount, OwnerID: p.OwnerID.String()})
		}
	}
	if raw, ok := section("spaces"); ok {
		var r struct {
			Results *struct {
				Nodes []struct {
					Space *struct {
						ID           string `json:"id"`
						Name         string `json:"name"`
						MembersCount int    `json:"membersCount"`
						TotalVideos  int    `json:"totalVideos"`
						IsArchived   bool   `json:"isArchived"`
					} `json:"space"`
				} `json:"nodes"`
			} `json:"spaceResults"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("%s spaces: %w", OpSearch, err)
		}
		if r.Results != nil {
			for _, n := range r.Results.Nodes {
				if n.Space == nil {
					continue
				}
				s := n.Space
				out.Spaces = append(out.Spaces, SearchSpaceHit{ID: s.ID, Name: s.Name, MembersCount: s.MembersCount, TotalVideos: s.TotalVideos, Archived: s.IsArchived})
			}
		}
	}
	if authErr != nil {
		return nil, authErr
	}
	return out, nil
}
