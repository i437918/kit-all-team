package gitx

import (
	"context"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemRunnerStripsAmbientGitRepositoryOverrides(t *testing.T) {
	repository := testutil.TempDir(t)
	if output, err := exec.Command("git", "init", repository).CombinedOutput(); err != nil {
		t.Skipf("system Git unavailable: %v: %s", err, output)
	}
	t.Setenv("GIT_DIR", filepath.Join(testutil.TempDir(t), "foreign.git"))
	t.Setenv("GIT_WORK_TREE", testutil.TempDir(t))
	result, err := (SystemRunner{}).Run(context.Background(), Command{Args: []string{"-C", repository, "rev-parse", "--git-dir"}})
	if err != nil {
		t.Fatalf("Run: %v stderr=%s", err, result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) != ".git" {
		t.Fatalf("git-dir=%q", result.Stdout)
	}
}
