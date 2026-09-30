package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"loom-pp-cli/internal/cliutil"
	"loom-pp-cli/internal/loom"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newDownloadCmd(flags))
	})
}

type downloadItem struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Bytes  int64  `json:"bytes,omitempty"`
	Size   int64  `json:"size,omitempty"`
	URL    string `json:"url,omitempty"`
	Error  string `json:"error,omitempty"`
	err    error  // the underlying failure, for the exit code
}

type downloadReport struct {
	OutDir     string         `json:"out_dir,omitempty"`
	Results    []downloadItem `json:"results"`
	Downloaded int            `json:"downloaded"`
	Skipped    int            `json:"skipped"`
	Failed     int            `json:"failed"`
	// NotAttempted counts videos left untouched because the run stopped
	// early (expired session, rate limit, --timeout); each is also listed
	// in Results with status "not_attempted".
	NotAttempted  int           `json:"not_attempted"`
	FetchFailures []itemFailure `json:"fetch_failures,omitempty"`
	// Capped is set when the library scan stopped at --max-pages with
	// older videos left unselected.
	Capped bool   `json:"capped,omitempty"`
	Note   string `json:"note,omitempty"`
}

func newDownloadCmd(flags *rootFlags) *cobra.Command {
	var (
		out         string
		urlOnly     bool
		concurrency int
		sel         videoSelector
	)
	cmd := &cobra.Command{
		Use:   "download [video]...",
		Short: "Download videos as MP4 files, resumably and rate-limited",
		Long: `Download each video's MP4 into --out as <created-date>-<slug>-<id8>.mp4.

Downloads are resumable: a finished file (same size as the server's copy) is
skipped, and an interrupted .part file continues with an HTTP Range request.
Transfers are paced and back off when the CDN rate-limits.

Select videos by id, share URL or 'latest', from a file (--from-file, one per
line), or in bulk with --all, --since, --before, --folder and --match
(--limit caps the scan). --url-only prints the short-lived signed MP4 links
instead of downloading.

Loom only has an MP4 for a video once it has been rendered for download; a
video that only has the streaming version reports a clear per-video error.

Exit codes: 0 when at least one video was downloaded or already complete
(failures are listed), the failures' own code when every video failed (3
when none has an MP4 or exists, 5 otherwise), 4 or 7 when an expired
session or rate limiting stopped the run, and 5 when an explicit --timeout
or Ctrl-C cut it off (even mid-transfer); the videos not started are listed
as not_attempted. Transfers are bounded only by an explicit --timeout.`,
		Example: strings.Trim(`
  loom-pp-cli download latest
  loom-pp-cli download 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --out ./videos
  loom-pp-cli download --since 30d --concurrency 3
  loom-pp-cli download --from-file ids.txt --json
  loom-pp-cli download https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --url-only`, "\n"),
		Annotations: map[string]string{"pp:data-source": "live", "pp:happy-args": "video=latest"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			// Input validation needs no network, so bad input exits 2 even
			// under --dry-run.
			if err := sel.validate(args); err != nil {
				return err
			}
			if concurrency < 1 || concurrency > 8 {
				return usageErr(fmt.Errorf("--concurrency must be between 1 and 8 (got %d)", concurrency))
			}
			if dryRunOK(flags) {
				would := "resolve each video's signed MP4 and stream it to disk; nothing was fetched or written"
				if urlOnly {
					would = "resolve and print each video's signed MP4 URL; nothing was fetched"
				}
				plan := transferPlan{DryRun: true, Action: "download", Targets: sel.describe(args), OutDir: out, Would: would}
				return emit(cmd, flags, plan, func(w io.Writer) error {
					fmt.Fprintf(w, "dry-run: would download %s to %s\n", strings.Join(plan.Targets, ", "), out)
					return nil
				})
			}
			if err := checkDataSource(cmd, flags); err != nil {
				return err
			}
			if !sel.hasInput(args) {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("name at least one <video> (id, share URL or 'latest'), or use --from-file, --all, --since, --before, --folder or --match"))
			}
			if cliutil.IsAnyHarness() {
				return writeHarnessRefusal(cmd.OutOrStdout(), flags, "download videos")
			}
			// The CDN transfer is bounded only by an explicit --timeout: a
			// large file legitimately takes longer than the API budget. The
			// selection runs under the same run context; each lookup gets
			// its own API budget inside selectVideos.
			runCtx, cancelRun := runContext(cmd, flags)
			defer cancelRun()
			found, err := selectVideos(runCtx, flags, args, sel, selectOpts{details: true})
			if err != nil {
				return loomErr(err)
			}
			lc, err := newLoomClient(flags)
			if err != nil {
				return err
			}
			if !urlOnly {
				if err := os.MkdirAll(out, 0o700); err != nil {
					return fmt.Errorf("creating --out %s: %w", out, err)
				}
			}
			report := downloadReport{Results: make([]downloadItem, 0, len(found.Videos)+len(found.Failures))}
			if !urlOnly {
				report.OutDir = out
			}
			if found.Capped {
				report.Capped, report.Note = true, capNote(found.MaxPages, "videos")
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", report.Note)
			}
			for _, f := range found.Failures {
				report.Results = append(report.Results, downloadItem{ID: f.ID, Status: "failed", Error: f.Error, err: f.err})
			}
			items, stop := downloadVideos(runCtx, cmd, flags, lc, found.Videos, downloadJob{out: out, urlOnly: urlOnly, concurrency: concurrency})
			report.Results = append(report.Results, items...)
			// stop is set when the run ended early, including an explicit
			// --timeout or Ctrl-C cutting off in-flight transfers with no
			// video left unstarted: the run then exits non-zero.
			outcome := tallyDownloads(&report, stop)
			if report.Failed > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d of %d downloads failed; %d completed, %d already complete\n", report.Failed, len(report.Results), report.Downloaded, report.Skipped)
			}
			if report.NotAttempted > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: stopped early; %d videos were not attempted (re-run to continue)\n", report.NotAttempted)
			}
			if err := emit(cmd, flags, report, func(w io.Writer) error {
				if urlOnly {
					for _, it := range report.Results {
						if it.URL != "" {
							fmt.Fprintln(w, it.URL)
						}
					}
					return nil
				}
				fmt.Fprintf(w, "%d downloaded, %d skipped, %d failed, %d not attempted\n", report.Downloaded, report.Skipped, report.Failed, report.NotAttempted)
				return nil
			}); err != nil {
				return err
			}
			return outcome.exitErr()
		},
	}
	cmd.Flags().StringVar(&out, "out", "./loom-downloads", "Directory to save MP4 files into")
	cmd.Flags().BoolVar(&urlOnly, "url-only", false, "Print each video's short-lived signed MP4 URL instead of downloading")
	cmd.Flags().IntVar(&concurrency, "concurrency", 2, "Videos to download at the same time (1-8)")
	registerVideoSelector(cmd, &sel, selectorOpts{})
	return cmd
}

