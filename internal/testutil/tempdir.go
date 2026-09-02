// Package testutil provides narrowly scoped fixtures for Team Kit tests.
package testutil

import (
	"path/filepath"
	"testing"
)

// TempDir returns a temporary directory without a platform redirect prefix.
// It is for test fixtures only: production paths must remain uncanonicalized
// so their safety validation can reject redirected path components.
func TempDir(t *testing.T) string {
	t.Helper()
	return resolveTempPath(t, t.TempDir())
}

func resolveTempPath(t *testing.T, directory string) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatalf("resolve temporary directory: %v", err)
	}
	return directory
}
