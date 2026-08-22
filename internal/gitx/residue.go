package gitx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

func preflightContentClone(destination string) (bool, error) {
	if err := pathsafe.ValidateDirectory(destination); err != nil {
		return false, unsafeGitResidue(err)
	}
	hasGit, err := gitDirectoryPresent(destination)
	if err != nil {
		return false, err
	}
	if hasGit {
		if err := validateGitMutationMetadata(destination); err != nil {
			return false, err
		}
		if err := requireReceiptOwnership(filepath.Join(destination, ".teamkit", "operation.json")); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := validateUnbornContentResidue(destination, false, false); err != nil {
		return false, err
	}
	return false, nil
}

func preflightDatabaseClone(workspaceRoot, destination string) (bool, error) {
	if err := pathsafe.ValidateDirectory(destination); err != nil {
		return false, unsafeGitResidue(err)
	}
	hasGit, err := gitDirectoryPresent(destination)
	if err != nil {
		return false, err
	}
	if !hasGit {
		entries, err := readDirectory(destination)
		if err != nil {
			return false, err
		}
		if len(entries) != 0 {
			return false, unsafeGitResidue(fmt.Errorf("database destination contains foreign data"))
		}
		return false, nil
	}
	if err := validateGitMutationMetadata(destination); err != nil {
		return false, err
	}
	if err := requireReceiptOwnership(filepath.Join(workspaceRoot, ".teamkit", "operation.json")); err != nil {
		return false, err
	}
	return true, nil
}

func validateUnbornContentResidue(destination string, requireGit, allowBareGit bool) error {
	entries, err := readDirectory(destination)
	if err != nil {
		return err
	}
	if len(entries) == 0 && !requireGit {
		return nil
	}
	if requireGit && allowBareGit && len(entries) == 1 && entries[0].Name() == ".git" {
		return validateGitMutationMetadata(destination)
	}
	wanted := map[string]bool{".env": false, ".teamkit": false}
	if requireGit {
		wanted[".git"] = false
	}
	for _, entry := range entries {
		if _, ok := wanted[entry.Name()]; !ok {
			return unsafeGitResidue(fmt.Errorf("content destination contains foreign entry %q", entry.Name()))
		}
		wanted[entry.Name()] = true
	}
	for name, present := range wanted {
		if !present {
			return unsafeGitResidue(fmt.Errorf("content destination is missing Team Kit residue %q", name))
		}
	}
	if err := requireRegular(filepath.Join(destination, ".env")); err != nil {
		return err
	}
	metadata := filepath.Join(destination, ".teamkit")
	if err := pathsafe.ValidateDirectory(metadata); err != nil {
		return unsafeGitResidue(err)
	}
	metadataEntries, err := os.ReadDir(metadata)
	if err != nil {
		return err
	}
	owned := map[string]bool{"owner": false, "operation.json": false}
	for _, entry := range metadataEntries {
		if entry.Name() == "operation.lock" {
			if err := requireRegular(filepath.Join(metadata, entry.Name())); err != nil {
				return err
			}
			continue
		}
		if _, ok := owned[entry.Name()]; !ok {
			return unsafeGitResidue(fmt.Errorf("Team Kit metadata contains foreign entry %q", entry.Name()))
		}
		owned[entry.Name()] = true
	}
	for name, present := range owned {
		if !present {
			return unsafeGitResidue(fmt.Errorf("Team Kit metadata is missing %q", name))
		}
		if err := requireRegular(filepath.Join(metadata, name)); err != nil {
			return err
		}
	}
	return nil
}

func validateUnbornDatabaseResidue(destination string) error {
	entries, err := readDirectory(destination)
	if err != nil {
		return err
	}
	if len(entries) != 1 || entries[0].Name() != ".git" {
		return unsafeGitResidue(fmt.Errorf("database destination contains foreign residue"))
	}
	return nil
}

func gitDirectoryPresent(destination string) (bool, error) {
	gitDirectory := filepath.Join(destination, ".git")
	info, err := os.Lstat(gitDirectory)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, unsafeGitResidue(fmt.Errorf(".git is not a directory"))
	}
	return true, nil
}

func requireReceiptOwnership(path string) error {
	if err := requireRegular(path); err != nil {
		return unsafeGitResidue(fmt.Errorf("partial Git state is not receipt-owned: %w", err))
	}
	return nil
}

func requireRegular(path string) error {
	if err := pathsafe.ValidateRegular(path); err != nil {
		return unsafeGitResidue(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return unsafeGitResidue(err)
	}
	if !info.Mode().IsRegular() {
		return unsafeGitResidue(fmt.Errorf("%s is not a regular file", path))
	}
	return nil
}

func readDirectory(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}

func unsafeGitResidue(err error) error {
	var coded *Error
	if errors.As(err, &coded) && coded.Code == "GIT_RESIDUE_UNSAFE" {
		return err
	}
	return &Error{Code: "GIT_RESIDUE_UNSAFE", Err: fmt.Errorf("initial clone residue is unsafe: %w", err)}
}
