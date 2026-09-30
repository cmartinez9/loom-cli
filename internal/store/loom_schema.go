package store

import (
	"context"
	"fmt"
)

// Loom-specific tables beside the generated resource tables. The generated
// "videos" table (from sync) is the library index; these hold what hydrate,
// grep, digest and activity need: transcripts as searchable phrases, Loom's
// AI artifacts, hydrate progress, and per-run engagement snapshots and seen
// comments. EnsureLoomSchema creates all of them.
var loomSchema = []string{
	`CREATE TABLE IF NOT EXISTS loom_transcripts (
		video_id TEXT PRIMARY KEY,
		version INTEGER,
		language TEXT,
		status TEXT,
		segment_count INTEGER NOT NULL DEFAULT 0,
		text TEXT,
		fetched_at TEXT NOT NULL
	)`,
	// id is an explicit INTEGER PRIMARY KEY so the FTS index's
	// content_rowid is stable: VACUUM may renumber an implicit rowid and
	// silently desync an external-content FTS table.
	loomPhrasesTable,
	`CREATE VIRTUAL TABLE IF NOT EXISTS loom_phrases_fts USING fts5(
		text,
		content='loom_phrases',
		content_rowid='id',
		tokenize='porter unicode61'
	)`,
	`CREATE TRIGGER IF NOT EXISTS loom_phrases_ai AFTER INSERT ON loom_phrases BEGIN
		INSERT INTO loom_phrases_fts(rowid, text) VALUES (new.id, new.text);
	END`,
	`CREATE TRIGGER IF NOT EXISTS loom_phrases_ad AFTER DELETE ON loom_phrases BEGIN
		INSERT INTO loom_phrases_fts(loom_phrases_fts, rowid, text) VALUES ('delete', old.id, old.text);
	END`,
	`CREATE TRIGGER IF NOT EXISTS loom_phrases_au AFTER UPDATE ON loom_phrases BEGIN
		INSERT INTO loom_phrases_fts(loom_phrases_fts, rowid, text) VALUES ('delete', old.id, old.text);
		INSERT INTO loom_phrases_fts(rowid, text) VALUES (new.id, new.text);
	END`,
	`CREATE TABLE IF NOT EXISTS loom_artifacts (
		video_id TEXT PRIMARY KEY,
		summary TEXT,
		summary_status TEXT,
		chapters TEXT,
		chapters_status TEXT,
		tasks TEXT,
		takeaways TEXT,
		fetched_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS loom_hydrate_state (
		video_id TEXT PRIMARY KEY,
		transcript_status TEXT,
		artifacts_status TEXT,
		last_error TEXT,
		attempted_at TEXT NOT NULL
	)`,
	// Which videos hydrate has already asked for key takeaways, so a later
	// `hydrate --takeaways` only fetches them for videos never asked before
	// (most non-meeting videos legitimately have none).
	`CREATE TABLE IF NOT EXISTS loom_takeaways_attempted (
		video_id TEXT PRIMARY KEY,
		attempted_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS loom_activity_snapshots (
		taken_at TEXT NOT NULL,
		video_id TEXT NOT NULL,
		name TEXT,
		views INTEGER NOT NULL DEFAULT 0,
		unique_views INTEGER NOT NULL DEFAULT 0,
		comments INTEGER NOT NULL DEFAULT 0,
		reactions INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (taken_at, video_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_loom_activity_video ON loom_activity_snapshots(video_id, taken_at)`,
	`CREATE TABLE IF NOT EXISTS loom_comments_seen (
		comment_id TEXT PRIMARY KEY,
		video_id TEXT NOT NULL,
		parent_id TEXT,
		author TEXT,
		author_id TEXT,
		text TEXT,
		created_at TEXT,
		first_seen_at TEXT NOT NULL
	)`,
}

const loomPhrasesTable = `CREATE TABLE IF NOT EXISTS loom_phrases (
		id INTEGER PRIMARY KEY,
		video_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		start REAL NOT NULL,
		end REAL NOT NULL,
		speaker INTEGER NOT NULL DEFAULT 0,
		text TEXT NOT NULL,
		UNIQUE (video_id, seq)
	)`

// EnsureLoomSchema creates the Loom tables if they do not exist, migrating a
// store created before loom_phrases had an explicit id. It is safe to call
// on every command that touches them.
func (s *Store) EnsureLoomSchema(ctx context.Context) error {
	s.lockForWrite()
	defer s.unlockAfterWrite()
	legacy, err := s.legacyPhrasesTable(ctx)
	if err != nil {
		return fmt.Errorf("inspecting loom tables: %w", err)
	}
	if legacy {
		return s.migrateLegacyPhrases(ctx)
	}
	for _, stmt := range loomSchema {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("creating loom tables: %w", err)
		}
	}
	return nil
}

// legacyPhrasesTable reports a loom_phrases table without the id column
// (the shape before the FTS index was keyed on an explicit id).
func (s *Store) legacyPhrasesTable(ctx context.Context) (bool, error) {
	var tables int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'loom_phrases'`).Scan(&tables); err != nil {
		return false, err
	}
	if tables == 0 {
		return false, nil
	}
	var idCols int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('loom_phrases') WHERE name = 'id'`).Scan(&idCols); err != nil {
		return false, err
	}
	return idCols == 0, nil
}

// migrateLegacyPhrases rebuilds loom_phrases with an explicit id, copying
// the existing rows, then recreates the FTS index and triggers and rebuilds
// the index from the copied rows, all in one transaction.
func (s *Store) migrateLegacyPhrases(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	steps := []string{
		`DROP TRIGGER IF EXISTS loom_phrases_ai`,
		`DROP TRIGGER IF EXISTS loom_phrases_ad`,
		`DROP TRIGGER IF EXISTS loom_phrases_au`,
		`DROP TABLE IF EXISTS loom_phrases_fts`,
		`ALTER TABLE loom_phrases RENAME TO loom_phrases_legacy`,
	}
	steps = append(steps, loomPhrasesTable,
		`INSERT INTO loom_phrases (video_id, seq, start, end, speaker, text)
			SELECT video_id, seq, start, end, speaker, text FROM loom_phrases_legacy ORDER BY video_id, seq`,
		`DROP TABLE loom_phrases_legacy`)
	steps = append(steps, loomSchema...)
	steps = append(steps, `INSERT INTO loom_phrases_fts(loom_phrases_fts) VALUES ('rebuild')`)
	for _, stmt := range steps {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrating loom_phrases: %w", err)
		}
	}
	return tx.Commit()
}
