//go:build windows

package gitx

import (
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstallHooks_RejectsJunctionWithoutTouchingTarget(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(external, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "db")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	hooks := filepath.Join(junction, ".git", "hooks")

	if ready, err := HooksReady(hooks); ready || !errors.Is(err, ErrHooksPath) {
		t.Fatalf("HooksReady() = %v, %v; want false, ErrHooksPath", ready, err)
	}
	if err := InstallHooks(hooks); !errors.Is(err, ErrHooksPath) {
		t.Fatalf("InstallHooks() error = %v, want ErrHooksPath", err)
	}
	for _, name := range []string{"pre-commit", "pre-push"} {
		if _, err := os.Stat(filepath.Join(external, ".git", "hooks", name)); !os.IsNotExist(err) {
			t.Fatalf("hook escaped through junction: %s: %v", name, err)
		}
	}
	contents, err := os.ReadFile(sentinel)
	if err != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, err)
	}
}
