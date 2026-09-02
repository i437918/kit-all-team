//go:build !windows

package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
)

func ensureRegistryDirectory(path string) error {
	if err := pathsafe.EnsureDirectory(path, 0o700); err != nil {
		return err
	}
	return validateRegistryDirectory(path)
}

func validateRegistryDirectory(path string) error {
	if err := pathsafe.ValidateDirectory(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: registry directory is redirected", privatefile.ErrUnsafePermissions)
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata == nil {
		return fmt.Errorf("%w: registry directory owner cannot be determined", privatefile.ErrUnsafePermissions)
	}
	if info.Mode().Perm() != fs.FileMode(0o700) {
		return fmt.Errorf("%w: registry directory mode must equal 0700", privatefile.ErrUnsafePermissions)
	}
	if int(metadata.Uid) != os.Geteuid() {
		return fmt.Errorf("%w: registry directory is not owned by current user", privatefile.ErrUnsafePermissions)
	}
	return nil
}

func replaceRegistryFile(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil && !errors.Is(closeErr, fs.ErrClosed) {
		return closeErr
	}
	return nil
}
