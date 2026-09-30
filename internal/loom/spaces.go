package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Space is a Loom space the user belongs to.
type Space struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Privacy   string `json:"privacy,omitempty"`
	IsPrimary bool   `json:"is_primary"`
}

// SpaceMembership is a space the user belongs to.
type SpaceMembership struct {
	Space
	MembershipID string `json:"membership_id,omitempty"`
	Unread       bool   `json:"unread"`
}

// SpaceMemberships lists the spaces the signed-in user belongs to.
func (l *Client) SpaceMemberships(ctx context.Context, first int) ([]SpaceMembership, error) {
	out, _, err := l.SpaceMembershipsPage(ctx, first, "")
	return out, err
}

// SpaceMembershipsPage returns one page of the user's space memberships.
// Under --dry-run it returns an empty page.
func (l *Client) SpaceMembershipsPage(ctx context.Context, first int, after string) ([]SpaceMembership, PageInfo, error) {
	out := make([]SpaceMembership, 0)
	if first <= 0 || first > 100 {
		first = 50
	}
	vars := map[string]any{"first": first}
	if after != "" {
		vars["after"] = after
	}
	data, err := l.Query(ctx, OpSpaceMemberships, vars)
	if err != nil {
		return nil, PageInfo{}, err
	}
	if l.IsDryRun() {
		return out, PageInfo{}, nil
	}
	payload, err := l.field(ctx, OpSpaceMemberships, data, "result")
	if err != nil {
		return nil, PageInfo{}, err
	}
	var r struct {
		Memberships *struct {
			Edges []struct {
				Node struct {
					ID     string `json:"id"`
					Unread bool   `json:"unread"`
					Space  *Space `json:"space"`
				} `json:"node"`
			} `json:"edges"`
			PageInfo struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"memberships"`
	}
	if err := json.Unmarshal(payload, &r); err != nil {
		return nil, PageInfo{}, fmt.Errorf("%s: %w", OpSpaceMemberships, err)
	}
	if r.Memberships == nil {
		return out, PageInfo{}, nil
	}
	for _, e := range r.Memberships.Edges {
		if e.Node.Space == nil {
			continue
		}
		out = append(out, SpaceMembership{Space: *e.Node.Space, MembershipID: e.Node.ID, Unread: e.Node.Unread})
	}
	return out, PageInfo{EndCursor: r.Memberships.PageInfo.EndCursor, HasNextPage: r.Memberships.PageInfo.HasNextPage}, nil
}

// SpaceInfo is a space with its counts.
type SpaceInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Privacy      string   `json:"privacy,omitempty"`
	IsPrimary    bool     `json:"is_primary"`
	TotalVideos  int      `json:"total_videos"`
	TotalFolders *int     `json:"total_folders,omitempty"`
	MembersCount int      `json:"members_count"`
	IsMember     bool     `json:"is_member"`
	Archived     bool     `json:"archived"`
	CanEdit      *bool    `json:"can_edit,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
	TopTags      []string `json:"top_tags,omitempty"`
}

type rawSpace struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Privacy             string `json:"privacy"`
	IsPrimary           bool   `json:"is_primary"`
	TotalVideos         int    `json:"totalVideos"`
	TotalFolders        *int   `json:"totalFolders"`
	MembersCount        int    `json:"membersCount"`
	CurrentUserIsMember bool   `json:"currentUserIsMember"`
	IsArchived          bool   `json:"isArchived"`
	CurrentUserCanEdit  *bool  `json:"currentUserCanEdit"`
	CreatedAt           string `json:"createdAt"`
	TopUsedTags         []struct {
		Tag string `json:"tag"`
	} `json:"topUsedTags"`
}

func (r rawSpace) info() SpaceInfo {
	s := SpaceInfo{ID: r.ID, Name: r.Name, Privacy: r.Privacy, IsPrimary: r.IsPrimary, TotalVideos: r.TotalVideos, TotalFolders: r.TotalFolders,
		MembersCount: r.MembersCount, IsMember: r.CurrentUserIsMember, Archived: r.IsArchived, CanEdit: r.CurrentUserCanEdit, CreatedAt: r.CreatedAt}
	for _, t := range r.TopUsedTags {
		s.TopTags = append(s.TopTags, t.Tag)
	}
	return s
}

// OpenSpaces lists the workspace's open spaces with their counts.
func (l *Client) OpenSpaces(ctx context.Context, first int) ([]SpaceInfo, error) {
	out := make([]SpaceInfo, 0)
	if first <= 0 || first > 100 {
		first = 50
	}
	data, err := l.Query(ctx, OpOpenSpaces, map[string]any{"first": first})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return out, nil
	}
	payload, err := l.field(ctx, OpOpenSpaces, data, "result")
	if err != nil {
		return nil, err
	}
	var r struct {
		Spaces *struct {
			Edges []struct {
				Node *rawSpace `json:"node"`
			} `json:"edges"`
		} `json:"spaces"`
	}
	if err := json.Unmarshal(payload, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpOpenSpaces, err)
	}
	if r.Spaces == nil {
		return out, nil
	}
	for _, e := range r.Spaces.Edges {
		if e.Node != nil {
			out = append(out, e.Node.info())
		}
	}
	return out, nil
}

