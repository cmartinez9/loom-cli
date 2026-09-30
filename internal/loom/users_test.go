package loom

import (
	"errors"
	"testing"
)

func TestParseUserProfile(t *testing.T) {
	cases := []struct {
		name     string
		data     string
		wantErr  bool
		notFound bool
	}{
		{"null user", `{"user":null}`, true, true},
		{"generic error", `{"user":{"__typename":"GenericError"}}`, true, true},
		{"regular", `{"user":{"__typename":"RegularUserPayload","user":{"id":"7","first_name":"AL","last_name":"BO","profile":{"profileUrl":"al-1","profileInfo":{"role":"PM"}}}}}`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParseUserProfile("7", []byte(tc.data))
			if tc.wantErr {
				var nf *NotFoundError
				if err == nil || (tc.notFound && !errors.As(err, &nf)) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Name != "Al Bo" || p.Role != "PM" || p.ProfileURL != "https://www.loom.com/profile/al-1" || p.Kind != "regular" {
				t.Fatalf("profile = %+v", p)
			}
		})
	}
}
