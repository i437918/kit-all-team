//go:build !windows

package pathsafe

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestReadRegular_POSIXRejectsSymlinkLeaf(t *testing.T) {
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
