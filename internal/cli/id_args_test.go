package cli

import (
	"strings"
	"testing"
)

func TestParseIDLines(t *testing.T) {
	got, err := parseIDLines(strings.NewReader("# comment\n\n8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60\n  https://www.loom.com/share/x-8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f61  \n"))
	if err != nil || len(got) != 2 || !strings.HasPrefix(got[1], "https://") {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestIDArgs(t *testing.T) {
	if _, err := folderIDArg("bad id!"); err == nil {
		t.Error("folder: want error")
	}
	for in, want := range map[string]string{
		"https://www.loom.com/looms/folders/weekly-demos-8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60": "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60",
		"https://www.loom.com/folders/8F3C1A9E2B7D4C6F9A0E1D2C3B4A5F60?x=1":                "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60",
		"folder_Opaque-1": "folder_Opaque-1",
	} {
		if id, err := folderIDArg(in); err != nil || id != want {
			t.Errorf("folderIDArg(%q) = %q, %v; want %q", in, id, err, want)
		}
	}
	if id, err := spaceIDArg("https://www.loom.com/spaces/team-1111aaaa-2222-4bbb-8ccc-3333dddd4444"); err != nil || id != "1111aaaa-2222-4bbb-8ccc-3333dddd4444" {
		t.Errorf("space URL: %q %v", id, err)
	}
	if _, err := folderIDArg(strings.Repeat("a", 129)); err == nil {
		t.Error("folder: 129 characters should be rejected")
	}
	if id, err := folderIDArg("8F3C1A9E2B7D4C6F9A0E1D2C3B4A5F60"); err != nil || id != "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60" {
		t.Errorf("folder: %q %v", id, err)
	}
	if _, err := spaceIDArg("1111aaaa-2222-4bbb-8ccc-3333dddd4444"); err != nil {
		t.Errorf("space: %v", err)
	}
	if _, err := spaceIDArg("../nope"); err == nil {
		t.Error("space: want error")
	}
	if _, err := userIDArg("12a"); err == nil {
		t.Error("user: want error")
	}
	if _, err := playlistIDArg("bad id!"); err == nil {
		t.Error("playlist: want error")
	}
	ids, err := expandVideoArgs([]string{"latest", "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60,https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f61"})
	if err != nil || len(ids) != 3 || ids[0] != "latest" || ids[2] != "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f61" {
		t.Errorf("video args: %v, %v", ids, err)
	}
	if _, err := expandVideoArgs([]string{"nope"}); err == nil || ExitCode(err) != 2 {
		t.Errorf("video args: want usage error, got %v", err)
	}
}

func TestOpaqueIDArg(t *testing.T) {
	good := []string{"51234", "1111aaaa2222bbbb3333cccc4444dddd", "abc_DEF-1"}
	bad := []string{"", "  ", "not an id", "a/b", "id;drop"}
	for _, s := range good {
		if _, err := opaqueIDArg("comment", s); err != nil {
			t.Errorf("opaqueIDArg(%q) unexpected error %v", s, err)
		}
	}
	for _, s := range bad {
		if _, err := opaqueIDArg("comment", s); err == nil {
			t.Errorf("opaqueIDArg(%q) expected error", s)
		}
	}
	ids, err := opaqueIDList("folder", []string{"a,b", "b c"})
	if err != nil || len(ids) != 3 {
		t.Fatalf("opaqueIDList = %v, %v", ids, err)
	}
}

func TestCheckVideoArg(t *testing.T) {
	for _, s := range []string{"latest", "LATEST", "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60", "https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"} {
		if err := checkVideoArg(s); err != nil {
			t.Errorf("checkVideoArg(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"nope", "12345", "https://example.com/share/x"} {
		if err := checkVideoArg(s); err == nil {
			t.Errorf("checkVideoArg(%q) expected error", s)
		}
	}
}
