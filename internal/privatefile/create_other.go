//go:build !windows

package privatefile

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// CreateTemp creates a non-Windows private temporary file with mode perm.
func CreateTemp(directory, prefix, suffix string, perm fs.FileMode) (*os.File, error) {
	file, err := os.CreateTemp(directory, prefix+"*"+suffix)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(perm); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}

func validatePermissions(_ string, info os.FileInfo) error {
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata == nil {
		return fmt.Errorf("file owner cannot be determined")
	}
	return validatePOSIXMetadata(info.Mode().Perm(), metadata.Uid, uint32(os.Geteuid()))
}

func validatePOSIXMetadata(mode fs.FileMode, owner, currentOwner uint32) error {
	if owner != currentOwner {
		return fmt.Errorf("file is not owned by the current user")
	}
	if mode.Perm()&^fs.FileMode(0o600) != 0 {
		return fmt.Errorf("file mode is broader than 0600")
	}
	return nil
}
