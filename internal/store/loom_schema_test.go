package store

import (
	"context"
	"path/filepath"
	"testing"
)

// legacyPhraseSchema is the loom_phrases shape before the explicit id: the
// FTS index keyed on the implicit rowid.
var legacyPhraseSchema = []string{
	`CREATE TABLE loom_phrases (
		video_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		start REAL NOT NULL,
		end REAL NOT NULL,
		speaker INTEGER NOT NULL DEFAULT 0,
		text TEXT NOT NULL,
		PRIMARY KEY (video_id, seq)
	)`,
	`CREATE VIRTUAL TABLE loom_phrases_fts USING fts5(text, content='loom_phrases', content_rowid='rowid', tokenize='porter unicode61')`,
	`CREATE TRIGGER loom_phrases_ai AFTER INSERT ON loom_phrases BEGIN
		INSERT INTO loom_phrases_fts(rowid, text) VALUES (new.rowid, new.text);
	END`,
	`CREATE TRIGGER loom_phrases_ad AFTER DELETE ON loom_phrases BEGIN
		INSERT INTO loom_phrases_fts(loom_phrases_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
	END`,
}

func TestLoomPhrasesLegacyMigration(t *testing.T) {
	ctx := context.Background()
	s, err := OpenWithContext(ctx, filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, stmt := range legacyPhraseSchema {
		if _, err := s.DB().ExecContext(ctx, stmt); err != nil {
			t.Fatalf("legacy schema: %v", err)
		}
	}
	for i, text := range []string{"welcome to the pricing review", "the funnel dropped last week", "pricing experiment results"} {
		if _, err := s.DB().ExecContext(ctx, `INSERT INTO loom_phrases (video_id, seq, start, end, speaker, text) VALUES ('v1', ?, ?, ?, 0, ?)`, i, float64(i), float64(i+1), text); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.EnsureLoomSchema(ctx); err != nil {
		t.Fatalf("migration: %v", err)
	}
	if legacy, err := s.legacyPhrasesTable(ctx); err != nil || legacy {
		t.Fatalf("still legacy after migration: %v %v", legacy, err)
	}
	search := func(label string) {
		t.Helper()
		hits, err := s.SearchPhrases(ctx, PhraseSearch{Match: `"pricing"`, Limit: 10})
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if len(hits) != 2 {
			t.Fatalf("%s: got %d hits, want 2: %+v", label, len(hits), hits)
		}
		for _, h := range hits {
			if h.VideoID != "v1" || (h.Seq != 0 && h.Seq != 2) {
				t.Fatalf("%s: unexpected hit %+v", label, h)
			}
		}
	}
	search("after migration")
	// A second call is a no-op on the new shape.
	if err := s.EnsureLoomSchema(ctx); err != nil {
		t.Fatal(err)
	}
	// Replacing a transcript and vacuuming keeps the index in step.
	if err := s.SaveTranscript(ctx, TranscriptRow{VideoID: "v2", Status: "success"}, []PhraseRow{{Text: "an unrelated phrase"}, {Text: "pricing again"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM loom_phrases WHERE video_id = 'v2'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `VACUUM`); err != nil {
		t.Fatal(err)
	}
	search("after vacuum")
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO loom_phrases_fts(loom_phrases_fts) VALUES ('integrity-check')`); err != nil {
		t.Fatalf("fts integrity-check: %v", err)
	}
}
