package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/registry"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestNewRunner_DoesNotCreateRegistry(t *testing.T) {
	base := testutil.TempDir(t)
	localAppData := filepath.Join(base, "missing-local")
	xdgConfig := filepath.Join(base, "missing-xdg")
	userHome := filepath.Join(base, "missing-home")
	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	registryPath, err := registry.DefaultPath(registry.LocationOptions{GOOS: runtime.GOOS, Getenv: os.Getenv, UserHomeDir: os.UserHomeDir})
	if err != nil {
		t.Fatal(err)
	}
	_ = newRunner(strings.NewReader(""), io.Discard, io.Discard)
	if _, err := os.Lstat(registryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructor touched registry file %q: %v", registryPath, err)
	}
	if _, err := os.Lstat(filepath.Dir(registryPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructor touched registry directory %q: %v", filepath.Dir(registryPath), err)
	}
}
