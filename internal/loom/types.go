package loom

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Video is the normalized library record the CLI prints and stores. Field
// names match the spec's Video type so the generated typed store table
// populates its columns from these keys.
type Video struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	CreatedAt       string   `json:"created_at"`
	DurationSeconds float64  `json:"duration_seconds"`
	Duration        string   `json:"duration"`
	Views           int      `json:"views"`
	UniqueViews     int      `json:"unique_views,omitempty"`
	Comments        int      `json:"comments"`
	Reactions       int      `json:"reactions"`
	Privacy         string   `json:"privacy,omitempty"`
	Visibility      string   `json:"visibility,omitempty"`
	Archived        bool     `json:"archived"`
	FolderID        string   `json:"folder_id,omitempty"`
	FolderName      string   `json:"folder_name,omitempty"`
	OwnerID         string   `json:"owner_id,omitempty"`
	OwnerName       string   `json:"owner_name,omitempty"`
	IsOwner         bool     `json:"is_owner"`
	DownloadEnabled bool     `json:"download_enabled"`
	CommentsEnabled *bool    `json:"comments_enabled,omitempty"`
	Tags            []string `json:"tags"`
	Spaces          []Ref    `json:"spaces,omitempty"`
	WorkspaceID     string   `json:"workspace_id,omitempty"`
	WorkspaceName   string   `json:"workspace_name,omitempty"`
	Complete        *bool    `json:"complete,omitempty"`
	Width           int      `json:"width,omitempty"`
	Height          int      `json:"height,omitempty"`
	ThumbnailURL    string   `json:"thumbnail_url,omitempty"`
	ShareURL        string   `json:"share_url"`
}

