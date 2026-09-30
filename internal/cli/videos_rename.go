package cli

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/cliutil"
	"github.com/cmartinez9/loom-cli/internal/loom"
)

func newVideosRenameCmd(flags *rootFlags) *cobra.Command {
	var sel videoSelector
	var replace string
	cmd := &cobra.Command{
		Use:   "rename [video] [new-name]",
		Short: "Rename one video, or bulk-rename by regex over your library",
		Long: `Rename a video (UpdateVideoName).

Single mode: rename <video> <new-name>, where <video> is an id, share URL or
"latest". Renaming one video needs no --yes.

Bulk mode: --match <regex> --replace <template> renames every library video
whose name matches, rewriting the name with Go regexp replacement syntax
($1, ${name}). Narrow the selection with --since, --before, --folder and
--limit. Bulk renames require --yes; --dry-run lists each old and new name
first.

Bulk exit codes: 0 when at least one rename succeeded (failures are listed
in the output), the failures' own code (5 for API errors) when every rename
failed, and 4 or 7 when an expired session or rate limiting stopped the run
(the renames not attempted are listed).`,
		Example: strings.Trim(`
  loom-pp-cli videos rename latest "Q3 planning walkthrough" --dry-run
  loom-pp-cli videos rename 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 "Onboarding demo v2"
  loom-pp-cli videos rename --match "^Untitled" --replace "Draft" --since 30d --dry-run --json`, "\n"),
		Annotations: map[string]string{"pp:happy-args": "video=latest;name=Renamed demo"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if sel.Match != "" || cmd.Flags().Changed("replace") {
				return runBulkRename(cmd, flags, args, sel, replace)
			}
			if sel.filtered() || sel.Limit != 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--since/--before/--folder/--limit select videos for bulk rename; add --match <regex> --replace <template>"))
			}
			return runVideoTextWrite(cmd, flags, args, textWrite{textLabel: "new-name", verb: "rename video", op: loom.OpUpdateName, confirm: noConfirm}, func(id, name string, _ int) map[string]any {
				return map[string]any{"id": id, "name": name}
			})
		},
	}
	cmd.Flags().StringVar(&replace, "replace", "", "Bulk mode: replacement template for names matching --match ($1, ${name} expand)")
	registerVideoSelector(cmd, &sel, selectorOpts{
		matchUsage: "Bulk mode: rename videos whose name matches this regular expression",
		noAll:      true, // bulk rename selects with --match
		noFromFile: true, // and never takes an id list
	})
	return cmd
}

// planRenames applies re/template to each target and drops unchanged names.
func planRenames(targets []selectedVideo, re *regexp.Regexp, template string) []selectedVideo {
	out := make([]selectedVideo, 0, len(targets))
	for _, t := range targets {
		if !re.MatchString(t.Name) {
			continue
		}
		newName := strings.TrimSpace(re.ReplaceAllString(t.Name, template))
		if newName == "" || newName == t.Name {
			continue
		}
		t.NewName = newName
		out = append(out, t)
	}
	return out
}

type bulkRenameResult struct {
	ID      string `json:"id"`
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
	OK      bool   `json:"ok"`
	// Status is renamed, failed or not_attempted (the run stopped first).
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// bulkRenameReport is the JSON printed after a bulk rename.
type bulkRenameReport struct {
	OK           bool               `json:"ok"`
	Action       string             `json:"action"`
	Operation    string             `json:"operation"`
	Renamed      int                `json:"renamed"`
	Failed       int                `json:"failed"`
	NotAttempted int                `json:"not_attempted"`
	Results      []bulkRenameResult `json:"results"`
}

func runBulkRename(cmd *cobra.Command, flags *rootFlags, args []string, sel videoSelector, template string) error {
	const action = "bulk rename videos"
	if len(args) > 0 {
		return usageErr(fmt.Errorf("bulk rename selects videos with --match and filters; drop the positional arguments"))
	}
	if sel.Match == "" && !flags.dryRun {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("--match <regex> is required with --replace"))
	}
	if !cmd.Flags().Changed("replace") && !flags.dryRun {
		_ = cmd.Usage()
		return usageErr(fmt.Errorf("--replace <template> is required with --match"))
	}
	if err := sel.validate(nil); err != nil {
		return err
	}
	f, err := buildSelectFilter(time.Now(), sel.Window, sel.Folder, sel.Match)
	if err != nil {
		return err
	}
	if err := requireConfirm(flags, action, confirmLibrary); err != nil {
		return err
	}
	found := selection{Videos: make([]loom.Video, 0)}
	targets := make([]selectedVideo, 0)
	if sel.Match != "" {
		ctx, cancel := boundCtx(cmd.Context(), flags)
		defer cancel()
		found, err = selectVideos(ctx, flags, nil, sel, selectOpts{source: loom.SourceMine})
		if err != nil {
			return err
		}
		targets = planRenames(found.targets(), f.Match, template)
	}
	calls := make([]map[string]any, 0, len(targets))
	for _, t := range targets {
		calls = append(calls, map[string]any{"id": t.ID, "name": t.NewName})
	}
	if flags.dryRun || len(targets) == 0 {
		plan := newBulkPlan(flags, action, loom.OpUpdateName, nil, found, targets)
		plan.Calls = calls
		return emitBulkPlan(cmd, flags, plan)
	}
	if cliutil.IsAnyHarness() {
		return writeHarnessRefusal(cmd.OutOrStdout(), flags, action)
	}
	report := bulkRenameReport{Action: action, Operation: loom.OpUpdateName, Results: make([]bulkRenameResult, 0, len(targets))}
	outcome := bulkOutcome{what: "rename"}
	for i, t := range targets {
		r := bulkRenameResult{ID: t.ID, OldName: t.Name, NewName: t.NewName}
		if outcome.stopped() {
			r.Status, r.Error = statusNotAttempted, outcome.notAttemptedMsg()
			report.NotAttempted++
			report.Results = append(report.Results, r)
			continue
		}
		_, sent, err := runMutation(cmd, flags, action, loom.OpUpdateName, calls[i], []string{t.ID}, confirmLibrary)
		if err == nil && !sent {
			err = fmt.Errorf("not sent")
		}
		switch {
		case err == nil:
			r.OK, r.Status = true, "renamed"
			report.Renamed++
			outcome.ok()
		default:
			r.Status, r.Error = "failed", err.Error()
			report.Failed++
			outcome.fail(err)
			if loom.IsRunStopping(err) {
				// Every later rename would fail the same way.
				outcome.halt(err)
			}
		}
		report.Results = append(report.Results, r)
	}
	report.OK = report.Failed == 0 && report.NotAttempted == 0
	if err := emit(cmd, flags, report, func(w io.Writer) error {
		for _, r := range report.Results {
			status := r.Status
			if r.Error != "" {
				status += ": " + r.Error
			}
			fmt.Fprintf(w, "%s  %q -> %q  %s\n", r.ID, r.OldName, r.NewName, status)
		}
		fmt.Fprintf(w, "%d renamed, %d failed, %d not attempted\n", report.Renamed, report.Failed, report.NotAttempted)
		return nil
	}); err != nil {
		return err
	}
	if report.Failed > 0 || report.NotAttempted > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d of %d renames failed, %d not attempted\n", report.Failed, len(targets), report.NotAttempted)
	}
	return outcome.exitErr()
}
