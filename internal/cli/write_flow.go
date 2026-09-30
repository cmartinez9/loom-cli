package cli

// The write flow every mutating command shares: the --yes gate, the
// --dry-run preview, the harness refusal, sending one mutation, and the
// runners for each shape of write command (one video, one video plus text,
// one opaque id, one opaque id plus text, and bulk writes over a video
// selection).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/cliutil"
)

// confirm says whether a write needs --yes, and why. The empty value means
// the write needs no confirmation (owner-only metadata such as renaming one
// video).
type confirm string

const (
	noConfirm confirm = ""
	// confirmLibrary gates destructive or bulk changes to the library.
	confirmLibrary confirm = "changes your Loom library"
	// confirmVisible gates writes other people see (sharing, comments,
	// reactions, action items, viewer settings).
	confirmVisible confirm = "is visible to other people"
)

// requireYes is the one --yes gate. reason completes the sentence "<reason>;
// re-run with --yes ..." (for example "delete video x changes your Loom
// library"). It runs before any network IO, so an unconfirmed write never
// resolves, scans or sends anything. Dry-runs and Printing Press harnesses
// fall through: runMutation previews or refuses them.
func requireYes(flags *rootFlags, reason string) error {
	if flags.dryRun || flags.yes || cliutil.IsAnyHarness() {
		return nil
	}
	return usageErr(fmt.Errorf("%s; re-run with --yes to confirm, or --dry-run to preview", reason))
}

// requireConfirm applies requireYes for a write that needs confirmation.
func requireConfirm(flags *rootFlags, action string, c confirm) error {
	if c == noConfirm {
		return nil
	}
	return requireYes(flags, action+" "+string(c))
}

// mutationPlan is the preview printed for a write under --dry-run.
type mutationPlan struct {
	DryRun    bool           `json:"dry_run"`
	Action    string         `json:"action"`
	Operation string         `json:"operation"`
	Variables map[string]any `json:"variables"`
	Targets   []string       `json:"targets,omitempty"`
	Note      string         `json:"note,omitempty"`
}

