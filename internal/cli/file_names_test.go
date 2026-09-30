package cli

import (
	"strings"
	"testing"
)

func TestFileSlug(t *testing.T) {
	cases := map[string]string{
		"Weekly Demo MVP [Sprint 2026]": "weekly-demo-mvp-sprint-2026",
		"  Ünïcode — only!! ":           "n-code-only",
		"":                              "video",
		"🎉🎉":                            "video",
	}
	for in, want := range cases {
		if got := fileSlug(in, 60); got != want {
			t.Errorf("fileSlug(%q) = %q, want %q", in, got, want)
		}
	}
	if got := fileSlug(strings.Repeat("abc ", 40), 20); len(got) > 20 || strings.HasSuffix(got, "-") {
		t.Errorf("long slug = %q", got)
	}
}

func TestVideoFileBase(t *testing.T) {
	v := testVideo("8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60", "Weekly Demo", "2026-01-15T10:00:00.000Z")
	if got, err := videoFileBase(v); err != nil || got != "2026-01-15-weekly-demo-8f3c1a9e" {
		t.Fatalf("got %q, %v", got, err)
	}
	v.CreatedAt = ""
	if got, err := videoFileBase(v); err != nil || !strings.HasPrefix(got, "undated-") {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, bad := range []string{"../../etc/passwd", "8F3C1A9E2B7D4C6F9A0E1D2C3B4A5F60", "8f3c1a9e", "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f6/", ""} {
		v.ID = bad
		if got, err := videoFileBase(v); err == nil {
			t.Errorf("videoFileBase(id %q) = %q, want an error", bad, got)
		}
	}
}
