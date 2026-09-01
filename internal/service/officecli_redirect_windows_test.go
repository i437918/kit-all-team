//go:build windows

package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func redirectOfficeCLIPath(t *testing.T, link, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create OfficeCLI junction: %v: %s", err, output)
	}
}
