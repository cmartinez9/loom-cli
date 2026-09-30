package loom

import (
	"errors"
	"fmt"
	neturl "net/url"
	"strings"
)

// Redaction of short-lived CloudFront-signed cdn.loom.com URLs, so they
// never reach error messages, logs or stored rows.

// redactError carries a message with the signed URL removed. Unwrap
// exposes only the underlying cause (e.g. context.DeadlineExceeded), never
// the *url.Error that holds the signed URL.
type redactError struct {
	msg   string
	cause error
}

func (e *redactError) Error() string { return e.msg }
func (e *redactError) Unwrap() error { return e.cause }

// redactedErr wraps a transport error for a signed URL without the URL's
// query string (Policy, Key-Pair-Id, Signature).
func redactedErr(action, url string, err error) error {
	cause := err
	var ue *neturl.Error
	if errors.As(err, &ue) {
		cause = ue.Err
	}
	return &redactError{msg: fmt.Sprintf("%s %s: %s", action, redactSigned(url), RedactErr(err, url)), cause: cause}
}

// RedactErr renders err with any copy of the signed URL, its query string or
// a stray CloudFront signature parameter removed. Use it whenever an error
// that may mention a signed cdn.loom.com URL is printed or stored.
func RedactErr(err error, url string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if url != "" {
		msg = strings.ReplaceAll(msg, url, redactSigned(url))
		if i := strings.IndexByte(url, '?'); i >= 0 && i+1 < len(url) {
			msg = strings.ReplaceAll(msg, url[i+1:], "<signed>")
		}
	}
	// Belt and braces: strip any CloudFront signature that slipped through
	// (for example a URL re-encoded by the transport).
	for _, key := range []string{"Policy=", "Signature=", "Key-Pair-Id="} {
		for {
			i := strings.Index(msg, key)
			if i < 0 {
				break
			}
			start := strings.LastIndexAny(msg[:i], "?&")
			if start < 0 {
				start = i
			}
			end := strings.IndexAny(msg[i:], " \n\"'")
			if end < 0 {
				msg = msg[:start] + "?<signed>"
			} else {
				msg = msg[:start] + "?<signed>" + msg[i+end:]
			}
		}
	}
	return msg
}

// redactSigned drops the CloudFront query string (Policy, Key-Pair-Id,
// Signature) so signed URLs never land in error messages or logs.
func redactSigned(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i] + "?<signed>"
	}
	return u
}

// RedactSignedURL is exported for commands that print download targets.
func RedactSignedURL(u string) string { return redactSigned(u) }
