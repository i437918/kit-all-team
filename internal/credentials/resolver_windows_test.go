//go:build windows

package credentials

import (
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolverRejectsJunctionApplicationHomeBeforeOpeningSecretStore(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	home := filepath.Join(root, "application")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", home, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	assertResolverRejectsHomeBeforeSecrets(t, hermesDesired(t), home)
}
