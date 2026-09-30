package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func TestNotFoundHintNamesTheObject(t *testing.T) {
	cases := []struct {
		err  error
		want string
		not  string
	}{
		{&loom.UnionError{Op: loom.OpFolder, Typename: "NotFoundError"}, "folder does not exist", "video"},
		{&loom.NotFoundError{Kind: "space", ID: "x"}, "space does not exist", "video does not exist"},
		{&loom.NotFoundError{Kind: "playlist", ID: "x"}, "playlists list", "video does not exist"},
		{&loom.UnionError{Op: loom.OpTags, Typename: "PrivateVideo"}, "video does not exist", "folder"},
	}
	for _, tc := range cases {
		err := loomErr(tc.err)
		var ce *cliError
		if !errors.As(err, &ce) || ce.code != 3 || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), tc.not) {
			t.Errorf("loomErr(%v) = %v, want exit 3 mentioning %q and not %q", tc.err, err, tc.want, tc.not)
		}
	}
	err := loomErr(&loom.CDNError{URL: "https://cdn.loom.com/x.mp4?<signed>", Status: 403})
	var ce *cliError
	if !errors.As(err, &ce) || ce.code != 5 || !strings.Contains(err.Error(), "downloads") {
		t.Errorf("CDN 403 = %v, want exit 5 with a signed-link hint", err)
	}
	if err := loomErr(fmt.Errorf("op: %w", loom.ErrMutationRejected)); !errors.As(err, &ce) || ce.code != 5 {
		t.Errorf("rejected mutation = %v, want exit 5", err)
	}
}

func TestTermSafeWriter(t *testing.T) {
	var b strings.Builder
	w := newTermSafeWriter(&b)
	in := []byte("Demo\x1b[31m red\x07 ‮evil\ttab\nline\x00 café \xc2\x9b8bit \xff")
	// Write one byte at a time so multi-byte characters split across writes.
	for i := range in {
		if n, err := w.Write(in[i : i+1]); n != 1 || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if strings.ContainsAny(got, "\x1b\x07\x00‮\u009b") {
		t.Fatalf("control characters survived: %q", got)
	}
	for _, keep := range []string{"Demo[31m red", "evil\ttab\nline", "café", "8bit �"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("output %q lost %q", got, keep)
		}
	}
	// A character cut off at the end becomes U+FFFD on Flush.
	b.Reset()
	w = newTermSafeWriter(&b)
	_, _ = w.Write([]byte("x\xc3"))
	_ = w.Flush()
	if b.String() != "x�" {
		t.Fatalf("truncated tail = %q", b.String())
	}
}

func TestEmitHumanOutputIsTerminalSafe(t *testing.T) {
	cmd := &cobra.Command{}
	var out strings.Builder
	cmd.SetOut(&out)
	err := writeHuman(cmd, func(w io.Writer) error {
		_, err := io.WriteString(w, "name\x1b]0;pwned\x07 ok\n")
		return err
	})
	if err != nil || out.String() != "name]0;pwned ok\n" {
		t.Fatalf("writeHuman = %q, %v", out.String(), err)
	}
}
