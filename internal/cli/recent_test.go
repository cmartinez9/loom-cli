package cli

import (
	"testing"
	"time"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

func TestWatchedSince(t *testing.T) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	acts := watchedSince([]loom.Activity{{Time: "2026-06-02T00:00:00Z"}, {Time: "2026-05-01T00:00:00Z"}, {Time: "garbage"}}, start)
	if len(acts) != 2 {
		t.Fatalf("activity = %+v", acts)
	}
}
