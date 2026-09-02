package secrets

import (
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStoreSave_WritesApplicationLocalPrivateFile(t *testing.T) {
	appHome := filepath.Join(testutil.TempDir(t), "hermes")
	store, err := NewStore(appHome)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.Save(map[string]string{"API_KEY": "teamkit-secret-canary"})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(appHome, ".env") {
		t.Fatalf("path = %q; want app-local .env", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v; want 0600", info.Mode())
	}
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), "teamkit-secret-canary") {
		t.Fatalf("secret file content = %q, %v", contents, err)
	}
}

func TestStoreStatusAndRedact_NeverExposeSecretValue(t *testing.T) {
	store, err := NewStore(testutil.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.Save(map[string]string{"API_KEY": "teamkit-secret-canary"})
	if err != nil {
		t.Fatal(err)
	}
	status := store.Status()
	if status.Path != path || !status.Configured || strings.Contains(status.String(), "teamkit-secret-canary") {
		t.Fatalf("status = %+v, string=%q", status, status.String())
	}
	if got := store.Redact("failure: teamkit-secret-canary"); strings.Contains(got, "teamkit-secret-canary") {
		t.Fatalf("redacted string leaked canary: %q", got)
	}
}

func TestNewStore_RejectsEmptyOrRelativeApplicationHome(t *testing.T) {
	for _, applicationHome := range []string{"", ".", "relative-app"} {
		store, err := NewStore(applicationHome)
		if err == nil || store != nil {
			t.Fatalf("NewStore(%q) = %#v, %v; want nil error", applicationHome, store, err)
		}
	}
}

func TestNewStore_RejectsRedirectedApplicationHome(t *testing.T) {
	external := testutil.TempDir(t)
	home := filepath.Join(testutil.TempDir(t), "application")
	if err := os.Symlink(external, home); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store, err := NewStore(home)
	if err == nil || store != nil {
		t.Fatalf("NewStore(%q) = %#v, %v; want path-safety error", home, store, err)
	}
}

func TestNewStore_RejectsRedirectedSecretFile(t *testing.T) {
	home := testutil.TempDir(t)
	sentinel := filepath.Join(testutil.TempDir(t), "outside.env")
	if err := os.WriteFile(sentinel, []byte("API_KEY=teamkit-secret-canary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(home, ".env")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store, err := NewStore(home)
	if err == nil || store != nil {
		t.Fatalf("NewStore(%q) = %#v, %v; want path-safety error", home, store, err)
	}
}

func TestStoreLoadAndSave_RevalidateApplicationHomeBeforeSecretAccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("junction revalidation is covered by the Windows-specific test")
	}
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
			if err := os.WriteFile(sentinel, []byte("API_KEY=teamkit-secret-canary\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, home); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}

			if err := operation.run(store); err == nil {
				t.Fatal("secret operation followed a redirected application home")
			}
			data, err := os.ReadFile(sentinel)
			if err != nil || string(data) != "API_KEY=teamkit-secret-canary\n" {
				t.Fatalf("external secret changed to %q: %v", data, err)
			}
		})
	}
}

