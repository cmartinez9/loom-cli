package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

// Reads over the synced resource tables (videos, folders, spaces).

// LocalVideo is the subset of the synced videos table that hydrate and
// digest read (through LocalVideos).
type LocalVideo struct {
	ID              string
	Name            string
	CreatedAt       string
	DurationSeconds float64
	Views           int
	Comments        int
	Reactions       int
	FolderName      string
	Archived        bool
	ShareURL        string
}

// LocalVideos lists synced videos created at or after since, newest first.
// since is an RFC3339 UTC lower bound (windowBounds.startRFC3339 in the cli
// package); empty means all. The comparison is on the stored text, so the
// caller re-checks parsed times when it also needs an upper bound.
func (s *Store) LocalVideos(ctx context.Context, since string) ([]LocalVideo, error) {
	q := `SELECT id, COALESCE(name,''), COALESCE(created_at,''), COALESCE(duration_seconds,0), COALESCE(views,0),
		COALESCE(comments,0), COALESCE(reactions,0), COALESCE(folder_name,''), COALESCE(archived,0), COALESCE(share_url,'')
		FROM videos`
	args := []any{}
	if since != "" {
		q += ` WHERE created_at >= ?`
		args = append(args, since)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]LocalVideo, 0)
	for rows.Next() {
		var v LocalVideo
		var archived int
		if err := rows.Scan(&v.ID, &v.Name, &v.CreatedAt, &v.DurationSeconds, &v.Views, &v.Comments, &v.Reactions, &v.FolderName, &archived, &v.ShareURL); err != nil {
			return nil, err
		}
		v.Archived = archived != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

// HasLocalVideos reports whether sync has stored at least one video, so an
// empty window can be told apart from a store that was never filled.
func (s *Store) HasLocalVideos(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM videos)`).Scan(&n); err != nil {
		return false, err
	}
	return n != 0, nil
}

// SyncedVideos returns every synced video as the loom.Video sync stored in
// the data column, newest first. A store with no videos table yet (created
// before any videos sync) is empty.
func (s *Store) SyncedVideos(ctx context.Context) ([]loom.Video, error) {
	raws, err := s.syncedData(ctx, "videos")
	if err != nil {
		return nil, err
	}
	out := make([]loom.Video, 0, len(raws))
	for _, raw := range raws {
		var v loom.Video
		if json.Unmarshal(raw, &v) != nil || v.ID == "" {
			continue
		}
		if v.Tags == nil {
			v.Tags = []string{}
		}
		if v.ShareURL == "" {
			v.ShareURL = loom.ShareURL(v.ID)
		}
		out = append(out, v)
	}
	return out, nil
}

// SyncedFolders returns the synced folders, by name.
func (s *Store) SyncedFolders(ctx context.Context) ([]loom.Folder, error) {
	raws, err := s.syncedData(ctx, "folders")
	if err != nil {
		return nil, err
	}
	out := make([]loom.Folder, 0, len(raws))
	for _, raw := range raws {
		var f loom.Folder
		if json.Unmarshal(raw, &f) == nil && f.ID != "" {
			out = append(out, f)
		}
	}
	return out, nil
}

// SyncedSpaces returns the synced spaces, by name.
func (s *Store) SyncedSpaces(ctx context.Context) ([]loom.Space, error) {
	raws, err := s.syncedData(ctx, "spaces")
	if err != nil {
		return nil, err
	}
	out := make([]loom.Space, 0, len(raws))
	for _, raw := range raws {
		var sp loom.Space
		if json.Unmarshal(raw, &sp) == nil && sp.ID != "" {
			out = append(out, sp)
		}
	}
	return out, nil
}

// syncedDataQueries are the only synced tables syncedData reads; each
// query is a constant, so no SQL is built from caller input.
var syncedDataQueries = map[string]string{
	"videos":  `SELECT data FROM videos ORDER BY COALESCE(created_at, '') DESC`,
	"folders": `SELECT data FROM folders ORDER BY COALESCE(name, '')`,
	"spaces":  `SELECT data FROM spaces ORDER BY COALESCE(name, '')`,
}

// syncedData returns the non-empty data column of a synced table. A missing
// table is empty, not an error.
func (s *Store) syncedData(ctx context.Context, table string) ([]json.RawMessage, error) {
	q, ok := syncedDataQueries[table]
	if !ok {
		return nil, fmt.Errorf("unknown synced table %q", table)
	}
	out := make([]json.RawMessage, 0)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return out, nil
		}
		return nil, fmt.Errorf("reading local %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var d sql.NullString
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("reading local %s: %w", table, err)
		}
		if d.Valid && d.String != "" {
			out = append(out, json.RawMessage(d.String))
		}
	}
	return out, rows.Err()
}

// SyncedVideoText returns a synced video's name and description, or
// sql.ErrNoRows when the video is not synced.
func (s *Store) SyncedVideoText(ctx context.Context, videoID string) (name, description string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(name,''), COALESCE(description,'') FROM videos WHERE id = ?`, videoID).
		Scan(&name, &description)
	return name, description, err
}

// SyncedVideoCreatedAt returns a synced video's created_at, or "" when the
// video is not synced or has none.
func (s *Store) SyncedVideoCreatedAt(ctx context.Context, videoID string) string {
	var created sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM videos WHERE id = ?`, videoID).Scan(&created); err != nil {
		return ""
	}
	return created.String
}

// SyncedVideoTags returns the tags saved with a synced video, or
// sql.ErrNoRows when the video is not synced.
func (s *Store) SyncedVideoTags(ctx context.Context, videoID string) ([]string, error) {
	var raw sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT json_extract(data, '$.tags') FROM videos WHERE id = ?`, videoID).Scan(&raw); err != nil {
		return nil, err
	}
	tags := make([]string, 0)
	if raw.Valid && strings.TrimSpace(raw.String) != "" {
		if err := json.Unmarshal([]byte(raw.String), &tags); err != nil {
			return nil, fmt.Errorf("decoding stored tags: %w", err)
		}
	}
	return tags, nil
}

// HasLocalVideo reports whether the store knows the video at all: a synced
// row, or a transcript, AI artifacts or comments saved for it (write-through
// also stores videos that were never synced, such as other people's).
func (s *Store) HasLocalVideo(ctx context.Context, videoID string) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM videos WHERE id = ?)
		OR EXISTS (SELECT 1 FROM loom_transcripts WHERE video_id = ?)
		OR EXISTS (SELECT 1 FROM loom_artifacts WHERE video_id = ?)
		OR EXISTS (SELECT 1 FROM loom_comments_seen WHERE video_id = ?)`, videoID, videoID, videoID, videoID).Scan(&found)
	if err != nil {
		return false, err
	}
	return found != 0, nil
}

// LatestActiveVideoID returns the newest synced video that is not archived
// (what "latest" means live), or "" when there is none.
func (s *Store) LatestActiveVideoID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM videos WHERE COALESCE(archived, 0) = 0 ORDER BY created_at DESC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
