package hermes

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

// ErrInstallLayout reports a managed Hermes checkout that crosses a redirected
// filesystem component or otherwise cannot be inspected safely.
var ErrInstallLayout = errors.New("HERMES_INSTALL_VERIFICATION_FAILED")

type checkoutGit interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type systemCheckoutGit struct{}

func (systemCheckoutGit) Run(ctx context.Context, directory string, args ...string) ([]byte, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	git, err = filepath.Abs(git)
	if err != nil {
		return nil, err
	}
	if err := pathsafe.ValidateRegular(git); err != nil {
		return nil, err
	}
	commandArgs := []string{
		"--no-replace-objects",
		"-c", "core.fsmonitor=false",
		"-c", "core.untrackedCache=false",
		"-c", "core.hooksPath=",
		"--git-dir", filepath.Join(directory, ".git"),
		"--work-tree", directory,
	}
	commandArgs = append(commandArgs, args...)
	command := exec.CommandContext(ctx, git, commandArgs...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	return command.CombinedOutput()
}

// ManagedInstallReady verifies the exact Team Kit-managed POSIX Hermes
// checkout below hermesHome. A missing or drifted checkout is not ready; an
// unsafe filesystem layout or an unverifiable Git repository is an error.
func ManagedInstallReady(hermesHome string) (bool, error) {
	return managedInstallReady(context.Background(), hermesHome, HermesSourceOrigin, HermesSourceCommit, systemCheckoutGit{})
}

func managedInstallReady(ctx context.Context, hermesHome, expectedOrigin, expectedCommit string, git checkoutGit) (bool, error) {
	if !filepath.IsAbs(hermesHome) {
		return false, fmt.Errorf("%w: HERMES_HOME must be absolute", ErrInstallLayout)
	}
	home := filepath.Clean(hermesHome)
	checkout := filepath.Join(home, ".teamkit", "hermes-agent-source")
	executable := filepath.Join(checkout, "venv", "bin", "hermes")

	for _, directory := range []string{
		home,
		filepath.Join(home, ".teamkit"),
		checkout,
		filepath.Join(checkout, ".git"),
		filepath.Join(checkout, "venv"),
		filepath.Join(checkout, "venv", "bin"),
	} {
		if err := pathsafe.ValidateDirectory(directory); err != nil {
			return false, fmt.Errorf("%w: %v", ErrInstallLayout, err)
		}
	}
	for _, directory := range []string{home, filepath.Join(home, ".teamkit"), checkout, filepath.Join(checkout, ".git")} {
		info, err := os.Lstat(directory)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("%w: checkout directory is unsafe", ErrInstallLayout)
		}
	}

	origin, err := git.Run(ctx, checkout, "config", "--local", "--get", "remote.origin.url")
	if err != nil {
		return false, fmt.Errorf("%w: origin: %v", ErrInstallLayout, err)
	}
	if string(origin) != expectedOrigin+"\n" {
		return false, nil
	}
	commit, err := git.Run(ctx, checkout, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return false, fmt.Errorf("%w: commit: %v", ErrInstallLayout, err)
	}
	if string(commit) != expectedCommit+"\n" {
		return false, nil
	}
	status, err := git.Run(ctx, checkout, "status", "--porcelain=v1", "--untracked-files=all", "--ignored=no")
	if err != nil {
		return false, fmt.Errorf("%w: worktree: %v", ErrInstallLayout, err)
	}
	if len(status) != 0 {
		return false, nil
	}
	return executableReady(executable)
}

func executableReady(path string) (bool, error) {
	if !filepath.IsAbs(path) {
		return false, nil
	}
	if err := pathsafe.ValidateRegular(path); err != nil {
		return false, fmt.Errorf("%w: %v", ErrInstallLayout, err)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() == 0 {
		return false, nil
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return false, nil
	}
	return true, nil
}
