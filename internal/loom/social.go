package loom

import (
	"context"
	"encoding/json"
	"fmt"
)

// Comment is one comment or reply on a video.
type Comment struct {
	ID        string    `json:"id"`
	VideoID   string    `json:"video_id"`
	ParentID  string    `json:"parent_id,omitempty"`
	Author    string    `json:"author"`
	AuthorID  string    `json:"author_id,omitempty"`
	Text      string    `json:"text"`
	Start     float64   `json:"start"`
	Timestamp string    `json:"timestamp"`
	Link      string    `json:"link"`
	CreatedAt string    `json:"created_at"`
	Edited    bool      `json:"edited"`
	Deleted   bool      `json:"deleted"`
	Reactions int       `json:"reactions"`
	Replies   []Comment `json:"replies"`
}

// AnonymousAuthor labels a comment whose author Loom does not name.
const AnonymousAuthor = "Anonymous"

// AuthorLabel is the comment's author, or AnonymousAuthor when unnamed.
func (c Comment) AuthorLabel() string {
	if c.Author == "" {
		return AnonymousAuthor
	}
	return c.Author
}

// WalkComments calls fn for every comment and reply, depth first in thread
// order, with depth 0 for top-level comments.
func WalkComments(cs []Comment, fn func(c Comment, depth int)) {
	var walk func(list []Comment, depth int)
	walk = func(list []Comment, depth int) {
		for _, c := range list {
			fn(c, depth)
			walk(c.Replies, depth+1)
		}
	}
	walk(cs, 0)
}

type rawComment struct {
	ID           json.Number       `json:"id"`
	PlainContent string            `json:"plainContent"`
	Content      string            `json:"content"`
	TimeStamp    *float64          `json:"time_stamp"`
	UserName     string            `json:"user_name"`
	UserID       json.Number       `json:"user_id"`
	AnonUserID   string            `json:"anon_user_id"`
	Edited       bool              `json:"edited"`
	CreatedAt    string            `json:"createdAt"`
	DeletedAt    *string           `json:"deletedAt"`
	Reactions    []json.RawMessage `json:"reactions"`
	Children     []rawComment      `json:"children_comments"`
}

func (r rawComment) normalize(videoID, parentID string) Comment {
	c := Comment{
		ID:        r.ID.String(),
		VideoID:   videoID,
		ParentID:  parentID,
		Author:    TitleName(r.UserName),
		AuthorID:  r.UserID.String(),
		Text:      r.PlainContent,
		CreatedAt: r.CreatedAt,
		Edited:    r.Edited,
		Deleted:   r.DeletedAt != nil && *r.DeletedAt != "",
		Reactions: len(r.Reactions),
		Replies:   make([]Comment, 0),
	}
	if c.Text == "" {
		c.Text = r.Content
	}
	if c.AuthorID == "" {
		c.AuthorID = r.AnonUserID
	}
	if r.TimeStamp != nil {
		c.Start = MillisToSeconds(*r.TimeStamp)
	}
	c.Timestamp = FormatClock(c.Start)
	c.Link = DeepLink(videoID, c.Start)
	for _, ch := range r.Children {
		c.Replies = append(c.Replies, ch.normalize(videoID, c.ID))
	}
	return c
}

// FetchComments returns a video's threaded comments (fetchPagedComments).
// Deleted comments are dropped unless includeDeleted is set.
func (l *Client) FetchComments(ctx context.Context, id string, includeDeleted bool) ([]Comment, error) {
	out := make([]Comment, 0)
	// One page of up to 500 top-level comments covers any realistic video;
	// replies arrive nested under their parent.
	data, err := l.Query(ctx, OpComments, map[string]any{"id": id, "first": 500})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return out, nil
	}
	raw, err := l.field(ctx, OpComments, data, "video")
	if err != nil {
		return nil, err
	}
	var r struct {
		Comments []rawComment `json:"video_comments"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpComments, err)
	}
	for _, rc := range r.Comments {
		c := rc.normalize(id, "")
		if c.Deleted && !includeDeleted {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// Reaction is one emoji reaction on a video.
type Reaction struct {
	ID        string  `json:"id"`
	VideoID   string  `json:"video_id"`
	Emoji     string  `json:"emoji"`
	Category  string  `json:"category,omitempty"`
	User      string  `json:"user"`
	Start     float64 `json:"start"`
	Timestamp string  `json:"timestamp"`
	CreatedAt string  `json:"created_at,omitempty"`
}

// FetchReactions returns a video's emoji reactions.
func (l *Client) FetchReactions(ctx context.Context, id string) ([]Reaction, error) {
	data, err := l.Query(ctx, OpReactions, map[string]any{"videoId": id})
	if err != nil {
		return nil, err
	}
	out := make([]Reaction, 0)
	if l.IsDryRun() {
		return out, nil
	}
	raw, err := l.field(ctx, OpReactions, data, "videoReactionsForVideo")
	if err != nil {
		return nil, err
	}
	var r struct {
		Reactions []struct {
			ID               json.Number `json:"id"`
			Time             *float64    `json:"time"`
			Reaction         string      `json:"reaction"`
			ExtendedReaction string      `json:"extended_reaction"`
			Category         string      `json:"category"`
			CreatedAt        string      `json:"createdAt"`
			AnonUserName     string      `json:"anon_user_name"`
			User             *struct {
				DisplayName string `json:"display_name"`
			} `json:"user"`
		} `json:"reactions"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpReactions, err)
	}
	for _, x := range r.Reactions {
		re := Reaction{ID: x.ID.String(), VideoID: id, Emoji: x.ExtendedReaction, Category: x.Category, CreatedAt: x.CreatedAt}
		if re.Emoji == "" {
			re.Emoji = x.Reaction
		}
		if x.User != nil {
			re.User = TitleName(x.User.DisplayName)
		} else {
			re.User = x.AnonUserName
		}
		if x.Time != nil {
			re.Start = MillisToSeconds(*x.Time)
		}
		re.Timestamp = FormatClock(re.Start)
		out = append(out, re)
	}
	return out, nil
}