// runMutation executes a Loom mutation with the safety rails every write
// command shares:
//   - --dry-run prints the exact operation and variables and sends nothing;
//   - any Printing Press harness (verify, dogfood) refuses the real write;
//   - writes that need confirmation (c) require --yes (requireYes).
//
// It returns the mutation's data object (nil when nothing was sent).
func runMutation(cmd *cobra.Command, flags *rootFlags, action, op string, vars map[string]any, targets []string, c confirm) (json.RawMessage, bool, error) {
	if flags.dryRun {
		plan := mutationPlan{DryRun: true, Action: action, Operation: op, Variables: vars, Targets: targets, Note: "preview only; nothing was sent to Loom"}
		err := emit(cmd, flags, plan, func(w io.Writer) error {
			fmt.Fprintf(w, "dry-run: would %s\n  operation: %s\n", action, op)
			b, _ := json.Marshal(vars)
			fmt.Fprintf(w, "  variables: %s\n", b)
			return nil
		})
		return nil, false, err
	}
	if cliutil.IsAnyHarness() {
		return nil, false, writeHarnessRefusal(cmd.OutOrStdout(), flags, action)
	}
	if err := requireConfirm(flags, action, c); err != nil {
		return nil, false, err
	}
	lc, err := newLoomClient(flags)
	if err != nil {
		return nil, false, err
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	data, err := lc.Mutate(ctx, op, vars)
	if err != nil {
		return nil, false, loomErr(err)
	}
	// Mutations answer with a union (GenericError and friends) or a bare
	// boolean; a signed-out session is normalized before mapping so it
	// exits 4, not "not found".
	if err := lc.CheckMutation(ctx, op, data); err != nil {
		return nil, false, loomErr(err)
	}
	return data, true, nil
}

// mutationResult is the JSON printed after a real write.
type mutationResult struct {
	OK        bool            `json:"ok"`
	Action    string          `json:"action"`
	Operation string          `json:"operation"`
	Targets   []string        `json:"targets,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// printMutation reports a completed write.
func printMutation(cmd *cobra.Command, flags *rootFlags, action, op string, targets []string, data json.RawMessage) error {
	res := mutationResult{OK: true, Action: action, Operation: op, Targets: targets, Result: data}
	return emit(cmd, flags, res, func(w io.Writer) error {
		fmt.Fprintf(w, "done: %s\n", action)
		return nil
	})
}

// runWrite is the common tail of every single-target write: send through
// runMutation (dry-run preview, harness refusal, --yes) and report.
func runWrite(cmd *cobra.Command, flags *rootFlags, action, op string, vars map[string]any, targets []string, c confirm) error {
	data, sent, err := runMutation(cmd, flags, action, op, vars, targets, c)
	if err != nil || !sent {
		return err
	}
	return printMutation(cmd, flags, action, op, targets, data)
}

// placeholder returns v, or "<name>" when v is empty (dry-run previews for
// invocations that omit required input).
func placeholder(v, name string) string {
	if v == "" {
		return "<" + name + ">"
	}
	return v
}

// resolveVideoForWrite resolves a validated positional video argument. Under
// --dry-run "latest" stays literal so the preview needs no network.
func resolveVideoForWrite(ctx context.Context, flags *rootFlags, arg string) (string, error) {
	if arg == "" {
		return "<video>", nil
	}
	lc, err := newLoomClient(flags)
	if err != nil {
		return "", err
	}
	return resolveVideoArg(ctx, lc, arg)
}

// readTextSource returns the inline text, or the contents of --from-file
// ("-" reads stdin). Under --dry-run an unreadable file becomes a
// placeholder instead of an error.
func readTextSource(cmd *cobra.Command, flags *rootFlags, inline, fromFile string) (string, error) {
	if fromFile == "" {
		return inline, nil
	}
	var b []byte
	var err error
	if fromFile == "-" {
		b, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1<<20))
	} else {
		b, err = os.ReadFile(filepath.Clean(fromFile))
	}
	if err != nil {
		if flags.dryRun {
			return "<contents of " + fromFile + ">", nil
		}
		return "", usageErr(fmt.Errorf("--from-file: %w", err))
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// runSingleVideoWrite is the flow of a write that takes exactly one video
// positional and no other required input (delete, recover, duplicate, pin,
// follow, watch-later).
func runSingleVideoWrite(cmd *cobra.Command, flags *rootFlags, args []string, verb, op string, c confirm, buildVars func(id string) map[string]any) error {
	if len(args) > 1 {
		return usageErr(fmt.Errorf("expected one <video> (id, share URL or latest)"))
	}
	var videoArg string
	if len(args) > 0 {
		if err := checkVideoArg(args[0]); err != nil {
			return err
		}
		videoArg = args[0]
	} else if !flags.dryRun {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<video> is required (id, share URL or latest)"))
	}
	if err := requireConfirm(flags, verb+" video "+placeholder(videoArg, "video"), c); err != nil {
		return err
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	id, err := resolveVideoForWrite(ctx, flags, videoArg)
	if err != nil {
		return err
	}
	return runWrite(cmd, flags, verb+" video "+id, op, buildVars(id), []string{id}, c)
}

// textWrite describes a `<target> <text>` write: the text positional's
// name, the verb (the action is verb+" "+<target>), the operation and the
// confirmation it needs.
type textWrite struct {
	// textLabel names the text positional in messages ("text", "name",
	// "emoji").
	textLabel string
	verb      string
	op        string
	confirm   confirm
	// at is the --at playback offset, for writes that have the flag; nil
	// means no --at (the offset passed to buildVars is then 0).
	at *string
	// fromFile is a --from-file flag that may supply the text instead of
	// the positional; nil means the command has no such flag.
	fromFile *string
}

// runVideoTextWrite is the flow of `<video> <text>` writes: comments add,
// tasks add and reactions add (with --at), and single-video videos rename
// and videos describe (without). w.confirm gates the write on --yes
// (outside --dry-run) before any network IO.
func runVideoTextWrite(cmd *cobra.Command, flags *rootFlags, args []string, w textWrite, buildVars func(id, text string, ms int) map[string]any) error {
	if len(args) > 2 {
		return usageErr(fmt.Errorf("expected <video> <%s>; quote text that contains spaces", w.textLabel))
	}
	fromFile := ""
	if w.fromFile != nil {
		fromFile = *w.fromFile
	}
	if len(args) > 1 && fromFile != "" {
		return usageErr(fmt.Errorf("pass the %s inline or with --from-file, not both", w.textLabel))
	}
	var videoArg, text string
	if len(args) > 0 {
		if err := checkVideoArg(args[0]); err != nil {
			return err
		}
		videoArg = args[0]
	}
	if len(args) > 1 {
		text = args[1]
	}
	ms := 0
	if w.at != nil {
		var err error
		if ms, err = parseAtMillis(*w.at); err != nil {
			return err
		}
	}
	if videoArg == "" && !flags.dryRun {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<video> and <%s> are both required", w.textLabel))
	}
	text, err := readTextSource(cmd, flags, text, fromFile)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" && !flags.dryRun {
		_ = cmd.Usage()
		if w.fromFile != nil {
			return usageErr(fmt.Errorf("<%s> is required (inline or --from-file)", w.textLabel))
		}
		return usageErr(fmt.Errorf("<video> and <%s> are both required", w.textLabel))
	}
	if err := requireConfirm(flags, w.verb+" "+placeholder(videoArg, "video"), w.confirm); err != nil {
		return err
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	id, err := resolveVideoForWrite(ctx, flags, videoArg)
	if err != nil {
		return err
	}
	return runWrite(cmd, flags, w.verb+" "+id, w.op, buildVars(id, placeholder(text, w.textLabel), ms), []string{id}, w.confirm)
}

// runIDTextWrite is the flow of `<id> <text>` writes on an opaque id: tasks
// edit, folders rename and comments react. kind names the id ("task",
// "folder", "comment"); w.at and w.fromFile are not used.
func runIDTextWrite(cmd *cobra.Command, flags *rootFlags, args []string, kind string, w textWrite, buildVars func(id, text string) map[string]any) error {
	if len(args) > 2 {
		return usageErr(fmt.Errorf("expected <%s-id> <%s>; quote text that contains spaces", kind, w.textLabel))
	}
	id, text := "", ""
	if len(args) > 0 {
		v, err := opaqueIDArg(kind, args[0])
		if err != nil {
			return err
		}
		id = v
	}
	if len(args) > 1 {
		text = strings.TrimSpace(args[1])
	}
	if (id == "" || text == "") && !flags.dryRun {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<%s-id> and <%s> are both required", kind, w.textLabel))
	}
	id = placeholder(id, kind+"-id")
	action := w.verb + " " + id
	if err := requireConfirm(flags, action, w.confirm); err != nil {
		return err
	}
	return runWrite(cmd, flags, action, w.op, buildVars(id, placeholder(text, w.textLabel)), []string{id}, w.confirm)
}

// runIDWrite is the flow of writes that take one opaque id and nothing else
// required (comments delete, tasks delete/approve/done, reactions delete).
func runIDWrite(cmd *cobra.Command, flags *rootFlags, args []string, kind, verb, op string, c confirm, buildVars func(id string) map[string]any) error {
	if len(args) > 1 {
		return usageErr(fmt.Errorf("expected one <%s-id>", kind))
	}
	id := ""
	if len(args) > 0 {
		v, err := opaqueIDArg(kind, args[0])
		if err != nil {
			return err
		}
		id = v
	} else if !flags.dryRun {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("<%s-id> is required", kind))
	}
	id = placeholder(id, kind+"-id")
	action := verb + " " + id
	if err := requireConfirm(flags, action, c); err != nil {
		return err
	}
	return runWrite(cmd, flags, action, op, buildVars(id), []string{id}, c)
}

// runBulkVideoWrite is the shared flow of archive, unarchive, move and share:
// validate, gate on --yes (c says why: confirmLibrary for archive and move,
// confirmVisible for share), select, preview or send one bulk mutation.
func runBulkVideoWrite(cmd *cobra.Command, flags *rootFlags, args []string, s videoSelector, source, action, op string, c confirm, buildVars func(ids []string) map[string]any) error {
	if !s.hasInput(args) && !flags.dryRun {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("name at least one video (id, share URL or latest), --from-file, --all, or a filter (--since, --before, --folder, --match)"))
	}
	if err := s.validate(args); err != nil {
		return err
	}
	if err := requireConfirm(flags, action, c); err != nil {
		return err
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	sel, err := selectVideos(ctx, flags, args, s, selectOpts{source: source})
	if err != nil {
		return err
	}
	ids := sel.ids()
	vars := buildVars(ids)
	if flags.dryRun || len(ids) == 0 {
		return emitBulkPlan(cmd, flags, newBulkPlan(flags, action, op, vars, sel, sel.targets()))
	}
	return runWrite(cmd, flags, action, op, vars, ids, c)
}

// bulkPlan is the preview (or empty-selection report) of a bulk write. It
// carries the same keys as a single write's dry-run plus the selected
// targets with names, their count and the scan effort.
type bulkPlan struct {
	DryRun        bool             `json:"dry_run"`
	Action        string           `json:"action"`
	Operation     string           `json:"operation"`
	Variables     map[string]any   `json:"variables,omitempty"`
	Calls         []map[string]any `json:"calls,omitempty"`
	Count         int              `json:"count"`
	Targets       []selectedVideo  `json:"targets"`
	ScannedVideos int              `json:"scanned_videos"`
	MaxPages      int              `json:"max_pages,omitempty"`
	Capped        bool             `json:"capped,omitempty"`
	Note          string           `json:"note,omitempty"`
}

// newBulkPlan describes targets (picked from sel) and sel's scan effort.
func newBulkPlan(flags *rootFlags, action, op string, vars map[string]any, sel selection, targets []selectedVideo) bulkPlan {
	if targets == nil {
		targets = make([]selectedVideo, 0)
	}
	p := bulkPlan{DryRun: flags.dryRun, Action: action, Operation: op, Variables: vars, Count: len(targets), Targets: targets}
	if sel.Filtered {
		p.ScannedVideos, p.MaxPages, p.Capped = sel.Scanned, sel.MaxPages, sel.Capped
	}
	switch {
	case len(targets) == 0 && sel.Filtered && sel.Capped:
		p.Note = fmt.Sprintf("no videos matched in %d scanned (%s); nothing was sent to Loom", sel.Scanned, capNote(sel.MaxPages, "videos"))
	case len(targets) == 0:
		p.Note = "no videos selected; nothing was sent to Loom"
	case flags.dryRun:
		p.Note = "preview only; nothing was sent to Loom"
	}
	if sel.Capped && len(targets) > 0 {
		p.Note += " (" + capNote(sel.MaxPages, "videos") + ")"
	}
	return p
}

// emitBulkPlan prints a bulk plan as JSON or a short human table.
func emitBulkPlan(cmd *cobra.Command, flags *rootFlags, p bulkPlan) error {
	return emit(cmd, flags, p, func(w io.Writer) error {
		if p.DryRun {
			fmt.Fprintf(w, "dry-run: would %s (%d video(s))\n", p.Action, p.Count)
		} else {
			fmt.Fprintf(w, "nothing to do: %s selected %d video(s)\n", p.Action, p.Count)
		}
		fmt.Fprintf(w, "  operation: %s\n", p.Operation)
		if len(p.Variables) > 0 {
			b, _ := json.Marshal(p.Variables)
			fmt.Fprintf(w, "  variables: %s\n", b)
		}
		if len(p.Targets) > 0 {
			tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
			if p.Targets[0].NewName != "" || len(p.Calls) > 0 {
				fmt.Fprintln(tw, "  ID\tNAME\tNEW NAME")
				for _, t := range p.Targets {
					fmt.Fprintf(tw, "  %s\t%s\t%s\n", t.ID, t.Name, t.NewName)
				}
			} else {
				fmt.Fprintln(tw, "  ID\tNAME")
				for _, t := range p.Targets {
					fmt.Fprintf(tw, "  %s\t%s\n", t.ID, t.Name)
				}
			}
			_ = tw.Flush()
		}
		if p.Note != "" {
			fmt.Fprintf(w, "  note: %s\n", strings.TrimSpace(p.Note))
		}
		return nil
	})
}