// downloadJob is how one download run fetches its videos.
type downloadJob struct {
	out         string
	urlOnly     bool
	concurrency int
}

// downloadVideos downloads (or, with urlOnly, resolves) videos with a small
// worker pool and returns one item per video, in input order, plus the error
// that stopped the run early (nil when it ran to the end). Once any worker
// hits an error no later video can get past (expired session, rate
// limiting) or the run context ends (explicit --timeout, Ctrl-C), the
// workers start no more videos; those are reported as not_attempted.
func downloadVideos(runCtx context.Context, cmd *cobra.Command, flags *rootFlags, lc *loom.Client, videos []loom.Video, job downloadJob) ([]downloadItem, error) {
	items := make([]downloadItem, len(videos))
	dl := loom.NewDownloader()
	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		run        = bulkOutcome{what: "download"} // only run.stop is used, guarded by mu
		progressMu sync.Mutex
	)
	// stopped reports whether the run has stopped, recording the end of
	// the run context as the stop cause.
	stopped := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if err := runCtx.Err(); err != nil {
			run.halt(err)
		}
		return run.stopped()
	}
	human := wantsHumanTable(cmd.OutOrStdout(), flags)
	jobs := make(chan int)
	for w := 0; w < job.concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				v := videos[i]
				item := downloadItem{ID: v.ID, Name: v.Name}
				if stopped() {
					mu.Lock()
					item.Status, item.Error, item.err = statusNotAttempted, run.notAttemptedMsg(), run.stop
					mu.Unlock()
					items[i] = item
					continue
				}
				item = downloadOne(runCtx, flags, lc, dl, v, job)
				if item.err != nil {
					if stop := stopCause(runCtx, item.err); stop != nil {
						mu.Lock()
						run.halt(stop)
						mu.Unlock()
					}
				}
				items[i] = item
				if human {
					progressMu.Lock()
					if item.Status == "failed" {
						fmt.Fprintf(cmd.ErrOrStderr(), "failed     %s: %s\n", v.ID, termSafe(item.Error))
					} else if !job.urlOnly {
						fmt.Fprintf(cmd.ErrOrStderr(), "%-10s %s\n", item.Status, termSafe(item.Path))
					}
					progressMu.Unlock()
				}
			}
		}()
	}
	for i := range videos {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return items, run.stop
}

