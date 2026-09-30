package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"loom-pp-cli/internal/loom"
)

func TestRenderExportMarkdownAndIdempotentWrite(t *testing.T) {
	id := "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"
	b := &exportBundle{
		Video:       loom.Video{ID: id, Name: "Demo", CreatedAt: "2026-01-15T10:00:00Z", Duration: "1:05", ShareURL: loom.ShareURL(id)},
		Summary:     "A short summary.",
		Chapters:    []loom.Chapter{{Start: 12, Timestamp: "0:12", Title: "Intro", Link: loom.DeepLink(id, 12)}},
		ActionItems: []loom.Task{},
		Takeaways:   []string{},
		Comments:    []loom.Comment{{Author: "Ann", Text: "nice", Timestamp: "0:03", Link: loom.DeepLink(id, 3), Replies: []loom.Comment{{Author: "Bo", Text: "thanks"}}}},
		Transcript:  []loom.Segment{{Start: 0, Text: "Hello"}, {Start: 75, Text: "Bye"}},
	}
	md := renderExportMarkdown(b)
	for _, want := range []string{"# Demo", "## Summary\n\nA short summary.", "- [0:12](" + loom.ShareURL(id) + "?t=12) Intro",
		"_No action items._", "## Transcript", "- [1:15](" + loom.ShareURL(id) + "?t=75) Bye", "  - **Bo**"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n%s", want, md)
		}
	}
	if strings.Contains(md, "Key takeaways") {
		t.Error("empty takeaways section rendered")
	}
	if renderExportMarkdown(b) != md {
		t.Error("render is not deterministic")
	}
	dir := t.TempDir()
	path, status, _, err := writeExportBundle(dir, "md", b, false)
	if err != nil || status != "written" {
		t.Fatalf("first write: %s %v", status, err)
	}
	if _, status, _, _ = writeExportBundle(dir, "md", b, false); status != "skipped" {
		t.Fatalf("second write status = %s", status)
	}
	if _, status, _, _ = writeExportBundle(dir, "md", b, true); status != "written" {
		t.Fatalf("forced write status = %s", status)
	}
	b.Summary = "Changed."
	if _, status, _, _ = writeExportBundle(dir, "md", b, false); status != "written" {
		t.Fatalf("changed write status = %s", status)
	}
	if filepath.Base(path) != "2026-01-15-demo-8f3c1a9e.md" {
		t.Fatalf("path = %s", path)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

// TestMarkdownEscapesLoomFields: every inline Loom field in an export or a
// digest is escaped, so names, owners, comments or transcript text cannot
// inject links, HTML, emphasis, headings or extra list items; action items
// render with one line format in both.
func TestMarkdownEscapesLoomFields(t *testing.T) {
	id := "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"
	task := loom.Task{Content: "Ship [it](http://x)\n- injected", Owner: "*Ann*", Start: 95, Timestamp: "1:35", Link: loom.DeepLink(id, 95)}
	wantTask := "- [ ] Ship \\[it\\](http://x) - injected — \\*Ann\\* ([1:35](" + loom.ShareURL(id) + "?t=95))\n"
	if got := mdTaskLine(task); got != wantTask {
		t.Fatalf("mdTaskLine = %q, want %q", got, wantTask)
	}
	if got := mdTaskLine(loom.Task{Content: "Done", ResolvedAt: "2026-01-01T00:00:00Z"}); got != "- [x] Done\n" {
		t.Fatalf("resolved task = %q", got)
	}
	for in, want := range map[string]string{
		"# heading":    "- \\# heading\n",
		"- nested":     "- \\- nested\n",
		"1. ordered":   "- 1\\. ordered\n",
		"<b>x</b>":     "- \\<b\\>x\\</b\\>\n",
		"a\n\nb":       "- a b\n",
		"plain _text_": "- plain \\_text\\_\n",
	} {
		if got := mdBullet(in); got != want {
			t.Errorf("mdBullet(%q) = %q, want %q", in, got, want)
		}
	}

	b := &exportBundle{
		Video:       loom.Video{ID: id, Name: "Demo", ShareURL: loom.ShareURL(id), OwnerName: "<script>", FolderName: "a_b", Tags: []string{"[x]"}},
		ActionItems: []loom.Task{task},
		Takeaways:   []string{"# big"},
		Comments:    []loom.Comment{{Author: "**Bo**", Text: "see [here](http://evil)\nnext"}},
		Transcript:  []loom.Segment{{Start: 1, Text: "<img src=x>"}},
	}
	md := renderExportMarkdown(b)
	for _, want := range []string{"**Owner:** \\<script\\>", "**Folder:** a\\_b", "**Tags:** \\[x\\]", wantTask, "- \\# big",
		"- **\\*\\*Bo\\*\\*** at", "see \\[here\\](http://evil) next", "\\<img src=x\\>"} {
		if !strings.Contains(md, want) {
			t.Errorf("export markdown missing %q\n%s", want, md)
		}
	}
	for _, bad := range []string{"<script>", ") <img", "[here](http://evil)"} {
		if strings.Contains(md, bad) {
			t.Errorf("export markdown contains unescaped %q", bad)
		}
	}
	res := digestResult{Videos: []digestVideo{{VideoID: id, Name: "Demo", ShareURL: loom.ShareURL(id), Hydrated: true,
		ActionItems: []loom.Task{task}, Takeaways: []string{"<b>bold</b>"}}}}
	dmd := renderDigestMarkdown(res)
	if !strings.Contains(dmd, wantTask) || !strings.Contains(dmd, "- \\<b\\>bold\\</b\\>") {
		t.Errorf("digest markdown does not escape its fields:\n%s", dmd)
	}
}
