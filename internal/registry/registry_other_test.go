//go:build !windows

package registry

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestRegistryAtomicWrite_POSIXProtectsDirectoryTemporaryAndFinal(t *testing.T) {
	path := protectedRegistryPath(t)
	var temporaryMode fs.FileMode
	original := createRegistryTemp
	createRegistryTemp = func(directory, prefix, suffix string, perm fs.FileMode) (*os.File, error) {
		file, err := original(directory, prefix, suffix, perm)
		if err == nil {
			info, statErr := file.Stat()
			if statErr != nil {
				_ = file.Close()
				return nil, statErr
			}
			temporaryMode = info.Mode().Perm()
		}
		return file, err
	}
	defer func() { createRegistryTemp = original }()
	if err := writeRegistryAtomic(path, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeRegistryAtomic(path, []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	directoryInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := directoryInfo.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || directoryInfo.Mode().Perm() != 0o700 || temporaryMode != 0o600 || fileInfo.Mode().Perm() != 0o600 || string(body) != "second\n" {
		t.Fatalf("uid=%v dir=%o temp=%o file=%o body=%q", stat, directoryInfo.Mode().Perm(), temporaryMode, fileInfo.Mode().Perm(), body)
	}
}

func TestRegistryAtomicWrite_POSIXRejectsSymlinkWithoutTouchingSentinel(t *testing.T) {
	directory := filepath.Join(testutil.TempDir(t), "registry")
	if err := ensureRegistryDirectory(directory); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(testutil.TempDir(t), "sentinel")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "environments.json")
	if err := os.Symlink(sentinel, target); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := writeRegistryAtomic(target, []byte("changed")); err == nil {
		t.Fatal("symlink accepted")
	}
	body, err := os.ReadFile(sentinel)
	if err != nil || string(body) != "unchanged" {
		t.Fatalf("sentinel=%q err=%v", body, err)
	}
}
