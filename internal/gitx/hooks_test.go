package gitx

import (
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedPreCommit_BlocksDevelopAndAllowsFeatureBranch(t *testing.T) {
	repository, _ := newManagedHookRepository(t)
	runGit(t, repository, "checkout", "-b", "develop")
	writeAndStage(t, repository, "develop.txt", "blocked")

	output, err := gitCommand(repository, "commit", "-m", "must fail").CombinedOutput()
	if err == nil {
		t.Fatalf("commit on develop succeeded: %s", output)
	}
	if !strings.Contains(string(output), "commits") {
		t.Fatalf("commit rejection output = %q", output)
	}

	runGit(t, repository, "checkout", "-b", "feature/allowed")
	runGit(t, repository, "commit", "-m", "allowed")
}

func TestManagedPrePush_BlocksEveryUpdateToDevelop(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string, string)
		refspec string
		args    []string
	}{
		{
			name:    "feature source",
			refspec: "HEAD:refs/heads/develop",
		},
		{
			name: "forced update",
			prepare: func(t *testing.T, repository, _ string) {
				original := runGitOutput(t, repository, "rev-parse", "HEAD")
				writeAndStage(t, repository, "remote-newer.txt", "remote")
				runGit(t, repository, "commit", "-m", "remote newer")
				runGit(t, repository, "push", "--no-verify", "origin", "HEAD:refs/heads/develop")
				runGit(t, repository, "reset", "--hard", original)
			},
			refspec: "HEAD:refs/heads/develop",
			args:    []string{"--force"},
		},
		{
			name: "deletion",
			prepare: func(t *testing.T, repository, _ string) {
				runGit(t, repository, "push", "--no-verify", "origin", "HEAD:refs/heads/develop")
			},
			refspec: ":refs/heads/develop",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, remote := newManagedHookRepository(t)
			if test.prepare != nil {
				test.prepare(t, repository, remote)
			}
			args := append([]string{"push"}, test.args...)
			args = append(args, "origin", test.refspec)
			output, err := gitCommand(repository, args...).CombinedOutput()
			if err == nil {
				t.Fatalf("push to develop succeeded: %s", output)
			}
			if !strings.Contains(string(output), "pushes to develop") {
				t.Fatalf("push rejection output = %q", output)
			}
		})
	}
}

func TestManagedPrePush_AllowsFeatureDestination(t *testing.T) {
	repository, _ := newManagedHookRepository(t)
	runGit(t, repository, "push", "origin", "HEAD:refs/heads/feature/allowed")
}

func TestInstallHooks_RejectsSymlinkOrUnmanagedCollision(t *testing.T) {
	t.Run("unmanaged hook", func(t *testing.T) {
		dir := testutil.TempDir(t)
		if err := os.WriteFile(filepath.Join(dir, "pre-commit"), []byte("user hook\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := InstallHooks(dir); !errors.Is(err, ErrHookCollision) {
			t.Fatalf("InstallHooks error = %v", err)
		}
	})
	t.Run("symlinked hooks directory", func(t *testing.T) {
		root, external := testutil.TempDir(t), testutil.TempDir(t)
		hooks := filepath.Join(root, "hooks")
		if err := os.Symlink(external, hooks); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := InstallHooks(hooks); !errors.Is(err, ErrHooksPath) {
			t.Fatalf("InstallHooks error = %v", err)
		}
	})
}

func TestInstallHooks_WritesPortableExecutableHooks(t *testing.T) {
	dir := testutil.TempDir(t)
	if err := InstallHooks(dir); err != nil {
		t.Fatal(err)
	}
	for name, content := range HookContents() {
		path := filepath.Join(dir, name)
		got, err := os.ReadFile(path)
		if err != nil || string(got) != content {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, content)
		}
	}
}

func TestInstallHooks_AcceptsAbsentAndByteIdenticalManagedHooks(t *testing.T) {
	dir := testutil.TempDir(t)
	if err := InstallHooks(dir); err != nil {
		t.Fatalf("absent hooks: %v", err)
	}
	if err := InstallHooks(dir); err != nil {
		t.Fatalf("byte-identical hooks: %v", err)
	}
}

