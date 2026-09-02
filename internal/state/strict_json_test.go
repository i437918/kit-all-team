package state

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestStoreLoadOperation_RejectsDuplicateObjectKeysRecursively(t *testing.T) {
	store, root, data := strictJSONOperationFixture(t)
	tests := []struct {
		name string
		from string
		to   string
	}{
		{"top-level operation field", `{"schema_version":1,"plan"`, `{"schema_version":1,"schema_version":1,"plan"`},
		{"nested desired field", `"project":"apa"`, `"project":"apa","project":"apa"`},
		{"nested action field", `"id":"10-prepare-workspace"`, `"id":"10-prepare-workspace","id":"10-prepare-workspace"`},
		{"nested checkpoint field", `"action_id":"10-prepare-workspace"`, `"action_id":"10-prepare-workspace","action_id":"10-prepare-workspace"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := replaceStrictJSONOnce(t, data, test.from, test.to)
			if err := os.WriteFile(filepath.Join(root, ".teamkit", "operation.json"), mutated, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.LoadOperation(); err == nil || !strings.Contains(err.Error(), "OPERATION_INVALID") {
				t.Fatalf("LoadOperation() error = %v", err)
			}
		})
	}
}

func TestStoreLoadPlanAndReceipt_RejectDuplicateNestedFields(t *testing.T) {
	store, root, plan, receipt := strictJSONStateFixture(t)
	if err := store.SavePlan(plan); err != nil {
		t.Fatal(err)
	}
	planData, err := os.ReadFile(filepath.Join(root, ".teamkit", "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	planData = replaceStrictJSONOnce(t, planData, `"id":"10-prepare-workspace"`, `"id":"10-prepare-workspace","id":"10-prepare-workspace"`)
	if err := os.WriteFile(filepath.Join(root, ".teamkit", "plan.json"), planData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadPlan(); err == nil || !strings.Contains(err.Error(), "PLAN_INVALID") {
		t.Fatalf("LoadPlan() error = %v", err)
	}

	if err := store.SaveReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	receiptData, err := os.ReadFile(filepath.Join(root, ".teamkit", "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	receiptData = replaceStrictJSONOnce(t, receiptData, `"action_id":"10-prepare-workspace"`, `"action_id":"10-prepare-workspace","action_id":"10-prepare-workspace"`)
	if err := os.WriteFile(filepath.Join(root, ".teamkit", "receipt.json"), receiptData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadReceipt(); err == nil || !strings.Contains(err.Error(), "RECEIPT_INVALID") {
		t.Fatalf("LoadReceipt() error = %v", err)
	}
}

func strictJSONOperationFixture(t *testing.T) (*Store, string, []byte) {
	t.Helper()
	store, root, plan, receipt := strictJSONStateFixture(t)
	if err := store.SaveOperation(plan, receipt); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".teamkit", "operation.json"))
	if err != nil {
		t.Fatal(err)
	}
	return store, root, data
}

func strictJSONStateFixture(t *testing.T) (*Store, string, reconcile.OperationPlan, *reconcile.Receipt) {
	t.Helper()
	root := testutil.TempDir(t)
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppCursor, AppInstalled: true,
		KitHome: root, Project: domain.ProjectAPA, Role: domain.RoleDeveloper,
		Toolchain: domain.ToolchainAIRules1C,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := reconcile.OperationPlan{
		ContractHash: "fixture-contract",
		Actions: []reconcile.Action{{
			ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true,
		}},
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	return store, root, plan, reconcile.NewReceipt(desired, plan)
}

func replaceStrictJSONOnce(t *testing.T, data []byte, from, to string) []byte {
	t.Helper()
	if bytes.Count(data, []byte(from)) != 1 {
		t.Fatalf("fixture occurrence count for %q = %d in %s", from, bytes.Count(data, []byte(from)), data)
	}
	return bytes.Replace(data, []byte(from), []byte(to), 1)
}
