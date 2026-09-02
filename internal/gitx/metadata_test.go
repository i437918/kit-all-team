package gitx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestRunMutation_RevalidatesGitMetadataAfterSuccessfulCommand(t *testing.T) {
	for _, test := range []struct {
		name      string
		transform func(string) error
	}{
		{
			name: "deleted",
			transform: func(gitDirectory string) error {
				return os.Remove(gitDirectory)
			},
		},
		{
			name: "replaced by file",
			transform: func(gitDirectory string) error {
				if err := os.Remove(gitDirectory); err != nil {
					return err
				}
				return os.WriteFile(gitDirectory, []byte("not a Git directory"), 0o600)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			worktree := testutil.TempDir(t)
			gitDirectory := filepath.Join(worktree, ".git")
			if err := os.Mkdir(gitDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			calls := 0
			runner := commandRunnerFunc(func(Command) (Result, error) {
				calls++
				return Result{}, test.transform(gitDirectory)
			})

			err := NewRepository(runner).runMutation(context.Background(), worktree, Command{Args: []string{"status"}}, Credentials{})
			if ErrorCode(err) != "GIT_METADATA_UNSAFE" {
				t.Fatalf("runMutation() error = %v, code = %q; want GIT_METADATA_UNSAFE", err, ErrorCode(err))
			}
			if calls != 1 {
				t.Fatalf("runner calls = %d; want 1", calls)
			}
		})
	}
}

func TestRunRepositoryCreationMutation_RequiresCreatedGitRootAfterSuccess(t *testing.T) {
	for _, test := range []struct {
		name    string
		command Command
	}{
		{name: "init", command: Command{Args: []string{"-C", "destination", "init"}}},
		{name: "clone", command: Command{Args: []string{"clone", "remote", "destination"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			worktree := testutil.TempDir(t)
			calls := 0
			runner := commandRunnerFunc(func(Command) (Result, error) {
				calls++
				return Result{}, nil
			})

			err := NewRepository(runner).runRepositoryCreationMutation(context.Background(), worktree, test.command, Credentials{})
			if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("runRepositoryCreationMutation() error = %v; want GIT_METADATA_UNSAFE wrapping fs.ErrNotExist", err)
			}
			if calls != 1 {
				t.Fatalf("runner calls = %d; want 1", calls)
			}
		})
	}
}

func TestRunRepositoryCreationMutation_AcceptsSafeCreatedGitRoot(t *testing.T) {
	for _, operation := range []string{"init", "clone"} {
		t.Run(operation, func(t *testing.T) {
			worktree := testutil.TempDir(t)
			calls := 0
			runner := commandRunnerFunc(func(Command) (Result, error) {
				calls++
				return Result{}, os.Mkdir(filepath.Join(worktree, ".git"), 0o700)
			})

			err := NewRepository(runner).runRepositoryCreationMutation(context.Background(), worktree, Command{Args: []string{operation}}, Credentials{})
			if err != nil {
				t.Fatalf("runRepositoryCreationMutation() error = %v; want nil", err)
			}
			if calls != 1 {
				t.Fatalf("runner calls = %d; want 1", calls)
			}
		})
	}
}

func TestRunMutation_PreservesCommandFailureWithoutPostcheck(t *testing.T) {
	worktree := testutil.TempDir(t)
	gitDirectory := filepath.Join(worktree, ".git")
	if err := os.Mkdir(gitDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	calls := 0
	runner := commandRunnerFunc(func(Command) (Result, error) {
		calls++
		if err := os.Remove(gitDirectory); err != nil {
			return Result{}, err
		}
		return Result{}, errors.New("injected command failure")
	})

	err := NewRepository(runner).runMutation(context.Background(), worktree, Command{Args: []string{"fetch"}}, Credentials{})
	if ErrorCode(err) != "GIT_COMMAND_FAILED" {
		t.Fatalf("runMutation() error = %v, code = %q; want GIT_COMMAND_FAILED", err, ErrorCode(err))
	}
	if calls != 1 {
		t.Fatalf("runner calls = %d; want 1", calls)
	}
}

func TestRunRepositoryCreationMutation_PreservesCommandFailureWithoutPostcheck(t *testing.T) {
	worktree := testutil.TempDir(t)
	calls := 0
	runner := commandRunnerFunc(func(Command) (Result, error) {
		calls++
		return Result{}, errors.New("injected creation failure")
	})

	err := NewRepository(runner).runRepositoryCreationMutation(context.Background(), worktree, Command{Args: []string{"init"}}, Credentials{})
	if ErrorCode(err) != "GIT_COMMAND_FAILED" {
		t.Fatalf("runRepositoryCreationMutation() error = %v, code = %q; want GIT_COMMAND_FAILED", err, ErrorCode(err))
	}
	if calls != 1 {
		t.Fatalf("runner calls = %d; want 1", calls)
	}
}

func TestValidateGitMetadataTree_ContinuesWhenEnumeratedEntryDisappears(t *testing.T) {
	root := testutil.TempDir(t)
	for iteration := 0; iteration < 128; iteration++ {
		path := filepath.Join(root, fmt.Sprintf("maintenance-%03d.lock", iteration))
		if err := os.WriteFile(path, []byte("lock"), 0o600); err != nil {
			t.Fatal(err)
		}
		removed := false
		readDir := func(directory string) ([]os.DirEntry, error) {
			entries, err := os.ReadDir(directory)
			if err == nil && directory == root && !removed {
				if err := os.Remove(path); err != nil {
					t.Fatalf("remove enumerated entry: %v", err)
				}
				removed = true
			}
			return entries, err
		}

		if err := validateGitMetadataTreeWith(root, readDir, os.Lstat); err != nil {
			t.Fatalf("iteration %d: validateGitMetadataTreeWith() error = %v; want nil", iteration, err)
		}
	}
}

func TestValidateGitMetadataTree_ContinuesWhenNestedDirectoryDisappearsDuringRecursion(t *testing.T) {
	root := testutil.TempDir(t)
	objects := filepath.Join(root, "objects")
	if err := os.Mkdir(objects, 0o700); err != nil {
		t.Fatal(err)
	}
	readDir := func(directory string) ([]os.DirEntry, error) {
		if directory == objects {
			if err := os.Remove(objects); err != nil {
				t.Fatalf("remove nested directory: %v", err)
			}
		}
		return os.ReadDir(directory)
	}

	if err := validateGitMetadataTreeWith(root, readDir, os.Lstat); err != nil {
		t.Fatalf("validateGitMetadataTreeWith() error = %v; want nil", err)
	}
}

func TestValidateGitMetadataTree_FailsClosedWhenEnumeratedEntryCannotBeInspected(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "permission", err: fs.ErrPermission},
		{name: "IO", err: io.ErrUnexpectedEOF},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := testutil.TempDir(t)
			path := filepath.Join(root, "maintenance.lock")
			if err := os.WriteFile(path, []byte("lock"), 0o600); err != nil {
				t.Fatal(err)
			}
			lstat := func(candidate string) (fs.FileInfo, error) {
				if candidate == path {
					return nil, &os.PathError{Op: "lstat", Path: candidate, Err: test.err}
				}
				return os.Lstat(candidate)
			}

			err := validateGitMetadataTreeWith(root, os.ReadDir, lstat)
			if !errors.Is(err, test.err) {
				t.Fatalf("validateGitMetadataTreeWith() error = %v; want wrapping %v", err, test.err)
			}
		})
	}
}

func TestValidateGitMutationMetadata_RejectsMissingGitRoot(t *testing.T) {
	worktree := testutil.TempDir(t)

	err := validateGitMutationMetadata(worktree)
	if ErrorCode(err) != "GIT_METADATA_UNSAFE" {
		t.Fatalf("validateGitMutationMetadata() error = %v, code = %q; want GIT_METADATA_UNSAFE", err, ErrorCode(err))
	}
}

func TestValidateGitMutationMetadata_RejectsGitRootDisappearingDuringTraversal(t *testing.T) {
	worktree := testutil.TempDir(t)
	gitDirectory := filepath.Join(worktree, ".git")
	if err := os.Mkdir(gitDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	readDir := func(directory string) ([]os.DirEntry, error) {
		entries, err := os.ReadDir(directory)
		if err == nil && directory == gitDirectory {
			if err := os.Remove(gitDirectory); err != nil {
				t.Fatalf("remove Git root: %v", err)
			}
		}
		return entries, err
	}

	err := validateGitMutationMetadataModeWith(worktree, true, readDir, os.Lstat)
	if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("validateGitMutationMetadataModeWith() error = %v; want GIT_METADATA_UNSAFE wrapping fs.ErrNotExist", err)
	}
}
