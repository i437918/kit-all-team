package pathsafe

import (
	"errors"
	"fmt"
	"io"
)

// ErrTooLarge reports that a regular public file exceeded its bounded read limit.
var ErrTooLarge = errors.New("regular file exceeds read limit")

var afterRegularOpen = func(string) error { return nil }

// ReadRegular opens a public regular file without following a redirected leaf,
// validates the opened handle, and returns at most limit bytes from that handle.
// Unlike privatefile.OpenValidated it deliberately imposes no owner-only ACL.
func ReadRegular(path string, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("%w: negative limit", ErrTooLarge)
	}
	if err := ValidateRegular(path); err != nil {
		return nil, err
	}
	file, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := afterRegularOpen(path); err != nil {
		return nil, err
	}
	if err := validateOpenedRegularFile(file); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsafe, err)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}
