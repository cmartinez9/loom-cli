// Copyright 2026 cmartinez9 and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/store"
)

func TestGrepFTSQuery(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		any     bool
		want    string
		wantErr bool
	}{
		{"single word", "funnel", false, `"funnel"`, false},
		{"phrase", "pricing experiment", false, `"pricing experiment"`, false},
		{"any words", "pricing experiment", true, `"pricing" OR "experiment"`, false},
		{"any dedupes case-insensitively", "Pricing pricing", true, `"Pricing"`, false},
		{"operators become words", "cats AND dogs OR NEAR", false, `"cats AND dogs OR NEAR"`, false},
		{"quotes stripped", `say "hello"`, false, `"say hello"`, false},
		{"star minus colon caret", "pre* -not col:val ^start", false, `"pre not col val start"`, false},
		{"parens", "NEAR(a b)", true, `"NEAR" OR "a" OR "b"`, false},
		{"unicode letters", "café naïve", false, `"café naïve"`, false},
		{"digits", "4th of July", false, `"4th of July"`, false},
		{"only punctuation", `"*" - :`, false, "", true},
		{"empty", "   ", false, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := grepFTSQuery(tc.in, tc.any)
			if (err != nil) != tc.wantErr {
				t.Fatalf("grepFTSQuery(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("grepFTSQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGrepJoinContext(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"one"}, "one"},
		{[]string{" a ", "", "b"}, "a b"},
	}
	for _, tc := range cases {
		if got := grepJoinContext(tc.in); got != tc.want {
			t.Fatalf("grepJoinContext(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestGrepSearchStore runs escaped queries against a real FTS5 index: hostile
// input must never raise an FTS syntax error, and matches are stemmed and
// case-insensitive.
func TestGrepSearchStore(t *testing.T) {
	ctx := context.Background()
	db, _ := newTestStore(t)
	idA := "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"
	idB := "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f61"
	insertTestVideo(t, db, idA, "Pricing review", "2026-09-01T10:00:00.000Z", 120)
	insertTestVideo(t, db, idB, "Old demo", "2020-01-01T10:00:00.000Z", 60)
	if err := db.SaveTranscript(ctx, store.TranscriptRow{VideoID: idA, Status: "success"}, []store.PhraseRow{
		{Start: 0, Text: "Welcome to the review."},
		{Start: 12.5, Text: "Our pricing experiment doubled signups."},
		{Start: 75, Text: "Next we look at the FUNNEL and AND OR NEAR words."},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveTranscript(ctx, store.TranscriptRow{VideoID: idB, Status: "success"}, []store.PhraseRow{
		{Start: 3, Text: "The funnel was priced differently back then."},
	}); err != nil {
		t.Fatal(err)
	}
	n, err := db.HydratedTranscriptCount(ctx)
	if err != nil || n != 2 {
		t.Fatalf("HydratedTranscriptCount = %d, %v; want 2", n, err)
	}
	cases := []struct {
		name     string
		in       string
		any      bool
		video    string
		since    string
		wantHits []string // video ids in rank order (set when order is deterministic)
		wantN    int
	}{
		{"phrase", "pricing experiment", false, "", "", []string{idA}, 1},
		{"case insensitive", "FUNNEL", false, "", "", nil, 2},
		{"stemmed", "prices", false, "", "", nil, 2},
		{"video filter", "funnel", false, idB, "", []string{idB}, 1},
		{"since filter", "funnel", false, "", "2026-01-01T00:00:00Z", []string{idA}, 1},
		{"operators literal", "AND OR NEAR", false, "", "", []string{idA}, 1},
		{"hostile input", `"a" AND ( b* -c NEAR/3 :d ^e`, false, "", "", nil, 0},
		{"any words", "signups welcome", true, "", "", nil, 2},
		{"no match", "zzzzqqqxx", false, "", "", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			match, err := grepFTSQuery(tc.in, tc.any)
			if err != nil {
				t.Fatalf("grepFTSQuery: %v", err)
			}
			hits, err := db.SearchPhrases(ctx, store.PhraseSearch{Match: match, VideoID: tc.video, Since: tc.since, Limit: 20})
			if err != nil {
				t.Fatalf("SearchPhrases(%q) error: %v", match, err)
			}
			if len(hits) != tc.wantN {
				t.Fatalf("SearchPhrases(%q) = %d hits, want %d: %+v", match, len(hits), tc.wantN, hits)
			}
			for i, id := range tc.wantHits {
				if hits[i].VideoID != id {
					t.Fatalf("hit %d video = %s, want %s", i, hits[i].VideoID, id)
				}
			}
		})
	}
	window, err := db.PhraseWindow(ctx, idA, 1, 1, 1)
	if err != nil || len(window) != 3 {
		t.Fatalf("PhraseWindow = %v, %v; want 3 phrases", window, err)
	}
}

// TestGrepCommandJSON drives the command end to end against a temp store.
func TestGrepCommandJSON(t *testing.T) {
	ctx := context.Background()
	db, path := newTestStore(t)
	id := "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"
	insertTestVideo(t, db, id, "Pricing review", "2026-09-01T10:00:00.000Z", 120)
	if err := db.SaveTranscript(ctx, store.TranscriptRow{VideoID: id, Status: "success"}, []store.PhraseRow{
		{Start: 0, Text: "Hello."},
		{Start: 3725, Text: "The funnel numbers."},
		{Start: 3730, Text: "Goodbye."},
	}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	out, _, err := runCLI(t, "grep", "funnel", "--db", path, "--json")
	if err != nil {
		t.Fatalf("grep error: %v", err)
	}
	var rows []grepRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("grep output is not JSON: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Timestamp != "1:02:05" || r.Link != "https://www.loom.com/share/"+id+"?t=3725" || r.VideoName != "Pricing review" {
		t.Fatalf("unexpected row: %+v", r)
	}
	if r.Context != "Hello. The funnel numbers. Goodbye." {
		t.Fatalf("context = %q", r.Context)
	}

	out, _, err = runCLI(t, "grep", "zzzzqqqxx", "--db", path, "--json")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("no-match output = %q, %v; want []", out, err)
	}
}

func TestGrepCommandErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
	}{
		{"json without phrase", []string{"grep", "--json"}, 2},
		{"bad since", []string{"grep", "the", "--since", "soon", "--json"}, 2},
		{"bad limit", []string{"grep", "the", "--limit", "0", "--json"}, 2},
		{"bad video", []string{"grep", "the", "--video", "nope", "--json"}, 2},
		{"punctuation only", []string{"grep", "***", "--json"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runCLI(t, tc.args...)
			var ce *cliError
			if !errors.As(err, &ce) || ce.code != tc.code {
				t.Fatalf("err = %v, want exit %d", err, tc.code)
			}
		})
	}
}

func TestGrepMissingMirrorIsEmpty(t *testing.T) {
	out, errOut, err := runCLI(t, "grep", "the", "--db", t.TempDir()+"/missing.db", "--json")
	if err != nil || strings.TrimSpace(out) != "[]" || !strings.Contains(errOut, "sync") {
		t.Fatalf("missing mirror: out=%q err=%v stderr=%q", out, err, errOut)
	}
}

func TestGrepSnippetCentresOnMatch(t *testing.T) {
	text := strings.Repeat("lead in words ", 10) + "the pricing experiment ran for two weeks " + strings.Repeat("and then more words ", 10)
	got := grepSnippet(text, []string{"pricing"}, 40)
	if !strings.Contains(got, "pricing") {
		t.Fatalf("snippet %q does not show the match", got)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("snippet %q should be cut on both sides", got)
	}
	if n := len([]rune(got)); n > 40 {
		t.Fatalf("snippet is %d runes, want <= 40", n)
	}
	// Stemmed match: "priced" found via the term's prefix.
	if got := grepSnippet(strings.Repeat("x ", 50)+"we priced it "+strings.Repeat("y ", 50), []string{"pricing"}, 30); !strings.Contains(got, "priced") {
		t.Fatalf("stemmed snippet %q misses the match", got)
	}
	if got := grepSnippet("short text", []string{"short"}, 40); got != "short text" {
		t.Fatalf("short text changed: %q", got)
	}
}

func TestTermSafe(t *testing.T) {
	in := "Demo\x1b[31m red\x07 ‮evil\ttab\nline\x00"
	got := termSafe(in)
	if strings.ContainsAny(got, "\x1b\x07\x00‮") {
		t.Fatalf("termSafe left control characters: %q", got)
	}
	if !strings.Contains(got, "\ttab\nline") || !strings.Contains(got, "[31m red") {
		t.Fatalf("termSafe removed too much: %q", got)
	}
}

// TestSearchHumanOutputIsTermSafe: search prints synced rows (untrusted
// Loom text) in human mode through the sanitizing writer.
func TestSearchHumanOutputIsTermSafe(t *testing.T) {
	prev := humanFriendly
	humanFriendly = true
	defer func() { humanFriendly = prev }()
	cmd := &cobra.Command{}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	// A raw bidi override is valid inside a JSON string.
	row := json.RawMessage("{\"id\":\"1\",\"name\":\"evil\u202ered\"}")
	if err := outputSearchResults(cmd, &rootFlags{}, []json.RawMessage{row}, 10, DataProvenance{Source: "local"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); strings.ContainsRune(got, '\u202e') || !strings.Contains(got, "evilred") {
		t.Fatalf("human search output not sanitized: %q", got)
	}
}
