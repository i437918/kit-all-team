//go:build windows

package pathsafe

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"golang.org/x/sys/windows"
)

func TestComparisonKey_WindowsCaseAndAliasAreEqual(t *testing.T) {
	longPath := filepath.Join(testutil.TempDir(t), "A Long Directory Name For Teamkit")
	if err := os.Mkdir(longPath, 0o700); err != nil {
		t.Fatal(err)
	}
	longUTF16, err := windows.UTF16PtrFromString(longPath)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	length, err := windows.GetShortPathName(longUTF16, &buffer[0], uint32(len(buffer)))
	if err != nil || length == 0 || int(length) >= len(buffer) {
		t.Skipf("8.3 alias unavailable: %v", err)
	}
	shortPath := windows.UTF16ToString(buffer[:length])
	if strings.EqualFold(filepath.Clean(shortPath), filepath.Clean(longPath)) || !strings.Contains(shortPath, "~") {
		t.Skip("8.3 aliases are disabled on this volume")
	}
	longKey, err := ComparisonKey(longPath)
	if err != nil {
		t.Fatal(err)
	}
	shortKey, err := ComparisonKey(strings.ToUpper(shortPath))
	if err != nil {
		t.Fatal(err)
	}
	if longKey != shortKey {
		t.Fatalf("long=%q short=%q", longKey, shortKey)
	}
	canonical, err := CanonicalPath(shortPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(canonical, "~") || !filepath.IsAbs(canonical) {
		t.Fatalf("canonical=%q", canonical)
	}
}

func TestCanonicalPath_WindowsPreservesOperationalFinalPathError(t *testing.T) {
	root := testutil.TempDir(t)
	cause := windows.ERROR_ACCESS_DENIED

	_, err := canonicalPathWithResolver(root, func(string) (string, error) {
		return "", cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want underlying cause %v", err, cause)
	}
	if errors.Is(err, ErrUnsafe) {
		t.Fatalf("operational error was marked unsafe: %v", err)
	}
}

func TestComparisonKey_RejectsJunctionBeforeFinalPath(t *testing.T) {
	root, external := testutil.TempDir(t), testutil.TempDir(t)
	junction := filepath.Join(root, "junction")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	if _, err := ComparisonKey(filepath.Join(junction, "kit")); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("err=%v", err)
	}
}

func TestEnsureDirectory_RejectsJunctionAncestorWithoutTouchingTarget(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "junction")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}

	err := EnsureDirectory(filepath.Join(junction, "new-directory"), 0o700)
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("EnsureDirectory() error = %v, want ErrUnsafe", err)
	}
	if _, err := os.Stat(filepath.Join(external, "new-directory")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through junction: %v", err)
	}
	contents, err := os.ReadFile(sentinel)
	if err != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, err)
	}
}

func TestOverlaps_UsesWindowsCaseInsensitiveCanonicalPaths(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "MixedCase")
	got, err := Overlaps(root, filepath.Join(strings.ToUpper(root), "CHILD"))
	if err != nil || !got {
		t.Fatalf("Overlaps() = %t, %v; want true, nil", got, err)
	}
}

func TestOverlaps_ResolvesWindowsShortPathAlias(t *testing.T) {
	longPath := filepath.Join(testutil.TempDir(t), "A Long Directory Name For Teamkit")
	if err := os.Mkdir(longPath, 0o700); err != nil {
		t.Fatal(err)
	}
	longUTF16, err := windows.UTF16PtrFromString(longPath)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	length, err := windows.GetShortPathName(longUTF16, &buffer[0], uint32(len(buffer)))
	if err != nil || length == 0 || int(length) >= len(buffer) {
		t.Skipf("8.3 alias unavailable: %v", err)
	}
	shortPath := windows.UTF16ToString(buffer[:length])
	if strings.EqualFold(filepath.Clean(shortPath), filepath.Clean(longPath)) || !strings.Contains(shortPath, "~") {
		t.Skip("8.3 aliases are disabled on this volume")
	}

	got, err := Overlaps(longPath, filepath.Join(shortPath, "child"))
	if err != nil || !got {
		t.Fatalf("Overlaps(%q, %q) = %t, %v; want true, nil", longPath, shortPath, got, err)
	}
}
