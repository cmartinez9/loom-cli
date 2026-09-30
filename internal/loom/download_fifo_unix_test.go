//go:build !windows

package loom

import (
	"context"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestDownloaderRefusesFIFOs checks that a FIFO planted at the destination,
// the .part file or the resume sidecar is refused without being opened: an
// open of a FIFO with no writer would block the command forever.
func TestDownloaderRefusesFIFOs(t *testing.T) {
	body := []byte("payload")
	srv := cdnServer(t, body, nil)
	defer srv.Close()
	dir := t.TempDir()

	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ok, err := statNoFollow(fifo); ok || err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("statNoFollow(fifo) = ok %v, err %v; want a not-a-regular-file refusal", ok, err)
		}
		if _, ok := readMeta(fifo); ok {
			t.Error("readMeta accepted a FIFO")
		}
		d := NewDownloader()
		for _, name := range []string{"a.mp4", "b.mp4.part", "c.mp4.part.meta"} {
			p := filepath.Join(dir, name)
			if err := syscall.Mkfifo(p, 0o600); err != nil {
				t.Errorf("mkfifo %s: %v", name, err)
				continue
			}
			dest := strings.TrimSuffix(strings.TrimSuffix(p, ".meta"), ".part")
			if _, err := d.Download(context.Background(), srv.URL+"/v.mp4", dest); err == nil || !strings.Contains(err.Error(), "not a regular file") {
				t.Errorf("%s: err = %v, want a not-a-regular-file refusal", name, err)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a FIFO blocked the download")
	}
}
