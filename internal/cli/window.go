package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/cliutil"
)

// parseWindow parses a relative look-back flag such as --since 30d. Empty
// input returns 0 (flag not set). Anything else must be a positive duration:
// zero, negative or unparseable values are a usage error (exit 2) naming the
// flag, so "--since 0d" never silently means "no bound".
func parseWindow(flag, value string) (time.Duration, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return 0, nil
	}
	d, err := cliutil.ParseDurationLoose(v)
	if err != nil {
		return 0, usageErr(fmt.Errorf("--%s %q is not a duration; use forms like 7d, 2w, 36h or 90m", flag, value))
	}
	if d <= 0 {
		return 0, usageErr(fmt.Errorf("--%s %q must be a positive window such as 7d; leave the flag out for no bound", flag, value))
	}
	return d, nil
}

// emptyWindowErr is the one message for a window whose upper bound is not
// more recent than its lower bound, so it selects nothing.
func emptyWindowErr(sinceFlag, since, beforeFlag, before string) error {
	return usageErr(fmt.Errorf("--%s %s is not more recent than --%s %s, so the window is empty; use a --%s shorter than --%s", beforeFlag, before, sinceFlag, since, beforeFlag, sinceFlag))
}

// timeWindow is the one recording-time window flag pair: --since (how far
// back the window starts) and an upper bound (how long ago it ends), both
// look-backs from now. List, bulk, export and download spell the upper bound
// --before with a hidden --until alias; digest spells it --until with a
// hidden --before alias. An unset flag leaves that side unbounded.
type timeWindow struct {
	Since  string
	Before string
	// beforeFlag is the visible name of the upper bound ("before" or
	// "until"), used in messages.
	beforeFlag string
}

// windowBounds is a resolved window. A zero Start or End is unbounded.
type windowBounds struct {
	Start time.Time
	End   time.Time
}

// registerWindowFlags declares --since and the upper bound on cmd. It is a
// plain function (cmd first) so doc checks can see the flags it declares.
// beforeFlag is "before" (alias "until") or "until" (alias "before").
func registerWindowFlags(cmd *cobra.Command, w *timeWindow, beforeFlag, sinceUsage, beforeUsage string) {
	w.beforeFlag = beforeFlag
	alias := "until"
	if beforeFlag == "until" {
		alias = "before"
	}
	cmd.Flags().StringVar(&w.Since, "since", w.Since, sinceUsage)
	cmd.Flags().StringVar(&w.Before, beforeFlag, w.Before, beforeUsage)
	cmd.Flags().StringVar(&w.Before, alias, w.Before, "Alias of --"+beforeFlag)
	_ = cmd.Flags().MarkHidden(alias)
}

// upperFlag is the visible name of the window's upper bound.
func (w timeWindow) upperFlag() string {
	if w.beforeFlag == "" {
		return "before"
	}
	return w.beforeFlag
}

// set reports whether either side of the window was given.
func (w timeWindow) set() bool {
	return strings.TrimSpace(w.Since) != "" || strings.TrimSpace(w.Before) != ""
}

// bounds resolves the window relative to now: bad durations and empty
// windows are usage errors (exit 2).
func (w timeWindow) bounds(now time.Time) (windowBounds, error) {
	var b windowBounds
	since, err := parseWindow("since", w.Since)
	if err != nil {
		return b, err
	}
	before, err := parseWindow(w.upperFlag(), w.Before)
	if err != nil {
		return b, err
	}
	if since > 0 {
		b.Start = now.Add(-since)
	}
	if before > 0 {
		b.End = now.Add(-before)
	}
	if since > 0 && before > 0 && before >= since {
		return b, emptyWindowErr("since", w.Since, w.upperFlag(), w.Before)
	}
	return b, nil
}

// bounded reports whether either side of the window is set.
func (b windowBounds) bounded() bool { return !b.Start.IsZero() || !b.End.IsZero() }

// contains reports whether t falls in the window: at or after Start and
// before End, with a zero bound left open.
func (b windowBounds) contains(t time.Time) bool {
	if !b.Start.IsZero() && t.Before(b.Start) {
		return false
	}
	if !b.End.IsZero() && !t.Before(b.End) {
		return false
	}
	return true
}

// startRFC3339 is the window's lower bound as RFC3339 UTC for store
// queries, or "" when the window is open at the start.
func (b windowBounds) startRFC3339() string {
	if b.Start.IsZero() {
		return ""
	}
	return b.Start.UTC().Format(time.RFC3339)
}
