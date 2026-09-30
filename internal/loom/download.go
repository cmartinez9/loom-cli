package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cmartinez9/loom-cli/internal/cliutil"
)

// Downloader streams signed cdn.loom.com MP4s to disk. It is resumable
// (HTTP Range onto a .part file), skips files that are already complete,
// paces requests with its own AdaptiveLimiter and returns
// *cliutil.RateLimitError when the CDN keeps answering 429.
//
// Signed URLs never appear in its errors: every message goes through
// RedactSignedURL.
type Downloader struct {
	HTTP       *http.Client
	lim        *cliutil.AdaptiveLimiter
	MaxRetries int
}

// NewDownloader builds a downloader with no whole-transfer timeout (large
// files take as long as they take); stalled servers are still cut off by
// the response-header timeout. Bound a transfer with the context.
func NewDownloader() *Downloader {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 60 * time.Second
	return &Downloader{
		HTTP:       &http.Client{Transport: tr},
		lim:        cliutil.NewAdaptiveLimiter(2),
		MaxRetries: 3,
	}
}

// Download statuses.
const (
	DownloadStatusDownloaded = "downloaded"
	DownloadStatusResumed    = "resumed"
	DownloadStatusSkipped    = "skipped"
)

// DownloadResult describes one completed transfer.
type DownloadResult struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Bytes  int64  `json:"bytes"`
	Size   int64  `json:"size"`
}

// PartPath is where an in-progress download is written.
func PartPath(dest string) string { return dest + ".part" }

// MetaPath is the sidecar next to a .part file that records which remote
// object (ETag / Last-Modified / size) the partial bytes came from, so a
// resume never splices bytes from two different versions of a file.
func MetaPath(dest string) string { return PartPath(dest) + ".meta" }

