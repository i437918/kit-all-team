// Package privatefile creates atomic files whose contents are readable only by
// the current user.
package privatefile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrUnsafePermissions identifies a pre-existing secret file that is not
// provably restricted to the current user.
var ErrUnsafePermissions = errors.New("SECRET_FILE_PERMISSIONS_UNSAFE")

// Validate verifies that path is a non-redirected regular file with private
// platform permissions. A missing file is valid and represents no secrets.
func Validate(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: secret path is not a regular file", ErrUnsafePermissions)
	}
	if err := validatePermissions(path, info); err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafePermissions, err)
	}
	return nil
}

// WriteAtomic replaces path with data using a private temporary file in the
// same directory, so the final rename cannot cross filesystems.
func WriteAtomic(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := CreateTemp(filepath.Dir(path), ".teamkit-private-", "", perm)
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
