package cli

// Video selection and outcome accounting for the multi-video commands: the
// bulk writes (archive, unarchive, move, share, bulk rename), videos export
// and download. They share one selector (the same flags, validation and
// resolution), one positional-argument expander, and one bulkOutcome that
// decides the exit code of a partly failed run.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

// videoSelector is the one set of video-selection flags: a recording-time
// window (--since, --before), --folder, --match and --max-pages over the
// live library, plus --all, --from-file and --limit.
type videoSelector struct {
	Window   timeWindow
	Folder   string
	Match    string
	MaxPages int
	All      bool
	FromFile string
	Limit    int
}

// selectorOpts are a command's opt-outs from the shared selector flags,
// only where a flag makes no sense for that command.
type selectorOpts struct {
	// matchUsage overrides the --match help text.
	matchUsage string
	// noAll drops --all (bulk rename selects with --match instead).
	noAll bool
	// noFromFile drops --from-file (bulk rename takes no id list).
	noFromFile bool
}

// registerVideoSelector declares the selector flags on cmd. It is a plain
// function (cmd first) so doc checks can see the flags it declares.
func registerVideoSelector(cmd *cobra.Command, s *videoSelector, o selectorOpts) {
	registerWindowFlags(cmd, &s.Window, "before",
		"Select videos recorded within this window (e.g. 7d, 24h, 4w)",
		"Select videos recorded longer ago than this (e.g. 90d)")
	cmd.Flags().StringVar(&s.Folder, "folder", "", "Select videos in this folder (32-hex id or folder URL)")
	matchUsage := o.matchUsage
	if matchUsage == "" {
		matchUsage = "Select videos whose name matches this case-insensitive regular expression"
	}
	cmd.Flags().StringVar(&s.Match, "match", "", matchUsage)
	if !o.noAll {
		cmd.Flags().BoolVar(&s.All, "all", false, "Select every video in the library (up to --max-pages) instead of naming ids")
	}
	if !o.noFromFile {
		cmd.Flags().StringVar(&s.FromFile, "from-file", "", "Read video ids or share URLs from a file, one per line (# comments allowed)")
	}
	cmd.Flags().IntVar(&s.Limit, "limit", 0, "Maximum videos to select from the library scan (0 = no limit)")
	registerMaxPages(cmd, &s.MaxPages, "videos", true)
}

// filtered reports whether any library filter is set.
func (s videoSelector) filtered() bool {
	return s.Window.set() || s.Folder != "" || s.Match != ""
}

// scans reports whether the selection reads the live library (a filter or
// --all).
func (s videoSelector) scans() bool { return s.All || s.filtered() }

// hasInput reports whether any selection input was given.
func (s videoSelector) hasInput(args []string) bool {
	return len(args) > 0 || s.FromFile != "" || s.scans()
}

// validate checks everything that needs no network: positional ids, the
// window, --folder, --match, --limit and --max-pages. Bad input exits 2.
func (s videoSelector) validate(args []string) error {
	if _, err := expandVideoArgs(args); err != nil {
		return err
	}
	if _, err := buildSelectFilter(time.Now(), s.Window, s.Folder, s.Match); err != nil {
		return err
	}
	if s.Limit < 0 {
		return usageErr(fmt.Errorf("--limit must be 0 (no limit) or more (got %d)", s.Limit))
	}
	if s.scans() {
		if _, err := effectiveMaxPages(s.MaxPages); err != nil {
			return err
		}
	}
	return nil
}

// describe renders the selection without any IO, for --dry-run previews.
func (s videoSelector) describe(args []string) []string {
	out := make([]string, 0, len(args)+5)
	out = append(out, args...)
	if s.FromFile != "" {
		out = append(out, "ids from "+s.FromFile)
	}
	if s.All {
		out = append(out, "all library videos")
	}
	if s.Window.Since != "" {
		out = append(out, "videos recorded in the last "+s.Window.Since)
	}
	if s.Window.Before != "" {
		out = append(out, "videos recorded more than "+s.Window.Before+" ago")
	}
	if s.Folder != "" {
		out = append(out, "videos in folder "+s.Folder)
	}
	if s.Match != "" {
		out = append(out, "videos whose name matches "+s.Match)
	}
	return out
}

