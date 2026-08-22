package gitx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

var (
	ErrHooksPath     = errors.New("GIT_HOOKS_PATH_UNSAFE")
	ErrHookCollision = errors.New("GIT_HOOK_COLLISION")
)

const preCommitGuard = `#!/bin/sh
branch="$(git symbolic-ref --quiet --short HEAD || true)"
if [ "$branch" = "develop" ]; then
  echo "teamkit: commits on develop are blocked" >&2
  exit 1
fi
exit 0
`

const prePushGuard = `#!/bin/sh
while read -r local_ref local_oid remote_ref remote_oid
do
  if [ "$remote_ref" = "refs/heads/develop" ]; then
    echo "teamkit: pushes to develop are blocked" >&2
    exit 1
  fi
done
exit 0
`

// HookContents returns portable hook scripts that permit feature branches and reject develop.
func HookContents() map[string]string {
	return map[string]string{"pre-commit": preCommitGuard, "pre-push": prePushGuard}
}

// HooksReady reports whether both managed hooks are safe regular files whose
// bytes exactly match the current Team Kit hook contract.
func HooksReady(hooksDirectory string) (bool, error) {
	if err := validateHooksDirectory(hooksDirectory); err != nil {
		return false, err
	}
	for name, expected := range HookContents() {
		path := filepath.Join(hooksDirectory, name)
		if err := validateHookFile(path); err != nil {
			return false, err
		}
		metadata, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !metadata.Mode().IsRegular() {
			return false, ErrHooksPath
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		if string(contents) != expected {
			return false, nil
		}
		if !hookModeReady(metadata) {
			return false, nil
		}
	}
	return true, nil
}

// InstallHooks materializes Team Kit's managed safety hooks in hooksDirectory.
func InstallHooks(hooksDirectory string) error {
	if err := validateHooksDirectory(filepath.Dir(hooksDirectory)); err != nil {
		return err
	}
	if err := pathsafe.EnsureDirectory(hooksDirectory, 0o700); err != nil {
		return hookPathError(err)
	}
	hooks := HookContents()
	missing := make([]string, 0, len(hooks))
	repairMode := make([]string, 0, len(hooks))
	for name, content := range hooks {
		path := filepath.Join(hooksDirectory, name)
		if err := validateHookFile(path); err != nil {
			return err
		}
		metadata, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, name)
			continue
		}
		if err != nil {
			return err
		}
		if !metadata.Mode().IsRegular() {
			return ErrHooksPath
		}
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(existing) != content {
			return ErrHookCollision
		}
		if !hookModeReady(metadata) {
			repairMode = append(repairMode, name)
		}
	}
	for _, name := range repairMode {
		if err := validateHooksDirectory(hooksDirectory); err != nil {
			return err
		}
		path := filepath.Join(hooksDirectory, name)
		if err := validateHookFile(path); err != nil {
			return err
		}
		if err := repairManagedHookMode(path, hooks[name]); err != nil {
			return err
		}
	}
	for _, name := range missing {
		content := hooks[name]
		path := filepath.Join(hooksDirectory, name)
		if err := validateHooksDirectory(hooksDirectory); err != nil {
			return err
		}
		if err := validateHookFile(path); err != nil {
			return err
		}
		temporary, err := os.CreateTemp(hooksDirectory, ".teamkit-hook-")
		if err != nil {
			return err
		}
		temporaryPath := temporary.Name()
		if err := temporary.Chmod(0o755); err == nil {
			_, err = temporary.WriteString(content)
		}
		if err == nil {
			err = temporary.Sync()
		}
		if closeErr := temporary.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = validateHooksDirectory(hooksDirectory)
		}
		if err == nil {
			err = validateHookFile(path)
		}
		if err == nil {
			err = validateHookFile(temporaryPath)
		}
		if err == nil {
			err = os.Rename(temporaryPath, path)
		}
		_ = os.Remove(temporaryPath)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateHooksDirectory(path string) error {
	if err := pathsafe.ValidateDirectory(path); err != nil {
		return hookPathError(err)
	}
	return nil
}

func validateHookFile(path string) error {
	if err := pathsafe.ValidateRegular(path); err != nil {
		return hookPathError(err)
	}
	return nil
}

func hookPathError(err error) error {
	if errors.Is(err, pathsafe.ErrUnsafe) {
		return fmt.Errorf("%w: %v", ErrHooksPath, err)
	}
	return err
}
