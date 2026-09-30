package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cmartinez9/loom-cli/internal/client"
	"github.com/cmartinez9/loom-cli/internal/cliutil"
	"github.com/cmartinez9/loom-cli/internal/cliutil/testenv"
	"github.com/cmartinez9/loom-cli/internal/config"
)

// failingTransport fails every request the way net/http does: with a
// *url.Error whose message embeds the full request URL.
type failingTransport struct{}

func (failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, errors.New("connection reset by peer")
}

func TestFetchCDNRedactsSignedURL(t *testing.T) {
	l := New(nil)
	l.cdn = &http.Client{Transport: failingTransport{}}
	signed := "https://cdn.loom.com/sessions/transcriptions/x.json?Policy=POLICYVALUE&Key-Pair-Id=KEYPAIRVALUE&Signature=SIGVALUE"
	_, err := l.FetchCDN(context.Background(), signed)
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	for _, leak := range []string{"Signature=", "Policy=", "Key-Pair-Id=", "POLICYVALUE", "KEYPAIRVALUE", "SIGVALUE"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("error leaks %q: %s", leak, msg)
		}
	}
	if !strings.Contains(msg, "cdn.loom.com/sessions/transcriptions/x.json") {
		t.Fatalf("error should still name the redacted URL: %s", msg)
	}
	// A deadline still unwraps for callers that stop on it.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l.cdn = &http.Client{}
	if _, err := l.FetchCDN(ctx, signed); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "Signature=") {
		t.Fatalf("canceled fetch err = %v", err)
	}
}

