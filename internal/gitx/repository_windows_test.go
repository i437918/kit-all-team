//go:build windows

package gitx

import (
	"context"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

func TestUpdateContent_RejectsJunctionedGitObjectsBeforeFetch(t *testing.T) {
	directory := testutil.TempDir(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	redirected := filepath.Join(directory, ".git", "objects")
	createJunction(t, redirected, external)

	runner := branchRunner("https://git.example/content.git", "content-alpha", func(operation string) error {
		if operation == "fetch" {
			return os.WriteFile(filepath.Join(redirected, "sentinel"), []byte("escaped"), 0o600)
		}
		return nil
	})
	err := NewRepository(runner).UpdateContent(context.Background(), directory, "https://git.example/content.git", "content-alpha", Credentials{})
	if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, pathsafe.ErrUnsafe) {
		t.Fatalf("UpdateContent error=%v, want GIT_METADATA_UNSAFE wrapping pathsafe.ErrUnsafe", err)
	}
	assertSentinel(t, sentinel)
}

func TestCloneContent_RejectsJunctionIntroducedBeforeCheckout(t *testing.T) {
	directory := testutil.TempDir(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	redirected := filepath.Join(directory, ".git", "objects")
	runner := branchRunner("https://git.example/content.git", "content-alpha", func(operation string) error {
		switch operation {
		case "fetch":
			createJunction(t, redirected, external)
		case "checkout":
			return os.WriteFile(filepath.Join(redirected, "sentinel"), []byte("escaped"), 0o600)
		}
		return nil
	})
	err := NewRepository(runner).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", directory, Credentials{})
	if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, pathsafe.ErrUnsafe) {
		t.Fatalf("CloneContent error=%v, want GIT_METADATA_UNSAFE wrapping pathsafe.ErrUnsafe", err)
	}
	assertSentinel(t, sentinel)
}

func createJunction(t *testing.T, junction, target string) {
	t.Helper()
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
}
