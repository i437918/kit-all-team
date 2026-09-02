package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTempDir_ReturnsResolvedTemporaryDirectory(t *testing.T) {
	directory := TempDir(t)
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatalf("resolve temporary directory: %v", err)
	}
	if directory != resolved {
		t.Fatalf("TempDir() = %q, want resolved path %q", directory, resolved)
	}
}

func TestResolveTempPath_CanonicalizesFixtureExpectation(t *testing.T) {
	rawTarget := t.TempDir() + string(filepath.Separator) + "."
	expected, err := filepath.EvalSymlinks(rawTarget)
	if err != nil {
		t.Fatalf("resolve expected target: %v", err)
	}

	if got := resolveTempPath(t, rawTarget); got != expected {
		t.Fatalf("resolveTempPath() = %q, want %q", got, expected)
	}
}

func TestResolveTempPath_ResolvesRedirectedFixture(t *testing.T) {
	target := t.TempDir()
	redirected := filepath.Join(t.TempDir(), "redirected")
	if err := os.Symlink(target, redirected); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	expected, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("resolve expected target: %v", err)
	}

	if got := resolveTempPath(t, redirected); got != expected {
		t.Fatalf("resolveTempPath() = %q, want %q", got, expected)
	}
}
