package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Summary is Loom's AI summary (the auto-description feature).
type Summary struct {
	VideoID string `json:"video_id"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// FetchSummary returns Loom's AI summary for a video.
func (l *Client) FetchSummary(ctx context.Context, id string) (*Summary, error) {
	data, err := l.Query(ctx, OpSummary, map[string]any{"videoId": id})
	if err != nil {
		return nil, err
	}
	out := &Summary{VideoID: id}
	if l.IsDryRun() {
		return out, nil
	}
	raw, err := l.field(ctx, OpSummary, data, "getAutoFeatureStatuses")
	if err != nil {
		return nil, err
	}
	var r struct {
		Summary *string `json:"autoSummary"`
		Status  *string `json:"autoSummaryStatus"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpSummary, err)
	}
	if r.Summary != nil {
		out.Summary = *r.Summary
	}
	if r.Status != nil {
		out.Status = strings.ToLower(*r.Status)
	}
	if out.Status == "" && out.Summary != "" {
		out.Status = "success"
	}
	return out, nil
}

// Chapter is one AI chapter.
type Chapter struct {
	Start     float64 `json:"start"`
	Timestamp string  `json:"timestamp"`
	Title     string  `json:"title"`
	Link      string  `json:"link"`
}

// Chapters is a video's chapter list.
type Chapters struct {
	VideoID   string    `json:"video_id"`
	Status    string    `json:"status"`
	Edited    bool      `json:"edited"`
	UpdatedAt string    `json:"updated_at,omitempty"`
	Chapters  []Chapter `json:"chapters"`
}

// FetchChapters returns Loom's AI chapters for a video.
func (l *Client) FetchChapters(ctx context.Context, id string) (*Chapters, error) {
	data, err := l.Query(ctx, OpChapters, map[string]any{"videoId": id})
	if err != nil {
		return nil, err
	}
	out := &Chapters{VideoID: id, Chapters: make([]Chapter, 0)}
	if l.IsDryRun() {
		return out, nil
	}
	raw, err := l.field(ctx, OpChapters, data, "fetchVideoChapters")
	if err != nil {
		return nil, err
	}
	var r struct {
		Content   *string `json:"content"`
		Status    *string `json:"auto_chapter_status"`
		UpdatedAt string  `json:"updatedAt"`
		EditedAt  *string `json:"edited_at"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpChapters, err)
	}
	if r.Status != nil {
		out.Status = strings.ToLower(*r.Status)
	}
	out.UpdatedAt = r.UpdatedAt
	out.Edited = r.EditedAt != nil && *r.EditedAt != ""
	if r.Content != nil {
		out.Chapters = ParseChapters(id, *r.Content)
	}
	if out.Status == "" {
		if len(out.Chapters) > 0 {
			out.Status = "success"
		} else {
			out.Status = "none"
		}
	}
	return out, nil
}

var chapterLineRe = regexp.MustCompile(`^\s*((?:\d{1,2}:)?\d{1,2}:\d{2})\s*[-–—:]?\s*(.+?)\s*$`)

// ParseChapters parses Loom's "mm:ss Title" chapter text.
func ParseChapters(videoID, content string) []Chapter {
	out := make([]Chapter, 0)
	for _, line := range strings.Split(content, "\n") {
		m := chapterLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sec, err := ParseClock(m[1])
		if err != nil {
			continue
		}
		out = append(out, Chapter{Start: sec, Timestamp: FormatClock(sec), Title: m[2], Link: DeepLink(videoID, sec)})
	}
	return out
}

// Task is one AI action item.
type Task struct {
	ID         string  `json:"id"`
	VideoID    string  `json:"video_id"`
	Content    string  `json:"content"`
	Start      float64 `json:"start"`
	Timestamp  string  `json:"timestamp"`
	Link       string  `json:"link"`
	Source     string  `json:"source,omitempty"`
	Type       string  `json:"type,omitempty"`
	Owner      string  `json:"owner,omitempty"`
	CreatedAt  string  `json:"created_at,omitempty"`
	ApprovedAt string  `json:"approved_at,omitempty"`
	ResolvedAt string  `json:"resolved_at,omitempty"`
}

// FetchTasks returns Loom's action items for a video.
func (l *Client) FetchTasks(ctx context.Context, id string) ([]Task, error) {
	data, err := l.Query(ctx, OpTasks, map[string]any{"videoId": id})
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0)
	if l.IsDryRun() {
		return out, nil
	}
	raw, err := l.field(ctx, OpTasks, data, "getVideoTasks")
	if err != nil {
		return nil, err
	}
	var r struct {
		Tasks []struct {
			ID           json.Number `json:"id"`
			VideoID      string      `json:"video_id"`
			Content      string      `json:"content"`
			TimeStamp    *float64    `json:"time_stamp"`
			ActivityType string      `json:"activity_type"`
			Source       string      `json:"source"`
			CreatedAt    string      `json:"createdAt"`
			ApprovedAt   *string     `json:"approved_at"`
			ResolvedAt   *string     `json:"resolved_at"`
			Owner        *struct {
				DisplayName string `json:"display_name"`
			} `json:"owner"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpTasks, err)
	}
	for _, t := range r.Tasks {
		task := Task{ID: t.ID.String(), VideoID: id, Content: t.Content, Source: t.Source, Type: t.ActivityType, CreatedAt: t.CreatedAt}
		if t.TimeStamp != nil {
			task.Start = MillisToSeconds(*t.TimeStamp)
		}
		task.Timestamp = FormatClock(task.Start)
		task.Link = DeepLink(id, task.Start)
		if t.Owner != nil {
			task.Owner = TitleName(t.Owner.DisplayName)
		}
		if t.ApprovedAt != nil {
			task.ApprovedAt = *t.ApprovedAt
		}
		if t.ResolvedAt != nil {
			task.ResolvedAt = *t.ResolvedAt
		}
		out = append(out, task)
	}
	return out, nil
}

