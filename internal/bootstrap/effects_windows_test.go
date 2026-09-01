//go:build windows

package bootstrap

import (
	"context"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

func TestEffects_HermesProfileRejectsJunctionParent(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state.HermesHome(), 0o700); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(state.HermesHome(), "profiles")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, testutil.TempDir(t)).CombinedOutput(); err != nil {
		t.Fatalf("create test junction: %v: %s", err, output)
	}
	createCalls := 0
	effects := &Effects{
		Git:       GitPortFunc{},
		OfficeCLI: officeCLIFixture(state),
		Profile: ProfilePortFuncs{CreateFunc: func(context.Context, string) error {
			createCalls++
			return nil
		}},
	}
	err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain})
	if !errors.Is(err, ErrForeignProfile) {
		t.Fatalf("InstallToolchain error = %v; want ErrForeignProfile", err)
	}
	if createCalls != 0 {
		t.Fatalf("profile create crossed junction parent: calls=%d", createCalls)
	}
}

func TestEffects_DatabaseRejectsJunctionBeforeGit(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(home, "db")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("create test junction: %v: %s", err, output)
	}
	gitCalls := 0
	effects := &Effects{OfficeCLI: officeCLIFixture(state), Git: GitPortFunc{
		CloneDatabaseFunc:  func(context.Context, string, string) error { gitCalls++; return nil },
		UpdateDatabaseFunc: func(context.Context, string, string) error { gitCalls++; return nil },
	}}

	err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionSyncDatabase})
	if !errors.Is(err, ErrForeignWorkspace) {
		t.Fatalf("SyncDatabase error = %v, want ErrForeignWorkspace", err)
	}
	if gitCalls != 0 {
		t.Fatalf("Git crossed db junction: calls=%d", gitCalls)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
}
