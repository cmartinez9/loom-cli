package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

const (
	bulkVidA = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60"
	bulkVidB = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f61"
	bulkVidC = "8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f62"
)

const signedInMe = `{"data":{"me":{"id":"1","first_name":"Test","last_name":"User"}}}`

// fakeVideoJSON is a GetVideoCardDetails answer for one video.
func fakeVideoJSON(id, name string) string {
	return fmt.Sprintf(`{"data":{"video":{"__typename":"RegularUserVideo","id":%q,"name":%q,"createdAt":%q}}}`,
		id, name, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
}

// fakeLibraryPage is one GetLoomsForLibrary page with the given videos.
func fakeLibraryPage(videos ...[2]string) string {
	edges := make([]string, 0, len(videos))
	for _, v := range videos {
		edges = append(edges, fmt.Sprintf(`{"node":{"__typename":"RegularUserVideo","id":%q,"name":%q,"createdAt":%q}}`,
			v[0], v[1], time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)))
	}
	return `{"data":{"getLooms":{"__typename":"GetLoomsForLibraryPayload","videos":{"edges":[` + strings.Join(edges, ",") +
		`],"pageInfo":{"endCursor":"","hasNextPage":false}}}}}`
}

func TestExpandVideoArgs(t *testing.T) {
	got, err := expandVideoArgs([]string{bulkVidA + "," + bulkVidB, "latest", "https://www.loom.com/share/" + bulkVidA, "LAST"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != bulkVidA+" "+bulkVidB+" latest" {
		t.Fatalf("expandVideoArgs = %v, want split, deduped, latest normalized", got)
	}
	var ce *cliError
	if _, err := expandVideoArgs([]string{"not-a-video"}); !errors.As(err, &ce) || ce.code != 2 {
		t.Fatalf("bad id: %v, want usage error", err)
	}
	if got, err := expandVideoArgs(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("no args: %v %v", got, err)
	}
}

func TestBulkOutcomeExit(t *testing.T) {
	missing := videoNotFoundErr(bulkVidA)
	apiFail := errors.New("HTTP 500")
	cases := []struct {
		name string
		set  func(o *bulkOutcome)
		want int
	}{
		{"all ok", func(o *bulkOutcome) { o.ok(); o.ok() }, 0},
		{"partial failure", func(o *bulkOutcome) { o.ok(); o.fail(apiFail) }, 0},
		{"every item missing", func(o *bulkOutcome) { o.fail(missing); o.fail(missing) }, 3},
		{"every item failed differently", func(o *bulkOutcome) { o.fail(missing); o.fail(apiFail) }, 5},
		{"session expired stops", func(o *bulkOutcome) { o.ok(); o.halt(loom.ErrSessionExpired) }, 4},
		{"rate limit stops", func(o *bulkOutcome) { o.ok(); o.halt(loomErr(&loomRateLimit)) }, 7},
		{"explicit timeout stops", func(o *bulkOutcome) { o.ok(); o.halt(context.DeadlineExceeded) }, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := bulkOutcome{what: "export"}
			tc.set(&o)
			if got := exitCode(o.exitErr()); got != tc.want {
				t.Fatalf("exit %d, want %d (%v)", got, tc.want, o.exitErr())
			}
		})
	}
}

func TestStopCause(t *testing.T) {
	live := context.Background()
	if stopCause(live, context.DeadlineExceeded) != nil {
		t.Fatal("a per-item deadline must not stop the run while the run context is alive")
	}
	if stopCause(live, loom.ErrSessionExpired) == nil {
		t.Fatal("an expired session stops the run")
	}
	done, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(stopCause(done, errors.New("x")), context.Canceled) {
		t.Fatal("an ended run context stops the run")
	}
}

// TestExportSelectionFailures: an unknown named video is a per-item failure
// listed first; the export still writes the others and exits 0. A session
// that expires during selection stops the command with exit 4.
func TestExportSelectionFailures(t *testing.T) {
	respond := func(op string, vars map[string]any) string {
		switch op {
		case loom.OpMe:
			return signedInMe
		case loom.OpVideoDetails:
			if vars["videoId"] == bulkVidA {
				return `{"data":{"video":{"__typename":"NotFoundError","message":"not found"}}}`
			}
			return fakeVideoJSON(fmt.Sprint(vars["videoId"]), "Weekly demo")
		}
		return `{"data":{}}`
	}
	fakeLoomServerVars(t, respond)
	out, stderr, err := runCLI(t, "videos", "export", bulkVidA, bulkVidB, "--out", t.TempDir(), "--insecure", "--json")
	if exitCode(err) != 0 {
		t.Fatalf("exit %d (%v), want 0 when one video exported\nstderr: %s", exitCode(err), err, stderr)
	}
	var rep exportReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if len(rep.Results) != 2 || rep.Results[0].ID != bulkVidA || rep.Results[0].Status != "failed" || !strings.Contains(rep.Results[0].Error, "not found") {
		t.Fatalf("results = %+v, want the missing video failed first", rep.Results)
	}
	if rep.Results[1].ID != bulkVidB || rep.Results[1].Status != "written" || rep.Failed != 1 || rep.Written != 1 {
		t.Fatalf("results = %+v", rep)
	}

	fakeLoomServer(t, func(op string) string {
		return `{"errors":[{"message":"User is not logged in","extensions":{"code":"UNAUTHENTICATED"}}],"data":null}`
	})
	_, _, err = runCLI(t, "videos", "export", bulkVidA, bulkVidB, "--out", t.TempDir(), "--insecure", "--json")
	if exitCode(err) != 4 {
		t.Fatalf("signed out during selection: exit %d (%v), want 4", exitCode(err), err)
	}
}

// TestDownloadFailureIndexing: selection failures and per-video failures
// each stay attached to their own video, and when every video fails the
// exit code comes from the failures (3: missing video, no MP4).
func TestDownloadFailureIndexing(t *testing.T) {
	fakeLoomServerVars(t, func(op string, vars map[string]any) string {
		switch op {
		case loom.OpMe:
			return signedInMe
		case loom.OpVideoDetails:
			if vars["videoId"] == bulkVidA {
				return `{"data":{"video":null}}`
			}
			return fakeVideoJSON(fmt.Sprint(vars["videoId"]), "Clip")
		case loom.OpDownloadURL:
			return `{"data":{"getVideoTranscodedUrl":{"__typename":"EntityNotFoundError"}}}`
		}
		return `{"data":{}}`
	})
	out, stderr, err := runCLI(t, "download", bulkVidA, bulkVidB, bulkVidC, "--out", t.TempDir(), "--insecure", "--json")
	if exitCode(err) != 3 {
		t.Fatalf("exit %d (%v), want 3\nstderr: %s", exitCode(err), err, stderr)
	}
	var rep downloadReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if len(rep.Results) != 3 || rep.Failed != 3 {
		t.Fatalf("report = %+v", rep)
	}
	want := map[string]string{bulkVidA: "not found", bulkVidB: "MP4", bulkVidC: "MP4"}
	for i, id := range []string{bulkVidA, bulkVidB, bulkVidC} {
		r := rep.Results[i]
		if r.ID != id || !strings.Contains(r.Error, want[id]) {
			t.Errorf("result %d = %+v, want %s failing with %q", i, r, id, want[id])
		}
		if f := rep.FetchFailures[i]; f.ID != id || f.Error != r.Error {
			t.Errorf("fetch failure %d = %+v, want it to match result %+v", i, f, r)
		}
	}
}

// TestBulkRenameEarlyStop: once a rename hits an expired session, the
// remaining renames are not attempted, are listed as not_attempted, and the
// command exits 4 even though an earlier rename succeeded.
func TestBulkRenameEarlyStop(t *testing.T) {
	renames := 0
	fakeLoomServer(t, func(op string) string {
		switch op {
		case loom.OpLibrary:
			return fakeLibraryPage([2]string{bulkVidA, "Untitled one"}, [2]string{bulkVidB, "Untitled two"}, [2]string{bulkVidC, "Untitled three"})
		case loom.OpUpdateName:
			renames++
			if renames == 1 {
				return `{"data":{"updateVideoName":{"__typename":"RegularUserVideo","id":"x"}}}`
			}
			return `{"data":{"updateVideoName":{"__typename":"UserNotAuthorizedError","message":"no"}}}`
		case loom.OpMe:
			return `{"data":{"me":null}}`
		}
		return `{"data":{}}`
	})
	out, stderr, err := runCLI(t, "videos", "rename", "--match", "^Untitled", "--replace", "Draft", "--yes", "--insecure", "--json")
	if exitCode(err) != 4 {
		t.Fatalf("exit %d (%v), want 4\nstderr: %s\nout: %s", exitCode(err), err, stderr, out)
	}
	var rep bulkRenameReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if renames != 2 || rep.Renamed != 1 || rep.Failed != 1 || rep.NotAttempted != 1 {
		t.Fatalf("renames sent %d, report %+v; want 1 renamed, 1 failed, 1 not attempted", renames, rep)
	}
	if last := rep.Results[2]; last.Status != "not_attempted" || last.ID != bulkVidC {
		t.Fatalf("last result = %+v", last)
	}
}

// TestBulkSelectionSources: bulk writes take the shared selector flags:
// --from-file ids come first, then the --all scan capped by --limit.
func TestBulkSelectionSources(t *testing.T) {
	fakeLoomServer(t, func(op string) string {
		if op == loom.OpLibrary {
			return fakeLibraryPage([2]string{bulkVidA, "One"}, [2]string{bulkVidB, "Two"}, [2]string{bulkVidC, "Three"})
		}
		return `{"data":{}}`
	})
	ids := t.TempDir() + "/ids.txt"
	if err := writeTestFile(ids, "# targets\n"+bulkVidC+"\n"); err != nil {
		t.Fatal(err)
	}
	out, _, err := runCLI(t, "videos", "archive", "--from-file", ids, "--all", "--limit", "1", "--dry-run", "--insecure", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var plan bulkPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if plan.Count != 2 || plan.Targets[0].ID != bulkVidC || plan.Targets[1].ID != bulkVidA {
		t.Fatalf("plan targets = %+v, want the file's id then one scanned video", plan.Targets)
	}
}

// TestDownloadStopExitsNonZero: when an explicit --timeout or Ctrl-C ends
// the run while transfers are in flight, no video is left not_attempted,
// yet the run must still exit non-zero; only a run that ended on its own
// exits 0 with its failures listed.
func TestDownloadStopExitsNonZero(t *testing.T) {
	inFlight := func() downloadReport {
		return downloadReport{Results: []downloadItem{
			{ID: bulkVidA, Status: loom.DownloadStatusDownloaded},
			{ID: bulkVidB, Status: "failed", Error: "context deadline exceeded", err: context.DeadlineExceeded},
		}}
	}
	rep := inFlight()
	if o := tallyDownloads(&rep, nil); exitCode(o.exitErr()) != 0 {
		t.Fatalf("a finished run with one failure: exit %d, want 0", exitCode(o.exitErr()))
	}
	rep = inFlight()
	o := tallyDownloads(&rep, context.DeadlineExceeded)
	if got := exitCode(o.exitErr()); got != 5 || rep.Downloaded != 1 || rep.Failed != 1 || rep.NotAttempted != 0 {
		t.Fatalf("timeout mid-transfer: exit %d (%v), report %+v; want exit 5", got, o.exitErr(), rep)
	}
	if !strings.Contains(o.exitErr().Error(), "--timeout") {
		t.Fatalf("timeout message = %v", o.exitErr())
	}

	// A Ctrl-C before any video starts: every video is not_attempted and
	// the returned stop error makes the run exit non-zero without saying
	// anything about --timeout.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	videos := []loom.Video{{ID: bulkVidA}, {ID: bulkVidB}, {ID: bulkVidC}}
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	items, stop := downloadVideos(ctx, cmd, &rootFlags{}, nil, videos, downloadJob{out: t.TempDir(), concurrency: 2})
	if !errors.Is(stop, context.Canceled) {
		t.Fatalf("stop = %v, want context.Canceled", stop)
	}
	rep = downloadReport{Results: items}
	o = tallyDownloads(&rep, stop)
	if rep.NotAttempted != 3 || exitCode(o.exitErr()) == 0 {
		t.Fatalf("report %+v, exit %d; want 3 not attempted and a non-zero exit", rep, exitCode(o.exitErr()))
	}
	for _, it := range items {
		if it.Status != statusNotAttempted || !strings.Contains(it.Error, "the download stopped early") {
			t.Fatalf("item = %+v", it)
		}
	}
	if msg := o.exitErr().Error(); !strings.Contains(msg, "interrupted") || strings.Contains(msg, "--timeout") {
		t.Fatalf("Ctrl-C message = %q", msg)
	}
}
