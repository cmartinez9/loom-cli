package loom

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseRecentVideosDropsTranscript(t *testing.T) {
	data := []byte(`{"recentUserVideos":[{"id":"8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60","owner_id":42,"name":"A","complete":true,
	  "summary":"sum","playable_duration":10,"createdAt":"2026-01-01T00:00:00Z","transcript_phrases":[{"value":"secret words"}],
	  "video_properties":{"recording_type":"screen_cam"},"owner":{"display_name":"BOB"}}]}`)
	rows, err := ParseRecentVideos(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].OwnerID != "42" || rows[0].Owner != "Bob" || !rows[0].Complete || rows[0].Summary != "sum" {
		t.Fatalf("rows = %+v", rows)
	}
	raw, _ := json.Marshal(rows)
	b := string(raw)
	if strings.Contains(b, "secret words") || strings.Contains(b, "transcript") {
		t.Fatalf("transcript leaked: %s", b)
	}
}

func TestParseCalendarMeetings(t *testing.T) {
	data := []byte(`{"me":{"id":"1","calendars":[{"guid":"c1","meetings":[{"guid":"m1","title":"Sync","startsAt":"2026-01-01T10:00:00Z","durationMins":30,
	  "attendeeCount":4,"videoId":null,"organizer":{"first_name":"ANN","last_name":"LEE"}}]},{"guid":"c2","meetings":[]}]}}`)
	rows, err := ParseCalendarMeetings(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].DurationMinutes != 30 || rows[0].Organizer != "Ann Lee" || rows[0].CalendarID != "c1" {
		t.Fatalf("rows = %+v", rows)
	}
	empty, err := ParseCalendarMeetings([]byte(`{"me":{"id":"1","calendars":[]}}`))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %v, %v", empty, err)
	}
	if _, err := ParseCalendarMeetings([]byte(`{"me":null}`)); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v", err)
	}
}
