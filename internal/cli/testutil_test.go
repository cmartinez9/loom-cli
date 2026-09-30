package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cmartinez9/loom-cli/internal/client"
	"github.com/cmartinez9/loom-cli/internal/cliutil"
	"github.com/cmartinez9/loom-cli/internal/cliutil/testenv"
	"github.com/cmartinez9/loom-cli/internal/config"
	"github.com/cmartinez9/loom-cli/internal/loom"
	"github.com/cmartinez9/loom-cli/internal/store"
)

// newTestStore opens a temp SQLite store with the Loom tables.
func newTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "loom.db")
	db, err := store.OpenWithContext(context.Background(), path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureLoomSchema(context.Background()); err != nil {
		t.Fatalf("EnsureLoomSchema: %v", err)
	}
	return db, path
}

// insertTestVideo adds a synced video row the way sync would.
func insertTestVideo(t *testing.T, db *store.Store, id, name, createdAt string, duration float64) {
	t.Helper()
	_, err := db.DB().Exec(`INSERT INTO videos (id, data, name, created_at, duration_seconds, views, comments, reactions, share_url)
		VALUES (?, '{}', ?, ?, ?, 0, 0, 0, ?)`, id, name, createdAt, duration, "https://www.loom.com/share/"+id)
	if err != nil {
		t.Fatalf("insert video: %v", err)
	}
}

// runCLI executes the root command with args and returns stdout, stderr
// and the error.
func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	testenv.Isolate(t)
	cmd := RootCmd()
	cmd.SetArgs(args)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// fakeLoom is the one fake Loom GraphQL server of the cli tests: it answers
// every call with respond(operationName) and counts the calls it answered.
type fakeLoom struct {
	srv   *httptest.Server
	calls atomic.Int32
}

// newFakeLoom starts a fake Loom server for the test. The generated
// transport speaks HTTP/2, so it serves TLS with h2.
func newFakeLoom(t *testing.T, respond func(op string) string) *fakeLoom {
	t.Helper()
	return newFakeLoomVars(t, func(op string, _ map[string]any) string { return respond(op) })
}

// newFakeLoomVars is newFakeLoom for answers that depend on the call's
// variables (for example a per-video answer keyed on videoId).
func newFakeLoomVars(t *testing.T, respond func(op string, vars map[string]any) string) *fakeLoom {
	t.Helper()
	testenv.Isolate(t)
	f := &fakeLoom{}
	f.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		var body struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respond(body.OperationName, body.Variables))
	}))
	f.srv.EnableHTTP2 = true
	f.srv.StartTLS()
	t.Cleanup(f.srv.Close)
	return f
}

// client returns a Loom client wired straight to the fake server.
func (f *fakeLoom) client() *loom.Client {
	c := client.New(&config.Config{BaseURL: f.srv.URL}, 5*time.Second, 0)
	c.HTTPClient = f.srv.Client()
	c.HTTPClient.Timeout = 5 * time.Second
	return loom.New(c)
}

// fakeLoomClient returns a Loom client whose GraphQL calls are answered by
// respond(operationName).
func fakeLoomClient(t *testing.T, respond func(op string) string) *loom.Client {
	t.Helper()
	return newFakeLoom(t, respond).client()
}

// fakeLoomServer points the CLI at a fake Loom server (LOOM_BASE_URL; pass
// --insecure for its TLS cert) and returns its call counter.
func fakeLoomServer(t *testing.T, respond func(op string) string) *atomic.Int32 {
	t.Helper()
	return fakeLoomServerVars(t, func(op string, _ map[string]any) string { return respond(op) })
}

// fakeLoomServerVars is fakeLoomServer with the call's variables.
func fakeLoomServerVars(t *testing.T, respond func(op string, vars map[string]any) string) *atomic.Int32 {
	t.Helper()
	f := newFakeLoomVars(t, respond)
	t.Setenv("LOOM_BASE_URL", f.srv.URL)
	return &f.calls
}

// writeTestFile writes content to path (0600).
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

// loomRateLimit is a typed rate-limit failure for tests.
var loomRateLimit = cliutil.RateLimitError{URL: "/graphql"}