// downloadOne resolves one video's signed MP4 URL (with its own API budget)
// and, unless urlOnly, streams it to disk under the run context.
func downloadOne(runCtx context.Context, flags *rootFlags, lc *loom.Client, dl *loom.Downloader, v loom.Video, job downloadJob) downloadItem {
	item := downloadItem{ID: v.ID, Name: v.Name}
	// Each signed-URL lookup gets its own API budget: a command-wide
	// deadline would expire during earlier transfers and fail every later
	// video.
	urlCtx, urlCancel := boundCtx(runCtx, flags)
	url, err := lc.DownloadURL(urlCtx, v.ID)
	urlCancel()
	if err == nil && job.urlOnly {
		item.Status, item.URL = "resolved", url
		return item
	}
	if err == nil {
		var base string
		if base, err = videoFileBase(v); err == nil {
			dest := filepath.Join(job.out, base+".mp4")
			item.Path = dest
			var res *loom.DownloadResult
			if res, err = dl.Download(runCtx, url, dest); err == nil {
				item.Status, item.Bytes, item.Size = res.Status, res.Bytes, res.Size
				return item
			}
		}
	}
	if errors.Is(err, loom.ErrNoMP4) {
		err = notFoundErr(err)
	}
	// Loom and Downloader errors carry redacted URLs only; RedactErr is a
	// second guard.
	item.Status, item.Error, item.err = "failed", loom.RedactErr(err, url), err
	return item
}

// tallyDownloads counts a download report's results and returns the run's
// outcome. stop is the error that ended the run early (from downloadVideos),
// nil when it ran to the end; a stopped run never exits 0.
func tallyDownloads(report *downloadReport, stop error) bulkOutcome {
	outcome := bulkOutcome{what: "download"}
	if stop != nil {
		outcome.halt(stop)
	}
	for _, it := range report.Results {
		switch it.Status {
		case "failed":
			report.Failed++
			report.FetchFailures = append(report.FetchFailures, itemFailure{ID: it.ID, Error: it.Error, err: it.err})
			outcome.fail(it.err)
			if it.err != nil && loom.IsRunStopping(it.err) {
				outcome.halt(it.err)
			}
		case statusNotAttempted:
			report.NotAttempted++
			outcome.halt(it.err)
		case loom.DownloadStatusSkipped:
			report.Skipped++
			outcome.ok()
		default:
			report.Downloaded++
			outcome.ok()
		}
	}
	return outcome
}
