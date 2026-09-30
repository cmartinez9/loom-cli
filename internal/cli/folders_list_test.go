package cli

import (
	"testing"

	"loom-pp-cli/internal/loom"
)

func TestRootFolderFirst(t *testing.T) {
	rows := rootFolderFirst([]loom.Folder{{ID: "a", Name: "A"}, {ID: "r", Name: "My Videos", SpecialID: "org-my-videos"}}, 0)
	if rows[0].ID != "r" {
		t.Fatalf("root not first: %+v", rows)
	}
	if got := rootFolderFirst(rows, 1); len(got) != 1 {
		t.Fatalf("limit ignored: %+v", got)
	}
}
