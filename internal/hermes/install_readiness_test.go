package hermes

import (
	"context"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManagedInstallReady_RequiresExactPinnedDetachedHeadAndExecutable(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		origin     string
		commit     string
		status     string
		executable bool
		want       bool
	}{
		{name: "exact pinned clean checkout", origin: HermesSourceOrigin, commit: HermesSourceCommit, executable: true, want: true},
		{name: "wrong origin", origin: "https://attacker.invalid/hermes.git", commit: HermesSourceCommit, executable: true},
		{name: "wrong commit", origin: HermesSourceOrigin, commit: strings.Repeat("0", 40), executable: true},
		{name: "dirty tracked checkout", origin: HermesSourceOrigin, commit: HermesSourceCommit, status: " M hermes_cli/main.py\n", executable: true},
		{name: "untracked checkout", origin: HermesSourceOrigin, commit: HermesSourceCommit, status: "?? injected.py\n", executable: true},
		{name: "missing executable", origin: HermesSourceOrigin, commit: HermesSourceCommit},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			home := testutil.TempDir(t)
			writeManagedInstallFixture(t, home, fixture.executable)

			git := checkoutGitFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				switch {
				case strings.Contains(joined, "config --local --get remote.origin.url"):
					return []byte(fixture.origin + "\n"), nil
				case strings.Contains(joined, "rev-parse --verify HEAD^{commit}"):
					return []byte(fixture.commit + "\n"), nil
				case strings.Contains(joined, "status --porcelain"):
					return []byte(fixture.status), nil
				default:
					t.Fatalf("unexpected Git inspection: %q", joined)
					return nil, nil
				}
			})
			got, err := managedInstallReady(context.Background(), home, HermesSourceOrigin, HermesSourceCommit, git)
			if err != nil {
				t.Fatalf("ManagedInstallReady() error = %v", err)
			}
			if got != fixture.want {
				t.Fatalf("ManagedInstallReady() = %v, want %v", got, fixture.want)
			}
		})
	}
}

func TestManagedInstallReady_RejectsNonExecutableBinaryOnPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot represent POSIX executable permission bits")
	}
	home := testutil.TempDir(t)
	writeManagedInstallFixture(t, home, true)
	executable := filepath.Join(home, "hermes-agent", "venv", "bin", "hermes")
	if err := os.Chmod(executable, 0o600); err != nil {
		t.Fatal(err)
	}

	ready, err := managedInstallReady(context.Background(), home, HermesSourceOrigin, HermesSourceCommit, successfulCheckoutGit())
	if err != nil {
		t.Fatalf("ManagedInstallReady() error = %v", err)
	}
	if ready {
		t.Fatal("non-executable Hermes binary was reported ready")
	}
}

func TestManagedInstallReady_RejectsEmptyExecutable(t *testing.T) {
	home := testutil.TempDir(t)
	writeManagedInstallFixture(t, home, true)
	executable := filepath.Join(home, "hermes-agent", "venv", "bin", "hermes")
	if err := os.WriteFile(executable, nil, 0o700); err != nil {
		t.Fatal(err)
	}

	ready, err := managedInstallReady(context.Background(), home, HermesSourceOrigin, HermesSourceCommit, successfulCheckoutGit())
	if err != nil {
		t.Fatalf("ManagedInstallReady() error = %v", err)
	}
	if ready {
		t.Fatal("empty Hermes executable was reported ready")
	}
}

func TestManagedInstallReady_RejectsRedirectedExecutable(t *testing.T) {
	home := testutil.TempDir(t)
	writeManagedInstallFixture(t, home, false)
	external := filepath.Join(testutil.TempDir(t), "hermes")
	if err := os.WriteFile(external, []byte("external\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(home, "hermes-agent", "venv", "bin", "hermes")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, executable); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	ready, err := ManagedInstallReady(home)
	if ready || !errors.Is(err, ErrInstallLayout) {
		t.Fatalf("ManagedInstallReady() = %v, %v; want false, ErrInstallLayout", ready, err)
	}
}

func writeManagedInstallFixture(t *testing.T, home string, executable bool) {
	t.Helper()
	checkout := filepath.Join(home, "hermes-agent")
	if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".git", "HEAD"), []byte(HermesSourceCommit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !executable {
		return
	}
	path := filepath.Join(checkout, "venv", "bin", "hermes")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

type checkoutGitFunc func(context.Context, string, ...string) ([]byte, error)

func (f checkoutGitFunc) Run(ctx context.Context, directory string, args ...string) ([]byte, error) {
	return f(ctx, directory, args...)
}

func successfulCheckoutGit() checkoutGitFunc {
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "config --local --get remote.origin.url"):
			return []byte(HermesSourceOrigin + "\n"), nil
		case strings.Contains(joined, "rev-parse --verify HEAD^{commit}"):
			return []byte(HermesSourceCommit + "\n"), nil
		case strings.Contains(joined, "status --porcelain"):
			return nil, nil
		default:
			return nil, errors.New("unexpected Git inspection")
		}
	}
}
