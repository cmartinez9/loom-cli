package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// FetchTags returns the tags on a video (GetTagsByVideoId). The payload is
// aliased as `result` in the operation document.
func (l *Client) FetchTags(ctx context.Context, id string) ([]string, error) {
	data, err := l.Query(ctx, OpTags, map[string]any{"videoId": id})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	if l.IsDryRun() {
		return out, nil
	}
	raw, err := l.field(ctx, OpTags, data, "result")
	if err != nil {
		return nil, err
	}
	var r struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpTags, err)
	}
	for _, t := range r.Tags {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

// Backlink is a place a video is linked or embedded (Confluence, Jira, ...).
type Backlink struct {
	ID         string `json:"id"`
	IsSynced   bool   `json:"is_synced"`
	Source     string `json:"source"`
	SourceLink string `json:"source_link"`
	Title      string `json:"title"`
}

// FetchBacklinks returns where a video is linked from (GetVideoBacklinks).
func (l *Client) FetchBacklinks(ctx context.Context, id string) ([]Backlink, error) {
	data, err := l.Query(ctx, OpBacklinks, map[string]any{"videoId": id})
	if err != nil {
		return nil, err
	}
	out := make([]Backlink, 0)
	if l.IsDryRun() {
		return out, nil
	}
	raw, err := l.field(ctx, OpBacklinks, data, "getVideoBacklinks")
	if err != nil {
		return nil, err
	}
	var r struct {
		Backlinks []struct {
			ID         flexID `json:"id"`
			IsSynced   bool   `json:"isSynced"`
			Source     string `json:"source"`
			SourceLink string `json:"sourceLink"`
			Title      string `json:"title"`
		} `json:"backlinks"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpBacklinks, err)
	}
	for _, b := range r.Backlinks {
		out = append(out, Backlink{ID: b.ID.String(), IsSynced: b.IsSynced, Source: b.Source, SourceLink: b.SourceLink, Title: b.Title})
	}
	return out, nil
}

// VideoLink is a call-to-action link shown inside the player.
type VideoLink struct {
	ID        string  `json:"id"`
	URL       string  `json:"url"`
	Title     string  `json:"title"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Timestamp string  `json:"timestamp"`
	Pinned    bool    `json:"pinned"`
	Auto      bool    `json:"auto"`
}

// FetchVideoLinks returns a video's in-player CTA links (VideoLinksAndSettings).
func (l *Client) FetchVideoLinks(ctx context.Context, id string) ([]VideoLink, error) {
	data, err := l.Query(ctx, OpLinks, map[string]any{"videoId": id})
	if err != nil {
		return nil, err
	}
	out := make([]VideoLink, 0)
	if l.IsDryRun() {
		return out, nil
	}
	raw, err := l.field(ctx, OpLinks, data, "videoLinksAndSettings")
	if err != nil {
		return nil, err
	}
	var r struct {
		Links []struct {
			ID      flexID   `json:"id"`
			URL     string   `json:"url"`
			Title   string   `json:"title"`
			IsAuto  bool     `json:"isAuto"`
			Pinned  bool     `json:"pinned"`
			StartMs *float64 `json:"startMs"`
			EndMs   *float64 `json:"endMs"`
		} `json:"links"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpLinks, err)
	}
	for _, x := range r.Links {
		link := VideoLink{ID: x.ID.String(), URL: x.URL, Title: x.Title, Pinned: x.Pinned, Auto: x.IsAuto}
		if x.StartMs != nil {
			link.Start = MillisToSeconds(*x.StartMs)
		}
		if x.EndMs != nil {
			link.End = MillisToSeconds(*x.EndMs)
		}
		link.Timestamp = FormatClock(link.Start)
		out = append(out, link)
	}
	return out, nil
}

// flexID decodes an id that Loom may send as a JSON string or number.
type flexID string

func (f *flexID) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*f = flexID(v)
		return nil
	}
	*f = flexID(s)
	return nil
}

func (f flexID) String() string { return string(f) }

// missingVideoTypenames are union members Loom answers with when a video id
// is unknown or the signed-in user cannot open it.
var missingVideoTypenames = map[string]bool{
	"InvalidRequestWarning":  true,
	"NotFoundError":          true,
	"PrivateVideo":           true,
	"UserNotAuthorizedError": true,
}

// VideoExists probes GetVideoCardDetails to tell "this video does not exist
// (or is not visible to you)" apart from other failures. Per-video
// operations disagree on how they report unknown ids (InvalidRequestWarning,
// GenericError, a null field, an empty list), so commands call this on
// their error and empty paths. It returns (false, nil) when Loom has no
// video the user can open, and an error only when the probe itself failed.
func (l *Client) VideoExists(ctx context.Context, id string) (bool, error) {
	data, err := l.Query(ctx, OpVideoDetails, map[string]any{"videoId": id})
	if err != nil {
		return false, err
	}
	if l.IsDryRun() {
		return true, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return false, fmt.Errorf("%s: %w", OpVideoDetails, err)
	}
	raw := obj["video"]
	if len(raw) == 0 || string(raw) == "null" {
		return l.videoMissing(ctx)
	}
	var head struct {
		Typename string `json:"__typename"`
		ID       string `json:"id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return false, fmt.Errorf("%s: %w", OpVideoDetails, err)
	}
	if missingVideoTypenames[head.Typename] {
		return l.videoMissing(ctx)
	}
	return head.ID != "" || head.Typename != "", nil
}

// videoMissing is VideoExists' answer for every "no such video" shape (a
// null video, NotFoundError, PrivateVideo, InvalidRequestWarning,
// UserNotAuthorizedError): Loom gives a signed-out session the same
// answers, so the session is checked (one shared, cached Me probe) before
// reporting the video as missing.
func (l *Client) videoMissing(ctx context.Context) (bool, error) {
	if l.sessionGone(ctx) {
		return false, ErrSessionExpired
	}
	return false, nil
}
