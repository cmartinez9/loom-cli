package cli

// The local store as every command sees it: where it lives (--db), whether
// it exists, the one missing-mirror hint, opening it, and the --data-source
// policy for commands that can answer from it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
	"github.com/cmartinez9/loom-cli/internal/store"
)

// resolveDBPath returns --db, or the default store path when it is empty.
func resolveDBPath(dbPath string) string {
	if dbPath == "" {
		return defaultDBPath("loom-pp-cli")
	}
	return dbPath
}

// localStoreExists resolves --db and reports whether the local mirror has
// been created there.
func localStoreExists(dbPath string) (string, bool) {
	path := resolveDBPath(dbPath)
	_, err := os.Stat(path)
	return path, !os.IsNotExist(err)
}

// missingMirrorHint tells the user there is no local mirror at path and
// which commands create it: the first as "run:", any others as "then:".
func missingMirrorHint(w io.Writer, path string, next ...string) {
	fmt.Fprintf(w, "no local mirror at %s\n", path)
	for i, n := range next {
		label := "then"
		if i == 0 {
			label = "run"
		}
		fmt.Fprintf(w, "%s: %s\n", label, n)
	}
}

// openLoomStore opens (creating if needed) the local store and ensures the
// Loom tables exist.
func openLoomStore(ctx context.Context, dbPath string) (*store.Store, error) {
	db, err := store.OpenWithContext(ctx, resolveDBPath(dbPath))
	if err != nil {
		return nil, fmt.Errorf("opening local database: %w", err)
	}
	if err := db.EnsureLoomSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// The one rule for a local read that finds nothing:
//   - no local store at all: an empty result, a stderr hint naming the
//     commands that create it, and exit 0 (missingLocalMirror for lists,
//     readVideoLocal for per-video reads, and grep, digest and hydrate);
//   - a store that exists but has no such video (including "latest" against
//     a store with no active videos): exit 3 (localVideoMissingErr).

// missingLocalMirror is the missing-store guard of the list commands: when
// the store does not exist it prints the hint (naming the sync resource
// that fills it) and the empty result, and returns true.
func missingLocalMirror(cmd *cobra.Command, flags *rootFlags, dbPath, resource string, empty any) (bool, error) {
	path, ok := localStoreExists(dbPath)
	if ok {
		return false, nil
	}
	missingMirrorHint(cmd.ErrOrStderr(), path, "loom-pp-cli sync --resources "+resource+" --db "+path)
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return true, printJSONFiltered(cmd.OutOrStdout(), empty, flags)
	}
	return true, nil
}

// localVideoMissingErr is the exit-3 error for a video the local store does
// not have.
func localVideoMissingErr(id string) error {
	return notFoundErr(fmt.Errorf("video %s is not in the local store\nhint: run 'loom-pp-cli sync' (and 'hydrate' for transcripts and AI artifacts), or use --data-source live", id))
}

// readLocalStore opens the store read-only for one read.
func readLocalStore[T any](ctx context.Context, dbPath string, read func(*store.Store) (T, error)) (T, error) {
	var zero T
	db, err := store.OpenReadOnlyContext(ctx, resolveDBPath(dbPath))
	if err != nil {
		return zero, fmt.Errorf("opening local store: %w", err)
	}
	defer db.Close()
	return read(db)
}

// readSyncedVideos reads the synced videos, newest first.
func readSyncedVideos(ctx context.Context, dbPath string) ([]loom.Video, error) {
	return readLocalStore(ctx, dbPath, func(db *store.Store) ([]loom.Video, error) { return db.SyncedVideos(ctx) })
}

// readSyncedFolders reads the synced folders.
func readSyncedFolders(ctx context.Context, dbPath string) ([]loom.Folder, error) {
	return readLocalStore(ctx, dbPath, func(db *store.Store) ([]loom.Folder, error) { return db.SyncedFolders(ctx) })
}

// readSyncedSpaces reads the synced spaces.
func readSyncedSpaces(ctx context.Context, dbPath string) ([]loom.Space, error) {
	return readLocalStore(ctx, dbPath, func(db *store.Store) ([]loom.Space, error) { return db.SyncedSpaces(ctx) })
}

// dataSourceMode is the normalized --data-source value: auto, live or local.
func dataSourceMode(flags *rootFlags) string {
	mode := strings.ToLower(strings.TrimSpace(flags.dataSource))
	if mode == "" {
		return sourceAuto
	}
	return mode
}

// Data-source policies a command declares in its pp:data-source annotation.
// The annotation is the one statement of which sources a command supports,
// and checkDataSource is the one place that enforces it:
//
//	auto  -> live, local and auto (live with a local fallback) all work
//	live  -> Loom only; --data-source local is a usage error (exit 2)
//	local -> the local store only; --data-source live is a usage error
const (
	sourceAuto  = "auto"
	sourceLive  = "live"
	sourceLocal = "local"
)

// checkDataSource enforces the command's declared data-source policy
// against --data-source. Every hand-written read calls it right after its
// --dry-run short-circuit; the per-video reads get it from runVideoRead.
func checkDataSource(cmd *cobra.Command, flags *rootFlags) error {
	mode := dataSourceMode(flags)
	name := strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
	switch commandDataSourceAnnotation(cmd) {
	case sourceLive:
		if mode == sourceLocal {
			return usageErr(fmt.Errorf("%s reads live Loom data only and has no local copy; drop --data-source local (list commands, transcripts, summaries, chapters, grep and digest can read the local store)", name))
		}
	case sourceLocal:
		if mode == sourceLive {
			return usageErr(fmt.Errorf("%s reads only the local store (filled by 'sync' and 'hydrate'); drop --data-source live", name))
		}
	}
	return nil
}

// localFallbackNote tells the user results came from the local store
// because the Loom session expired.
func localFallbackNote(cmd *cobra.Command, dbPath, what string) {
	fmt.Fprintf(cmd.ErrOrStderr(), "note: Loom session expired; showing the %s saved in the local store (%s). Run 'loom-pp-cli auth login --chrome' for live data.\n", what, dbPath)
}

// withDataSource applies the --data-source policy for list commands:
// local runs local(); live runs live(); auto runs live() and falls back to
// local() (with a note) only when the Loom session has expired and a local
// mirror exists. live() returns its fetch error unmapped.
func withDataSource(cmd *cobra.Command, flags *rootFlags, dbPath, what string, live, local func() error) error {
	if err := checkDataSource(cmd, flags); err != nil {
		return err
	}
	mode := dataSourceMode(flags)
	if mode == sourceLocal {
		return local()
	}
	err := live()
	if err == nil {
		return nil
	}
	if mode == sourceAuto && errors.Is(err, loom.ErrSessionExpired) {
		if path, ok := localStoreExists(dbPath); ok {
			localFallbackNote(cmd, path, what)
			return local()
		}
	}
	return loomErr(err)
}
