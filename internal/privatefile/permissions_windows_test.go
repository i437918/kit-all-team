//go:build windows

package privatefile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"golang.org/x/sys/windows"
)

func TestValidateWindowsSecurityDescriptor_RejectsNonCurrentOwner(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	current := user.User.Sid
	foreign := "S-1-5-18"
	if current.String() == foreign {
		foreign = "S-1-5-32-545"
	}
	descriptor, err := windows.SecurityDescriptorFromString("O:" + foreign + "D:P(A;;FA;;;" + current.String() + ")")
	if err != nil {
		t.Fatal(err)
	}

	if err := validateWindowsSecurityDescriptor(descriptor, current); err == nil {
		t.Fatal("foreign owner with a current-user-only DACL was accepted")
	}
}

func TestValidateWindowsSecurityDescriptor_AcceptsProtectedCurrentOwnerOnlyDACL(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	current := user.User.Sid
	descriptor, err := windows.SecurityDescriptorFromString("O:" + current.String() + "D:P(A;;FA;;;" + current.String() + ")")
	if err != nil {
		t.Fatal(err)
	}

	if err := validateWindowsSecurityDescriptor(descriptor, current); err != nil {
		t.Fatalf("current owner-only descriptor rejected: %v", err)
	}
}

func TestOpenValidated_WindowsRejectsSymlinkLeaf(t *testing.T) {
	directory := testutil.TempDir(t)
	target := filepath.Join(directory, "target")
	if err := WriteAtomic(target, []byte("canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	file, err := OpenValidated(link)
	if file != nil {
		_ = file.Close()
	}
	if !errors.Is(err, ErrUnsafePermissions) {
		t.Fatalf("OpenValidated() error = %v, want ErrUnsafePermissions", err)
	}
}

func TestOpenValidated_WindowsValidationAndReadStayBoundToOpenedHandle(t *testing.T) {
	directory := testutil.TempDir(t)
	path := filepath.Join(directory, "registry")
	if err := WriteAtomic(path, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(directory, "replacement")
	if err := os.WriteFile(replacement, []byte("unsafe"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := afterValidatedOpen
	afterValidatedOpen = func(string) error {
		backup := filepath.Join(directory, "opened")
		if err := os.Rename(path, backup); err != nil {
			return err
		}
		return os.Rename(replacement, path)
	}
	defer func() { afterValidatedOpen = original }()

	file, err := OpenValidated(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil || string(body) != "safe" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}
