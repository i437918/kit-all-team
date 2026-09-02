package operationlock

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
)

func TestAcquireRejectsConcurrentHolderAndReleases(t *testing.T) {
	root := testutil.TempDir(t)
	if err := os.Mkdir(filepath.Join(root, ".teamkit"), 0o700); err != nil {
		t.Fatal(err)
	}
	first, err := Acquire(root)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Close()

	path := filepath.Join(root, ".teamkit", "operation.lock")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("lock path is not a regular file: info=%v err=%v", info, err)
	}
	if err := privatefile.Validate(path); err != nil {
		t.Fatalf("lock file is not owner-only: %v", err)
	}

	if _, err := Acquire(root); !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("second Acquire error = %v, want ErrOperationInProgress", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	third, err := Acquire(root)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("third Close: %v", err)
	}
}

func TestAcquireRejectsRedirectedLockPath(t *testing.T) {
	root := testutil.TempDir(t)
	metadata := filepath.Join(root, ".teamkit")
	if err := os.Mkdir(metadata, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(testutil.TempDir(t), "outside.lock")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(metadata, "operation.lock")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Acquire(root); !errors.Is(err, pathsafe.ErrUnsafe) {
		t.Fatalf("Acquire redirected lock error = %v, want pathsafe.ErrUnsafe", err)
	}
}

func TestAcquireIsReleasedWhenHolderProcessDies(t *testing.T) {
	root := testutil.TempDir(t)
	if err := os.Mkdir(filepath.Join(root, ".teamkit"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=TestOperationLockProcessHelper$")
	command.Env = append(os.Environ(), "TEAMKIT_LOCK_HELPER_ROOT="+root)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	ready, err := reader.ReadString('\n')
	if err != nil || ready != "LOCKED\n" {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("helper readiness = %q, %v", ready, err)
	}
	if _, err := Acquire(root); !errors.Is(err, ErrOperationInProgress) {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("Acquire while helper runs = %v, want ErrOperationInProgress", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for {
		lock, acquireErr := Acquire(root)
		if acquireErr == nil {
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
			break
		}
		if !errors.Is(acquireErr, ErrOperationInProgress) || time.Now().After(deadline) {
			t.Fatalf("Acquire after helper death = %v", acquireErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOperationLockProcessHelper(t *testing.T) {
	root := os.Getenv("TEAMKIT_LOCK_HELPER_ROOT")
	if root == "" {
		return
	}
	lock, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Println("LOCKED")
	for {
		time.Sleep(time.Hour)
	}
}
