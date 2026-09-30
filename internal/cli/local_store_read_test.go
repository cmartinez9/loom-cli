package cli

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cmartinez9/loom-cli/internal/loom"
	"github.com/cmartinez9/loom-cli/internal/store"
)

func TestLiveOnlyCommandsRejectLocalSource(t *testing.T) {
	for _, args := range [][]string{
		{"videos", "get", "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60", "--data-source", "local"},
		{"folders", "get", "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60", "--data-source", "local"},
		{"playlists", "list", "--data-source", "local"},
		{"recent", "--data-source", "local"},
		{"videos", "search", "demo", "--data-source", "local"},
	} {
		_, _, err := runCLI(t, args...)
		var ce *cliError
		if !errors.As(err, &ce) || ce.code != 2 || !strings.Contains(err.Error(), "live Loom data only") {
			t.Fatalf("%v: err = %v, want a usage error about live data", args, err)
		}
	}
}

// seedLocalStore creates a store with two synced videos (the newer one
// archived) and a transcript for the older, active one.
func seedLocalStore(t *testing.T) (path, active, archived string) {
	t.Helper()
	db, path := newTestStore(t)
	active, archived = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60", "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f61"
	now := time.Now().UTC()
	for _, v := range []loom.Video{
		{ID: active, Name: "Active demo", CreatedAt: now.Add(-48 * time.Hour).Format(time.RFC3339)},
		{ID: archived, Name: "Archived demo", CreatedAt: now.Add(-time.Hour).Format(time.RFC3339), Archived: true},
	} {
		data, _ := json.Marshal(v)
		if _, err := db.DB().Exec(`INSERT INTO videos (id, data, name, created_at, archived) VALUES (?, ?, ?, ?, ?)`,
			v.ID, string(data), v.Name, v.CreatedAt, v.Archived); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SaveTranscript(context.Background(), store.TranscriptRow{VideoID: active, Status: "success", Text: "hello there"},
		[]store.PhraseRow{{Start: 0, End: 2, Text: "hello there"}}); err != nil {
		t.Fatal(err)
	}
	// Close so the WAL is checkpointed: list reads open the store
	// read-only and immutable.
	_ = db.Close()
	return path, active, archived
}

// TestLocalReadMissRules pins the one rule for local misses: no store is an
// empty result with a hint and exit 0; a store without the video (including
// latest when every video is archived) exits 3.
func TestLocalReadMissRules(t *testing.T) {
	path, active, archived := seedLocalStore(t)

	out, stderr, err := runCLI(t, "transcript", active, "--data-source", "local", "--db", filepath.Join(t.TempDir(), "none.db"), "--json")
	if err != nil || !strings.Contains(stderr, "no local mirror") || !strings.Contains(out, `"unavailable"`) {
		t.Fatalf("missing store: err=%v stderr=%q out=%q, want exit 0 with a hint and an empty result", err, stderr, out)
	}
	for name, args := range map[string][]string{
		"unknown video transcript": {"transcript", "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f6f"},
		"unknown video comments":   {"comments", "list", "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f6f"},
		"known video, no summary":  {"summary", active},
	} {
		_, _, err := runCLI(t, append(args, "--data-source", "local", "--db", path, "--json")...)
		if exitCode(err) != 3 {
			t.Errorf("%s: exit %d (%v), want 3", name, exitCode(err), err)
		}
	}
	// latest skips the newer archived video, like the live latest.
	out, _, err = runCLI(t, "transcript", "latest", "--data-source", "local", "--db", path, "--json")
	if err != nil || !strings.Contains(out, active) || strings.Contains(out, archived) {
		t.Fatalf("local latest: err=%v out=%s, want the active video", err, out)
	}
	db, err := store.OpenWithContext(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`UPDATE videos SET archived = 1`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, _, err := runCLI(t, "transcript", "latest", "--data-source", "local", "--db", path, "--json"); exitCode(err) != 3 {
		t.Fatalf("latest with only archived videos: exit %d (%v), want 3", exitCode(err), err)
	}
}

// TestAutoFallbackOnExpiredSession: in auto mode an expired session falls
// back to the local store (with a note) for per-video reads and lists, and
// a local miss then reports the expired session (exit 4), never "no data".
func TestAutoFallbackOnExpiredSession(t *testing.T) {
	path, active, _ := seedLocalStore(t)
	fakeLoomServer(t, func(string) string {
		return `{"errors":[{"message":"UserNotAuthorizedError: User is not logged in","extensions":{"code":"INTERNAL_SERVER_ERROR"}}],"data":null}`
	})
	out, stderr, err := runCLI(t, "transcript", active, "--db", path, "--insecure", "--json")
	if err != nil || !strings.Contains(stderr, "session expired") || !strings.Contains(out, "hello there") {
		t.Fatalf("transcript fallback: err=%v stderr=%q out=%q", err, stderr, out)
	}
	out, stderr, err = runCLI(t, "videos", "list", "--db", path, "--insecure", "--json")
	if err != nil || !strings.Contains(stderr, "session expired") || !strings.Contains(out, active) {
		t.Fatalf("videos list fallback: err=%v stderr=%q out=%q", err, stderr, out)
	}
	if _, _, err := runCLI(t, "transcript", "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f6f", "--db", path, "--insecure", "--json"); exitCode(err) != 4 {
		t.Fatalf("fallback miss: exit %d (%v), want 4 (the expired session)", exitCode(err), err)
	}
	if _, _, err := runCLI(t, "videos", "list", "--db", filepath.Join(t.TempDir(), "none.db"), "--insecure", "--json"); exitCode(err) != 4 {
		t.Fatalf("list with no store to fall back on: exit %d (%v), want 4", exitCode(err), err)
	}
}
