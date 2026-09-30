package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The signed-in user's recent recordings, video activity feed and
// calendar meetings.

// RecentVideo is one recently recorded video. Transcript text is never kept.
type RecentVideo struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	CreatedAt       string  `json:"created_at"`
	DurationSeconds float64 `json:"duration_seconds"`
	Duration        string  `json:"duration"`
	Summary         string  `json:"summary,omitempty"`
	Complete        bool    `json:"complete"`
	Owner           string  `json:"owner,omitempty"`
	OwnerID         string  `json:"owner_id,omitempty"`
	RecordingType   string  `json:"recording_type,omitempty"`
	ShareURL        string  `json:"share_url"`
}

// RecentUserVideos lists videos the user recorded between start and end
// (RFC3339). The operation also returns transcript phrases; they are
// discarded here.
func (l *Client) RecentUserVideos(ctx context.Context, start, end string, offset, limit int) ([]RecentVideo, error) {
	out := make([]RecentVideo, 0)
	if limit <= 0 {
		limit = 25
	}
	data, err := l.Query(ctx, OpRecentVideos, map[string]any{"startDate": start, "endDate": end, "offset": offset, "limit": limit})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return out, nil
	}
	return ParseRecentVideos(data)
}

// ParseRecentVideos decodes a RecentUserVideos data object, dropping
// transcript_phrases.
func ParseRecentVideos(data json.RawMessage) ([]RecentVideo, error) {
	out := make([]RecentVideo, 0)
	var r struct {
		Videos []struct {
			ID               string      `json:"id"`
			OwnerID          json.Number `json:"owner_id"`
			Name             string      `json:"name"`
			Complete         *bool       `json:"complete"`
			Summary          *string     `json:"summary"`
			PlayableDuration float64     `json:"playable_duration"`
			CreatedAt        string      `json:"createdAt"`
			VideoProperties  *struct {
				RecordingType string `json:"recording_type"`
			} `json:"video_properties"`
			Owner *struct {
				DisplayName string `json:"display_name"`
			} `json:"owner"`
		} `json:"recentUserVideos"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpRecentVideos, err)
	}
	for _, v := range r.Videos {
		if v.ID == "" {
			continue
		}
		rv := RecentVideo{ID: v.ID, Name: v.Name, CreatedAt: v.CreatedAt, DurationSeconds: round3(v.PlayableDuration),
			Duration: FormatClock(v.PlayableDuration), OwnerID: v.OwnerID.String(), ShareURL: ShareURL(v.ID)}
		if v.Complete != nil {
			rv.Complete = *v.Complete
		}
		if v.Summary != nil {
			rv.Summary = *v.Summary
		}
		if v.VideoProperties != nil {
			rv.RecordingType = v.VideoProperties.RecordingType
		}
		if v.Owner != nil {
			rv.Owner = TitleName(v.Owner.DisplayName)
		}
		out = append(out, rv)
	}
	return out, nil
}

// Activity is one entry of the user's video activity history.
type Activity struct {
	Type      string `json:"type"`
	Time      string `json:"time"`
	VideoID   string `json:"video_id"`
	VideoName string `json:"video_name"`
	ShareURL  string `json:"share_url"`
}

// ActivityFeed returns the user's recent video activity (views, reactions,
// comments), newest first. Entries whose video is gone are dropped.
func (l *Client) ActivityFeed(ctx context.Context, limit, offset int) ([]Activity, error) {
	out := make([]Activity, 0)
	if limit <= 0 {
		limit = 25
	}
	data, err := l.Query(ctx, OpActivityFeed, map[string]any{"limit": limit, "offset": offset})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return out, nil
	}
	var r struct {
		CurrentUser *struct {
			Typename string `json:"__typename"`
			History  []struct {
				Type   string `json:"activityType"`
				Time   string `json:"activityTime"`
				Target *struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"target"`
			} `json:"activityHistory"`
		} `json:"currentUser"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpActivityFeed, err)
	}
	if r.CurrentUser == nil || r.CurrentUser.Typename == "UserNotAuthorizedError" {
		return nil, ErrSessionExpired
	}
	if strings.HasSuffix(r.CurrentUser.Typename, "Error") {
		return nil, &UnionError{Op: OpActivityFeed, Typename: r.CurrentUser.Typename}
	}
	for _, h := range r.CurrentUser.History {
		if h.Target == nil || h.Target.ID == "" {
			continue
		}
		out = append(out, Activity{Type: h.Type, Time: h.Time, VideoID: h.Target.ID, VideoName: h.Target.Name, ShareURL: ShareURL(h.Target.ID)})
	}
	return out, nil
}

// Meeting is one calendar meeting Loom knows about.
type Meeting struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	StartsAt        string `json:"starts_at"`
	DurationMinutes int    `json:"duration_minutes"`
	Platform        string `json:"platform,omitempty"`
	URL             string `json:"url,omitempty"`
	Organizer       string `json:"organizer,omitempty"`
	AttendeeCount   int    `json:"attendee_count"`
	Recurring       bool   `json:"recurring"`
	Record          bool   `json:"record"`
	Past            bool   `json:"past"`
	VideoID         string `json:"video_id,omitempty"`
	CalendarID      string `json:"calendar_id,omitempty"`
}

// CalendarMeetings lists meetings on the user's connected calendars between
// start and end (RFC3339).
func (l *Client) CalendarMeetings(ctx context.Context, start, end string) ([]Meeting, error) {
	out := make([]Meeting, 0)
	data, err := l.Query(ctx, OpCalendarMeetings, map[string]any{"rangeStart": start, "rangeEnd": end})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return out, nil
	}
	return ParseCalendarMeetings(data)
}

// ParseCalendarMeetings flattens every connected calendar's meetings.
func ParseCalendarMeetings(data json.RawMessage) ([]Meeting, error) {
	out := make([]Meeting, 0)
	var r struct {
		Me *struct {
			Calendars []struct {
				GUID     string `json:"guid"`
				Meetings []struct {
					GUID          string  `json:"guid"`
					Title         string  `json:"title"`
					StartsAt      string  `json:"startsAt"`
					Platform      string  `json:"platform"`
					URL           string  `json:"url"`
					DurationMins  float64 `json:"durationMins"`
					Recurring     bool    `json:"recurring"`
					AttendeeCount int     `json:"attendeeCount"`
					Record        bool    `json:"record"`
					Past          bool    `json:"past"`
					VideoID       *string `json:"videoId"`
					Organizer     *struct {
						FirstName string `json:"first_name"`
						LastName  string `json:"last_name"`
					} `json:"organizer"`
				} `json:"meetings"`
			} `json:"calendars"`
		} `json:"me"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpCalendarMeetings, err)
	}
	if r.Me == nil {
		return nil, ErrSessionExpired
	}
	for _, c := range r.Me.Calendars {
		for _, m := range c.Meetings {
			mt := Meeting{ID: m.GUID, Title: m.Title, StartsAt: m.StartsAt, DurationMinutes: int(m.DurationMins), Platform: m.Platform, URL: m.URL,
				AttendeeCount: m.AttendeeCount, Recurring: m.Recurring, Record: m.Record, Past: m.Past, CalendarID: c.GUID}
			if m.VideoID != nil {
				mt.VideoID = *m.VideoID
			}
			if m.Organizer != nil {
				mt.Organizer = strings.TrimSpace(TitleName(m.Organizer.FirstName) + " " + TitleName(m.Organizer.LastName))
			}
			out = append(out, mt)
		}
	}
	return out, nil
}
