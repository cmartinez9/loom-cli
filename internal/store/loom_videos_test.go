package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func openLoomTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := OpenWithContext(context.Background(), filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureLoomSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestGetTranscriptRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openLoomTestStore(t)
	if _, _, err := db.GetTranscript(ctx, "v1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing transcript err = %v, want sql.ErrNoRows", err)
	}
	phrases := []PhraseRow{{Start: 0, End: 1.5, Speaker: 0, Text: "first"}, {Start: 1.5, End: 3, Speaker: 1, Text: "second"}}
	if err := db.SaveTranscript(ctx, TranscriptRow{VideoID: "v1", Version: 2, Language: "en", Status: "success", Text: "first\nsecond"}, phrases); err != nil {
		t.Fatal(err)
	}
	row, got, err := db.GetTranscript(ctx, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if row.Version != 2 || row.Language != "en" || row.Status != "success" || row.SegmentCount != 2 || row.FetchedAt == "" {
		t.Fatalf("row = %+v", row)
	}
	if len(got) != 2 || got[1].Text != "second" || got[1].Speaker != 1 || got[1].Start != 1.5 {
		t.Fatalf("phrases = %+v", got)
	}
	// Saving again replaces the phrases rather than appending.
	if err := db.SaveTranscript(ctx, TranscriptRow{VideoID: "v1", Status: "success"}, phrases[:1]); err != nil {
		t.Fatal(err)
	}
	if _, got, _ := db.GetTranscript(ctx, "v1"); len(got) != 1 {
		t.Fatalf("phrases after resave = %+v, want 1", got)
	}
}

func TestSyncedReaders(t *testing.T) {
	ctx := context.Background()
	db := openLoomTestStore(t)
	for _, q := range []string{
		`INSERT INTO videos (id, data, name, description, created_at, archived) VALUES
			('v1', '{"id":"v1","name":"Old","created_at":"2026-01-01T00:00:00Z","tags":["a"]}', 'Old', 'first', '2026-01-01T00:00:00Z', 0),
			('v2', '{"id":"v2","name":"New","created_at":"2026-02-01T00:00:00Z"}', 'New', '', '2026-02-01T00:00:00Z', 1),
			('bad', '', 'Bad', '', '2025-01-01T00:00:00Z', 0)`,
		`INSERT INTO folders (id, data, name) VALUES ('f2', '{"id":"f2","name":"Zeta"}', 'Zeta'), ('f1', '{"id":"f1","name":"Alpha"}', 'Alpha')`,
		`INSERT INTO spaces (id, data, name) VALUES ('s1', '{"id":"s1","name":"Team"}', 'Team')`,
	} {
		if _, err := db.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	vids, err := db.SyncedVideos(ctx)
	if err != nil || len(vids) != 2 || vids[0].ID != "v2" || vids[1].ID != "v1" {
		t.Fatalf("SyncedVideos = %+v, %v; want v2, v1 (newest first, empty data skipped)", vids, err)
	}
	if vids[0].Tags == nil || vids[0].ShareURL == "" {
		t.Fatalf("SyncedVideos defaults: %+v", vids[0])
	}
	folders, err := db.SyncedFolders(ctx)
	if err != nil || len(folders) != 2 || folders[0].Name != "Alpha" {
		t.Fatalf("SyncedFolders = %+v, %v; want sorted by name", folders, err)
	}
	spaces, err := db.SyncedSpaces(ctx)
	if err != nil || len(spaces) != 1 || spaces[0].ID != "s1" {
		t.Fatalf("SyncedSpaces = %+v, %v", spaces, err)
	}
	if name, desc, err := db.SyncedVideoText(ctx, "v1"); err != nil || name != "Old" || desc != "first" {
		t.Fatalf("SyncedVideoText = %q %q %v", name, desc, err)
	}
	if _, _, err := db.SyncedVideoText(ctx, "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("SyncedVideoText(missing) err = %v", err)
	}
	if tags, err := db.SyncedVideoTags(ctx, "v1"); err != nil || len(tags) != 1 || tags[0] != "a" {
		t.Fatalf("SyncedVideoTags = %v %v", tags, err)
	}
	if id, err := db.LatestActiveVideoID(ctx); err != nil || id != "v1" {
		t.Fatalf("LatestActiveVideoID = %q %v, want v1 (v2 is archived)", id, err)
	}
	for id, want := range map[string]bool{"v1": true, "v9": false} {
		if got, err := db.HasLocalVideo(ctx, id); err != nil || got != want {
			t.Fatalf("HasLocalVideo(%s) = %v %v", id, got, err)
		}
	}
	if err := db.SaveTranscript(ctx, TranscriptRow{VideoID: "v9", Status: "success"}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.HasLocalVideo(ctx, "v9"); !got {
		t.Fatal("a video with only a saved transcript is known locally")
	}
}
