package loom

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseVideoID(t *testing.T) {
	const id = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{id, id, false},
		{strings.ToUpper(id), id, false},
		{"https://www.loom.com/share/" + id, id, false},
		{"https://www.loom.com/share/" + id + "?sid=abc&t=12", id, false},
		{"loom.com/embed/" + id, id, false},
		{"https://www.loom.com/share/weekly-demo-" + id, id, false},
		{"", "", true},
		{"not-a-video", "", true},
		{"https://example.com/share/" + id, "", true},
		{"8f3c1a9e", "", true},
	}
	for _, c := range cases {
		got, err := ParseVideoID(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseVideoID(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ParseVideoID(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestParsePhrases(t *testing.T) {
	body := []byte(`{"phrases":[
		{"ts":0.011,"value":"Hey friends, demoing the project.","ranges":[{"source":{"monologue":0}}]},
		{"ts":5.468,"value":"  ","ranges":[]},
		{"ts":5.5,"value":"Second phrase here.","ranges":[{"source":{"monologue":1}}]}
	],"schemaVersion":"1"}`)
	segs, err := ParsePhrases(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2 (blank phrase dropped)", len(segs))
	}
	if segs[0].Start != 0.011 || segs[0].End != 5.5 {
		t.Errorf("segment 0 timing = %v-%v, want 0.011-5.5", segs[0].Start, segs[0].End)
	}
	if segs[1].Speaker != 1 {
		t.Errorf("segment 1 speaker = %d, want 1", segs[1].Speaker)
	}
	if segs[1].End <= segs[1].Start {
		t.Errorf("last segment end %v not after start %v", segs[1].End, segs[1].Start)
	}
	if _, err := ParsePhrases([]byte(`not json`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFormatters(t *testing.T) {
	segs := []Segment{{Start: 1.5, End: 3.25, Text: "one"}, {Start: 3725.0, End: 3726.0, Text: "two"}}
	srt := FormatSRT(segs)
	if !strings.Contains(srt, "00:00:01,500 --> 00:00:03,250\none") {
		t.Errorf("SRT timing wrong:\n%s", srt)
	}
	vtt := FormatVTT(segs)
	if !strings.HasPrefix(vtt, "WEBVTT") || !strings.Contains(vtt, "01:02:05.000 --> 01:02:06.000") {
		t.Errorf("VTT wrong:\n%s", vtt)
	}
	ts := FormatTimestamped(segs)
	if !strings.Contains(ts, "[0:01] one") || !strings.Contains(ts, "[1:02:05] two") {
		t.Errorf("timestamped wrong:\n%s", ts)
	}
}

func TestClock(t *testing.T) {
	if got := FormatClock(65.9); got != "1:05" {
		t.Errorf("FormatClock(65.9) = %q", got)
	}
	for in, want := range map[string]float64{"1:05": 65, "01:02:05": 3725, "42": 42} {
		got, err := ParseClock(in)
		if err != nil || got != want {
			t.Errorf("ParseClock(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseClock("1:xx"); err == nil {
		t.Error("ParseClock(1:xx) should fail")
	}
	if got := DeepLink("abc", 47.9); got != "https://www.loom.com/share/abc?t=47" {
		t.Errorf("DeepLink = %q", got)
	}
	if got := DeepLink("abc", 0); got != "https://www.loom.com/share/abc" {
		t.Errorf("DeepLink at 0 = %q", got)
	}
}

func TestParseChapters(t *testing.T) {
	content := "00:00 Intro and agenda\n00:47 Tools setup\nnot a chapter\n1:02:03 - Wrap up"
	ch := ParseChapters("vid", content)
	if len(ch) != 3 {
		t.Fatalf("got %d chapters, want 3: %+v", len(ch), ch)
	}
	if ch[1].Start != 47 || ch[1].Title != "Tools setup" || ch[1].Link != "https://www.loom.com/share/vid?t=47" {
		t.Errorf("chapter 1 = %+v", ch[1])
	}
	if ch[2].Start != 3723 || ch[2].Title != "Wrap up" {
		t.Errorf("chapter 2 = %+v", ch[2])
	}
}

func TestNormalizeVideo(t *testing.T) {
	raw := json.RawMessage(`{"__typename":"RegularUserVideo","id":"abc","name":"Demo","description":"AI text","createdAt":"2026-01-15T10:00:00.000Z",
		"archived":false,"playable_duration":125.5,"totalReactions":2,"totalComments":1,"views":{"total":4,"distinct":3},
		"owner_id":1234567,"owner":{"first_name":"ANN","last_name":"LEE"},"folder":{"id":"f1","name":"My Videos"},
		"download_enabled":true,"privacy":"public","visibility":"owner","tags":null,"current_user_is_owner":true}`)
	v, err := NormalizeVideo(raw)
	if err != nil {
		t.Fatal(err)
	}
	if v.OwnerID != "1234567" || v.OwnerName != "Ann Lee" || v.Views != 4 || v.UniqueViews != 3 {
		t.Errorf("owner/views wrong: %+v", v)
	}
	if v.Duration != "2:05" || v.FolderName != "My Videos" || v.ShareURL != "https://www.loom.com/share/abc" {
		t.Errorf("derived fields wrong: %+v", v)
	}
	if v.Tags == nil {
		t.Error("tags should be [] not nil")
	}
	if _, err := NormalizeVideo(json.RawMessage(`{"name":"no id"}`)); err == nil {
		t.Error("expected error for missing id")
	}
}

func TestCheckUnion(t *testing.T) {
	if err := CheckUnion(OpVideoDetails, json.RawMessage(`{"__typename":"RegularUserVideo","id":"x"}`)); err != nil {
		t.Errorf("success member flagged: %v", err)
	}
	err := CheckUnion(OpVideoDetails, json.RawMessage(`{"__typename":"PrivateVideo","message":"private"}`))
	var ue *UnionError
	if !errors.As(err, &ue) || ue.Typename != "PrivateVideo" {
		t.Errorf("PrivateVideo not flagged: %v", err)
	}
	if err := CheckUnion(OpLibrary, json.RawMessage(`{"__typename":"UserNotAuthorizedError"}`)); !errors.As(err, &ue) || ue.Typename != "UserNotAuthorizedError" {
		t.Errorf("CheckUnion is pure: library UserNotAuthorizedError should be a UnionError, got %v", err)
	}
	if err := CheckUnion(OpTags, json.RawMessage(`{"__typename":"UserNotAuthorizedError"}`)); !errors.As(err, &ue) || ue.Typename != "UserNotAuthorizedError" {
		t.Errorf("per-video UserNotAuthorizedError should stay a UnionError, got %v", err)
	}
}

func TestNormalizeAuthRules(t *testing.T) {
	ctx := context.Background()
	var l *Client // no probe possible: only the pure rules apply
	cases := []struct {
		name    string
		err     error
		expired bool
	}{
		{"unauthenticated code", &GraphQLError{Op: OpTags, Messages: []string{"boom"}, Codes: []string{"UNAUTHENTICATED"}}, true},
		{"signed-out text", &GraphQLError{Op: OpTags, Messages: []string{"UserNotAuthorizedError: User is not logged in"}, Codes: []string{"INTERNAL_SERVER_ERROR"}}, true},
		{"library not authorized", &UnionError{Op: OpLibrary, Typename: "UserNotAuthorizedError"}, true},
		{"search not authorized", &UnionError{Op: OpSearch, Typename: "UserNotAuthorizedError"}, true},
		{"per-video not authorized without probe", &UnionError{Op: OpTags, Typename: "UserNotAuthorizedError"}, false},
		{"validation error", &GraphQLError{Op: OpTags, Messages: []string{"Cannot query field"}, Codes: []string{"GRAPHQL_VALIDATION_FAILED"}}, false},
		{"plain error", errors.New("dial tcp: refused"), false},
	}
	for _, tc := range cases {
		got := l.normalizeAuth(ctx, tc.err)
		if errors.Is(got, ErrSessionExpired) != tc.expired {
			t.Errorf("%s: normalizeAuth = %v, expired want %v", tc.name, got, tc.expired)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	_, err := decode(OpMe, []byte(`{"errors":[{"message":"No logged in viewer found","extensions":{"code":"UNAUTHENTICATED"}}],"data":{"me":null}}`))
	if !errors.Is((*Client)(nil).normalizeAuth(context.Background(), err), ErrSessionExpired) {
		t.Errorf("UNAUTHENTICATED should normalize to ErrSessionExpired, got %v", err)
	}
	_, err = decode(OpMe, []byte(`{"errors":[{"message":"Cannot query field","extensions":{"code":"GRAPHQL_VALIDATION_FAILED"}}]}`))
	var ge *GraphQLError
	if !errors.As(err, &ge) {
		t.Errorf("validation error should be GraphQLError, got %v", err)
	}
	data, err := decode(OpMe, []byte(`{"data":{"me":{"id":"1"}}}`))
	if err != nil || !strings.Contains(string(data), `"id":"1"`) {
		t.Errorf("decode success = %s, %v", data, err)
	}
}

func TestFlattenTakeaways(t *testing.T) {
	cases := map[string]int{
		`["a","b",""]`:                           2,
		`[{"text":"x"},{"content":"y"},{"z":1}]`: 2,
		`"- one\n- two\n"`:                       2,
		`null`:                                   0,
	}
	for in, want := range cases {
		if got := flattenTakeaways(json.RawMessage(in)); len(got) != want {
			t.Errorf("flattenTakeaways(%s) = %v, want %d items", in, got, want)
		}
	}
}

func TestOperationsEmbedded(t *testing.T) {
	entries, err := opsFS.ReadDir("ops")
	if err != nil || len(entries) < 50 {
		t.Fatalf("only %d operations embedded (%v)", len(entries), err)
	}
	for _, op := range []string{OpLibrary, OpVideoDetails, OpTranscript, OpChapters, OpSummary, OpTakeaways, OpComments, OpTasks,
		OpReactions, OpTags, OpBacklinks, OpDownloadURL, OpFolders, OpSpaceMemberships, OpUpdateName, OpArchive, OpMoveVideos,
		OpDeleteVideo, OpRecoverVideo, OpDuplicateVideo, OpPin, OpCreateFolder, OpRenameFolder, OpDeleteFolders, OpShareToSpaces,
		OpCreateComment, OpEditComment, OpDeleteComment, OpCreateTask, OpDeleteTask, OpAddReaction, OpToggleFollow} {
		doc, err := Document(op)
		if err != nil {
			t.Errorf("missing op %s: %v", op, err)
			continue
		}
		if !strings.Contains(doc, " "+op) {
			t.Errorf("op %s document does not declare operation name %s", op, op)
		}
	}
}

func TestTitleName(t *testing.T) {
	for in, want := range map[string]string{"ANN": "Ann", "Mary Ann": "Mary Ann", "": "", "JO ANNE": "Jo Anne", "ÉMILE ÖZIL": "Émile Özil"} {
		if got := TitleName(in); got != want {
			t.Errorf("TitleName(%q) = %q, want %q", in, got, want)
		}
	}
}
