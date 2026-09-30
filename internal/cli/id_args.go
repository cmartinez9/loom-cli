package cli

// Validation of positional and flag ids: videos (id, share URL or latest),
// folders, spaces, playlists, users, comments, tasks and reactions, plus id
// lists and --from-file id files. Everything here is pure (no network IO);
// bad input is a usage error (exit 2).

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/loom"
)

var (
	// opaqueIDRe is the one shape every non-video Loom id must have: an
	// opaque token of letters, digits, '-' or '_'.
	opaqueIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	// containerURLRe pulls the last path segment out of a loom.com folder
	// or space URL (…/folders/<id>, …/looms/folders/<slug>-<id>, …/spaces/<id>).
	containerURLRe = regexp.MustCompile(`(?i)loom\.com/(?:looms/)?(folder|space)s?/([^/?#\s]+)`)
	// slugSuffixIDRe finds a hex id or UUID at the end of a slugged segment.
	slugSuffixIDRe = regexp.MustCompile(`(?i)([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-f]{32})$`)
	// hexishRe matches hex ids and UUIDs, which Loom compares lowercase.
	hexishRe = regexp.MustCompile(`(?i)^[0-9a-f-]+$`)
	userIDRe = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// latestAliases resolve to the newest video in the signed-in user's library,
// so `transcript latest` works without looking up an id first.
var latestAliases = map[string]bool{"latest": true, "last": true, "newest": true}

// isLatestArg reports whether arg is one of the "latest" aliases.
func isLatestArg(arg string) bool {
	return latestAliases[strings.ToLower(strings.TrimSpace(arg))]
}

// splitArgList splits one argument on commas, spaces, tabs and newlines, so
// "a,b c" names two ids.
func splitArgList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
}

// videoIDArg parses a video id or share URL, returning a usage error for
// anything else.
func videoIDArg(arg string) (string, error) {
	id, err := loom.ParseVideoID(arg)
	if err != nil {
		return "", usageErr(err)
	}
	return id, nil
}

// checkVideoArg validates one video argument: "latest" (and its aliases), a
// 32-hex id, or a loom.com share URL.
func checkVideoArg(arg string) error {
	if isLatestArg(arg) {
		return nil
	}
	_, err := videoIDArg(arg)
	return err
}

// requireVideoArg enforces exactly one positional video argument.
func requireVideoArg(cmd *cobra.Command, args []string) (string, error) {
	if len(args) == 0 {
		_ = cmd.Usage()
		return "", usageErr(fmt.Errorf("a video is required: pass a 32-hex video id, a loom.com share URL, or latest"))
	}
	if len(args) > 1 {
		_ = cmd.Usage()
		return "", usageErr(fmt.Errorf("expected one video, got %d arguments", len(args)))
	}
	return args[0], nil
}

// opaqueIDArg validates a Loom id of the given kind (folder, space,
// playlist, comment, task, reaction). Folder and space ids may also be
// given as their loom.com URL. Hex ids and UUIDs are lowercased.
func opaqueIDArg(kind, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", usageErr(fmt.Errorf("%s id is empty", kind))
	}
	if m := containerURLRe.FindStringSubmatch(s); m != nil && strings.EqualFold(m[1], kind) {
		s = m[2]
		if id := slugSuffixIDRe.FindString(s); id != "" {
			s = id
		}
	}
	if !opaqueIDRe.MatchString(s) {
		switch kind {
		case "folder", "space", "playlist":
			return "", usageErr(fmt.Errorf("%q is not a Loom %s id; run 'loom-pp-cli %ss list' to find one", s, kind, kind))
		}
		return "", usageErr(fmt.Errorf("%q is not a valid Loom %s id (letters, digits, '-' or '_')", s, kind))
	}
	if hexishRe.MatchString(s) {
		s = strings.ToLower(s)
	}
	return s, nil
}

// opaqueIDList splits comma/space separated ids of one kind, validates each
// and drops duplicates.
func opaqueIDList(kind string, values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		for _, part := range splitArgList(v) {
			id, err := opaqueIDArg(kind, part)
			if err != nil {
				return nil, err
			}
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out, nil
}

// folderIDArg validates a folder id or loom.com folder URL.
func folderIDArg(arg string) (string, error) { return opaqueIDArg("folder", arg) }

// spaceIDArg validates a space id or loom.com space URL.
func spaceIDArg(arg string) (string, error) { return opaqueIDArg("space", arg) }

// playlistIDArg validates a playlist id.
func playlistIDArg(arg string) (string, error) { return opaqueIDArg("playlist", arg) }

// userIDArg validates a numeric user id.
func userIDArg(arg string) (string, error) {
	id := strings.TrimSpace(arg)
	if userIDRe.MatchString(id) {
		return id, nil
	}
	return "", usageErr(fmt.Errorf("%q is not a Loom user id (digits only)", arg))
}

// readIDFile reads one id or share URL per line (blank lines and # comments
// skipped).
func readIDFile(path string) ([]string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, usageErr(fmt.Errorf("--from-file: %w", err))
	}
	defer f.Close()
	return parseIDLines(f)
}

func parseIDLines(r io.Reader) ([]string, error) {
	out := make([]string, 0)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}
