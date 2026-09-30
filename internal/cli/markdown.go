package cli

// Markdown rendering shared by chapters --format md, digest and videos
// export, so chapter lines, action items, bullets and escaping look the same
// everywhere. Every inline Loom field (names, titles, owners, folders, tags,
// action items, takeaways, comments, transcript text) goes through mdInline.

import (
	"fmt"
	"regexp"
	"strings"

	"loom-pp-cli/internal/loom"
)

// mdEscaper escapes the characters that would turn untrusted text (video
// names, chapter titles) into Markdown syntax inside a line: emphasis, code
// spans, links and raw HTML.
var mdEscaper = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `*`, `\*`, `_`, `\_`, "`", "\\`", `<`, `\<`, `>`, `\>`)

// mdBlockStart matches text that Markdown would read as a heading, list
// item, ordered-list item or thematic break when it starts a line or a
// list item.
var mdBlockStart = regexp.MustCompile(`^(#|[-+](\s|$)|-{3,}|\d+[.)])`)

// mdEscape escapes untrusted text for use inside a Markdown line or link text.
func mdEscape(s string) string { return mdEscaper.Replace(s) }

// mdInline makes untrusted text safe as inline Markdown: whitespace
// (including newlines, which would end a list item) collapses to single
// spaces, Markdown syntax is escaped, and a leading heading or list marker
// is escaped so it stays literal text.
func mdInline(s string) string {
	s = mdEscape(strings.Join(strings.Fields(s), " "))
	if loc := mdBlockStart.FindStringIndex(s); loc != nil {
		// Escape the marker's last punctuation character ("#", "-", "+",
		// "." or ")").
		i := loc[1] - 1
		for i > 0 && (s[i] == ' ' || s[i] == '\t') {
			i--
		}
		s = s[:i] + `\` + s[i:]
	}
	return s
}

// mdBullet renders untrusted text as one Markdown bullet.
func mdBullet(text string) string { return "- " + mdInline(text) + "\n" }

// mdChapterLine renders one chapter as a Markdown bullet whose timestamp
// links to the moment it starts (?t=<seconds>).
func mdChapterLine(timestamp, link, title string) string {
	return fmt.Sprintf("- [%s](%s) %s\n", timestamp, link, mdInline(title))
}

// mdTaskLine renders one action item as a Markdown task-list line: a
// checkbox (checked when resolved), the text, the owner when Loom named
// one, and a link to the moment it was mentioned when it has a timestamp.
func mdTaskLine(t loom.Task) string {
	check := " "
	if t.ResolvedAt != "" {
		check = "x"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- [%s] %s", check, mdInline(t.Content))
	if owner := strings.TrimSpace(t.Owner); owner != "" {
		b.WriteString(" — " + mdInline(owner))
	}
	if t.Start > 0 && t.Link != "" {
		fmt.Fprintf(&b, " ([%s](%s))", t.Timestamp, t.Link)
	}
	b.WriteString("\n")
	return b.String()
}
