package loom

import (
	"encoding/json"
	"testing"
)

func TestFlexID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`{"id":"abc"}`, "abc"},
		{`{"id":123}`, "123"},
		{`{"id":null}`, ""},
		{`{}`, ""},
	}
	for _, c := range cases {
		var v struct {
			ID flexID `json:"id"`
		}
		if err := json.Unmarshal([]byte(c.in), &v); err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if v.ID.String() != c.want {
			t.Errorf("%s: got %q want %q", c.in, v.ID, c.want)
		}
	}
}
