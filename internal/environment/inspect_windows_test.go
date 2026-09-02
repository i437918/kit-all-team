//go:build windows

package environment

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"golang.org/x/sys/windows"
)

func TestInspector_WindowsCandidateAliasMatchesDesiredAndReturnsDesiredHome(t *testing.T) {
	root, desired := readyEnvironmentFixture(t)
	candidate := strings.ToUpper(root)
	got, state, err := NewInspector().Inspect(context.Background(), candidate)
	if err != nil || state != Ready {
		t.Fatalf("state=%v err=%v", state, err)
	}
	if got.Home != desired.KitHome() {
		t.Fatalf("home=%q want desired home %q", got.Home, desired.KitHome())
	}
}

func TestInspector_WindowsRejectsJunctionRoot(t *testing.T) {
	parent := testutil.TempDir(t)
	external, _ := readyEnvironmentFixture(t)
	sentinel := filepath.Join(external, "sentinel")
	writeFile(t, sentinel, "unchanged")
	junction := filepath.Join(parent, "junction")
	output, mkErr := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput()
	if mkErr != nil {
		t.Fatalf("mklink: %v: %s", mkErr, output)
	}
	_, state, err := NewInspector().Inspect(context.Background(), junction)
	if state != Foreign || err == nil {
		t.Fatalf("state=%v err=%v", state, err)
	}
	data, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(data) != "unchanged" {
		t.Fatalf("sentinel=%q err=%v", data, readErr)
	}
}

func TestInspector_WindowsCanonicalizationOperationalErrorIsInspectionFailure(t *testing.T) {
	root, _ := readyEnvironmentFixture(t)
	cause := &os.PathError{Op: "CreateFile", Path: root, Err: windows.ERROR_ACCESS_DENIED}
	probe := inspector{comparisonKey: func(string) (string, error) {
		return "", cause
	}}

	_, state, err := probe.Inspect(context.Background(), root)
	var inspectionErr *Error
	if state != InspectionFailed || !errors.As(err, &inspectionErr) || inspectionErr.State != InspectionFailed {
		t.Fatalf("state=%v err=%T %v", state, err, err)
	}
	if !errors.Is(err, cause) || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("underlying cause was not preserved: %v", err)
	}
	if errors.Is(err, pathsafe.ErrUnsafe) {
		t.Fatalf("operational error was marked unsafe: %v", err)
	}
}