// expandVideoArgs is the one reader of positional video arguments: each may
// hold a comma- or space-separated list; every part must be a 32-hex id, a
// loom.com share URL or "latest" (any alias, returned as "latest"). Ids are
// de-duplicated in order. Bad input is a usage error (exit 2).
func expandVideoArgs(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	seen := map[string]bool{}
	for _, a := range args {
		for _, part := range splitArgList(a) {
			id := "latest"
			if !isLatestArg(part) {
				parsed, err := videoIDArg(part)
				if err != nil {
					return nil, err
				}
				id = parsed
			}
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out, nil
}

// itemFailure is one failed item in a multi-video command. err keeps the
// underlying error so the exit code can be derived from it.
type itemFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
	err   error
}

// selection is the outcome of positional ids, --from-file and a library
// scan: the videos to work on, in order, and the inputs that could not be
// resolved.
type selection struct {
	Videos   []loom.Video
	Failures []itemFailure
	Scanned  int
	MaxPages int
	Capped   bool // the scan stopped at --max-pages with older videos left
	Filtered bool // a library scan ran
}

// selectOpts says how a command resolves its selection.
type selectOpts struct {
	// source is the library to scan: loom.SourceMine (default) or
	// loom.SourceArchived.
	source string
	// details fetches every named video (name, recording date) and turns
	// a failed lookup into a per-item failure; without it named ids are
	// used as-is.
	details bool
}

// newLoomReadClient builds a Loom client that sends read queries even under
// --dry-run, so a bulk write's preview can list the real targets. It is only
// ever used for reads.
func newLoomReadClient(flags *rootFlags) (*loom.Client, error) {
	c, err := flags.newClient()
	if err != nil {
		return nil, err
	}
	c.DryRun = false
	return loom.New(c), nil
}

// selectVideos resolves positional ids (id, share URL or latest) and
// --from-file entries and, when a filter or --all is set, scans the live
// library. Named and scanned videos are combined and de-duplicated. Only an
// error no later lookup can get past (expired session, rate limit, the end
// of ctx) fails the whole selection; other lookup failures are per-item.
//
// ctx bounds the whole selection (export and download pass their run
// context); each network step (resolving latest, each detailed lookup, the
// library scan) gets its own API budget from boundCtx, so a long id list is
// never cut off by one shared deadline.
func selectVideos(ctx context.Context, flags *rootFlags, args []string, s videoSelector, o selectOpts) (selection, error) {
	sel := selection{Videos: make([]loom.Video, 0), Failures: make([]itemFailure, 0)}
	if err := s.validate(args); err != nil {
		return sel, err
	}
	f, _ := buildSelectFilter(time.Now(), s.Window, s.Folder, s.Match)
	inputs := append([]string{}, args...)
	if s.FromFile != "" {
		lines, err := readIDFile(s.FromFile)
		if err != nil {
			return sel, err
		}
		inputs = append(inputs, lines...)
	}
	ids, err := expandVideoArgs(inputs)
	if err != nil {
		return sel, err
	}
	seen := map[string]bool{}
	if len(ids) > 0 {
		// A scanning or detailed selection reads the live library anyway,
		// so resolve "latest" for real; otherwise a dry-run keeps it
		// literal.
		newClient := newLoomClient
		if s.scans() || o.details {
			newClient = newLoomReadClient
		}
		lc, err := newClient(flags)
		if err != nil {
			return sel, err
		}
		for _, id := range ids {
			if id == "latest" {
				rctx, cancel := boundCtx(ctx, flags)
				id, err = resolveVideoArg(rctx, lc, id)
				cancel()
				if err != nil {
					return sel, err
				}
			}
			if seen[id] {
				continue
			}
			v := loom.Video{ID: id}
			if o.details {
				vctx, cancel := boundCtx(ctx, flags)
				got, err := lc.GetVideo(vctx, id)
				cancel()
				if err != nil {
					if stop := stopCause(ctx, err); stop != nil {
						return sel, selectionStopErr(stop)
					}
					sel.Failures = append(sel.Failures, itemFailure{ID: id, Error: videoFailureMsg(id, err), err: videoLookupErr(id, err)})
					seen[id] = true
					continue
				}
				v = *got
			}
			seen[id] = true
			sel.Videos = append(sel.Videos, v)
		}
	}
	if s.scans() {
		maxPages, _ := effectiveMaxPages(s.MaxPages)
		rc, err := newLoomReadClient(flags)
		if err != nil {
			return sel, err
		}
		sctx, cancel := boundCtx(ctx, flags)
		scanned, err := scanLibrary(sctx, rc, scanOpts{Source: o.source, Filter: f, Limit: s.Limit, MaxPages: maxPages})
		cancel()
		if err != nil {
			return sel, loomErr(err)
		}
		sel.Scanned, sel.MaxPages, sel.Capped, sel.Filtered = scanned.Scanned, maxPages, scanned.Capped, true
		for _, v := range scanned.Videos {
			if seen[v.ID] {
				// A named target the scan also found: fill in its details.
				for i := range sel.Videos {
					if sel.Videos[i].ID == v.ID && sel.Videos[i].Name == "" {
						sel.Videos[i] = v
					}
				}
				continue
			}
			seen[v.ID] = true
			sel.Videos = append(sel.Videos, v)
		}
	}
	return sel, nil
}

// selectionStopErr is the error of a selection stopped by stopCause: a
// run-stopping Loom error keeps its typed code; the end of the run context
// (explicit --timeout, Ctrl-C) exits 5.
func selectionStopErr(stop error) error {
	if loom.IsRunStopping(stop) {
		return loomErr(stop)
	}
	return apiErr(fmt.Errorf("video selection stopped early (%w); nothing was exported or downloaded", stop))
}

// ids lists the selected video ids in order.
func (s selection) ids() []string {
	out := make([]string, 0, len(s.Videos))
	for _, v := range s.Videos {
		out = append(out, v.ID)
	}
	return out
}

// selectedVideo is one target of a bulk write as its plan shows it.
type selectedVideo struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	NewName   string `json:"new_name,omitempty"`
}

