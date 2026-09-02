package environment

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/config"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/state"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

func TestInspector_PendingFirstRunWinsBeforeMissingOwnerAndEnv(t *testing.T) {
	root, desired, plan := pendingOperationFixture(t)
	if _, err := os.Lstat(filepath.Join(root, ".teamkit", "owner")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owner exists: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".env")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("env exists: %v", err)
	}
	got, state, err := NewInspector().Inspect(context.Background(), root)
	var inspectionErr *Error
	if state != RetryRequired || !got.Pending || !errors.As(err, &inspectionErr) || inspectionErr.State != RetryRequired {
		t.Fatalf("got=%#v state=%v err=%T %v", got, state, err, err)
	}
	if got.Desired.Project() != desired.Project() || got.Desired.KitHome() != root || len(plan.Actions) == 0 {
		t.Fatalf("got=%#v desired=%#v plan=%#v", got, desired, plan)
	}
}

func TestInspector_TerminalUnsafeRootIsForeignBeforeFilesystemInspection(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "root\n\x1b\u0085\u202e")
	_, state, err := NewInspector().Inspect(context.Background(), root)
	var typed *Error
	if state != Foreign || !errors.As(err, &typed) || typed.State != Foreign || !errors.Is(err, ErrTerminalUnsafePath) {
		t.Fatalf("state=%v err=%T %v", state, err, err)
	}
	addState, err := NewInspector().ClassifyAdd(context.Background(), root)
	if addState != AddTargetReady || !errors.As(err, &typed) || typed.State != Foreign || !errors.Is(err, ErrTerminalUnsafePath) {
		t.Fatalf("addState=%v err=%T %v", addState, err, err)
	}
}

func TestInspector_PendingOperationDoesNotReadPoisonedOwnerOrEnv(t *testing.T) {
	root, _, _ := pendingOperationFixture(t)
	if err := os.WriteFile(filepath.Join(root, ".teamkit", "owner"), []byte("wrong\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=CANARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, state, err := NewInspector().Inspect(context.Background(), root)
	var inspectionErr *Error
	if state != RetryRequired || !errors.As(err, &inspectionErr) || inspectionErr.State != RetryRequired {
		t.Fatalf("state=%v err=%T %v", state, err, err)
	}
}

func TestInspector_ClassifiesStructuralFailuresWithTypedErrors(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(t *testing.T, root string)
		wantState InspectionState
	}{
		{"owner mismatch", func(t *testing.T, root string) { writeFile(t, filepath.Join(root, ".teamkit", "owner"), "wms\n") }, Foreign},
		{"home mismatch", func(t *testing.T, root string) { rewriteEnvHome(t, root, filepath.Join(root, "other")) }, Foreign},
		{"malformed env", func(t *testing.T, root string) { writeFile(t, filepath.Join(root, ".env"), "TOKEN=CANARY\n") }, Foreign},
		{"oversized env", func(t *testing.T, root string) {
			writeBytes(t, filepath.Join(root, ".env"), bytes.Repeat([]byte("x"), maxPublicEnvBytes+1))
		}, Foreign},
		{"missing root", func(t *testing.T, root string) {
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
		}, Foreign},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, _ := readyEnvironmentFixture(t)
			test.mutate(t, root)
			_, state, err := NewInspector().Inspect(context.Background(), root)
			var inspectionErr *Error
			if state != test.wantState || !errors.As(err, &inspectionErr) || inspectionErr.State != test.wantState {
				t.Fatalf("state=%v err=%T %v", state, err, err)
			}
		})
	}
}

func writeBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	writeBytes(t, path, []byte(data))
}

func rewriteEnvHome(t *testing.T, root, home string) {
	t.Helper()
	desired := readyDesiredState(t, root)
	values := config.Encode(desired)
	values["KIT_ALL_TEAM_HOME"] = home
	if err := workspace.WritePublicEnv(filepath.Join(root, ".env"), values); err != nil {
		t.Fatal(err)
	}
}

func pendingOperationFixture(t *testing.T) (string, domain.DesiredState, reconcile.OperationPlan) {
	t.Helper()
	root := filepath.Join(testutil.TempDir(t), "kit")
	desired := readyDesiredState(t, root)
	plan := reconcile.OperationPlan{ContractHash: "fixture-contract", Actions: []reconcile.Action{{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true}}}
	store, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil {
		t.Fatal(err)
	}
	return root, desired, plan
}

