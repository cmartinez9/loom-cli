package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Engagement snapshots and seen comments recorded by activity.

// ActivitySnapshot is one video's engagement counters at a point in time.
type ActivitySnapshot struct {
	TakenAt     string
	VideoID     string
	Name        string
	Views       int
	UniqueViews int
	Comments    int
	Reactions   int
}

// SnapshotTimeLayout is a fixed-width UTC layout so taken_at sorts
// lexicographically in time order.
const SnapshotTimeLayout = "2006-01-02T15:04:05.000000Z"

// LatestActivitySnapshots returns, for each requested video, its newest
// snapshot taken strictly before `before` (empty means any time).
func (s *Store) LatestActivitySnapshots(ctx context.Context, videoIDs []string, before string) (map[string]ActivitySnapshot, error) {
	out := map[string]ActivitySnapshot{}
	if len(videoIDs) == 0 {
		return out, nil
	}
	const chunk = 400
	for startIdx := 0; startIdx < len(videoIDs); startIdx += chunk {
		end := startIdx + chunk
		if end > len(videoIDs) {
			end = len(videoIDs)
		}
		ids := videoIDs[startIdx:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := make([]any, 0, len(ids)+1)
		for _, id := range ids {
			args = append(args, id)
		}
		q := `SELECT s.taken_at, s.video_id, COALESCE(s.name,''), s.views, s.unique_views, s.comments, s.reactions
			FROM loom_activity_snapshots s
			WHERE s.video_id IN (` + placeholders + `)`
		if before != "" {
			q += ` AND s.taken_at < ?`
			args = append(args, before)
		}
		q += ` ORDER BY s.video_id, s.taken_at`
		if err := s.latestSnapshotsInto(ctx, out, q, args); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// latestSnapshotsInto runs one chunk of LatestActivitySnapshots. Rows come
// in ascending taken_at order, so the last row per video wins.
func (s *Store) latestSnapshotsInto(ctx context.Context, out map[string]ActivitySnapshot, q string, args []any) error {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var snap ActivitySnapshot
		if err := rows.Scan(&snap.TakenAt, &snap.VideoID, &snap.Name, &snap.Views, &snap.UniqueViews, &snap.Comments, &snap.Reactions); err != nil {
			return err
		}
		out[snap.VideoID] = snap
	}
	return rows.Err()
}

// LastActivityCheck returns the newest snapshot time strictly before
// `before` (empty means any), or "" when no snapshot exists.
func (s *Store) LastActivityCheck(ctx context.Context, before string) (string, error) {
	var t sql.NullString
	q := `SELECT MAX(taken_at) FROM loom_activity_snapshots`
	args := []any{}
	if before != "" {
		q += ` WHERE taken_at < ?`
		args = append(args, before)
	}
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&t); err != nil {
		return "", err
	}
	return t.String, nil
}

// SaveActivitySnapshots records one snapshot row per video in a single
// transaction.
func (s *Store) SaveActivitySnapshots(ctx context.Context, snaps []ActivitySnapshot) error {
	if len(snaps) == 0 {
		return nil
	}
	s.lockForWrite()
	defer s.unlockAfterWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO loom_activity_snapshots
		(taken_at, video_id, name, views, unique_views, comments, reactions) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, sn := range snaps {
		if _, err := stmt.ExecContext(ctx, sn.TakenAt, sn.VideoID, sn.Name, sn.Views, sn.UniqueViews, sn.Comments, sn.Reactions); err != nil {
			return fmt.Errorf("saving activity snapshot: %w", err)
		}
	}
	return tx.Commit()
}

// SeenComment is a comment activity has already reported.
type SeenComment struct {
	CommentID string
	VideoID   string
	ParentID  string
	Author    string
	AuthorID  string
	Text      string
	CreatedAt string
}

// SeenCommentIDs returns the comment ids already recorded for a video.
func (s *Store) SeenCommentIDs(ctx context.Context, videoID string) (map[string]bool, error) {
	return s.idSet(ctx, `SELECT comment_id FROM loom_comments_seen WHERE video_id = ?`, videoID)
}

// RecordSeenComments marks comments as reported; existing rows keep their
// first_seen_at.
func (s *Store) RecordSeenComments(ctx context.Context, comments []SeenComment) error {
	if len(comments) == 0 {
		return nil
	}
	s.lockForWrite()
	defer s.unlockAfterWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO loom_comments_seen
		(comment_id, video_id, parent_id, author, author_id, text, created_at, first_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range comments {
		if _, err := stmt.ExecContext(ctx, c.CommentID, c.VideoID, c.ParentID, c.Author, c.AuthorID, c.Text, c.CreatedAt, now); err != nil {
			return fmt.Errorf("recording seen comment: %w", err)
		}
	}
	return tx.Commit()
}

// SeenComments returns the comments activity recorded for a video, oldest
// first, as flat rows linked by ParentID.
func (s *Store) SeenComments(ctx context.Context, videoID string) ([]SeenComment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT comment_id, COALESCE(parent_id,''), COALESCE(author,''), COALESCE(author_id,''),
		COALESCE(text,''), COALESCE(created_at,'') FROM loom_comments_seen WHERE video_id = ? ORDER BY created_at, comment_id`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SeenComment, 0)
	for rows.Next() {
		c := SeenComment{VideoID: videoID}
		if err := rows.Scan(&c.CommentID, &c.ParentID, &c.Author, &c.AuthorID, &c.Text, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// idSet runs a one-column query and returns its values as a set.
func (s *Store) idSet(ctx context.Context, query string, args ...any) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
