//go:build !windows

package loom

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// openNoFollow opens path without following a symbolic link at its last
// component: the kernel refuses the open (ELOOP) instead of writing through
// a link planted between a check and the open.
func openNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	f, err := os.OpenFile(filepath.Clean(path), flag|syscall.O_NOFOLLOW, perm)
	if err != nil && errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("refusing to open %s: it is a symbolic link", path)
	}
	return f, err
}
