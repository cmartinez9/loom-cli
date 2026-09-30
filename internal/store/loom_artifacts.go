package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Loom's AI artifacts (summary, chapters, action items, takeaways) per video.

// ArtifactRow holds Loom's AI output for a video. Chapters, Tasks and
// Takeaways are JSON arrays as the CLI prints them.
type ArtifactRow struct {
	VideoID        string
	Summary        string
	SummaryStatus  string
	Chapters       json.RawMessage
	ChaptersStatus string
	Tasks          json.RawMessage
	Takeaways      json.RawMessage
	FetchedAt      string
}

// SaveArtifacts upserts a video's AI artifacts.
func (s *Store) SaveArtifacts(ctx context.Context, a ArtifactRow) error {
	s.lockForWrite()
	defer s.unlockAfterWrite()
	if a.FetchedAt == "" {
		a.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO loom_artifacts (video_id, summary, summary_status, chapters, chapters_status, tasks, takeaways, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(video_id) DO UPDATE SET summary=excluded.summary, summary_status=excluded.summary_status,
		chapters=excluded.chapters, chapters_status=excluded.chapters_status, tasks=excluded.tasks,
		takeaways=excluded.takeaways, fetched_at=excluded.fetched_at`,
		a.VideoID, a.Summary, a.SummaryStatus, jsonOrEmptyArray(a.Chapters), a.ChaptersStatus, jsonOrEmptyArray(a.Tasks), jsonOrEmptyArray(a.Takeaways), a.FetchedAt)
	if err != nil {
		return fmt.Errorf("saving artifacts: %w", err)
	}
	return nil
}

// GetArtifacts returns a video's stored AI artifacts, or sql.ErrNoRows.
func (s *Store) GetArtifacts(ctx context.Context, videoID string) (*ArtifactRow, error) {
	var a ArtifactRow
	var summary, sstatus, chapters, cstatus, tasks, takeaways sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT video_id, summary, summary_status, chapters, chapters_status, tasks, takeaways, fetched_at FROM loom_artifacts WHERE video_id = ?`, videoID).
		Scan(&a.VideoID, &summary, &sstatus, &chapters, &cstatus, &tasks, &takeaways, &a.FetchedAt)
	if err != nil {
		return nil, err
	}
	a.Summary, a.SummaryStatus, a.ChaptersStatus = summary.String, sstatus.String, cstatus.String
	a.Chapters = json.RawMessage(jsonOrEmptyArray(json.RawMessage(chapters.String)))
	a.Tasks = json.RawMessage(jsonOrEmptyArray(json.RawMessage(tasks.String)))
	a.Takeaways = json.RawMessage(jsonOrEmptyArray(json.RawMessage(takeaways.String)))
	return &a, nil
}

func jsonOrEmptyArray(b json.RawMessage) string {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		return "[]"
	}
	return s
}