func TestStoreSave_PreservesApplicationSettingsAndUpsertsSuppliedKeysDeterministically(t *testing.T) {
	appHome := testutil.TempDir(t)
	path := filepath.Join(appHome, ".env")
	existing := "# Application-managed setting\nUNRELATED_SETTING=keep\nAPI_KEY=old-value\n# Keep this comment\n"
	if err := writePrivateAtomic(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(appHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(map[string]string{
		"Z_KEY":   "last",
		"API_KEY": "teamkit-secret-canary",
		"A_KEY":   "first",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Application-managed setting\nUNRELATED_SETTING=keep\nAPI_KEY=teamkit-secret-canary\n# Keep this comment\nA_KEY=first\nZ_KEY=last\n"
	if string(got) != want {
		t.Fatalf("saved .env = %q; want %q", got, want)
	}
	if strings.Count(string(got), "API_KEY=") != 1 {
		t.Fatalf("upserted key was not singular: %q", got)
	}
}

func TestStoreLoad_ReturnsOnlyAllowlistedKeys(t *testing.T) {
	appHome := testutil.TempDir(t)
	path := filepath.Join(appHome, ".env")
	if err := writePrivateAtomic(path, []byte("# app configuration\nAPI_KEY=teamkit-secret-canary\nUNRELATED_SETTING=preserve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(appHome)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["API_KEY"] != "teamkit-secret-canary" {
		t.Fatalf("Load() = %#v; want only requested key", got)
	}
	if redacted := store.Redact("failed teamkit-secret-canary"); strings.Contains(redacted, "teamkit-secret-canary") {
		t.Fatalf("loaded secret was not registered for redaction: %q", redacted)
	}
}

func TestStoreLoad_RejectsUnsafePOSIXModeWithoutLeakingSecret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission test")
	}
	for _, mode := range []os.FileMode{0o644, 0o700} {
		t.Run(mode.String(), func(t *testing.T) {
			appHome := testutil.TempDir(t)
			path := filepath.Join(appHome, ".env")
			if err := os.WriteFile(path, []byte("API_KEY=teamkit-secret-canary\n"), mode); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(appHome)
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.Load("API_KEY")
			if err == nil || !strings.Contains(err.Error(), "SECRET_FILE_PERMISSIONS_UNSAFE") {
				t.Fatalf("Load error=%v", err)
			}
			if strings.Contains(err.Error(), "teamkit-secret-canary") {
				t.Fatalf("permission error leaked secret: %v", err)
			}
		})
	}
}

func TestStoreSave_RejectsUnsafePOSIXModeWithoutReadingOrReplacingSecret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission test")
	}
	home := testutil.TempDir(t)
	path := filepath.Join(home, ".env")
	const original = "API_KEY=teamkit-secret-canary\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
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

func TestStoreLoad_RejectsForeignOwnedPOSIXSecretFileWithoutReadingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership test")
	}
	if os.Geteuid() != 0 {
		t.Skip("changing a fixture owner requires root; metadata policy is covered in privatefile tests")
	}
	appHome := testutil.TempDir(t)
	path := filepath.Join(appHome, ".env")
	if err := os.WriteFile(path, []byte("API_KEY=teamkit-secret-canary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	foreignOwner := 1
	if foreignOwner == os.Geteuid() {
		foreignOwner++
	}
	if err := os.Chown(path, foreignOwner, -1); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(appHome)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Load("API_KEY")
	if err == nil || !strings.Contains(err.Error(), "SECRET_FILE_PERMISSIONS_UNSAFE") {
		t.Fatalf("Load error=%v", err)
	}
	if strings.Contains(err.Error(), "teamkit-secret-canary") {
		t.Fatalf("ownership error leaked secret: %v", err)
	}
}

func TestStoreLoadAndSave_RejectMalformedOrDuplicateFilesWithoutLeakingValues(t *testing.T) {
	tests := []struct {
		name      string
		contents  string
		operation func(*Store) error
	}{
		{
			name:      "duplicate key",
			contents:  "API_KEY=teamkit-secret-canary\nAPI_KEY=another-value\n",
			operation: func(store *Store) error { _, err := store.Load("API_KEY"); return err },
		},
		{
			name:      "invalid line",
			contents:  "API_KEY=teamkit-secret-canary\nthis is not dotenv\n",
			operation: func(store *Store) error { _, err := store.Load("API_KEY"); return err },
		},
		{
			name:     "save validates existing document",
			contents: "API_KEY=teamkit-secret-canary\nAPI_KEY=another-value\n",
			operation: func(store *Store) error {
				_, err := store.Save(map[string]string{"API_KEY": "replacement"})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appHome := testutil.TempDir(t)
			if err := os.WriteFile(filepath.Join(appHome, ".env"), []byte(tt.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(appHome)
			if err != nil {
				t.Fatal(err)
			}
			err = tt.operation(store)
			if err == nil {
				t.Fatal("operation accepted malformed secret file")
			}
			if strings.Contains(err.Error(), "teamkit-secret-canary") || strings.Contains(err.Error(), "another-value") {
				t.Fatalf("error leaked secret value: %v", err)
			}
		})
	}
}