// targets renders the selected videos for a bulk plan.
func (s selection) targets() []selectedVideo {
	out := make([]selectedVideo, 0, len(s.Videos))
	for _, v := range s.Videos {
		out = append(out, selectedVideo{ID: v.ID, Name: v.Name, CreatedAt: v.CreatedAt})
	}
	return out
}

// transferPlan is the --dry-run preview of export and download.
type transferPlan struct {
	DryRun  bool     `json:"dry_run"`
	Action  string   `json:"action"`
	Targets []string `json:"targets"`
	OutDir  string   `json:"out_dir"`
	Format  string   `json:"format,omitempty"`
	Force   bool     `json:"force,omitempty"`
	Would   string   `json:"would"`
}

// runContext is the context a multi-video run (export, download, hydrate)
// works under: bounded by --timeout only when the user set it explicitly.
// Each item's own API calls are bounded separately with boundCtx, so a long
// run is never cut off by the default per-call budget.
func runContext(cmd *cobra.Command, flags *rootFlags) (context.Context, context.CancelFunc) {
	if flags.timeoutExplicit && flags.timeout > 0 {
		return context.WithTimeout(cmd.Context(), flags.timeout)
	}
	return cmd.Context(), func() {}
}

// stopCause reports why a multi-video run must stop after err: a Loom error
// no later item can get past (loom.IsRunStopping: expired session, rate
// limiting), or the end of the run's own context (explicit --timeout,
// Ctrl-C). nil means err fails only its own item; in particular a per-item
// deadline is a per-item failure.
func stopCause(runCtx context.Context, err error) error {
	if err != nil && loom.IsRunStopping(err) {
		return err
	}
	if runCtx.Err() != nil {
		return runCtx.Err()
	}
	return nil
}

// bulkOutcome tallies a multi-video run (bulk rename, videos export,
// download, hydrate) and picks its exit code the same way everywhere:
//   - a run-stopping error (expired session, rate limit, the explicit
//     --timeout) exits with its own typed code (4, 7, 5);
//   - when every item failed, the exit code of the failures (3 when every
//     video was missing, 5 when they failed in different ways or with API
//     errors);
//   - otherwise 0, with the failures listed in the output.
type bulkOutcome struct {
	what      string // "export", "download", ... for messages
	succeeded int
	failures  []error
	stop      error
}

// ok records one item that succeeded.
func (o *bulkOutcome) ok() { o.succeeded++ }

// fail records one failed item.
func (o *bulkOutcome) fail(err error) {
	if err == nil {
		err = errors.New("failed")
	}
	o.failures = append(o.failures, err)
}

// halt records the error that stopped the run (the first one wins).
func (o *bulkOutcome) halt(err error) {
	if o.stop == nil {
		o.stop = err
	}
}

// stopped reports whether the run was stopped.
func (o *bulkOutcome) stopped() bool { return o.stop != nil }

// statusNotAttempted is the per-item status, in every multi-video report,
// of an item the run stopped before.
const statusNotAttempted = "not_attempted"

// notAttemptedMsg is the per-item error of an item the stopped run never
// started. Call it only once the run is stopped.
func (o *bulkOutcome) notAttemptedMsg() string {
	return "not attempted: the " + o.what + " stopped early (" + firstLine(o.stop.Error()) + ")"
}

// exitErr is the command's error for this outcome (nil for exit 0).
func (o *bulkOutcome) exitErr() error {
	if o.stop != nil {
		if !loom.IsRunStopping(o.stop) {
			switch {
			case errors.Is(o.stop, context.Canceled):
				return apiErr(fmt.Errorf("%s interrupted (%w); finished items are kept, re-run to continue", o.what, o.stop))
			case errors.Is(o.stop, context.DeadlineExceeded):
				return apiErr(fmt.Errorf("%s stopped early (%w); finished items are kept, re-run to continue (or raise --timeout)", o.what, o.stop))
			}
		}
		return loomErr(o.stop)
	}
	if len(o.failures) == 0 || o.succeeded > 0 {
		return nil
	}
	first := loomErr(o.failures[0])
	code := ExitCode(first)
	for _, err := range o.failures[1:] {
		if ExitCode(loomErr(err)) != code {
			code = 5
			break
		}
	}
	return &cliError{code: code, err: fmt.Errorf("every %s failed (%d); first error: %w", o.what, len(o.failures), first)}
}
