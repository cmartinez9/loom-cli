package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The signed-in user, their selected workspace, and other users' profiles.

// Me is the signed-in user.
type Me struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email,omitempty"`
}

// CurrentUser returns the signed-in user, or ErrSessionExpired.
func (l *Client) CurrentUser(ctx context.Context) (*Me, error) {
	m, expired, err := l.me(ctx)
	l.authMu.Lock()
	l.noteAuthLocked(expired, err)
	l.authMu.Unlock()
	return m, err
}

// me asks Loom who is signed in. expired reports a definitive "no session"
// answer (err is then ErrSessionExpired).
func (l *Client) me(ctx context.Context) (*Me, bool, error) {
	data, err := l.Query(ctx, OpMe, nil)
	if err != nil {
		return nil, errors.Is(err, ErrSessionExpired), err
	}
	var out struct {
		Me *struct {
			ID        json.Number `json:"id"`
			FirstName string      `json:"first_name"`
			LastName  string      `json:"last_name"`
			Email     string      `json:"email"`
		} `json:"me"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, false, fmt.Errorf("Me: %w", err)
	}
	if out.Me == nil {
		return nil, true, ErrSessionExpired
	}
	return &Me{ID: out.Me.ID.String(), FirstName: out.Me.FirstName, LastName: out.Me.LastName, Email: out.Me.Email}, false, nil
}

// Workspace is the selected Loom workspace (organization).
type Workspace struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type,omitempty"`
	MembershipID string `json:"membership_id,omitempty"`
	Role         string `json:"role,omitempty"`
}

// SelectedWorkspace returns the signed-in user's selected workspace.
func (l *Client) SelectedWorkspace(ctx context.Context) (*Workspace, error) {
	data, err := l.Query(ctx, OpSelectedWorkspace, nil)
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return &Workspace{}, nil
	}
	raw, err := l.field(ctx, OpSelectedWorkspace, data, "selectedWorkspaceMembership")
	if err != nil {
		return nil, err
	}
	var r struct {
		Membership *struct {
			ID           string `json:"id"`
			Role         string `json:"member_role"`
			Organization *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"organization"`
		} `json:"workspaceMembership"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpSelectedWorkspace, err)
	}
	if r.Membership == nil || r.Membership.Organization == nil || r.Membership.Organization.ID == "" {
		return nil, fmt.Errorf("%s: no workspace selected for this account", OpSelectedWorkspace)
	}
	o := r.Membership.Organization
	return &Workspace{ID: o.ID, Name: o.Name, Type: o.Type, MembershipID: r.Membership.ID, Role: r.Membership.Role}, nil
}

// UserProfile is a Loom user with profile fields.
type UserProfile struct {
	ID                  string `json:"id"`
	FirstName           string `json:"first_name"`
	LastName            string `json:"last_name"`
	Name                string `json:"name"`
	Email               string `json:"email,omitempty"`
	Kind                string `json:"kind"`
	AvatarURL           string `json:"avatar_url,omitempty"`
	ProfileURL          string `json:"profile_url,omitempty"`
	ProfileVideoCount   *int   `json:"profile_video_count,omitempty"`
	CommunityVideoCount *int   `json:"community_video_count,omitempty"`
	Role                string `json:"role,omitempty"`
	Location            string `json:"location,omitempty"`
}

// UserByID returns a user's profile.
func (l *Client) UserByID(ctx context.Context, id string) (*UserProfile, error) {
	data, err := l.Query(ctx, OpUser, map[string]any{"userId": id})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return &UserProfile{ID: id}, nil
	}
	return ParseUserProfile(id, data)
}

// ParseUserProfile decodes a GetUserByIdWithProfile data object.
func ParseUserProfile(id string, data json.RawMessage) (*UserProfile, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("%s: %w", OpUser, err)
	}
	raw := obj["user"]
	if len(raw) == 0 || string(raw) == "null" {
		return nil, &NotFoundError{Kind: "user", ID: id}
	}
	var r struct {
		Typename string `json:"__typename"`
		Message  string `json:"message"`
		User     *struct {
			ID        json.Number `json:"id"`
			Email     string      `json:"email"`
			FirstName string      `json:"first_name"`
			LastName  string      `json:"last_name"`
			Avatars   []struct {
				Thumb string `json:"thumb"`
				Large string `json:"large"`
			} `json:"avatars"`
			Profile *struct {
				ProfileURL          string `json:"profileUrl"`
				ProfileVideoCount   *int   `json:"profileVideoCount"`
				CommunityVideoCount *int   `json:"communityVideoCount"`
				ProfileInfo         *struct {
					Role     string `json:"role"`
					Location string `json:"location"`
				} `json:"profileInfo"`
			} `json:"profile"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpUser, err)
	}
	if r.User == nil {
		if r.Typename == "UserNotAuthorizedError" {
			return nil, &UnionError{Op: OpUser, Typename: r.Typename, Message: r.Message}
		}
		return nil, &NotFoundError{Kind: "user", ID: id, Msg: r.Message}
	}
	u := r.User
	p := &UserProfile{ID: u.ID.String(), FirstName: TitleName(u.FirstName), LastName: TitleName(u.LastName), Email: u.Email}
	p.Name = strings.TrimSpace(p.FirstName + " " + p.LastName)
	switch r.Typename {
	case "CommunityUserPayload":
		p.Kind = "community"
	default:
		p.Kind = "regular"
	}
	if len(u.Avatars) > 0 {
		p.AvatarURL = u.Avatars[0].Large
		if p.AvatarURL == "" {
			p.AvatarURL = u.Avatars[0].Thumb
		}
	}
	if u.Profile != nil {
		p.ProfileURL = u.Profile.ProfileURL
		if p.ProfileURL != "" && !strings.HasPrefix(p.ProfileURL, "http") {
			p.ProfileURL = "https://www.loom.com/profile/" + p.ProfileURL
		}
		p.ProfileVideoCount = u.Profile.ProfileVideoCount
		p.CommunityVideoCount = u.Profile.CommunityVideoCount
		if u.Profile.ProfileInfo != nil {
			p.Role, p.Location = u.Profile.ProfileInfo.Role, u.Profile.ProfileInfo.Location
		}
	}
	return p, nil
}

// UserVideoCount returns how many videos a user has in a workspace.
func (l *Client) UserVideoCount(ctx context.Context, userID, workspaceID string) (int, error) {
	vars := map[string]any{"userId": userID}
	if workspaceID != "" {
		vars["workspaceId"] = workspaceID
	}
	data, err := l.Query(ctx, OpUserVideoCount, vars)
	if err != nil {
		return 0, err
	}
	if l.IsDryRun() {
		return 0, nil
	}
	raw, err := l.field(ctx, OpUserVideoCount, data, "getTotalVideosCountByUser")
	if err != nil {
		return 0, err
	}
	var r struct {
		Count int `json:"videos_count"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return 0, fmt.Errorf("%s: %w", OpUserVideoCount, err)
	}
	return r.Count, nil
}
