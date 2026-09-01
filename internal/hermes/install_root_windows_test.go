//go:build windows

package hermes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func simulateInstallRootPathSwap(t *testing.T, root openedInstallRoot, replacement string) {
	t.Helper()
	opened, ok := root.(*windowsInstallRoot)
	if !ok {
		t.Fatalf("opened root type = %T", root)
	}
	opened.path = replacement
}

func TestOpenVerifiedInstallRoot_WindowsReadsBoundedRelativeFile(t *testing.T) {
	install, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	root, err := openVerifiedInstallRoot(RuntimeInfo{InstallDir: install, Executable: executable, Version: "0.20.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	data, err := root.ReadRegular(context.Background(), "hermes_cli\\config_defaults.py", 512<<10)
	if err != nil || string(data) != "DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n}\n" {
		t.Fatalf("ReadRegular()=%q,%v", data, err)
	}
	if _, err := root.ReadRegular(context.Background(), "..\\outside", 1); err == nil || errors.Is(err, ErrExecutableUnverified) {
		t.Fatalf("redirected relative read err=%v", err)
	}
}

func TestOpenVerifiedInstallRoot_WindowsRejectsRegularFileAsRoot(t *testing.T) {
	install, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	regular := filepath.Join(filepath.Dir(install), "not-a-directory")
	if err := os.WriteFile(regular, []byte("regular"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openVerifiedInstallRoot(RuntimeInfo{InstallDir: regular, Executable: executable, Version: "0.20.1"}); err == nil {
		t.Fatal("openVerifiedInstallRoot() succeeded for regular root")
	}
}