// Takeaways are Loom's meeting key takeaways.
type Takeaways struct {
	VideoID   string   `json:"video_id"`
	Available bool     `json:"available"`
	Takeaways []string `json:"takeaways"`
	Note      string   `json:"note,omitempty"`
}

// FetchTakeaways returns key takeaways (getMeetingTakeaways). Loom answers
// "No takeaways available" as a GenericError for non-meeting videos; that is
// reported as Available=false, not an error.
func (l *Client) FetchTakeaways(ctx context.Context, id string) (*Takeaways, error) {
	data, err := l.Query(ctx, OpTakeaways, map[string]any{"videoId": id})
	if err != nil {
		return nil, err
	}
	out := &Takeaways{VideoID: id, Takeaways: make([]string, 0)}
	if l.IsDryRun() {
		return out, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("%s: %w", OpTakeaways, err)
	}
	raw := obj["getMeetingTakeaways"]
	var head struct {
		Typename string `json:"__typename"`
		Message  string `json:"message"`
		Result   *struct {
			Takeaways json.RawMessage `json:"takeaways"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("%s: %w", OpTakeaways, err)
	}
	switch head.Typename {
	case "GetMeetingTakeawaysPayload":
	case "GenericError":
		out.Note = head.Message
		return out, nil
	default:
		if err := CheckUnion(OpTakeaways, raw); err != nil {
			return nil, l.normalizeAuth(ctx, err)
		}
	}
	if head.Result != nil {
		out.Takeaways = flattenTakeaways(head.Result.Takeaways)
	}
	out.Available = len(out.Takeaways) > 0
	return out, nil
}

// flattenTakeaways accepts the takeaways scalar as a string list, a single
// string, or a JSON object list with text/content fields.
func flattenTakeaways(raw json.RawMessage) []string {
	out := make([]string, 0)
	if len(raw) == 0 || string(raw) == "null" {
		return out
	}
	var list []any
	if json.Unmarshal(raw, &list) == nil {
		for _, item := range list {
			switch v := item.(type) {
			case string:
				if s := strings.TrimSpace(v); s != "" {
					out = append(out, s)
				}
			case map[string]any:
				for _, k := range []string{"text", "content", "title", "value"} {
					if s, ok := v[k].(string); ok && strings.TrimSpace(s) != "" {
						out = append(out, strings.TrimSpace(s))
						break
					}
				}
			}
		}
		return out
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		for _, line := range strings.Split(s, "\n") {
			line = strings.TrimSpace(strings.TrimLeft(line, "-*• "))
			if line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

// MillisToSeconds converts Loom's integer millisecond offsets (comment,
// task and reaction time_stamp/time fields) to seconds.
func MillisToSeconds(v float64) float64 {
	return round3(v / 1000)
}

// SecondsToMillis converts a playback offset in seconds to Loom's integer
// millisecond form for comment, task and reaction mutations.
func SecondsToMillis(sec float64) int {
	return int(sec*1000 + 0.5)
}