// fakeGraphQL returns a Client whose calls are answered by respond(op).
func fakeGraphQL(t *testing.T, respond func(op string) string) (*Client, *int) {
	t.Helper()
	testenv.Isolate(t)
	meCalls := 0
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			OperationName string `json:"operationName"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.OperationName == OpMe {
			meCalls++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respond(body.OperationName)))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	c := client.New(&config.Config{BaseURL: srv.URL}, 5*time.Second, 0)
	c.HTTPClient = srv.Client()
	return New(c), &meCalls
}

func TestCheckMutation(t *testing.T) {
	ctx := context.Background()
	signedOut, meCalls := fakeGraphQL(t, func(op string) string { return `{"data":{"me":null}}` })
	err := signedOut.CheckMutation(ctx, OpUpdateName, json.RawMessage(`{"updateVideoName":{"__typename":"UserNotAuthorizedError","message":"no"}}`))
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("signed-out union = %v, want ErrSessionExpired", err)
	}
	// A second failure reuses the cached probe answer.
	_ = signedOut.CheckMutation(ctx, OpUpdateName, json.RawMessage(`{"x":{"__typename":"NotFoundError"}}`))
	if *meCalls != 1 {
		t.Fatalf("Me probed %d times, want 1", *meCalls)
	}

	signedIn, _ := fakeGraphQL(t, func(op string) string { return `{"data":{"me":{"id":"1"}}}` })
	err = signedIn.CheckMutation(ctx, OpUpdateName, json.RawMessage(`{"updateVideoName":{"__typename":"NotFoundError","message":"gone"}}`))
	var ue *UnionError
	if !errors.As(err, &ue) || errors.Is(err, ErrSessionExpired) {
		t.Fatalf("signed-in not found = %v, want the UnionError", err)
	}
	if err := signedIn.CheckMutation(ctx, OpPin, json.RawMessage(`{"updateVideoPinStatus":false}`)); !errors.Is(err, ErrMutationRejected) {
		t.Fatalf("literal false = %v, want ErrMutationRejected", err)
	}
	if err := signedIn.CheckMutation(ctx, OpPin, json.RawMessage(`{"updateVideoPinStatus":true}`)); err != nil {
		t.Fatalf("literal true = %v, want nil", err)
	}
}

func TestFetchCDNRedactsBadRequestURL(t *testing.T) {
	l := New(nil)
	// A control character makes NewRequest fail with an error quoting the URL.
	_, err := l.FetchCDN(context.Background(), "https://cdn.loom.com/x.json?Signature=SIGVALUE\x7f")
	if err == nil || strings.Contains(err.Error(), "SIGVALUE") {
		t.Fatalf("bad-URL err = %v, want a redacted error", err)
	}
}

func TestSessionProbeCaching(t *testing.T) {
	ctx := context.Background()
	notFound := &UnionError{Op: OpTranscript, Typename: "NotFoundError"}

	// A valid answer is reused within the TTL, and concurrent callers share
	// one probe.
	signedIn, meCalls := fakeGraphQL(t, func(op string) string { return `{"data":{"me":{"id":"1"}}}` })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := signedIn.normalizeAuth(ctx, notFound); errors.Is(err, ErrSessionExpired) {
				t.Errorf("signed-in session reported expired")
			}
		}()
	}
	wg.Wait()
	if *meCalls != 1 {
		t.Fatalf("Me probed %d times by concurrent callers, want 1", *meCalls)
	}
	// Past the TTL the valid answer is re-checked.
	signedIn.authMu.Lock()
	signedIn.authValidAt = time.Now().Add(-sessionValidTTL - time.Second)
	signedIn.authMu.Unlock()
	_ = signedIn.normalizeAuth(ctx, notFound)
	if *meCalls != 2 {
		t.Fatalf("Me probed %d times after the TTL, want 2", *meCalls)
	}

	// An expired answer is cached for good.
	signedOut, outCalls := fakeGraphQL(t, func(op string) string { return `{"data":{"me":null}}` })
	for i := 0; i < 3; i++ {
		if err := signedOut.normalizeAuth(ctx, notFound); !errors.Is(err, ErrSessionExpired) {
			t.Fatalf("signed-out = %v, want ErrSessionExpired", err)
		}
	}
	if *outCalls != 1 {
		t.Fatalf("Me probed %d times for an expired session, want 1", *outCalls)
	}
}

func TestVideoExistsChecksSessionForEveryMissingShape(t *testing.T) {
	ctx := context.Background()
	shapes := []string{
		`{"data":{"video":null}}`,
		`{"data":{"video":{"__typename":"NotFoundError"}}}`,
		`{"data":{"video":{"__typename":"PrivateVideo"}}}`,
		`{"data":{"video":{"__typename":"InvalidRequestWarning"}}}`,
		`{"data":{"video":{"__typename":"UserNotAuthorizedError"}}}`,
	}
	for _, shape := range shapes {
		out, _ := fakeGraphQL(t, func(op string) string {
			if op == OpMe {
				return `{"data":{"me":null}}`
			}
			return shape
		})
		if ok, err := out.VideoExists(ctx, "x"); ok || !errors.Is(err, ErrSessionExpired) {
			t.Errorf("signed out, %s: (%v, %v), want ErrSessionExpired", shape, ok, err)
		}
		in, _ := fakeGraphQL(t, func(op string) string {
			if op == OpMe {
				return `{"data":{"me":{"id":"1"}}}`
			}
			return shape
		})
		if ok, err := in.VideoExists(ctx, "x"); ok || err != nil {
			t.Errorf("signed in, %s: (%v, %v), want (false, nil)", shape, ok, err)
		}
	}
}

func TestGetFolderNotFound(t *testing.T) {
	ctx := context.Background()
	for _, answer := range []string{
		`{"errors":[{"message":"Folder Not Found","extensions":{"code":"NOT_FOUND"}}],"data":null}`,
		`{"data":{"folder":null}}`,
		`{"data":{"folder":{"__typename":"NotFoundError","message":"gone"}}}`,
	} {
		lc, _ := fakeGraphQL(t, func(op string) string {
			if op == OpMe {
				return `{"data":{"me":{"id":"1"}}}`
			}
			return answer
		})
		_, err := lc.GetFolder(ctx, "f1")
		var nf *NotFoundError
		if !errors.As(err, &nf) || nf.Kind != "folder" {
			t.Errorf("%s: err = %v, want a folder NotFoundError", answer, err)
		}
	}
}

// TestThrottledTimeoutIsRateLimit: when a call's deadline runs out while the
// transport is waiting out Loom's 429s, the error is a rate limit (exit 7),
// not a plain timeout.
func TestThrottledTimeoutIsRateLimit(t *testing.T) {
	testenv.Isolate(t)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	c := client.New(&config.Config{BaseURL: srv.URL}, 5*time.Second, 0)
	c.HTTPClient = srv.Client()
	l := New(c)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_, err := l.Query(ctx, OpMe, nil)
	var rl *cliutil.RateLimitError
	if !errors.As(err, &rl) || !IsRunStopping(err) {
		t.Fatalf("err = %v, want a RateLimitError", err)
	}

	// A deadline with no 429 stays a deadline.
	slow := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"data":{"me":{"id":"1"}}}`))
	}))
	slow.EnableHTTP2 = true
	slow.StartTLS()
	t.Cleanup(slow.Close)
	c2 := client.New(&config.Config{BaseURL: slow.URL}, 5*time.Second, 0)
	c2.HTTPClient = slow.Client()
	l2 := New(c2)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	if _, err := l2.Query(ctx2, OpMe, nil); errors.As(err, &rl) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow call err = %v, want a plain deadline", err)
	}
}
