//go:build !windows

package privatefile

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openPrivateFile(path string) (*os.File, error) {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, fmt.Errorf("%w: private file is redirected", ErrUnsafePermissions)
		}
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, fmt.Errorf("private file descriptor is invalid")
	}
	return file, nil
}

func validateOpenedPrivateFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("private file handle is not a regular file")
	}
	return validatePermissions("", info)
}
