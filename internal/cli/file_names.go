package cli

// File names for exported and downloaded videos.

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

// videoIDHexRe is the only video id shape allowed into a file name.
var videoIDHexRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// fileSlug turns a title into a short lowercase file-name slug.
func fileSlug(name string, max int) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if max > 0 && len(s) > max {
		s = strings.TrimRight(s[:max], "-")
	}
	if s == "" {
		s = "video"
	}
	return s
}

// videoFileBase is "<created-date>-<slug>-<id8>" for export and download
// files. The id comes from Loom's response, so it is checked to be 32 hex
// characters before it becomes part of a path.
func videoFileBase(v loom.Video) (string, error) {
	if !videoIDHexRe.MatchString(v.ID) {
		return "", fmt.Errorf("Loom returned an unexpected video id %q; refusing to build a file name from it", truncate(v.ID, 40))
	}
	date := "undated"
	if t, ok := loom.ParseTime(v.CreatedAt); ok {
		date = t.UTC().Format("2006-01-02")
	}
	return date + "-" + fileSlug(v.Name, 60) + "-" + v.ID[:8], nil
}
