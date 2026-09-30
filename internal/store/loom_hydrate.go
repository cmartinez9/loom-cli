package store

import (
	"context"
	"fmt"
	"time"
)

// Per-video hydrate progress, so an interrupted backfill resumes.

// HydrateState records one video's backfill progress so hydrate can resume.
type HydrateState struct {
	VideoID          string
	TranscriptStatus string // done | pending | none | error
	ArtifactsStatus  string // done | partial | error
	LastError        string
	AttemptedAt      string
}

// SaveHydrateState upserts a video's hydrate progress.
func (s *Store) SaveHydrateState(ctx context.Context, h HydrateState) error {
	s.lockForWrite()
	defer s.unlockAfterWrite()
	if h.AttemptedAt == "" {
		h.AttemptedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO loom_hydrate_state (video_id, transcript_status, artifacts_status, last_error, attempted_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(video_id) DO UPDATE SET transcript_status=excluded.transcript_status, artifacts_status=excluded.artifacts_status,
		last_error=excluded.last_error, attempted_at=excluded.attempted_at`,
		h.VideoID, h.TranscriptStatus, h.ArtifactsStatus, h.LastError, h.AttemptedAt)
	if err != nil {
		return fmt.Errorf("saving hydrate state: %w", err)
	}
	return nil
}

// HydrateStates returns every recorded hydrate state keyed by video id.
func (s *Store) HydrateStates(ctx context.Context) (map[string]HydrateState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT video_id, COALESCE(transcript_status,''), COALESCE(artifacts_status,''), COALESCE(last_error,''), attempted_at FROM loom_hydrate_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]HydrateState{}
	for rows.Next() {
		var h HydrateState
		if err := rows.Scan(&h.VideoID, &h.TranscriptStatus, &h.ArtifactsStatus, &h.LastError, &h.AttemptedAt); err != nil {
			return nil, err
		}
		out[h.VideoID] = h
	}
	return out, rows.Err()
}

// TakeawaysAttempted returns the ids of videos hydrate already asked for key
// takeaways.
func (s *Store) TakeawaysAttempted(ctx context.Context) (map[string]bool, error) {
	return s.idSet(ctx, `SELECT video_id FROM loom_takeaways_attempted`)
}

// MarkTakeawaysAttempted records that key takeaways were requested for a video.
func (s *Store) MarkTakeawaysAttempted(ctx context.Context, videoID string) error {
	s.lockForWrite()
	defer s.unlockAfterWrite()
	_, err := s.db.ExecContext(ctx, `INSERT INTO loom_takeaways_attempted (video_id, attempted_at) VALUES (?, ?)
		ON CONFLICT(video_id) DO UPDATE SET attempted_at = excluded.attempted_at`,
		videoID, time.Now().UTC().Format(time.RFC3339))
	return err
}
