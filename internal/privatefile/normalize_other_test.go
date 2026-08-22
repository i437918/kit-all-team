//go:build !windows

package privatefile

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestNormalizeOwnerOnly_POSIXSets0600AndPreservesBytes(t *testing.T) {
	secret := []byte("HERMES_CUSTOM_LLM_API_KEY=sentinel\n")
	path := filepath.Join(testutil.TempDir(t), ".env")
	if err := os.WriteFile(path, secret, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(secret)

	if err := NormalizeOwnerOnly(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(secret) || sha256.Sum256(got) != before || !bytes.Equal(got, secret) {
		t.Fatal("secret bytes changed")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v; want 0600", info.Mode().Perm(), err)
	}
}

func TestNormalizeOwnerOnly_POSIXRejectsSymlinkLeaf(t *testing.T) {
	directory := testutil.TempDir(t)
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, ".env")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := NormalizeOwnerOnly(link); !errors.Is(err, ErrUnsafePermissions) {
		t.Fatalf("NormalizeOwnerOnly() error = %v, want ErrUnsafePermissions", err)
	}
}

func TestNormalizeOwnerOnly_POSIXRejectsInodeReplacement(t *testing.T) {
	directory := testutil.TempDir(t)
	path := filepath.Join(directory, ".env")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(directory, "replacement")
	if err := os.WriteFile(replacement, []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalHook := afterNormalizeOpen
	afterNormalizeOpen = func(string) error { return os.Rename(replacement, path) }
	defer func() { afterNormalizeOpen = originalHook }()

	if err := NormalizeOwnerOnly(path); !errors.Is(err, ErrUnsafePermissions) {
		t.Fatalf("NormalizeOwnerOnly() error = %v, want ErrUnsafePermissions", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "replacement" {
		t.Fatalf("replacement path was mutated: body=%q err=%v", got, err)
	}
}
