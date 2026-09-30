package cli

// Small formatters shared by the human (table and prose) output of sibling
// commands. JSON output never goes through them. Long text is shortened
// with truncate.

import (
	"fmt"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

// formatDate renders a Loom timestamp as a local YYYY-MM-DD date, or the
// input unchanged when it does not parse.
func formatDate(s string) string {
	if t, ok := loom.ParseTime(s); ok {
		return t.Local().Format("2006-01-02")
	}
	return s
}

// countNoun renders "1 video" / "2 videos".
func countNoun(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// yesNo renders a boolean as yes or no.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
