package cli

import "testing"

func TestIsLoomCookieDomain(t *testing.T) {
	for domain, want := range map[string]bool{
		"loom.com":        true,
		".loom.com":       true,
		"www.loom.com":    true,
		".WWW.Loom.com":   true,
		"evilloom.com":    false,
		"loom.com.evil":   false,
		"notloom.com":     false,
		"":                false,
		"cdn.loom.com.io": false,
	} {
		if got := isLoomCookieDomain(domain); got != want {
			t.Errorf("isLoomCookieDomain(%q) = %v, want %v", domain, got, want)
		}
	}
}
