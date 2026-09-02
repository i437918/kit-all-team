//go:build !windows

package service

import (
	"os"
	"path/filepath"
	"testing"
)

func redirectOfficeCLIPath(t *testing.T, link, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create OfficeCLI redirected path: %v", err)
	}
}
