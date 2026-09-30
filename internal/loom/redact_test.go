package loom

import (
	"errors"
	"strings"
	"testing"
)

func TestRedactErr(t *testing.T) {
	u := "https://cdn.loom.com/a.mp4?Policy=P1&Signature=S1&Key-Pair-Id=K1"
	err := errors.New(`Get "https://cdn.loom.com/a.mp4?Policy=P1&amp;Signature=S1&amp;Key-Pair-Id=K1": EOF`)
	got := RedactErr(err, u)
	for _, leak := range []string{"Signature=", "Policy=", "Key-Pair-Id="} {
		if strings.Contains(got, leak) {
			t.Fatalf("RedactErr leaked %q: %s", leak, got)
		}
	}
}

func TestRedactErrWithoutURL(t *testing.T) {
	url := "https://cdn.loom.com/sessions/transcoded/x.mp4?Policy=AAA&Key-Pair-Id=BBB&Signature=CCC"
	msg := RedactErr(errors.New("Get \""+url+"\": EOF"), url)
	if strings.Contains(msg, "AAA") || strings.Contains(msg, "CCC") {
		t.Fatalf("leaked: %s", msg)
	}
	msg = RedactErr(errors.New("fetch https://cdn.loom.com/y.mp4?Policy=AAA&Signature=CCC failed"), "")
	if strings.Contains(msg, "AAA") || strings.Contains(msg, "CCC") || !strings.Contains(msg, "failed") {
		t.Fatalf("belt-and-braces redaction failed: %s", msg)
	}
}
