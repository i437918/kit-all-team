package privatefile

import (
	"fmt"
	"os"
)

var afterValidatedOpen = func(string) error { return nil }

// OpenValidated opens an existing owner-only regular file without following a
// redirected leaf and validates type and permissions on the opened handle.
func OpenValidated(path string) (*os.File, error) {
	file, err := openPrivateFile(path)
	if err != nil {
		return nil, err
	}
	closeOnError := func(err error) (*os.File, error) {
		_ = file.Close()
		return nil, err
	}
	if err := afterValidatedOpen(path); err != nil {
		return closeOnError(err)
	}
	if err := validateOpenedPrivateFile(file); err != nil {
		return closeOnError(fmt.Errorf("%w: %v", ErrUnsafePermissions, err))
	}
	return file, nil
}
