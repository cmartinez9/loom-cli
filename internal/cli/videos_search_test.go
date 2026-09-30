package cli

import (
	"testing"

	"loom-pp-cli/internal/loom"
)

func TestShapeSearch(t *testing.T) {
	r := loom.NewSearchResults("q")
	r.Videos = append(r.Videos, loom.SearchVideoHit{ID: "1"}, loom.SearchVideoHit{ID: "2"}, loom.SearchVideoHit{ID: "3"})
	r.Tags = append(r.Tags, loom.SearchTagHit{Tag: "t"})
	shapeSearch(r, "videos", 2)
	if len(r.Videos) != 2 || r.Tags == nil || len(r.Tags) != 0 {
		t.Fatalf("videos=%d tags=%v", len(r.Videos), r.Tags)
	}
}
