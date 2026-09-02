//go:build !windows

package gitx

import (
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"testing"
)

func TestHooksReady_PosixRequiresExecutableManagedHooks(t *testing.T) {
	directory := testutil.TempDir(t)
	if err := InstallHooks(directory); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "pre-commit")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	ready, err := HooksReady(directory)
	if err != nil || ready {
		t.Fatalf("HooksReady() = %v, %v; want false, nil", ready, err)
	}
}

func TestInstallHooks_PosixRepairsOnlyByteIdenticalManagedHookMode(t *testing.T) {
	directory := testutil.TempDir(t)
	if err := InstallHooks(directory); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(directory, "pre-commit")
	if err := os.Chmod(managed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallHooks(directory); err != nil {
		t.Fatalf("repair byte-identical hook: %v", err)
	}
	info, err := os.Lstat(managed)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 != 0o111 {
		t.Fatalf("repaired mode = %04o; want every execute bit", info.Mode().Perm())
	}

	tampered := filepath.Join(directory, "pre-push")
	if err := os.WriteFile(tampered, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := InstallHooks(directory); !errors.Is(err, ErrHookCollision) {
		t.Fatalf("InstallHooks(tampered) error = %v; want ErrHookCollision", err)
	}
	after, err := os.ReadFile(tampered)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Lstat(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) || afterInfo.Mode().Perm() != 0o644 {
		t.Fatalf("tampered hook changed: bytes=%q mode=%04o", after, afterInfo.Mode().Perm())
	}
}
