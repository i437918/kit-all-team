//go:build windows

package registry

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"golang.org/x/sys/windows"
)

func requireRegistryFullAccessDACL(t *testing.T, path string) {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	current, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(current.User.Sid) {
		t.Fatalf("owner=%v err=%v", owner, err)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("control=%v err=%v", control, err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("dacl=%v err=%v", dacl, err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !sid.Equals(current.User.Sid) || ace.Mask != registryFileAllAccess {
		t.Fatalf("type=%d sid=%v mask=%#x want=%#x", ace.Header.AceType, sid, ace.Mask, registryFileAllAccess)
	}
}

func TestRegistryAtomicWrite_WindowsProtectsDirectoryTemporaryAndFinal(t *testing.T) {
	path := protectedRegistryPath(t)
	original := createRegistryTemp
	temporaryChecks := 0
	createRegistryTemp = func(directory, prefix, suffix string, perm fs.FileMode) (*os.File, error) {
		file, err := original(directory, prefix, suffix, perm)
		if err == nil {
			requireRegistryFullAccessDACL(t, file.Name())
			temporaryChecks++
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
	requireRegistryFullAccessDACL(t, filepath.Dir(path))
	requireRegistryFullAccessDACL(t, path)
	body, err := os.ReadFile(path)
	if err != nil || temporaryChecks != 2 || string(body) != "second\n" {
		t.Fatalf("checks=%d body=%q err=%v", temporaryChecks, body, err)
	}
}

func TestRegistryAtomicWrite_WindowsRejectsJunctionDirectory(t *testing.T) {
	parent, external := testutil.TempDir(t), testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(parent, "registry")
	output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput()
	if err != nil {
		t.Fatalf("mklink: %v: %s", err, output)
	}
	writeErr := writeRegistryAtomic(filepath.Join(junction, "environments.json"), []byte("changed"))
	if writeErr == nil || (!errors.Is(writeErr, pathsafe.ErrUnsafe) && !errors.Is(writeErr, privatefile.ErrUnsafePermissions)) {
		t.Fatalf("junction accepted or misclassified: %v", writeErr)
	}
	body, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(body) != "unchanged" {
		t.Fatalf("sentinel=%q err=%v writeErr=%v", body, readErr, writeErr)
	}
}

func TestStoreLoad_WindowsRejectsSymlinkLeafWithoutReadingCanary(t *testing.T) {
	path := protectedRegistryPath(t)
	target := protectedRegistryFixture(t, []byte(`{"schema_version":1,"homes":[]}`))
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, state, loadErr := New(path).Load(context.Background())
	if state != LoadUnavailable || !errors.Is(loadErr, privatefile.ErrUnsafePermissions) {
		t.Fatalf("state=%v err=%v", state, loadErr)
	}
	after, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("canary changed: before=%q after=%q err=%v", before, after, err)
	}
}

func TestStoreLoad_WindowsRejectsJunctionLeafWithoutReadingCanary(t *testing.T) {
	path := protectedRegistryPath(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", path, external).CombinedOutput()
	if err != nil {
		t.Fatalf("mklink: %v: %s", err, output)
	}
	_, state, loadErr := New(path).Load(context.Background())
	if state != LoadUnavailable || !errors.Is(loadErr, privatefile.ErrUnsafePermissions) {
		t.Fatalf("state=%v err=%v", state, loadErr)
	}
	body, err := os.ReadFile(sentinel)
	if err != nil || string(body) != "unchanged" {
		t.Fatalf("sentinel=%q err=%v", body, err)
	}
}
