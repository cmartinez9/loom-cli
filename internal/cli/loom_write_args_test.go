package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestParseAtMillis(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"", 0, false},
		{"0:05", 5000, false},
		{"1:23", 83000, false},
		{"1:02:03", 3723000, false},
		{"83", 83000, false},
		{"1.5", 1500, false},
		{"abc", 0, true},
		{"1:xx", 0, true},
		{"-3", 0, true},
	}
	for _, tc := range tests {
		got, err := parseAtMillis(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("parseAtMillis(%q) = %d, %v; want %d, err=%v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestParseOnOff(t *testing.T) {
	tests := []struct {
		in      string
		want    bool
		wantErr bool
	}{
		{"on", true, false}, {"ON", true, false}, {"true", true, false}, {"yes", true, false},
		{"off", false, false}, {"false", false, false}, {"no", false, false},
		{"maybe", false, true}, {"", false, true},
	}
	for _, tc := range tests {
		got, err := parseOnOff("downloads", tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("parseOnOff(%q) = %v, %v", tc.in, got, err)
		}
	}
}

func TestRequireYes(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	if err := requireYes(&rootFlags{}, "delete video x"); err == nil {
		t.Fatal("expected --yes error")
	}
	if err := requireYes(&rootFlags{yes: true}, "delete video x"); err != nil {
		t.Fatal(err)
	}
	if err := requireYes(&rootFlags{dryRun: true}, "delete video x"); err != nil {
		t.Fatal(err)
	}
}

func TestVisibleWritesNeedYes(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	const id = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"
	for _, args := range [][]string{
		{"videos", "settings", id, "--downloads", "off"},
		{"videos", "settings", "latest", "--comments", "on", "--json"},
		{"comments", "add", id, "Looks good", "--json"},
		{"comments", "edit", "latest", "123", "x"},
		{"comments", "react", "123", "👍", "--json"},
		{"reactions", "add", id, "👍", "--at", "0:05"},
		{"tasks", "add", "latest", "Follow up with design", "--json"},
		{"tasks", "edit", "7731", "Send the deck", "--json"},
		{"tasks", "approve", "7731"},
		{"tasks", "done", "7731", "--json"},
		{"tasks", "done", "7731", "--undo"},
		{"comments", "delete", "51234"},
		{"reactions", "delete", "90412", "--json"},
		{"videos", "share", id, "--space", "3333aaaa4444bbbb5555cccc6666dddd"},
	} {
		_, _, err := runCLI(t, args...)
		var ce *cliError
		if !errors.As(err, &ce) || ce.code != 2 || !strings.Contains(err.Error(), "--yes") {
			t.Fatalf("%v: err = %v, want exit 2 asking for --yes", args, err)
		}
		if !strings.Contains(err.Error(), string(confirmVisible)) {
			t.Fatalf("%v: err = %v, want the visibility reason %q", args, err, confirmVisible)
		}
		out, _, err := runCLI(t, append(args, "--dry-run")...)
		if err != nil || !strings.Contains(out, "dry") {
			t.Fatalf("%v --dry-run: out=%q err=%v, want a preview without --yes", args, out, err)
		}
	}
}

// TestOwnerOnlyWritesNeedNoYes: renaming or describing one video changes
// only owner metadata, so it needs no --yes (checked under --dry-run).
func TestOwnerOnlyWritesNeedNoYes(t *testing.T) {
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	for _, args := range [][]string{
		{"videos", "rename", "latest", "New name", "--dry-run", "--json"},
		{"videos", "describe", "latest", "New text", "--dry-run", "--json"},
	} {
		out, _, err := runCLI(t, args...)
		if err != nil || !strings.Contains(out, `"dry_run": true`) {
			t.Fatalf("%v: out=%q err=%v", args, out, err)
		}
	}
}
