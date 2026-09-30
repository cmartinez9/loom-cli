package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

// Shared argument handling for the write (organizing) commands: videos
// rename/describe/settings/..., folders, comments, tasks and reactions.

// exampleVideoID is the made-up id used in examples and happy-args.
const exampleVideoID = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"

// Loom enum values discovered by schema validation probing
// (PublicVideoCommentType and CommentType both accept COMMENT and REPLY).
const (
	commentTypeComment = "COMMENT"
	commentTypeReply   = "REPLY"
)

// parseAtMillis turns an --at playback offset ("1:23", "0:05", "83",
// "1:02:03") into Loom's integer milliseconds. Empty means 0.
func parseAtMillis(s string) (int, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	sec, err := loom.ParseClock(s)
	if err != nil || sec < 0 {
		return 0, usageErr(fmt.Errorf("--at %q is not a playback offset like 1:23 or 83", s))
	}
	return loom.SecondsToMillis(sec), nil
}

// parseOnOff parses an on/off style flag value.
func parseOnOff(flag, s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "yes", "1", "enable", "enabled":
		return true, nil
	case "off", "false", "no", "0", "disable", "disabled":
		return false, nil
	}
	return false, usageErr(fmt.Errorf("--%s must be on or off, got %q", flag, s))
}

// utcOffsetMinutes is the local zone's offset east of UTC in minutes
// (UTC-5 -> -300), sent as minutesFromUTC for Watch Later.
func utcOffsetMinutes(now time.Time) int {
	_, off := now.Zone()
	return off / 60
}
