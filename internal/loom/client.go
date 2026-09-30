package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cmartinez9/loom-cli/internal/client"
	"github.com/cmartinez9/loom-cli/internal/cliutil"
)

// graphqlPath is the single endpoint every Loom web-app data call uses.
const graphqlPath = "/graphql"

// Client runs Loom operations over the generated HTTP client, which already
// carries the cookie jar, required headers, adaptive rate limiting, retries
// and --dry-run previews.
type Client struct {
	c      *client.Client
	cdn    *http.Client
	cdnLim *cliutil.AdaptiveLimiter

	// authMu guards the cached session state and is held across the Me
	// probe, so concurrent workers that all hit an ambiguous error share
	// one probe instead of each asking Me. An expired session stays
	// expired for the client's life; a valid answer is trusted for
	// sessionValidTTL, after which the next ambiguous error probes again.
	authMu      sync.Mutex
	authExpired bool
	authValidAt time.Time

	// lastThrottle is when Loom last answered 429 (unix nanoseconds), so a
	// call whose deadline ran out while it waited for the rate-limit
	// budget reports rate limiting rather than a plain timeout.
	lastThrottle atomic.Int64
}

// sessionValidTTL is how long a "session is valid" probe answer is reused.
const sessionValidTTL = 2 * time.Minute

// New wraps a generated client. It watches the client's responses for 429
// answers (see lastThrottle) by wrapping its transport.
func New(c *client.Client) *Client {
	timeout := 60 * time.Second
	if c != nil && c.HTTPClient != nil && c.HTTPClient.Timeout > 0 {
		timeout = c.HTTPClient.Timeout
	}
	l := &Client{
		c:      c,
		cdn:    &http.Client{Timeout: timeout},
		cdnLim: cliutil.NewAdaptiveLimiter(4),
	}
	if c != nil && c.HTTPClient != nil {
		hc := *c.HTTPClient
		next := hc.Transport
		if next == nil {
			next = http.DefaultTransport
		}
		hc.Transport = throttleWatch{next: next, seen: &l.lastThrottle}
		c.HTTPClient = &hc
	}
	return l
}

// throttleWatch records the time of every 429 answer it passes through.
type throttleWatch struct {
	next http.RoundTripper
	seen *atomic.Int64
}

func (t throttleWatch) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(r)
	if err == nil && resp.StatusCode == http.StatusTooManyRequests {
		t.seen.Store(time.Now().UnixNano())
	}
	return resp, err
}

// throttledTimeout turns a deadline that ran out after Loom throttled this
// call (the transport retries 429s and then waits for the rate-limit budget
// to refill) into a *cliutil.RateLimitError, so it exits 7 like any other
// rate limit instead of looking like a slow network.
func (l *Client) throttledTimeout(op string, started time.Time, err error) error {
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || l.lastThrottle.Load() < started.UnixNano() {
		return err
	}
	return &cliutil.RateLimitError{URL: graphqlPath + " (" + op + ")", Body: "the time budget ran out while waiting for Loom's rate limit", Cause: err}
}

// IsDryRun reports whether requests only print previews.
func (l *Client) IsDryRun() bool { return l.c != nil && l.c.IsDryRun() }

type request struct {
	OperationName string         `json:"operationName"`
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
}

type response struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message    string `json:"message"`
		Extensions struct {
			Code string `json:"code"`
		} `json:"extensions"`
	} `json:"errors"`
}

// Query runs a read-only operation and returns its data object.
func (l *Client) Query(ctx context.Context, op string, vars map[string]any) (json.RawMessage, error) {
	return l.run(ctx, op, vars, false)
}

// Mutate runs a mutation. Under --dry-run the generated client returns a
// preview envelope instead of sending it; callers should check IsDryRun
// before interpreting the result.
func (l *Client) Mutate(ctx context.Context, op string, vars map[string]any) (json.RawMessage, error) {
	return l.run(ctx, op, vars, true)
}

