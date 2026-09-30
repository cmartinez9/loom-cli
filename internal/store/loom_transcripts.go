package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Stored transcripts and the full-text phrase index grep searches.

// PhraseRow is one stored transcript phrase.
type PhraseRow struct {
	Start   float64
	End     float64
	Speaker int
	Text    string
}

// TranscriptRow is a stored transcript header.
type TranscriptRow struct {
	VideoID      string
	Version      int
	Language     string
	Status       string
	SegmentCount int
	Text         string
	FetchedAt    string
}

// SaveTranscript replaces a video's transcript and its searchable phrases.
func (s *Store) SaveTranscript(ctx context.Context, t TranscriptRow, phrases []PhraseRow) error {
	s.lockForWrite()
	defer s.unlockAfterWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if t.FetchedAt == "" {
		t.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM loom_phrases WHERE video_id = ?`, t.VideoID); err != nil {
		return fmt.Errorf("clearing phrases: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO loom_phrases (video_id, seq, start, end, speaker, text) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, p := range phrases {
		if _, err := stmt.ExecContext(ctx, t.VideoID, i, p.Start, p.End, p.Speaker, p.Text); err != nil {
			return fmt.Errorf("inserting phrase: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO loom_transcripts (video_id, version, language, status, segment_count, text, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(video_id) DO UPDATE SET version=excluded.version, language=excluded.language, status=excluded.status,
		segment_count=excluded.segment_count, text=excluded.text, fetched_at=excluded.fetched_at`,
		t.VideoID, t.Version, t.Language, t.Status, len(phrases), t.Text, t.FetchedAt); err != nil {
		return fmt.Errorf("saving transcript: %w", err)
	}
	return tx.Commit()
}

// GetTranscript returns a stored transcript and its phrases, or sql.ErrNoRows.
func (s *Store) GetTranscript(ctx context.Context, videoID string) (*TranscriptRow, []PhraseRow, error) {
	var t TranscriptRow
	var version sql.NullInt64
	var lang, status, text sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT video_id, version, language, status, segment_count, text, fetched_at FROM loom_transcripts WHERE video_id = ?`, videoID).
		Scan(&t.VideoID, &version, &lang, &status, &t.SegmentCount, &text, &t.FetchedAt)
	if err != nil {
		return nil, nil, err
	}
	t.Version, t.Language, t.Status, t.Text = int(version.Int64), lang.String, status.String, text.String
	rows, err := s.db.QueryContext(ctx, `SELECT start, end, speaker, text FROM loom_phrases WHERE video_id = ? ORDER BY seq`, videoID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	phrases := make([]PhraseRow, 0)
	for rows.Next() {
		var p PhraseRow
		if err := rows.Scan(&p.Start, &p.End, &p.Speaker, &p.Text); err != nil {
			return nil, nil, err
		}
		phrases = append(phrases, p)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return &t, phrases, nil
}

// HydratedTranscriptCount returns how many videos have at least one stored
// transcript phrase.
func (s *Store) HydratedTranscriptCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT video_id) FROM loom_phrases`).Scan(&n)
	return n, err
}

// PhraseHit is one full-text match on a transcript phrase.
type PhraseHit struct {
	VideoID   string
	Seq       int
	Start     float64
	Text      string
	VideoName string
	CreatedAt string
	Rank      float64
}

// PhraseSearch filters a transcript full-text search.
type PhraseSearch struct {
	Match   string // an already-escaped FTS5 MATCH expression
	VideoID string // restrict to one video
	Since   string // created_at lower bound (RFC3339); empty means any
	Limit   int
}

// SearchPhrases runs an FTS5 MATCH over loom_phrases_fts, joined to the
// phrase rows and the synced videos table, best bm25 rank first.
func (s *Store) SearchPhrases(ctx context.Context, q PhraseSearch) ([]PhraseHit, error) {
	sqlText := `SELECT p.video_id, p.seq, p.start, p.text,
			COALESCE(v.name, ''), COALESCE(v.created_at, ''), bm25(loom_phrases_fts) AS score
		FROM loom_phrases_fts
		JOIN loom_phrases p ON p.id = loom_phrases_fts.rowid
		LEFT JOIN videos v ON v.id = p.video_id
		WHERE loom_phrases_fts MATCH ?`
	args := []any{q.Match}
	if q.VideoID != "" {
		sqlText += ` AND p.video_id = ?`
		args = append(args, q.VideoID)
	}
	if q.Since != "" {
		sqlText += ` AND v.created_at >= ?`
		args = append(args, q.Since)
	}
	sqlText += ` ORDER BY score, v.created_at DESC, p.seq LIMIT ?`
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PhraseHit, 0)
	for rows.Next() {
		var h PhraseHit
		if err := rows.Scan(&h.VideoID, &h.Seq, &h.Start, &h.Text, &h.VideoName, &h.CreatedAt, &h.Rank); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// PhraseWindow returns the texts of phrases seq-before..seq+after for one
// video, in order.
func (s *Store) PhraseWindow(ctx context.Context, videoID string, seq, before, after int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT text FROM loom_phrases WHERE video_id = ? AND seq BETWEEN ? AND ? ORDER BY seq`,
		videoID, seq-before, seq+after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
