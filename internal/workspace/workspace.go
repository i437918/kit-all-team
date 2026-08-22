// Package workspace manages the non-secret files that describe one Team Kit workspace.
package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

const ownerFile = ".teamkit/owner"

// ErrChanged reports that public workspace identity changed after selection.
var ErrChanged = errors.New("WORKSPACE_CHANGED")

// State classifies a workspace directory without modifying it.
type State string

const (
	// Empty means the directory has no entries.
	Empty State = "empty"
	// Managed means the directory contains only Team Kit ownership metadata.
	Managed State = "managed"
	// NonEmpty means the directory contains data Team Kit must not assume it owns.
	NonEmpty State = "nonempty"
)

// Error identifies a safe, stable error condition.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// ErrorCode returns the stable code for err, or an empty string for ordinary errors.
func ErrorCode(err error) string {
	var coded *Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}

// Classify reports whether root is empty, Team Kit managed, or contains foreign data.
func Classify(root string) (State, error) {
	if err := pathsafe.ValidateDirectory(root); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return Empty, nil
	}
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return Empty, nil
	}
	if len(entries) == 1 && entries[0].Name() == ".teamkit" {
		metadata := filepath.Join(root, ".teamkit")
		owner := filepath.Join(root, ownerFile)
		if err := pathsafe.ValidateDirectory(metadata); err != nil {
			return "", err
		}
		if err := pathsafe.ValidateRegular(owner); err != nil {
			return "", err
		}
		if _, err := os.Lstat(owner); err == nil {
			return Managed, nil
		}
	}
	return NonEmpty, nil
}

// EnsureOwner records project as the sole project allowed to use root.
func EnsureOwner(root, project string) error {
	if strings.TrimSpace(project) == "" || strings.ContainsAny(project, "\\/") {
		return &Error{Code: "PROJECT_INVALID", Err: fmt.Errorf("project name is invalid")}
	}
	if err := pathsafe.EnsureDirectory(root, 0o700); err != nil {
		return err
	}
	if err := pathsafe.EnsureDirectory(filepath.Join(root, ".teamkit"), 0o700); err != nil {
		return err
	}
	path := filepath.Join(root, ownerFile)
	if err := pathsafe.ValidateRegular(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if strings.TrimSpace(string(data)) != project {
			return &Error{Code: "WORKSPACE_OWNED", Err: fmt.Errorf("workspace belongs to another project")}
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	owner, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.TrimSpace(string(data)) != project {
			return &Error{Code: "WORKSPACE_OWNED", Err: fmt.Errorf("workspace belongs to another project")}
		}
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := owner.Write([]byte(project + "\n")); err != nil {
		_ = owner.Close()
		return err
	}
	if err := owner.Sync(); err != nil {
		_ = owner.Close()
		return err
	}
	return owner.Close()
}

// WriteFileAtomic replaces path with data after a complete temporary write.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := pathsafe.ValidateRegular(path); err != nil {
		return err
	}
	if err := pathsafe.EnsureDirectory(dir, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".teamkit-*")
	if err != nil {
		return err
	}
	tempPath := temporary.Name()
	defer os.Remove(tempPath)
	if err := temporary.Chmod(perm); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := pathsafe.ValidateDirectory(dir); err != nil {
		return err
	}
	if err := pathsafe.ValidateRegular(path); err != nil {
		return err
	}
	if err := pathsafe.ValidateRegular(tempPath); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
