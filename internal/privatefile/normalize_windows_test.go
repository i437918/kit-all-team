//go:build windows

package privatefile

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"golang.org/x/sys/windows"
)

func TestNormalizeOwnerOnly_WindowsProtectsInheritedDACLAndPreservesBytes(t *testing.T) {
	secret := []byte("TEAMKIT_PUBLIC_PROVIDER_API_KEY=sentinel\n")
	path := filepath.Join(testutil.TempDir(t), ".env")
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	makeWindowsFileBroad(t, path)
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
	if err := Validate(path); err != nil {
		t.Fatalf("normalized file is not owner-only: %v", err)
	}
}

func TestNormalizeOwnerOnly_WindowsRejectsSymlinkLeaf(t *testing.T) {
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

func TestNormalizeOwnerOnly_WindowsRejectsPathReplacement(t *testing.T) {
	directory := testutil.TempDir(t)
	path := filepath.Join(directory, ".env")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(directory, "replacement")
	if err := os.WriteFile(replacement, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalHook := afterNormalizeOpen
	afterNormalizeOpen = func(string) error {
		if err := os.Rename(path, filepath.Join(directory, "opened")); err != nil {
			return err
		}
		return os.Rename(replacement, path)
	}
	defer func() { afterNormalizeOpen = originalHook }()

	if err := NormalizeOwnerOnly(path); !errors.Is(err, ErrUnsafePermissions) {
		t.Fatalf("NormalizeOwnerOnly() error = %v, want ErrUnsafePermissions", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "replacement" {
		t.Fatalf("replacement path was mutated: body=%q err=%v", got, err)
	}
}

func makeWindowsFileBroad(t *testing.T, path string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:(A;;FA;;;" + user.User.Sid.String() + ")(A;;GR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if err := Validate(path); !errors.Is(err, ErrUnsafePermissions) {
		t.Fatalf("broad fixture unexpectedly private: %v", err)
	}
}
