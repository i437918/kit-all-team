//go:build windows

package workspace

import (
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

func TestWriteFileAtomic_RejectsJunctionParentWithoutTouchingTarget(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "settings")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}

	err := WriteFileAtomic(filepath.Join(junction, "state"), []byte("escaped"), 0o600)
	if !errors.Is(err, pathsafe.ErrUnsafe) {
		t.Fatalf("WriteFileAtomic() error = %v, want pathsafe.ErrUnsafe", err)
	}
	if _, err := os.Stat(filepath.Join(external, "state")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through junction: %v", err)
	}
	contents, err := os.ReadFile(sentinel)
	if err != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, err)
	}
}