func (l *Client) run(ctx context.Context, op string, vars map[string]any, mutation bool) (json.RawMessage, error) {
	if l == nil || l.c == nil {
		return nil, errors.New("loom client not initialized")
	}
	doc, err := Document(op)
	if err != nil {
		return nil, err
	}
	if vars == nil {
		vars = map[string]any{}
	}
	body := request{OperationName: op, Query: doc, Variables: vars}
	var raw json.RawMessage
	started := time.Now()
	if mutation {
		raw, _, err = l.c.Post(ctx, graphqlPath, body)
	} else {
		raw, _, err = l.c.PostQueryWithParams(ctx, graphqlPath, nil, body)
	}
	if err != nil {
		return nil, l.throttledTimeout(op, started, err)
	}
	if l.c.IsDryRun() {
		return raw, nil
	}
	data, err := decode(op, raw)
	if err != nil {
		return nil, l.normalizeAuth(ctx, err)
	}
	return data, nil
}

func decode(op string, raw json.RawMessage) (json.RawMessage, error) {
	var resp response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%s: decoding response: %w", op, err)
	}
	if len(resp.Errors) > 0 {
		gerr := &GraphQLError{Op: op}
		for _, e := range resp.Errors {
			gerr.Messages = append(gerr.Messages, e.Message)
			gerr.Codes = append(gerr.Codes, e.Extensions.Code)
		}
		// Partial data with errors is still an error for a CLI: surface it.
		// run passes it through normalizeAuth, which recognizes a
		// signed-out session.
		return nil, gerr
	}
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return nil, fmt.Errorf("%s: empty data in response", op)
	}
	return resp.Data, nil
}

// Field extracts a top-level field from a data object and checks its union
// typename, reporting error members as a typed UnionError. It is pure;
// fetchers use (*Client).field, which also normalizes a signed-out session.
func Field(op string, data json.RawMessage, name string) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("%s: decoding data: %w", op, err)
	}
	v, ok := obj[name]
	if !ok || len(v) == 0 || string(v) == "null" {
		if ok && op == OpVideoDetails {
			// getVideo answers null for ids that do not exist.
			return nil, &UnionError{Op: op, Typename: "NotFoundError", Message: "video not found"}
		}
		return nil, fmt.Errorf("%s: response has no %q field", op, name)
	}
	if err := CheckUnion(op, v); err != nil {
		return nil, err
	}
	return v, nil
}

// looksSignedOut matches Loom's signed-out GraphQL error text, which often
// arrives with code INTERNAL_SERVER_ERROR rather than UNAUTHENTICATED
// ("UserNotAuthorizedError: User is not logged in").
func looksSignedOut(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "not logged in") || strings.Contains(m, "no logged in viewer") || strings.Contains(m, "not authenticated")
}

// field is Field for fetchers: the same extraction, with a signed-out
// session normalized into ErrSessionExpired.
func (l *Client) field(ctx context.Context, op string, data json.RawMessage, name string) (json.RawMessage, error) {
	v, err := Field(op, data, name)
	if err != nil {
		return nil, l.normalizeAuth(ctx, err)
	}
	return v, nil
}

// signedOutOps are operations that only fail with UserNotAuthorizedError
// when the session itself is gone (they read the signed-in user's own data),
// so no Me probe is needed to tell "signed out" from "not yours".
var signedOutOps = map[string]bool{OpLibrary: true, OpSearch: true}

// probeTypenames are per-object union errors Loom also returns to a
// signed-out session; normalizeAuth asks Me once to tell the two apart.
var probeTypenames = map[string]bool{"UserNotAuthorizedError": true, "NotFoundError": true, "PrivateVideo": true}

