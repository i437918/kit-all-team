// Package pathsafe rejects filesystem paths that cross symbolic links or
// platform-specific redirection points before Team Kit mutates them.
package pathsafe

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrUnsafe reports a path that is empty, has a redirected component, or has
// an existing component of the wrong filesystem type.
var ErrUnsafe = errors.New("unsafe filesystem path")

// ValidateDirectory checks every existing component of path without following
// symbolic links or Windows reparse points. A missing final directory is valid.
func ValidateDirectory(path string) error {
	info, exists, err := validateComponents(path)
	if err != nil {
		return err
	}
	if exists && !info.IsDir() {
		return unsafeError(path, "not a directory")
	}
	return nil
}

// ValidateRegular checks every existing component of path without following
// symbolic links or Windows reparse points. A missing final file is valid.
func ValidateRegular(path string) error {
	info, exists, err := validateComponents(path)
	if err != nil {
		return err
	}
	if exists && !info.Mode().IsRegular() {
		return unsafeError(path, "not a regular file")
	}
	return nil
}

// ComparisonKey returns a platform-appropriate key for comparing an absolute
// directory path after validating every existing component.
func ComparisonKey(path string) (string, error) {
	canonical, err := CanonicalPath(path)
	if err != nil {
		return "", err
	}
	return comparisonKey(canonical), nil
}

// CanonicalPath returns the canonical form of an absolute directory path
// after validating every existing component.
func CanonicalPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", unsafeError(path, "canonicalization requires an absolute path")
	}
	clean := filepath.Clean(path)
	if err := ValidateDirectory(clean); err != nil {
		return "", err
	}
	return canonicalPath(clean)
}

func comparisonPath(path string) (string, error) { return ComparisonKey(path) }

// Overlaps reports whether left and right are the same canonical location or
// either path is an ancestor of the other. Both paths must be absolute.
func Overlaps(left, right string) (bool, error) {
	if !filepath.IsAbs(left) || !filepath.IsAbs(right) {
		return false, unsafeError(left+" | "+right, "overlap comparison requires absolute paths")
	}
	left, err := comparisonPath(filepath.Clean(left))
	if err != nil {
		return false, err
	}
	right, err = comparisonPath(filepath.Clean(right))
	if err != nil {
		return false, err
	}
	return containsPath(left, right) || containsPath(right, left), nil
}

func containsPath(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// EnsureDirectory creates path one component at a time and validates each
// component before and after creation.
func EnsureDirectory(path string, perm fs.FileMode) error {
	if err := ValidateDirectory(path); err != nil {
		return err
	}
	clean := filepath.Clean(path)
	info, err := os.Lstat(clean)
	if err == nil {
		if !info.IsDir() || unsafeComponent(info) {
			return unsafeError(clean, "directory is redirected")
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(clean)
	if parent == clean {
		return unsafeError(clean, "filesystem root does not exist")
	}
	if err := EnsureDirectory(parent, perm); err != nil {
		return err
	}
	if err := os.Mkdir(clean, perm); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return ValidateDirectory(clean)
}

func validateComponents(path string) (os.FileInfo, bool, error) {
	return validateComponentsWithLstat(path, os.Lstat)
}

func validateComponentsWithLstat(path string, lstat func(string) (os.FileInfo, error)) (os.FileInfo, bool, error) {
	if path == "" {
		return nil, false, unsafeError(path, "empty path")
	}
	clean := filepath.Clean(path)
	var final os.FileInfo
	finalExists := false
	for component := clean; ; component = filepath.Dir(component) {
		info, err := lstat(component)
		if err == nil {
			if unsafeComponent(info) {
				return nil, false, unsafeError(component, "redirected component")
			}
			if component == clean {
				final, finalExists = info, true
			} else if !info.IsDir() {
				return nil, false, unsafeError(component, "ancestor is not a directory")
			}
		} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return nil, false, err
		}
		parent := filepath.Dir(component)
		if parent == component {
			break
		}
	}
	return final, finalExists, nil
}

func unsafeError(path, reason string) error {
	return fmt.Errorf("%w: %s: %s", ErrUnsafe, reason, path)
}
