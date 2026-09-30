package cli

import (
	"strings"

	"errors"
	"testing"

	"github.com/spf13/cobra"
)

// exitCode is the process exit code an error from Execute maps to.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ce *cliError
	if errors.As(err, &ce) {
		return ce.code
	}
	return 1
}

// TestLoomFailureExitCodes pins the exit code of each way Loom reports a
// missing object or a signed-out session, across the commands that read
// one object: 4 for an expired session, 3 for an unknown video or folder.
func TestLoomFailureExitCodes(t *testing.T) {
	const signedIn = `{"data":{"me":{"id":"1","first_name":"Test","last_name":"User"}}}`
	signedOut := func(string) string {
		return `{"errors":[{"message":"UserNotAuthorizedError: User is not logged in","extensions":{"code":"INTERNAL_SERVER_ERROR"}}],"data":null}`
	}
	unionNotFound := func(op string) string {
		switch op {
		case "Me":
			return signedIn
		case "GetVideoCardDetails":
			return `{"data":{"video":{"__typename":"NotFoundError","message":"not found"}}}`
		case "FetchVideoTranscript":
			return `{"data":{"fetchVideoTranscript":{"__typename":"NotFoundError","message":"not found"}}}`
		case "GetFolder":
			return `{"data":{"folder":{"__typename":"NotFoundError","message":"not found"}}}`
		}
		return `{"data":{}}`
	}
	nullVideo := func(op string) string {
		switch op {
		case "Me":
			return signedIn
		case "GetVideoCardDetails":
			return `{"data":{"video":null}}`
		case "FetchVideoTranscript":
			return `{"data":{"fetchVideoTranscript":null}}`
		case "GetFolder":
			return `{"errors":[{"message":"Folder Not Found","extensions":{"code":"NOT_FOUND"}}],"data":null}`
		}
		return `{"data":{}}`
	}
	commands := map[string][]string{
		"videos get":    {"videos", "get", exampleVideoID},
		"transcript":    {"transcript", exampleVideoID, "--data-source", "live"},
		"summary":       {"summary", exampleVideoID, "--data-source", "live"},
		"chapters":      {"chapters", exampleVideoID, "--data-source", "live"},
		"videos export": {"videos", "export", exampleVideoID},
		"folders get":   {"folders", "get", "1111aaaa2222bbbb3333cccc4444dddd"},
	}
	cases := []struct {
		name    string
		respond func(string) string
		want    int
	}{
		{"signed out", signedOut, 4},
		{"union NotFound", unionNotFound, 3},
		{"null object", nullVideo, 3},
	}
	for _, tc := range cases {
		for name, args := range commands {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				out := t.TempDir()
				calls := fakeLoomServer(t, tc.respond)
				argv := append(append([]string{}, args...), "--insecure", "--json")
				if name == "videos export" {
					argv = append(argv, "--out", out)
				}
				_, stderr, err := runCLI(t, argv...)
				if got := exitCode(err); got != tc.want {
					t.Fatalf("exit %d, want %d (err: %v)\nstderr: %s", got, tc.want, err, stderr)
				}
				if calls.Load() == 0 {
					t.Fatal("the command never called the fake Loom server")
				}
			})
		}
	}
}

// TestDryRunFolderWriteSendsNothing: a folder write under --dry-run exits 0
// and never calls Loom, whatever Loom would have answered.
func TestDryRunFolderWriteSendsNothing(t *testing.T) {
	answers := map[string]func(string) string{
		"signed out": func(string) string {
			return `{"errors":[{"message":"User is not logged in","extensions":{"code":"UNAUTHENTICATED"}}],"data":null}`
		},
		"not found": func(string) string { return `{"data":{"folder":null}}` },
	}
	writes := map[string][]string{
		"folders rename": {"folders", "rename", "1111aaaa2222bbbb3333cccc4444dddd", "New name"},
		"folders delete": {"folders", "delete", "1111aaaa2222bbbb3333cccc4444dddd"},
		"folders move":   {"folders", "move", "1111aaaa2222bbbb3333cccc4444dddd", "--to", "5555eeee6666ffff7777aaaa8888bbbb"},
	}
	for an, respond := range answers {
		for wn, args := range writes {
			t.Run(an+"/"+wn, func(t *testing.T) {
				calls := fakeLoomServer(t, respond)
				out, stderr, err := runCLI(t, append(append([]string{}, args...), "--dry-run", "--insecure", "--json")...)
				if exitCode(err) != 0 || !strings.Contains(out, `"dry_run": true`) {
					t.Fatalf("exit %d, out %q, stderr %q", exitCode(err), out, stderr)
				}
				if n := calls.Load(); n != 0 {
					t.Fatalf("dry-run sent %d calls to Loom", n)
				}
			})
		}
	}
}

// TestDataSourcePolicy walks every command that declares a live-only or
// local-only pp:data-source policy and checks that the other source is a
// usage error (exit 2) naming --data-source, before any network IO. Commands
// that take a video get a valid one ("latest"): the per-video reads check
// the policy inside runVideoRead, after reading their argument.
func TestDataSourcePolicy(t *testing.T) {
	root := RootCmd()
	var paths [][]string
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		for _, child := range c.Commands() {
			p := append(append([]string{}, path...), child.Name())
			if child.RunE != nil {
				args := append([]string{}, p...)
				if strings.Contains(child.Annotations["pp:happy-args"], "video=latest") {
					args = append(args, "latest")
				}
				switch commandDataSourceAnnotation(child) {
				case sourceLive:
					paths = append(paths, append(args, "--data-source", "local"))
				case sourceLocal:
					paths = append(paths, append(args, "--data-source", "live"))
				}
			}
			walk(child, p)
		}
	}
	walk(root, nil)
	want := []string{"backlinks", "reactions list", "grep", "digest", "hydrate", "activity", "download", "videos export", "videos get", "recent"}
	seen := map[string]bool{}
	name := func(args []string) string {
		args = args[:len(args)-2]
		if args[len(args)-1] == "latest" {
			args = args[:len(args)-1]
		}
		return strings.Join(args, " ")
	}
	for _, p := range paths {
		seen[name(p)] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("%s declares no live/local-only policy", w)
		}
	}
	for _, args := range paths {
		t.Run(name(args), func(t *testing.T) {
			_, _, err := runCLI(t, args...)
			if exitCode(err) != 2 || !strings.Contains(err.Error(), "--data-source") {
				t.Fatalf("%v: exit %d (%v), want 2 naming --data-source", args, exitCode(err), err)
			}
		})
	}
}