func TestInspector_PendingReceiptMustMatchCandidateBeforeItIsDisplayable(t *testing.T) {
	candidate := filepath.Join(testutil.TempDir(t), "candidate")
	desiredHome := filepath.Join(testutil.TempDir(t), "receipt-home")
	desired := readyDesiredState(t, desiredHome)
	plan := reconcile.OperationPlan{ContractHash: "fixture-contract", Actions: []reconcile.Action{{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true}}}
	store, err := state.New(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil {
		t.Fatal(err)
	}
	got, inspectionState, err := NewInspector().Inspect(context.Background(), candidate)
	var inspectionErr *Error
	if inspectionState != Foreign || !errors.As(err, &inspectionErr) || inspectionErr.State != Foreign || got.Pending {
		t.Fatalf("got=%#v state=%v err=%T %v", got, inspectionState, err, err)
	}
}

func readyDesiredState(t *testing.T, root string) domain.DesiredState {
	t.Helper()
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
		KitHome: root, HermesHome: filepath.Join(filepath.Dir(root), "hermes"), HermesVersion: "0.20.2",
		Project: domain.ProjectAPA, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	return desired
}

func readyEnvironmentFixture(t *testing.T) (string, domain.DesiredState) {
	t.Helper()
	root := filepath.Join(testutil.TempDir(t), "kit")
	if err := os.MkdirAll(filepath.Join(root, ".teamkit"), 0o700); err != nil {
		t.Fatal(err)
	}
	desired := readyDesiredState(t, root)
	writeFile(t, filepath.Join(root, ".teamkit", "owner"), "apa\n")
	if err := workspace.WritePublicEnv(filepath.Join(root, ".env"), config.Encode(desired)); err != nil {
		t.Fatal(err)
	}
	return root, desired
}

func TestInspector_RejectsEmptyRelativeAndRedirectedMetadata(t *testing.T) {
	inspector := NewInspector()
	for _, home := range []string{"", "relative"} {
		_, state, err := inspector.Inspect(context.Background(), home)
		if state != Foreign || err == nil || !strings.Contains(err.Error(), "FOREIGN_WORKSPACE") {
			t.Fatalf("home=%q state=%v err=%v", home, state, err)
		}
	}
	for _, name := range []string{"owner", ".env"} {
		t.Run(name, func(t *testing.T) {
			root, _ := readyEnvironmentFixture(t)
			target := filepath.Join(root, ".teamkit", "owner")
			if name == ".env" {
				target = filepath.Join(root, ".env")
			}
			sentinel := filepath.Join(testutil.TempDir(t), "sentinel")
			writeFile(t, sentinel, "TEAMKIT_SECRET_CANARY\n")
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(sentinel, target); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			_, state, err := inspector.Inspect(context.Background(), root)
			if state != Foreign || err == nil || !strings.Contains(err.Error(), "FOREIGN_WORKSPACE") {
				t.Fatalf("state=%v err=%v", state, err)
			}
			body, readErr := os.ReadFile(sentinel)
			if readErr != nil || string(body) != "TEAMKIT_SECRET_CANARY\n" {
				t.Fatalf("sentinel=%q err=%v", body, readErr)
			}
		})
	}
}

func TestInspector_UnreadablePublicEnvIsInspectionFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode-bit denial is covered by native Windows ACL tests")
	}
	root, _ := readyEnvironmentFixture(t)
	path := filepath.Join(root, ".env")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	_, state, err := NewInspector().Inspect(context.Background(), root)
	if err == nil {
		t.Skip("native executor can read mode-zero fixture")
	}
	if state != InspectionFailed || err == nil || !strings.Contains(err.Error(), "WORKSPACE_INSPECTION_FAILED") {
		t.Fatalf("state=%v err=%v", state, err)
	}
}

func TestInspector_ClassifyAddIsReadOnlyAndDistinguishesTargets(t *testing.T) {
	inspector := NewInspector()
	missing := filepath.Join(testutil.TempDir(t), "missing")
	if got, err := inspector.ClassifyAdd(context.Background(), missing); err != nil || got != AddTargetReady {
		t.Fatalf("missing state=%v err=%v", got, err)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ClassifyAdd created missing target: %v", err)
	}
	empty := filepath.Join(testutil.TempDir(t), "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := inspector.ClassifyAdd(context.Background(), empty); err != nil || got != AddTargetReady {
		t.Fatalf("empty state=%v err=%v", got, err)
	}
	ready, _ := readyEnvironmentFixture(t)
	if got, err := inspector.ClassifyAdd(context.Background(), ready); err != nil || got != AddWorkspaceExists {
		t.Fatalf("ready state=%v err=%v", got, err)
	}
	foreign := filepath.Join(testutil.TempDir(t), "foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(foreign, "unrelated"), "unchanged")
	got, err := inspector.ClassifyAdd(context.Background(), foreign)
	var inspectionErr *Error
	if got != AddTargetReady || !errors.As(err, &inspectionErr) || inspectionErr.State != Foreign {
		t.Fatalf("foreign state=%v err=%T %v", got, err, err)
	}
}

func TestInspector_ClassifyAdd_AllowsValidatedWizardCheckpoint(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "checkpoint")
	if err := workspace.WriteWizardEnv(root, map[string]string{
		"TEAMKIT_APP_ID":         "codex",
		"TEAMKIT_MODE":           "add",
		"TEAMKIT_PROJECT_ID":     "asku",
		"TEAMKIT_ROLE_ID":        "architect",
		"TEAMKIT_TOOLCHAIN_ID":   "cc_1c_skills",
		"TEAMKIT_WORKSPACE_ROOT": root,
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := NewInspector().ClassifyAdd(context.Background(), root); err != nil || got != AddTargetReady {
		t.Fatalf("ClassifyAdd() = %v, %v; want %v, nil", got, err, AddTargetReady)
	}
}

func TestInspector_ClassifyAddRejectsRegularFileAncestorAsForeign(t *testing.T) {
	root := testutil.TempDir(t)
	ancestor := filepath.Join(root, "regular-file")
	writeFile(t, ancestor, "unchanged")

	got, err := NewInspector().ClassifyAdd(context.Background(), filepath.Join(ancestor, "child"))
	var inspectionErr *Error
	if got != AddTargetReady || !errors.As(err, &inspectionErr) || inspectionErr.State != Foreign {
		t.Fatalf("state=%v err=%T %v", got, err, err)
	}
	contents, readErr := os.ReadFile(ancestor)
	if readErr != nil || string(contents) != "unchanged" {
		t.Fatalf("regular-file ancestor = %q, %v", contents, readErr)
	}
}