// SpaceMember is one member of a space.
type SpaceMember struct {
	MembershipID string `json:"membership_id"`
	UserID       string `json:"user_id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Name         string `json:"name"`
	AvatarURL    string `json:"avatar_url,omitempty"`
}

// SpaceDetail is a space plus its first page of members.
type SpaceDetail struct {
	SpaceInfo
	Members []SpaceMember `json:"members"`
}

// SpaceDetails returns one space and up to first members.
func (l *Client) SpaceDetails(ctx context.Context, id string, first int) (*SpaceDetail, error) {
	if first <= 0 || first > 100 {
		first = 20
	}
	data, err := l.Query(ctx, OpSpace, map[string]any{"spaceId": id, "first": first})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return &SpaceDetail{SpaceInfo: SpaceInfo{ID: id}, Members: make([]SpaceMember, 0)}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("%s: %w", OpSpace, err)
	}
	spaceRaw := obj["getSpace"]
	if len(spaceRaw) == 0 || string(spaceRaw) == "null" {
		return nil, &NotFoundError{Kind: "space", ID: id}
	}
	if err := CheckUnion(OpSpace, spaceRaw); err != nil {
		var ue *UnionError
		if errors.As(err, &ue) && (ue.Typename == "UserNotAuthorizedError" || ue.Typename == "NotFoundError" || ue.Typename == "GenericError") {
			if serr := l.normalizeAuth(ctx, err); errors.Is(serr, ErrSessionExpired) {
				return nil, serr
			}
			return nil, &NotFoundError{Kind: "space", ID: id, Msg: ue.Message}
		}
		return nil, err
	}
	var sp struct {
		Space *rawSpace `json:"space"`
	}
	if err := json.Unmarshal(spaceRaw, &sp); err != nil {
		return nil, fmt.Errorf("%s: %w", OpSpace, err)
	}
	if sp.Space == nil {
		return nil, &NotFoundError{Kind: "space", ID: id}
	}
	out := &SpaceDetail{SpaceInfo: sp.Space.info(), Members: make([]SpaceMember, 0)}
	if membersRaw := obj["getSpaceMembers"]; len(membersRaw) > 0 && string(membersRaw) != "null" && CheckUnion(OpSpace, membersRaw) == nil {
		var mr struct {
			Members *struct {
				Edges []struct {
					Node struct {
						ID   string `json:"id"`
						User *struct {
							ID        json.Number `json:"id"`
							FirstName string      `json:"first_name"`
							LastName  string      `json:"last_name"`
							Avatars   []struct {
								Thumb string `json:"thumb"`
							} `json:"avatars"`
						} `json:"user"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"members"`
		}
		if err := json.Unmarshal(membersRaw, &mr); err == nil && mr.Members != nil {
			for _, e := range mr.Members.Edges {
				m := SpaceMember{MembershipID: e.Node.ID}
				if u := e.Node.User; u != nil {
					m.UserID = u.ID.String()
					m.FirstName, m.LastName = TitleName(u.FirstName), TitleName(u.LastName)
					m.Name = strings.TrimSpace(m.FirstName + " " + m.LastName)
					if len(u.Avatars) > 0 {
						m.AvatarURL = u.Avatars[0].Thumb
					}
				}
				out.Members = append(out.Members, m)
			}
		}
	}
	return out, nil
}
