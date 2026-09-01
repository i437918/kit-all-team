//go:build windows

package pathsafe

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestReadRegular_WindowsRejectsSymlinkLeaf(t *testing.T) {
	directory := testutil.TempDir(t)
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, ".env")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if data, err := ReadRegular(link, 64<<10); !errors.Is(err, ErrUnsafe) || data != nil {
		t.Fatalf("ReadRegular() data=%q err=%v, want ErrUnsafe", data, err)
	}
}

func TestReadRegular_WindowsRejectsJunctionLeaf(t *testing.T) {
	directory := testutil.TempDir(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(directory, ".env")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v: %s", err, output)
	}
	if data, err := ReadRegular(junction, 64<<10); !errors.Is(err, ErrUnsafe) || data != nil {
		t.Fatalf("ReadRegular() data=%q err=%v, want ErrUnsafe", data, err)
	}
	body, err := os.ReadFile(sentinel)
	if err != nil || string(body) != "unchanged" {
		t.Fatalf("sentinel=%q err=%v", body, err)
	}
}