// normalizeAuth is the single place that recognizes a signed-out session,
// however Loom reported it, and turns it into ErrSessionExpired:
//   - a GraphQL error with code UNAUTHENTICATED or signed-out text
//     ("User is not logged in", often under INTERNAL_SERVER_ERROR);
//   - UserNotAuthorizedError on an operation over the user's own data
//     (library listing, global search);
//   - a GraphQL "UserNotAuthorizedError" message, or a UserNotAuthorizedError,
//     NotFoundError or PrivateVideo union member on a per-object operation,
//     when one Me probe confirms the session is gone. The probe's answer is
//     cached on the client, so a run probes at most once.
//
// Every fetcher applies it at its boundary; other errors pass through.
func (l *Client) normalizeAuth(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, ErrSessionExpired) {
		return err
	}
	if l != nil && l.IsDryRun() {
		return err
	}
	probe := false
	op := ""
	var ge *GraphQLError
	var ue *UnionError
	switch {
	case errors.As(err, &ge):
		op = ge.Op
		for i, m := range ge.Messages {
			if (i < len(ge.Codes) && strings.EqualFold(ge.Codes[i], "UNAUTHENTICATED")) || looksSignedOut(m) {
				return ErrSessionExpired
			}
		}
		for _, m := range ge.Messages {
			if strings.Contains(m, "UserNotAuthorizedError") {
				probe = true
			}
		}
	case errors.As(err, &ue):
		op = ue.Op
		if ue.Typename == "UserNotAuthorizedError" && signedOutOps[ue.Op] {
			return ErrSessionExpired
		}
		probe = probeTypenames[ue.Typename]
	}
	if probe && op != OpMe && l.sessionGone(ctx) {
		return ErrSessionExpired
	}
	return err
}

// sessionGone reports whether the session has expired. Concurrent callers
// wait on one Me probe; an expired answer is cached for good and a valid
// one for sessionValidTTL. An inconclusive probe (network error) reports
// false and caches nothing.
func (l *Client) sessionGone(ctx context.Context) bool {
	if l == nil || l.c == nil {
		return false
	}
	l.authMu.Lock()
	defer l.authMu.Unlock()
	if l.authExpired {
		return true
	}
	if !l.authValidAt.IsZero() && time.Since(l.authValidAt) < sessionValidTTL {
		return false
	}
	_, expired, err := l.me(ctx)
	l.noteAuthLocked(expired, err)
	return expired
}

// noteAuthLocked caches a definitive probe answer; authMu must be held.
func (l *Client) noteAuthLocked(expired bool, err error) {
	if l.IsDryRun() {
		return
	}
	switch {
	case expired:
		l.authExpired = true
	case err == nil:
		l.authValidAt = time.Now()
	}
}

// CheckMutation inspects a mutation's data object: an error union member
// (GenericError, UserNotAuthorizedError, ...) or a top-level literal false
// is an error. Union errors pass through normalizeAuth, so a signed-out
// session is reported as ErrSessionExpired rather than "not found".
func (l *Client) CheckMutation(ctx context.Context, op string, data json.RawMessage) error {
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil {
		return nil
	}
	for name, v := range obj {
		if strings.TrimSpace(string(v)) == "false" {
			return fmt.Errorf("%s (%s.%s): %w", op, op, name, ErrMutationRejected)
		}
		if err := CheckUnion(op, v); err != nil {
			return l.normalizeAuth(ctx, err)
		}
	}
	return nil
}

// FetchCDN GETs a signed cdn.loom.com URL (transcripts, captions). Signed
// URLs carry their own CloudFront authorization, so no cookie is sent.
func (l *Client) FetchCDN(ctx context.Context, url string) ([]byte, error) {
	if err := l.cdnLim.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		// A malformed URL error quotes the whole signed URL.
		return nil, redactedErr("building request for", url, err)
	}
	resp, err := l.cdn.Do(req)
	if err != nil {
		// *url.Error embeds the full signed URL; never let it through.
		return nil, redactedErr("fetching", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		l.cdnLim.OnRateLimit()
		return nil, &cliutil.RateLimitError{URL: redactSigned(url), RetryAfter: cliutil.RetryAfter(resp)}
	}
	if resp.StatusCode >= 400 {
		return nil, &CDNError{URL: redactSigned(url), Status: resp.StatusCode}
	}
	l.cdnLim.OnSuccess()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, redactedErr("reading", url, err)
	}
	return body, nil
}
