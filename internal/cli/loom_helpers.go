package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/cliutil"
	"loom-pp-cli/internal/loom"
)

// newLoomClient builds the Loom GraphQL client on the generated transport
// (cookie jar, required headers, rate limiting, retries, --dry-run).
func newLoomClient(flags *rootFlags) (*loom.Client, error) {
	c, err := flags.newClient()
	if err != nil {
		return nil, err
	}
	return loom.New(c), nil
}

// loomErr maps Loom errors onto the CLI's typed exit codes:
// session expired -> 4, rate limited -> 7, private/unknown video -> 3,
// other GraphQL failures -> 5.
func loomErr(err error) error {
	if err == nil {
		return nil
	}
	var typed *cliError
	if errors.As(err, &typed) {
		return err
	}
	if errors.Is(err, loom.ErrSessionExpired) {
		return authErr(err)
	}
	var rl *cliutil.RateLimitError
	if errors.As(err, &rl) {
		return rateLimitErr(err)
	}
	var cdn *loom.CDNError
	if errors.As(err, &cdn) {
		// A signed-link refusal is not a Loom session problem (exit 5).
		if cdn.Refused() {
			return apiErr(fmt.Errorf("%w\nhint: signed CDN links are short-lived; re-run to get a fresh one. If it keeps failing, the owner disabled downloads for this video", err))
		}
		return apiErr(err)
	}
	var nf *loom.NotFoundError
	if errors.As(err, &nf) {
		return notFoundErr(fmt.Errorf("%w\nhint: %s", err, notFoundHint(nf.Kind)))
	}
	var ue *loom.UnionError
	if errors.As(err, &ue) {
		switch ue.Typename {
		case "PrivateVideo", "UserNotAuthorizedError", "NotFoundError":
			return notFoundErr(fmt.Errorf("%w\nhint: %s", err, notFoundHint(loom.ObjectKind(ue.Op))))
		case "VideoPasswordMissingOrIncorrect":
			return authErr(fmt.Errorf("%w\nhint: this video is password protected", err))
		}
		return apiErr(err)
	}
	if errors.Is(err, loom.ErrNoTranscript) {
		return notFoundErr(err)
	}
	if errors.Is(err, loom.ErrMutationRejected) {
		return apiErr(err)
	}
	var ge *loom.GraphQLError
	if errors.As(err, &ge) {
		return apiErr(fmt.Errorf("%w\nhint: Loom's internal API may have changed; run 'loom-pp-cli doctor' and report the operation name", err))
	}
	return classifyAPIErrorOnly(err)
}

// termSafeRune reports whether r may reach a terminal: newline and tab are
// kept; other control characters (C0, DEL, C1) and bidi overrides are not.
func termSafeRune(r rune) bool {
	if r == '\n' || r == '\t' {
		return true
	}
	return !unicode.IsControl(r) && !unicode.Is(unicode.Bidi_Control, r)
}

// termSafe strips control characters (except newline and tab) and bidi
// overrides from s. Human output goes through termSafeWriter, which applies
// the same rule to the whole stream.
func termSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if termSafeRune(r) {
			return r
		}
		return -1
	}, s)
}

// termSafeWriter strips control and bidi-override characters (keeping
// newline and tab) from everything written through it, so untrusted text
// (video names, comments, transcript text, author names) cannot inject
// terminal escape sequences into human output. Invalid UTF-8 bytes (which
// include raw 8-bit C1 controls) become U+FFFD. A multi-byte character split
// across writes is held until it completes; Flush emits any incomplete tail.
type termSafeWriter struct {
	w       io.Writer
	pending []byte
}

func newTermSafeWriter(w io.Writer) *termSafeWriter { return &termSafeWriter{w: w} }

func (t *termSafeWriter) Write(p []byte) (int, error) {
	buf := p
	if len(t.pending) > 0 {
		buf = append(t.pending, p...)
		t.pending = nil
	}
	out := make([]byte, 0, len(buf))
	for i := 0; i < len(buf); {
		r, size := utf8.DecodeRune(buf[i:])
		if r == utf8.RuneError && size <= 1 {
			if !utf8.FullRune(buf[i:]) {
				t.pending = append([]byte(nil), buf[i:]...)
				break
			}
			out = append(out, "\uFFFD"...)
			i++
			continue
		}
		if termSafeRune(r) {
			out = append(out, buf[i:i+size]...)
		}
		i += size
	}
	if len(out) > 0 {
		if _, err := t.w.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush writes a trailing incomplete character as U+FFFD.
func (t *termSafeWriter) Flush() error {
	if len(t.pending) == 0 {
		return nil
	}
	t.pending = nil
	_, err := io.WriteString(t.w, "\uFFFD")
	return err
}

// writeHuman runs write against stdout wrapped in a termSafeWriter. Every
// hand-written command's human output goes through it (emit does this for
// the callback it is given); JSON output never does.
func writeHuman(cmd *cobra.Command, write func(w io.Writer) error) error {
	sw := newTermSafeWriter(cmd.OutOrStdout())
	err := write(sw)
	if ferr := sw.Flush(); err == nil {
		err = ferr
	}
	return err
}

// notFoundHint explains a not-found answer for one kind of Loom object.
func notFoundHint(kind string) string {
	if kind == "" {
		kind = "object"
	}
	hint := fmt.Sprintf("the %s does not exist or your Loom account cannot open it", kind)
	switch kind {
	case "folder", "space", "playlist":
		hint += fmt.Sprintf("; run 'loom-pp-cli %ss list' to see yours", kind)
	case "video":
		hint += "; check the id or share URL ('latest' picks your newest recording)"
	}
	return hint
}

// emit prints v as JSON for machine output (--json, --agent, piped, --select,
// --csv...) and otherwise calls human to render prose or a table through a
// termSafeWriter, so untrusted text cannot reach the terminal as control
// sequences. JSON output is unchanged.
func emit(cmd *cobra.Command, flags *rootFlags, v any, human func(w io.Writer) error) error {
	if !wantsHumanTable(cmd.OutOrStdout(), flags) || human == nil {
		return printJSONFiltered(cmd.OutOrStdout(), v, flags)
	}
	return writeHuman(cmd, human)
}

// resolveVideoArg turns a positional into a video id: a 32-hex id, a
// loom.com share/embed URL, or "latest". Bad input is a usage error (exit 2).
func resolveVideoArg(ctx context.Context, lc *loom.Client, arg string) (string, error) {
	if isLatestArg(arg) {
		if lc.IsDryRun() {
			return "latest", nil
		}
		page, err := lc.ListVideos(ctx, loom.ListOptions{Limit: 1})
		if err != nil {
			return "", loomErr(err)
		}
		if len(page.Videos) == 0 {
			return "", notFoundErr(fmt.Errorf("your Loom library has no videos yet"))
		}
		return page.Videos[0].ID, nil
	}
	return videoIDArg(arg)
}
