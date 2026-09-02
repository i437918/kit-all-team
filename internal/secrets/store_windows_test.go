//go:build windows

package secrets

import (
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestStoreSave_WindowsEnvHasProtectedOwnerOnlyDACL(t *testing.T) {
	home := testutil.TempDir(t)
	if err := writePrivateAtomic(filepath.Join(home, ".env"), []byte("API_KEY=old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.Save(map[string]string{"API_KEY": "teamkit-secret-canary"})
	if err != nil {
		t.Fatal(err)
	}
	assertProtectedOwnerOnlyDACL(t, path)
}

func TestStoreSave_WindowsRejectsInheritedBroadDACLWithoutReplacingSecret(t *testing.T) {
	home := testutil.TempDir(t)
	path := filepath.Join(home, ".env")
	const original = "API_KEY=teamkit-secret-canary\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Save(map[string]string{"API_KEY": "replacement"})
	if err == nil || !strings.Contains(err.Error(), "SECRET_FILE_PERMISSIONS_UNSAFE") {
		t.Fatalf("Save error=%v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || string(data) != original {
		t.Fatalf("unsafe secret was changed to %q: %v", data, readErr)
	}
}

func TestStoreLoadAndSave_WindowsRevalidateJunctionApplicationHome(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*Store) error
	}{
		{name: "load", run: func(store *Store) error { _, err := store.Load("API_KEY"); return err }},
		{name: "save", run: func(store *Store) error {
			_, err := store.Save(map[string]string{"API_KEY": "replacement"})
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			root := testutil.TempDir(t)
			home := filepath.Join(root, "application")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(home)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(home); err != nil {
				t.Fatal(err)
			}
			external := testutil.TempDir(t)
			sentinel := filepath.Join(external, ".env")
			if err := writePrivateAtomic(sentinel, []byte("API_KEY=teamkit-secret-canary\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", home, external).CombinedOutput(); err != nil {
				t.Fatalf("create junction: %v: %s", err, output)
			}

			if err := operation.run(store); err == nil {
				t.Fatal("secret operation followed a junction application home")
			}
			data, err := os.ReadFile(sentinel)
			if err != nil || string(data) != "API_KEY=teamkit-secret-canary\n" {
				t.Fatalf("external secret changed to %q: %v", data, err)
			}
		})
	}
}

func TestNewStore_WindowsRejectsJunctionApplicationHome(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "application")
	external := testutil.TempDir(t)
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", home, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	store, err := NewStore(home)
	if err == nil || store != nil {
		t.Fatalf("NewStore(%q) = %#v, %v; want junction rejection", home, store, err)
	}
}

func TestStoreLoad_WindowsRejectsInheritedBroadDACL(t *testing.T) {
	home := testutil.TempDir(t)
	path := filepath.Join(home, ".env")
	if err := os.WriteFile(path, []byte("API_KEY=teamkit-secret-canary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("API_KEY"); err == nil || !strings.Contains(err.Error(), "SECRET_FILE_PERMISSIONS_UNSAFE") {
		t.Fatalf("Load error=%v", err)
	} else if strings.Contains(err.Error(), "teamkit-secret-canary") {
		t.Fatalf("permission error leaked secret: %v", err)
	}
}

func assertProtectedOwnerOnlyDACL(t *testing.T, path string) {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("DACL for %q inherits access entries", path)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount != 1 {
		if dacl == nil {
			t.Fatalf("DACL for %q is nil", path)
		}
		t.Fatalf("DACL for %q has %d entries; want one owner entry", path, dacl.AceCount)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		t.Fatalf("DACL entry type = %d; want allow", ace.Header.AceType)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		t.Fatal(err)
	}
	if owner == nil || !owner.Equals(user.User.Sid) {
		t.Fatalf("owner SID = %v; want current user %s", owner, user.User.Sid)
	}
	entrySID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if !entrySID.Equals(user.User.Sid) {
		t.Fatalf("DACL entry SID = %s; want current owner %s", entrySID, user.User.Sid)
	}
}