func TestHooksReady_RequiresBothExactManagedHooks(t *testing.T) {
	dir := filepath.Join(testutil.TempDir(t), "absent-hooks")
	ready, err := HooksReady(dir)
	if err != nil || ready {
		t.Fatalf("HooksReady(absent) = %v, %v; want false, nil", ready, err)
	}

	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		want   bool
	}{
		{name: "exact", want: true},
		{
			name: "missing pre-commit",
			mutate: func(t *testing.T, directory string) {
				if err := os.Remove(filepath.Join(directory, "pre-commit")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "missing pre-push",
			mutate: func(t *testing.T, directory string) {
				if err := os.Remove(filepath.Join(directory, "pre-push")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "tampered pre-commit",
			mutate: func(t *testing.T, directory string) {
				if err := os.WriteFile(filepath.Join(directory, "pre-commit"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "tampered pre-push",
			mutate: func(t *testing.T, directory string) {
				if err := os.WriteFile(filepath.Join(directory, "pre-push"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := testutil.TempDir(t)
			if err := InstallHooks(directory); err != nil {
				t.Fatal(err)
			}
			if test.mutate != nil {
				test.mutate(t, directory)
			}
			ready, err := HooksReady(directory)
			if err != nil || ready != test.want {
				t.Fatalf("HooksReady() = %v, %v; want %v, nil", ready, err, test.want)
			}
		})
	}
}

func TestHooksReady_RejectsRedirectedHook(t *testing.T) {
	directory := testutil.TempDir(t)
	if err := InstallHooks(directory); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(testutil.TempDir(t), "external-hook")
	if err := os.WriteFile(external, []byte(HookContents()["pre-push"]), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, "pre-push")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(directory, "pre-push")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	ready, err := HooksReady(directory)
	if ready || !errors.Is(err, ErrHooksPath) {
		t.Fatalf("HooksReady() = %v, %v; want false, ErrHooksPath", ready, err)
	}
}

func TestInstallHooks_RejectsSymlinkedAncestorWithoutTouchingTarget(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	if err := os.Mkdir(filepath.Join(external, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "db")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := InstallHooks(filepath.Join(link, ".git", "hooks")); !errors.Is(err, ErrHooksPath) {
		t.Fatalf("InstallHooks() error = %v, want ErrHooksPath", err)
	}
	if _, err := os.Lstat(filepath.Join(external, ".git", "hooks")); !os.IsNotExist(err) {
		t.Fatalf("hooks escaped through symlink: %v", err)
	}
	contents, err := os.ReadFile(sentinel)
	if err != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, err)
	}
}

func newManagedHookRepository(t *testing.T) (string, string) {
	t.Helper()
	root := testutil.TempDir(t)
	repository := filepath.Join(root, "repository")
	remote := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", remote)
	runGit(t, root, "init", repository)
	runGit(t, repository, "config", "user.name", "Team Kit Tests")
	runGit(t, repository, "config", "user.email", "teamkit-tests@example.invalid")
	runGit(t, repository, "checkout", "-b", "feature/work")
	writeAndStage(t, repository, "initial.txt", "initial")
	runGit(t, repository, "commit", "-m", "initial")
	runGit(t, repository, "remote", "add", "origin", remote)
	if err := InstallHooks(filepath.Join(repository, ".git", "hooks")); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	return repository, remote
}

func writeAndStage(t *testing.T, repository, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repository, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "--", name)
}

func runGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	if output, err := gitCommand(directory, args...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}

func runGitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	output, err := gitCommand(directory, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func gitCommand(directory string, args ...string) *exec.Cmd {
	command := exec.Command("git", args...)
	command.Dir = directory
	environment := make([]string, 0, len(os.Environ())+3)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(item), "GIT_") {
			environment = append(environment, item)
		}
	}
	command.Env = append(environment,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
	)
	return command
}
