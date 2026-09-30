package loom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"loom-pp-cli/internal/cliutil"
)

// Typed errors every fetcher reports, and the predicates commands use to
// map them onto exit codes.

// ErrSessionExpired means Loom rejected the connect.sid session cookie (or no
// cookie was configured). Loom signals this either as a GraphQL error with
// extensions.code UNAUTHENTICATED or as HTTP 200 with a union member typed
// UserNotAuthorizedError, never as HTTP 401.
var ErrSessionExpired = errors.New("Loom session expired or missing: sign in to loom.com in Chrome and run 'loom-pp-cli auth login --chrome', or set LOOM_COOKIE='connect.sid=<value>'")

// UnionError is a typed error member of a GraphQL union result, such as
// GenericError, PrivateVideo or UserNotAuthorizedError.
type UnionError struct {
	Op       string
	Typename string
	Message  string
}

func (e *UnionError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = "no message"
	}
	return fmt.Sprintf("%s returned %s: %s", e.Op, e.Typename, msg)
}

// GraphQLError is a non-auth error from Loom's GraphQL layer (validation,
// bad input, server errors).
type GraphQLError struct {
	Op       string
	Messages []string
	Codes    []string
}

func (e *GraphQLError) Error() string {
	return fmt.Sprintf("%s: %s", e.Op, strings.Join(e.Messages, "; "))
}

// errorTypenames are union members Loom uses for failures.
var errorTypenames = map[string]bool{
	"GenericError":                    true,
	"UserNotAuthorizedError":          true,
	"InvalidRequestWarning":           true,
	"PrivateVideo":                    true,
	"VideoPasswordMissingOrIncorrect": true,
	"NotFoundError":                   true,
}

// CheckUnion returns a UnionError when v is an object typed as an error member.
func CheckUnion(op string, v json.RawMessage) error {
	var head struct {
		Typename string `json:"__typename"`
		Message  string `json:"message"`
	}
	if json.Unmarshal(v, &head) != nil || head.Typename == "" {
		return nil
	}
	if errorTypenames[head.Typename] || strings.HasSuffix(head.Typename, "Error") {
		return &UnionError{Op: op, Typename: head.Typename, Message: head.Message}
	}
	return nil
}

// NotFoundError is a typed "no such object" answer for folders, spaces,
// playlists and users, so commands can map it to exit code 3 with a message
// that names the kind of object.
type NotFoundError struct {
	Kind string
	ID   string
	Msg  string
}

func (e *NotFoundError) Error() string {
	msg := fmt.Sprintf("%s %q not found", e.Kind, e.ID)
	if e.Msg != "" {
		msg += " (" + e.Msg + ")"
	}
	return msg
}

// ErrNoMP4 means Loom has no MP4 rendition for a video (only the HLS stream
// the web player uses). Loom creates the MP4 on demand when the owner uses
// Download in the web app.
var ErrNoMP4 = errors.New("Loom has no MP4 rendition for this video yet (only the streaming playlist); open the video on loom.com and use Download once so Loom renders an MP4, then retry")

// ErrNoTranscript means Loom has no transcript for the video (yet).
var ErrNoTranscript = errors.New("no transcript available for this video")

// ErrMutationRejected means a mutation answered with the literal false: Loom
// accepted the request but did not apply the change.
var ErrMutationRejected = errors.New("Loom answered false: the change was not applied")

// CDNError is an HTTP failure from cdn.loom.com for a signed URL. URL is
// already redacted. A 401/403 means the signed link expired or the owner
// disabled downloads; it is not a Loom session problem.
type CDNError struct {
	URL    string
	Status int
}

func (e *CDNError) Error() string {
	switch e.Status {
	case http.StatusForbidden, http.StatusUnauthorized:
		return fmt.Sprintf("CDN refused %s (HTTP %d): the signed link expired or downloads are disabled for this video", e.URL, e.Status)
	case http.StatusNotFound:
		return fmt.Sprintf("CDN has no file at %s (HTTP 404)", e.URL)
	}
	return fmt.Sprintf("fetching %s: CDN answered HTTP %d", e.URL, e.Status)
}

// Refused reports a 401/403 answer (expired link or downloads disabled).
func (e *CDNError) Refused() bool {
	return e.Status == http.StatusForbidden || e.Status == http.StatusUnauthorized
}

// IsRateLimited reports whether err is a CDN or API rate-limit failure.
func IsRateLimited(err error) bool {
	var rl *cliutil.RateLimitError
	return errors.As(err, &rl)
}

// IsRunStopping reports errors no later item of a multi-video run (export,
// download, hydrate, bulk rename, activity) can get past: an expired session
// or rate limiting. A deadline or cancel is not one by itself: a per-item
// deadline fails only that item, and callers stop on their own run
// context's Err().
func IsRunStopping(err error) bool {
	return errors.Is(err, ErrSessionExpired) || IsRateLimited(err)
}