// remoteObject is what HEAD reports about the signed object.
type remoteObject struct {
	Size         int64  `json:"size"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
}

// validator is the If-Range value for a resume: a strong ETag, else
// Last-Modified. Empty means the object cannot be validated.
func (o remoteObject) validator() string {
	if o.ETag != "" && !strings.HasPrefix(o.ETag, "W/") {
		return o.ETag
	}
	return o.LastModified
}

// refuseSymlink fails when path exists and is a symbolic link, so a
// planted link cannot redirect a download onto another file.
func refuseSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write %s: it is a symbolic link", path)
	}
	return nil
}

// readMeta reads a resume sidecar. Anything but a readable regular file
// (missing, a symbolic link, a FIFO, a directory) reports ok=false, and
// the regular-file check runs before the open so a FIFO cannot block it.
func readMeta(path string) (remoteObject, bool) {
	var o remoteObject
	want, ok, err := statNoFollow(path)
	if err != nil || !ok {
		return o, false
	}
	f, err := openNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return o, false
	}
	defer f.Close()
	if got, err := f.Stat(); err != nil || !os.SameFile(want, got) {
		// Swapped between the check and the open.
		return o, false
	}
	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil || json.Unmarshal(b, &o) != nil {
		return o, false
	}
	return o, true
}

// writeMeta records a resume sidecar. An existing sidecar that is not a
// regular file is refused before the open (a FIFO would block it).
func writeMeta(path string, o remoteObject) error {
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	if _, _, err := statNoFollow(path); err != nil {
		return err
	}
	f, err := openNoFollow(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// statNoFollow Lstats path and requires a regular file: a symbolic link is
// refused rather than followed, and a FIFO, device or directory is refused
// without ever being opened (opening a FIFO would block the command). A
// missing file reports ok=false.
func statNoFollow(path string) (os.FileInfo, bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("checking %s: %w", path, err)
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return nil, false, fmt.Errorf("refusing to open %s: it is a symbolic link", path)
	case !fi.Mode().IsRegular():
		return nil, false, fmt.Errorf("refusing to use %s: it is not a regular file", path)
	}
	return fi, true, nil
}

// errRangeNotSatisfiable is fetchInto's report of an HTTP 416 answer to a
// resume: the server has no bytes past the part file's size.
var errRangeNotSatisfiable = errors.New("range not satisfiable")

// Download streams url into dest. A complete dest (same size as the remote
// Content-Length) is skipped. A dest.part is resumed with a Range request
// guarded by If-Range, but only when its sidecar records the same remote
// object (ETag / Last-Modified / size); otherwise it restarts from zero, as
// it does when the server answers a resume with 416 while the part is
// shorter than the object. The .part is renamed onto dest once its size
// matches. dest, the .part and the sidecar are opened without following
// symbolic links (O_NOFOLLOW), and files are created 0600.
func (d *Downloader) Download(ctx context.Context, url, dest string) (*DownloadResult, error) {
	obj, err := d.remoteObject(ctx, url)
	if err != nil {
		return nil, err
	}
	part, meta := PartPath(dest), MetaPath(dest)
	for _, p := range []string{dest, part, meta} {
		if err := refuseSymlink(p); err != nil {
			return nil, err
		}
	}
	size := obj.Size
	if fi, ok, err := statNoFollow(dest); err != nil {
		return nil, err
	} else if ok && !fi.IsDir() && size > 0 && fi.Size() == size {
		return &DownloadResult{Path: dest, Status: DownloadStatusSkipped, Size: size}, nil
	}
	var offset int64
	if fi, ok, err := statNoFollow(part); err != nil {
		return nil, err
	} else if ok && !fi.IsDir() {
		offset = fi.Size()
		prev, ok := readMeta(meta)
		if !ok || obj.validator() == "" || prev != obj || (size > 0 && offset > size) {
			// Unknown or changed remote object: the partial bytes cannot
			// be trusted to belong to this file.
			_ = os.Remove(part)
			offset = 0
		}
	}
	if offset == 0 {
		if err := writeMeta(meta, obj); err != nil {
			return nil, fmt.Errorf("recording download state for %s: %w", dest, err)
		}
	}
	status := DownloadStatusDownloaded
	if offset > 0 {
		status = DownloadStatusResumed
	}
	var written int64
	if size <= 0 || offset < size {
		n, restarted, err := d.fetchInto(ctx, url, part, offset, obj.validator())
		if errors.Is(err, errRangeNotSatisfiable) {
			switch {
			case size > 0:
				// The server will not serve the rest of a part shorter
				// than the object: resuming again would loop forever.
				// Drop the part and its sidecar and start over.
				_ = os.Remove(part)
				_ = os.Remove(meta)
				if err := writeMeta(meta, obj); err != nil {
					return nil, fmt.Errorf("recording download state for %s: %w", dest, err)
				}
				n, _, err = d.fetchInto(ctx, url, part, 0, "")
				restarted = true
			default:
				// Unknown size: the part already holds the whole object.
				err = nil
			}
		}
		if err != nil {
			return nil, err
		}
		written = n
		if restarted {
			status = DownloadStatusDownloaded
		}
	}
	fi, ok, err := statNoFollow(part)
	if err == nil && !ok {
		err = os.ErrNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("checking %s: %w", part, err)
	}
	if size > 0 && fi.Size() != size {
		return nil, fmt.Errorf("download of %s incomplete: have %d of %d bytes (re-run to resume)", RedactSignedURL(url), fi.Size(), size)
	}
	if err := refuseSymlink(dest); err != nil {
		return nil, err
	}
	// rename(2) replaces dest itself and never follows a link there.
	if err := os.Rename(part, dest); err != nil {
		return nil, fmt.Errorf("finalizing %s: %w", dest, err)
	}
	_ = os.Remove(meta)
	return &DownloadResult{Path: dest, Status: status, Bytes: written, Size: fi.Size()}, nil
}

// remoteObject asks the CDN for the object's size and validators with
// HEAD. A zero size means unknown.
func (d *Downloader) remoteObject(ctx context.Context, url string) (remoteObject, error) {
	resp, err := d.do(ctx, http.MethodHead, url, "", "")
	if err != nil {
		return remoteObject{}, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return remoteObject{}, cdnStatusErr(url, resp.StatusCode)
	}
	o := remoteObject{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
	if resp.ContentLength > 0 {
		o.Size = resp.ContentLength
	}
	return o, nil
}

// fetchInto GETs url from offset and appends to part. A resume sends
// If-Range, so a changed object comes back whole (200): the part file is
// then truncated and restarted is true.
func (d *Downloader) fetchInto(ctx context.Context, url, part string, offset int64, ifRange string) (int64, bool, error) {
	rangeHdr := ""
	if offset > 0 {
		rangeHdr = "bytes=" + strconv.FormatInt(offset, 10) + "-"
	}
	resp, err := d.do(ctx, http.MethodGet, url, rangeHdr, ifRange)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	restarted := false
	flag := os.O_CREATE | os.O_WRONLY
	switch {
	case resp.StatusCode == http.StatusPartialContent && offset > 0:
		flag |= os.O_APPEND
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0:
		return 0, false, errRangeNotSatisfiable
	case resp.StatusCode == http.StatusOK:
		flag |= os.O_TRUNC
		restarted = offset > 0
	default:
		return 0, false, cdnStatusErr(url, resp.StatusCode)
	}
	f, err := openNoFollow(part, flag, 0o600)
	if err != nil {
		return 0, false, fmt.Errorf("opening %s: %w", part, err)
	}
	n, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return n, restarted, fmt.Errorf("streaming %s: %s (partial file kept; re-run to resume)", RedactSignedURL(url), RedactErr(copyErr, url))
	}
	if closeErr != nil {
		return n, restarted, fmt.Errorf("writing %s: %w", part, closeErr)
	}
	return n, restarted, nil
}

// do sends one request through the limiter, retrying 429 answers up to
// MaxRetries times before returning *cliutil.RateLimitError.
func (d *Downloader) do(ctx context.Context, method, url, rangeHdr, ifRange string) (*http.Response, error) {
	var lastRetry time.Duration
	for attempt := 0; ; attempt++ {
		if err := d.lim.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("building request for %s: %s", RedactSignedURL(url), RedactErr(err, url))
		}
		if rangeHdr != "" {
			req.Header.Set("Range", rangeHdr)
			if ifRange != "" {
				req.Header.Set("If-Range", ifRange)
			}
		}
		resp, err := d.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("requesting %s: %s", RedactSignedURL(url), RedactErr(err, url))
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			d.lim.OnSuccess()
			return resp, nil
		}
		d.lim.OnRateLimit()
		lastRetry = cliutil.RetryAfter(resp)
		_ = resp.Body.Close()
		if attempt >= d.MaxRetries {
			return nil, &cliutil.RateLimitError{URL: RedactSignedURL(url), RetryAfter: lastRetry}
		}
		wait := lastRetry
		if wait <= 0 {
			wait = cliutil.Backoff(attempt)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func cdnStatusErr(url string, code int) error {
	return &CDNError{URL: RedactSignedURL(url), Status: code}
}

// DownloadURL returns the short-lived signed MP4 URL for a video. Never log
// the result; use RedactSignedURL.
func (l *Client) DownloadURL(ctx context.Context, id string) (string, error) {
	data, err := l.Query(ctx, OpDownloadURL, map[string]any{"videoId": id})
	if err != nil {
		return "", err
	}
	if l.IsDryRun() {
		return "", nil
	}
	u, err := parseDownloadURL(id, data)
	if err != nil && !errors.Is(err, ErrNoMP4) {
		// A union error member may mean the session is gone.
		return "", l.normalizeAuth(ctx, err)
	}
	return u, err
}

// parseDownloadURL decodes a GetVideoTranscodedUrl data object. It is pure:
// DownloadURL normalizes a signed-out session on the errors it returns.
func parseDownloadURL(id string, data json.RawMessage) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return "", fmt.Errorf("%s: %w", OpDownloadURL, err)
	}
	raw := obj["getVideoTranscodedUrl"]
	if len(raw) == 0 || string(raw) == "null" {
		return "", ErrNoMP4
	}
	var r struct {
		Typename string `json:"__typename"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("%s: %w", OpDownloadURL, err)
	}
	switch r.Typename {
	case "VideoSource", "":
		if r.URL == "" {
			return "", ErrNoMP4
		}
		return r.URL, nil
	case "EntityNotFoundError":
		return "", ErrNoMP4
	case "VideoNotFoundError":
		return "", &UnionError{Op: OpDownloadURL, Typename: "NotFoundError", Message: "video " + id + " not found"}
	}
	if err := CheckUnion(OpDownloadURL, raw); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s: unexpected result type %s", OpDownloadURL, r.Typename)
}
