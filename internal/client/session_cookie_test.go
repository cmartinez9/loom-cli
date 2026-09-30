package client

import "testing"

func TestNormalizeSessionCookie(t *testing.T) {
	for in, want := range map[string]string{
		"s%3Aabc.def":            "connect.sid=s%3Aabc.def",
		"  s:abc.def  ":          "connect.sid=s:abc.def",
		"connect.sid=s%3Aabc":    "connect.sid=s%3Aabc",
		"other=1; connect.sid=x": "other=1; connect.sid=x",
		"eyJhbGciOi.payload.sig": "eyJhbGciOi.payload.sig",
		"":                       "",
	} {
		if got := normalizeSessionCookie(in); got != want {
			t.Errorf("normalizeSessionCookie(%q) = %q, want %q", in, got, want)
		}
	}
}
