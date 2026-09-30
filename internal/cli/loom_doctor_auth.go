package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cmartinez9/loom-cli/internal/cliutil"
)

// Loom sessions can also live only in the persisted cookie jar (cookies.json
// under the data dir, written by `auth login --chrome`). The HTTP client sends
// those cookies on every request, so doctor must count them as a configured
// credential instead of reporting LOOM_COOKIE as missing.
func init() {
	doctorAuthConfiguredHook = loomCookieJarSession
}

func loomCookieJarSession() (bool, string) {
	dir, err := cliutil.DataDir()
	if err != nil || dir == "" {
		return false, ""
	}
	data, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "cookies.json")))
	if err != nil {
		return false, ""
	}
	var rows []struct {
		Name    string    `json:"name"`
		Value   string    `json:"value"`
		Domain  string    `json:"domain"`
		Expires time.Time `json:"expires"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return false, ""
	}
	now := time.Now()
	for _, r := range rows {
		if r.Name != "connect.sid" || strings.TrimSpace(r.Value) == "" {
			continue
		}
		if !isLoomCookieDomain(r.Domain) {
			continue
		}
		if !r.Expires.IsZero() && r.Expires.Before(now) {
			continue
		}
		return true, "cookie jar"
	}
	return false, ""
}

// isLoomCookieDomain reports whether a cookie domain is loom.com or one of
// its subdomains (not a look-alike such as evilloom.com).
func isLoomCookieDomain(domain string) bool {
	d := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
	return d == "loom.com" || strings.HasSuffix(d, ".loom.com")
}
