// Package operationlock serializes mutations for one Team Kit workspace.
package operationlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
)

// ErrOperationInProgress reports a nonblocking conflict with another process.
var ErrOperationInProgress = errors.New("OPERATION_IN_PROGRESS")

// Lock owns one advisory operating-system lock until Close or process death.
type Lock struct {
	file *os.File
	once sync.Once
	err  error
}

// Acquire obtains the exclusive mutation lock for one canonical workspace.
// The caller must establish and validate workspace ownership before calling it.
func Acquire(workspaceRoot string) (*Lock, error) {
	if !filepath.IsAbs(workspaceRoot) {
		return nil, fmt.Errorf("OPERATION_LOCK_ROOT_INVALID")
	}
	metadata := filepath.Join(filepath.Clean(workspaceRoot), ".teamkit")
	if err := pathsafe.ValidateDirectory(metadata); err != nil {
		return nil, err
	}
	info, err := os.Lstat(metadata)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, pathsafe.ErrUnsafe
	}
	path := filepath.Join(metadata, "operation.lock")
	if err := pathsafe.ValidateRegular(path); err != nil {
		return nil, err
	}
	file, err := acquireFile(path)
	if err != nil {
		return nil, err
	}
	lock := &Lock{file: file}
	if err := validateLockedFile(path, file); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return lock, nil
}

func validateLockedFile(path string, file *os.File) error {
	if err := pathsafe.ValidateRegular(path); err != nil {
		return err
	}
	if err := privatefile.Validate(path); err != nil {
		return err
	}
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	named, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || opened.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, named) {
		return fmt.Errorf("%w: operation lock identity changed", pathsafe.ErrUnsafe)
	}
	return validateLockMode(opened)
}

// Close releases the advisory lock. It is safe to call more than once.
func (l *Lock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		l.err = releaseFile(l.file)
	})
	return l.err
}
