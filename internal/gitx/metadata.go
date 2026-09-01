package gitx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

func validateGitMutationMetadata(worktree string) error {
	return validateGitMutationMetadataMode(worktree, true)
}

func validateGitRepositoryCreationMetadata(worktree string) error {
	return validateGitMutationMetadataMode(worktree, false)
}

func validateGitMutationMetadataMode(worktree string, requireGitRoot bool) error {
	return validateGitMutationMetadataModeWith(worktree, requireGitRoot, os.ReadDir, os.Lstat)
}

func validateGitMutationMetadataModeWith(
	worktree string,
	requireGitRoot bool,
	readDir func(string) ([]os.DirEntry, error),
	lstat func(string) (fs.FileInfo, error),
) error {
	gitDirectory := filepath.Join(worktree, ".git")
	exists, err := gitMetadataRootExists(gitDirectory)
	if err != nil {
		return unsafeGitMetadata(err)
	}
	if !exists {
		if requireGitRoot {
			return unsafeGitMetadata(&os.PathError{Op: "lstat", Path: gitDirectory, Err: fs.ErrNotExist})
		}
		return nil
	}
	if err := validateGitMetadataTreeWith(gitDirectory, readDir, lstat); err != nil {
		return unsafeGitMetadata(err)
	}
	if exists, err := gitMetadataRootExists(gitDirectory); err != nil {
		return unsafeGitMetadata(err)
	} else if !exists {
		return unsafeGitMetadata(&os.PathError{Op: "lstat", Path: gitDirectory, Err: fs.ErrNotExist})
	}
	return nil
}

func gitMetadataRootExists(root string) (bool, error) {
	if err := pathsafe.ValidateDirectory(root); err != nil {
		return false, err
	}
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%w: Git metadata root is not a directory: %s", pathsafe.ErrUnsafe, root)
	}
	return true, nil
}

func validateGitMetadataTreeWith(
	root string,
	readDir func(string) ([]os.DirEntry, error),
	lstat func(string) (fs.FileInfo, error),
) error {
	if err := pathsafe.ValidateDirectory(root); err != nil {
		return err
	}
	entries, err := readDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := validateGitMetadataTreeWith(path, readDir, lstat); err != nil {
				return err
			}
			continue
		}
		if err := pathsafe.ValidateRegular(path); err != nil {
			return err
		}
	}
	return nil
}

func unsafeGitMetadata(err error) error {
	return &Error{Code: "GIT_METADATA_UNSAFE", Err: fmt.Errorf("repository Git metadata is unsafe: %w", err)}
}
