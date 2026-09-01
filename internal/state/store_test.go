package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestStoreLoadOperation_RejectsDocumentAboveBound(t *testing.T) {
	root := testutil.TempDir(t)
	if err := os.Mkdir(filepath.Join(root, ".teamkit"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".teamkit", "operation.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), MaxOperationBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadOperation(); err == nil || !strings.Contains(err.Error(), "OPERATION_TOO_LARGE") {
		t.Fatalf("err=%v", err)
	}
}

func TestStoreRoundTripsPlanAndRedactedReceipt(t *testing.T) {
	root := testutil.TempDir(t)
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSWindows, Application: domain.AppHermes, AppInstalled: true,
		KitHome: root, HermesHome: filepath.Join(root, "hermes"),
		Project: domain.ProjectWMS, Role: domain.RoleArchitect,
		Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := reconcile.Plan(desired, reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(plan); err != nil {
		t.Fatal(err)
	}
	loadedPlan, err := store.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loadedPlan, plan) {
		t.Fatalf("loaded plan = %+v, want %+v", loadedPlan, plan)
	}

	const canary = "TEAMKIT_SECRET_CANARY"
	receipt := reconcile.NewReceipt(desired, plan, canary)
	if err := receipt.Record(plan.Actions[0].ID, reconcile.EffectFailed, "failure "+canary); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(root, ".teamkit", "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bytes), canary) {
		t.Fatal("receipt file contains secret canary")
	}
	loadedReceipt, err := store.LoadReceipt(canary)
	if err != nil {
		t.Fatal(err)
	}
	if got := loadedReceipt.Checkpoints()[0].Diagnostic; !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("diagnostic = %q, want redaction", got)
	}
}

func TestStoreAtomicallyRoundTripsOperationEnvelope(t *testing.T) {
	root := testutil.TempDir(t)
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppCursor, AppInstalled: true,
		KitHome: root, Project: domain.ProjectWMS, Role: domain.RoleDeveloper,
		Toolchain: domain.ToolchainAIRules1C,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := reconcile.Plan(desired, reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	const canary = "TEAMKIT_OPERATION_SECRET_CANARY"
	receipt := reconcile.NewReceipt(desired, plan, canary)
	if err := receipt.Record(plan.Actions[0].ID, reconcile.EffectFailed, "failure "+canary); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOperation(plan, receipt); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".teamkit", "operation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), canary) {
		t.Fatal("operation envelope contains secret canary")
	}
	loadedPlan, loadedReceipt, err := store.LoadOperation(canary)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loadedPlan, plan) || !reflect.DeepEqual(loadedReceipt.Checkpoints(), receipt.Checkpoints()) {
		t.Fatalf("operation mismatch: plan=%#v receipt=%#v", loadedPlan, loadedReceipt.Checkpoints())
	}
	compatPlan, err := store.LoadPlan()
	if err != nil || !reflect.DeepEqual(compatPlan, plan) {
		t.Fatalf("LoadPlan from operation=%#v err=%v", compatPlan, err)
	}
	compatReceipt, err := store.LoadReceipt(canary)
	if err != nil || !reflect.DeepEqual(compatReceipt.Checkpoints(), receipt.Checkpoints()) {
		t.Fatalf("LoadReceipt from operation=%#v err=%v", compatReceipt, err)
	}
}

func TestNewRejectsRelativeStateRoot(t *testing.T) {
	if _, err := New("relative/workspace"); err == nil {
		t.Fatal("New() error = nil, want absolute-root rejection")
	}
}

func TestStoreLoadRejectsSymlinkedStateLeafWithoutReadingTarget(t *testing.T) {
	for _, name := range []string{"plan.json", "receipt.json"} {
		t.Run(name, func(t *testing.T) {
			root := testutil.TempDir(t)
			metadata := filepath.Join(root, ".teamkit")
			if err := os.Mkdir(metadata, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(testutil.TempDir(t), "sentinel")
			if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(metadata, name)); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			store, err := New(root)
			if err != nil {
				t.Fatal(err)
			}
			if name == "plan.json" {
				_, err = store.LoadPlan()
			} else {
				_, err = store.LoadReceipt()
			}
			if !errors.Is(err, pathsafe.ErrUnsafe) {
				t.Fatalf("load error = %v, want pathsafe.ErrUnsafe", err)
			}
			contents, readErr := os.ReadFile(target)
			if readErr != nil || string(contents) != "outside" {
				t.Fatalf("external sentinel = %q, %v", contents, readErr)
			}
		})
	}
}
