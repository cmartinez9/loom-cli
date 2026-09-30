package loom

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseGlobalSearch(t *testing.T) {
	data := []byte(`{
	  "videos":{"__typename":"SearchVideosPayload","videoResults":{"nodes":[
	    {"video":{"id":"8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60","name":"Demo","playable_duration":65.5,"createdAt":"2026-01-02T03:04:05Z","owner":{"display_name":"JANE DOE"}},
	     "matchedFields":[{"fieldName":"name"},{"fieldName":"transcriptText"}]}]}},
	  "users":{"__typename":"SearchWorkspaceMembersResult","users":[]},
	  "tags":{"__typename":"MatchedTags","tags":[{"tag":"demo","count":3,"uri":"/t/demo"}]},
	  "folders":{"__typename":"GenericError","message":"boom"},
	  "playlists":{"__typename":"SearchPlaylistsPayload","playlists":[]},
	  "spaces":{"__typename":"SearchSpacesPayload","spaceResults":{"nodes":[]}}
	}`)
	r, err := ParseGlobalSearch("demo", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Videos) != 1 || r.Videos[0].Duration != "1:05" || r.Videos[0].Owner != "Jane Doe" {
		t.Fatalf("videos = %+v", r.Videos)
	}
	if got := strings.Join(r.Videos[0].MatchedFields, ","); got != "name,transcriptText" {
		t.Fatalf("matched = %q", got)
	}
	if r.Videos[0].ShareURL != ShareURL("8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60") {
		t.Fatalf("share url = %q", r.Videos[0].ShareURL)
	}
	if len(r.Tags) != 1 || r.Tags[0].Count != 3 {
		t.Fatalf("tags = %+v", r.Tags)
	}
	if r.Folders == nil || len(r.Folders) != 0 || len(r.Warnings) != 1 {
		t.Fatalf("folders %v warnings %v", r.Folders, r.Warnings)
	}
	if r.Users == nil || r.Spaces == nil || r.Playlists == nil {
		t.Fatal("empty sections must be non-nil")
	}
}

func TestParseGlobalSearchSignedOut(t *testing.T) {
	data := []byte(`{"videos":{"__typename":"UserNotAuthorizedError","message":"no"}}`)
	_, err := ParseGlobalSearch("x", data)
	var ue *UnionError
	if !errors.As(err, &ue) || ue.Typename != "UserNotAuthorizedError" {
		t.Fatalf("err = %v, want a typed UserNotAuthorizedError", err)
	}
	if err := (*Client)(nil).normalizeAuth(context.Background(), err); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("normalized err = %v, want ErrSessionExpired", err)
	}
}
