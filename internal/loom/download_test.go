package loom

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cmartinez9/loom-cli/internal/cliutil"
)

func TestParseDownloadURL(t *testing.T) {
	cases := []struct {
		data    string
		want    string
		wantErr error
	}{
		{`{"getVideoTranscodedUrl":{"__typename":"VideoSource","url":"https://cdn.loom.com/x.mp4?Policy=p"}}`, "https://cdn.loom.com/x.mp4?Policy=p", nil},
		{`{"getVideoTranscodedUrl":{"__typename":"EntityNotFoundError"}}`, "", ErrNoMP4},
		{`{"getVideoTranscodedUrl":null}`, "", ErrNoMP4},
	}
	for _, tc := range cases {
		got, err := parseDownloadURL("id", []byte(tc.data))
		if tc.wantErr != nil {
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s: err = %v", tc.data, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q, %v", tc.data, got, err)
		}
	}
	_, err := parseDownloadURL("id", []byte(`{"getVideoTranscodedUrl":{"__typename":"VideoNotFoundError"}}`))
	var ue *UnionError
	if !errors.As(err, &ue) || ue.Typename != "NotFoundError" {
		t.Fatalf("not found err = %v", err)
	}
}

// cdnServer serves body with Range support and optional 429s.
func cdnServer(t *testing.T, body []byte, throttle *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if throttle != nil && *throttle > 0 {
			*throttle--
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 05 Jan 2026 10:00:00 GMT")
		if rg := r.Header.Get("Range"); rg != "" && (r.Header.Get("If-Range") == "" || r.Header.Get("If-Range") == `"v1"`) {
			start, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)-start))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(body[start:])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	}))
}

func TestDownloaderFullSkipResume(t *testing.T) {
	body := []byte(strings.Repeat("0123456789", 1000))
	srv := cdnServer(t, body, nil)
	defer srv.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "v.mp4")
	d := NewDownloader()
	url := srv.URL + "/v.mp4?Policy=abc&Signature=def&Key-Pair-Id=ghi"

	res, err := d.Download(context.Background(), url, dest)
	if err != nil || res.Status != DownloadStatusDownloaded || res.Size != int64(len(body)) {
		t.Fatalf("first: %+v %v", res, err)
	}
	res, err = d.Download(context.Background(), url, dest)
	if err != nil || res.Status != DownloadStatusSkipped {
		t.Fatalf("second: %+v %v", res, err)
	}
	if fi, err := os.Stat(dest); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("downloaded file mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}
	// An interrupted download leaves a .part plus a sidecar naming the
	// remote object: it resumes with Range + If-Range.
	interrupt := func(withMeta remoteObject, writeMetaFile bool) {
		t.Helper()
		if err := os.Rename(dest, PartPath(dest)); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(PartPath(dest), 3333); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(MetaPath(dest))
		if writeMetaFile {
			if err := writeMeta(MetaPath(dest), withMeta); err != nil {
				t.Fatal(err)
			}
		}
	}
	same := remoteObject{Size: int64(len(body)), ETag: `"v1"`, LastModified: "Mon, 05 Jan 2026 10:00:00 GMT"}
	interrupt(same, true)
	res, err = d.Download(context.Background(), url, dest)
	if err != nil || res.Status != DownloadStatusResumed || res.Bytes != int64(len(body)-3333) {
		t.Fatalf("resume: %+v %v", res, err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(body) {
		t.Fatal("resumed file content differs")
	}
	for _, p := range []string{PartPath(dest), MetaPath(dest)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s left behind", p)
		}
	}
	// No sidecar, or a sidecar for a different object: restart from zero.
	interrupt(same, false)
	res, err = d.Download(context.Background(), url, dest)
	if err != nil || res.Status != DownloadStatusDownloaded || res.Bytes != int64(len(body)) {
		t.Fatalf("missing sidecar should restart: %+v %v", res, err)
	}
	stale := same
	stale.ETag = `"v0"`
	interrupt(stale, true)
	res, err = d.Download(context.Background(), url, dest)
	if err != nil || res.Status != DownloadStatusDownloaded || res.Bytes != int64(len(body)) {
		t.Fatalf("changed object should restart: %+v %v", res, err)
	}
	got, _ = os.ReadFile(dest)
	if string(got) != string(body) {
		t.Fatal("restarted file content differs")
	}
}

func TestDownloaderRefusesSymlinks(t *testing.T) {
	body := []byte("payload")
	srv := cdnServer(t, body, nil)
	defer srv.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.txt")
	if err := os.WriteFile(target, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := NewDownloader()
	for _, link := range []string{filepath.Join(dir, "a.mp4"), PartPath(filepath.Join(dir, "b.mp4"))} {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}
		dest := strings.TrimSuffix(link, ".part")
		if _, err := d.Download(context.Background(), srv.URL+"/v.mp4", dest); err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("%s: err = %v, want symlink refusal", link, err)
		}
	}
	if got, _ := os.ReadFile(target); string(got) != "keep me" {
		t.Fatalf("symlink target was overwritten: %q", got)
	}
}

func TestDownloaderRateLimitAndRedaction(t *testing.T) {
	throttle := 100
	srv := cdnServer(t, []byte("x"), &throttle)
	defer srv.Close()
	d := NewDownloader()
	d.MaxRetries = 1
	url := srv.URL + "/v.mp4?Policy=SECRETPOLICY&Signature=SECRETSIG"
	_, err := d.Download(context.Background(), url, filepath.Join(t.TempDir(), "v.mp4"))
	var rl *cliutil.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want RateLimitError", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("signed query leaked: %v", err)
	}

	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer gone.Close()
	_, err = d.Download(context.Background(), gone.URL+"/v.mp4?Signature=SECRETSIG", filepath.Join(t.TempDir(), "v.mp4"))
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("403 err = %v", err)
	}
}

// TestDownloaderRestartsOn416 covers a server that refuses to serve the rest
// of a part shorter than the object: the part is dropped and the file
// downloaded again from zero instead of looping on "re-run to resume".
func TestDownloaderRestartsOn416(t *testing.T) {
	body := []byte(strings.Repeat("abcdefghij", 100))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "v.mp4")
	if err := os.WriteFile(PartPath(dest), body[:100], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeMeta(MetaPath(dest), remoteObject{Size: int64(len(body)), ETag: `"v1"`}); err != nil {
		t.Fatal(err)
	}
	res, err := NewDownloader().Download(context.Background(), srv.URL+"/v.mp4", dest)
	if err != nil || res.Status != DownloadStatusDownloaded || res.Bytes != int64(len(body)) {
		t.Fatalf("416 resume: %+v %v, want a full restart", res, err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(body) {
		t.Fatal("restarted file content differs")
	}
}

func TestOpenNoFollowRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if f, err := openNoFollow(link, os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
		f.Close()
		t.Fatal("openNoFollow followed a symbolic link")
	}
	if err := writeMeta(link, remoteObject{Size: 1}); err == nil {
		t.Fatal("writeMeta wrote through a symbolic link")
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatalf("link target changed: %q", got)
	}
}
