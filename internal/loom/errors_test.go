package loom

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"loom-pp-cli/internal/cliutil"
)

func TestFolderLookupErr(t *testing.T) {
	var nf *NotFoundError
	if err := folderLookupErr("f1", &GraphQLError{Op: OpFolder, Messages: []string{"Folder Not Found"}}); !errors.As(err, &nf) || nf.Kind != "folder" || nf.ID != "f1" {
		t.Fatalf("GraphQL not found = %v, want a folder NotFoundError", err)
	}
	if err := folderLookupErr("f1", &UnionError{Op: OpFolder, Typename: "NotFoundError"}); !errors.As(err, &nf) {
		t.Fatalf("union not found = %v, want a folder NotFoundError", err)
	}
	for _, other := range []error{errors.New("Folder Not Found"), ErrSessionExpired} {
		if err := folderLookupErr("f1", other); err != other {
			t.Fatalf("%v should pass through, got %v", other, err)
		}
	}
}

func TestCDNErrorStatus(t *testing.T) {
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer gone.Close()
	l := New(nil)
	_, err := l.FetchCDN(context.Background(), gone.URL+"/t.json?Signature=SECRET")
	var ce *CDNError
	if !errors.As(err, &ce) || !ce.Refused() || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v, want a refused *CDNError without the signature", err)
	}
}

func TestIsRunStopping(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&cliutil.RateLimitError{URL: "/graphql"}, true},
		{fmt.Errorf("wrapped: %w", &cliutil.RateLimitError{}), true},
		{ErrSessionExpired, true},
		{context.DeadlineExceeded, false},
		{fmt.Errorf("stop: %w", context.Canceled), false},
		{&cliutil.RateLimitError{URL: "/graphql", Cause: context.DeadlineExceeded}, true},
		{ErrNoTranscript, false},
		{errors.New("HTTP 500"), false},
	}
	for _, tc := range cases {
		if got := IsRunStopping(tc.err); got != tc.want {
			t.Errorf("IsRunStopping(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