// Ref is an id/name pair.
type Ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// rawVideo mirrors the RegularUserVideo fields the ops select.
type rawVideo struct {
	Typename          string      `json:"__typename"`
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Description       *string     `json:"description"`
	CreatedAt         string      `json:"createdAt"`
	Archived          bool        `json:"archived"`
	Complete          *bool       `json:"complete"`
	PlayableDuration  float64     `json:"playable_duration"`
	TotalReactions    int         `json:"totalReactions"`
	TotalComments     int         `json:"totalComments"`
	OwnerID           json.Number `json:"owner_id"`
	CurrentUserIsOwnr bool        `json:"current_user_is_owner"`
	DownloadEnabled   bool        `json:"download_enabled"`
	CommentsEnabled   *bool       `json:"comments_enabled"`
	Privacy           string      `json:"privacy"`
	Visibility        string      `json:"visibility"`
	Tags              []string    `json:"tags"`
	Message           string      `json:"message"`
	Views             *struct {
		Total    int `json:"total"`
		Distinct int `json:"distinct"`
	} `json:"views"`
	Owner *struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"owner"`
	Folder *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"folder"`
	Organization *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"organization"`
	Spaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"spaces"`
	VideoProperties *struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"video_properties"`
	Thumbnails *struct {
		Default string `json:"default"`
	} `json:"thumbnails"`
}

// NormalizeVideo converts a RegularUserVideo JSON object into a Video.
func NormalizeVideo(raw json.RawMessage) (Video, error) {
	var r rawVideo
	if err := json.Unmarshal(raw, &r); err != nil {
		return Video{}, fmt.Errorf("decoding video: %w", err)
	}
	if r.ID == "" {
		return Video{}, fmt.Errorf("video object has no id")
	}
	v := Video{
		ID:              r.ID,
		Name:            r.Name,
		CreatedAt:       r.CreatedAt,
		DurationSeconds: round3(r.PlayableDuration),
		Duration:        FormatClock(r.PlayableDuration),
		Comments:        r.TotalComments,
		Reactions:       r.TotalReactions,
		Privacy:         r.Privacy,
		Visibility:      r.Visibility,
		Archived:        r.Archived,
		OwnerID:         r.OwnerID.String(),
		IsOwner:         r.CurrentUserIsOwnr,
		DownloadEnabled: r.DownloadEnabled,
		CommentsEnabled: r.CommentsEnabled,
		Tags:            r.Tags,
		Complete:        r.Complete,
		ShareURL:        ShareURL(r.ID),
	}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	if r.Description != nil {
		v.Description = *r.Description
	}
	if r.Views != nil {
		v.Views = r.Views.Total
		v.UniqueViews = r.Views.Distinct
	}
	if r.Owner != nil {
		v.OwnerName = strings.TrimSpace(TitleName(r.Owner.FirstName) + " " + TitleName(r.Owner.LastName))
	}
	if r.Folder != nil {
		v.FolderID, v.FolderName = r.Folder.ID, r.Folder.Name
	}
	if r.Organization != nil {
		v.WorkspaceID, v.WorkspaceName = r.Organization.ID, r.Organization.Name
	}
	for _, s := range r.Spaces {
		v.Spaces = append(v.Spaces, Ref{ID: s.ID, Name: s.Name})
	}
	if r.VideoProperties != nil {
		v.Width, v.Height = r.VideoProperties.Width, r.VideoProperties.Height
	}
	if r.Thumbnails != nil {
		v.ThumbnailURL = r.Thumbnails.Default
	}
	return v, nil
}

// TitleName turns an all-caps account name ("ANN LEE") into "Ann Lee";
// mixed-case names are left alone.
func TitleName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s != strings.ToUpper(s) {
		return s
	}
	parts := strings.Fields(strings.ToLower(s))
	for i, p := range parts {
		r, size := utf8.DecodeRuneInString(p)
		parts[i] = string(unicode.ToUpper(r)) + p[size:]
	}
	return strings.Join(parts, " ")
}

var (
	hexIDRe  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	urlIDRe  = regexp.MustCompile(`loom\.com/(?:share|embed|v|i)/(?:[^/?#]*-)?([0-9a-f]{32})`)
	anyHexRe = regexp.MustCompile(`([0-9a-f]{32})`)
)

// ParseVideoID accepts a 32-hex video id or any loom.com share/embed URL
// (including slugged "name-<id>" forms and ?t= / ?sid= query strings).
func ParseVideoID(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", fmt.Errorf("video id is empty")
	}
	lower := strings.ToLower(s)
	if hexIDRe.MatchString(lower) {
		return lower, nil
	}
	if m := urlIDRe.FindStringSubmatch(lower); m != nil {
		return m[1], nil
	}
	if strings.Contains(lower, "loom.com") {
		if m := anyHexRe.FindStringSubmatch(lower); m != nil {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("%q is not a Loom video id (32 hex characters) or a loom.com/share/<id> URL", input)
}

// ShareURL is the canonical share page for a video.
func ShareURL(id string) string { return "https://www.loom.com/share/" + id }

// DeepLink is a share URL that starts playback at the given second.
func DeepLink(id string, seconds float64) string {
	s := int(seconds)
	if s <= 0 {
		return ShareURL(id)
	}
	return ShareURL(id) + "?t=" + strconv.Itoa(s)
}

// FormatClock renders seconds as h:mm:ss (or m:ss under an hour).
func FormatClock(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds)
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// ParseClock parses "mm:ss", "hh:mm:ss" or plain seconds into seconds.
func ParseClock(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty timestamp")
	}
	if !strings.Contains(s, ":") {
		return strconv.ParseFloat(s, 64)
	}
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("bad timestamp %q", s)
	}
	var total float64
	for _, p := range parts {
		n, err := strconv.ParseFloat(p, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("bad timestamp %q", s)
		}
		total = total*60 + n
	}
	return total, nil
}

// ParseTime parses Loom's ISO timestamps.
func ParseTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func round3(f float64) float64 {
	return float64(int64(f*1000+0.5)) / 1000
}

// PageInfo is a Relay page cursor.
type PageInfo struct {
	EndCursor   string `json:"end_cursor"`
	HasNextPage bool   `json:"has_next_page"`
}
