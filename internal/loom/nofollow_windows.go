//go:build windows

package loom

import "os"

// openNoFollow refuses a symbolic link at path and opens it. Windows has no
// O_NOFOLLOW; the Lstat check narrows but does not close the race.
func openNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	if err := refuseSymlink(path); err != nil {
		return nil, err
	}
	return os.OpenFile(path, flag, perm)
}
