package workspace

import (
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

func TestClassify_EmptyAndForeignResidue(t *testing.T) {
	dir := testutil.TempDir(t)

	state, err := Classify(dir)
	if err != nil || state != Empty {
		t.Fatalf("empty workspace = %q, %v; want %q, nil", state, err, Empty)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err = Classify(dir)
	if err != nil || state != NonEmpty {
		t.Fatalf("foreign residue = %q, %v; want %q, nil", state, err, NonEmpty)
	}
}

func TestEnsureOwner_RejectsAnotherProject(t *testing.T) {
	dir := testutil.TempDir(t)
	if err := EnsureOwner(dir, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOwner(dir, "alpha"); err != nil {
		t.Fatalf("same owner: %v", err)
	}
	if err := EnsureOwner(dir, "beta"); ErrorCode(err) != "WORKSPACE_OWNED" {
		t.Fatalf("other owner error = %v, code %q; want WORKSPACE_OWNED", err, ErrorCode(err))
	}
}

func TestEnsureOwner_ConcurrentClaimsNeverBothSucceed(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		root := filepath.Join(testutil.TempDir(t), "workspace")
		start := make(chan struct{})
		results := make(chan struct {
			project string
			err     error
		}, 32)
		var ready sync.WaitGroup
		ready.Add(32)
		for index := 0; index < 32; index++ {
			project := "alpha"
			if index%2 != 0 {
				project = "beta"
			}
			go func() {
				ready.Done()
				<-start
				results <- struct {
					project string
					err     error
				}{project: project, err: EnsureOwner(root, project)}
			}()
		}
		ready.Wait()
		close(start)

		succeeded := map[string]bool{}
		for index := 0; index < 32; index++ {
			result := <-results
			if result.err == nil {
				succeeded[result.project] = true
			} else if ErrorCode(result.err) != "WORKSPACE_OWNED" {
				t.Fatalf("attempt %d project %q error = %v", attempt, result.project, result.err)
			}
		}
		if len(succeeded) != 1 {
			t.Fatalf("attempt %d successful competing claims = %#v; want one project only", attempt, succeeded)
		}
	}
}

func TestWriteFileAtomic_ReplacesOnlyCompleteContent(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "settings", "state")
	if err := WriteFileAtomic(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "second" {
		t.Fatalf("content = %q, %v; want second, nil", got, err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v; want 0600", info.Mode())
	}
}

func TestWriteFileAtomic_RejectsSymlinkedParentWithoutTouchingTarget(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "settings")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	err := WriteFileAtomic(filepath.Join(link, "state"), []byte("escaped"), 0o600)
	if !errors.Is(err, pathsafe.ErrUnsafe) {
		t.Fatalf("WriteFileAtomic() error = %v, want pathsafe.ErrUnsafe", err)
	}
	if _, err := os.Stat(filepath.Join(external, "state")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through symlink: %v", err)
	}
	contents, err := os.ReadFile(sentinel)
	if err != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, err)
	}
}
