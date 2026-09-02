package reconcile_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

func TestReceiptJSON_IsDeterministicAndRedactsSecrets(t *testing.T) {
	canary := "TEAMKIT_SECRET_CANARY_9f84c3"
	plan, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	first := reconcile.NewReceipt(testDesiredState(t), plan, canary)
	second := reconcile.NewReceipt(testDesiredState(t), plan, canary)
	if err := first.Record(plan.Actions[0].ID, reconcile.EffectFailed, "provider rejected "+canary); err != nil {
		t.Fatal(err)
	}
	if err := second.Record(plan.Actions[0].ID, reconcile.EffectFailed, "provider rejected "+canary); err != nil {
		t.Fatal(err)
	}

	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("equal receipts differ:\n%s\n%s", firstJSON, secondJSON)
	}
	if bytes.Contains(firstJSON, []byte(canary)) {
		t.Fatalf("receipt leaked canary: %s", firstJSON)
	}
	if !bytes.Contains(firstJSON, []byte("[REDACTED]")) {
		t.Fatalf("receipt omitted redaction marker: %s", firstJSON)
	}

	var shape struct {
		SchemaVersion int    `json:"schema_version"`
		StateVersion  string `json:"state_version"`
		PlanHash      string `json:"plan_hash"`
		Desired       struct {
			Project      string `json:"project"`
			AppInstalled bool   `json:"app_installed"`
		} `json:"desired"`
		Checkpoints []struct {
			ActionID string `json:"action_id"`
			Status   string `json:"status"`
		} `json:"checkpoints"`
	}
	if err := json.Unmarshal(firstJSON, &shape); err != nil {
		t.Fatal(err)
	}
	if shape.SchemaVersion != 1 || len(shape.StateVersion) != 64 || len(shape.PlanHash) != 64 || shape.Desired.Project != "aisuz" || !shape.Desired.AppInstalled {
		t.Fatalf("unexpected receipt shape: %#v", shape)
	}
	if len(shape.Checkpoints) != len(plan.Actions) || shape.Checkpoints[0].Status != string(reconcile.EffectFailed) {
		t.Fatalf("unexpected checkpoints: %#v", shape.Checkpoints)
	}
}

func TestReceiptJSON_RoundTripsHermesVersionAndLegacy(t *testing.T) {
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true, KitHome: "/kit", HermesHome: "/home/dev/.hermes", HermesVersion: "0.20.2", Project: domain.ProjectAPA, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills})
	if err != nil {
		t.Fatal(err)
	}
	receipt := reconcile.NewReceipt(desired, reconcile.OperationPlan{})
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"hermes_version":"0.20.2"`)) {
		t.Fatalf("receipt=%s", data)
	}
	loaded, err := reconcile.ParseReceipt(data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loaded.DesiredState()
	if err != nil || got.HermesVersion() != "0.20.2" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestReceiptRecord_FailedEffectCannotAdvanceCheckpoint(t *testing.T) {
	plan, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	receipt := reconcile.NewReceipt(testDesiredState(t), plan)
	if err := receipt.Record(plan.Actions[0].ID, reconcile.EffectFailed, "network unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := receipt.Record(plan.Actions[1].ID, reconcile.EffectSucceeded, ""); err == nil || !strings.Contains(err.Error(), "CHECKPOINT_OUT_OF_ORDER") {
		t.Fatalf("Record(next action) error = %v, want CHECKPOINT_OUT_OF_ORDER", err)
	}
	checkpoints := receipt.Checkpoints()
	if checkpoints[0].Status != reconcile.EffectFailed || checkpoints[1].Status != reconcile.EffectPending {
		t.Fatalf("failed action advanced receipt: %#v", checkpoints)
	}
}

func TestReceiptMatchesDesiredStateExactly(t *testing.T) {
	desired := testDesiredState(t)
	receipt := reconcile.NewReceipt(desired, reconcile.OperationPlan{})
	if !receipt.MatchesDesired(desired) {
		t.Fatal("receipt rejected its own desired state")
	}
	other, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppCursor, AppInstalled: true,
		KitHome: desired.KitHome(), Project: domain.ProjectWMS,
		Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.MatchesDesired(other) {
		t.Fatal("receipt accepted a different desired state")
	}
}

func TestRetryActions_SelectsOnlyIncompleteIdempotentEffects(t *testing.T) {
	plan, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	receipt := reconcile.NewReceipt(testDesiredState(t), plan)
	if err := receipt.Record(plan.Actions[0].ID, reconcile.EffectSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	if err := receipt.Record(plan.Actions[1].ID, reconcile.EffectFailed, "temporary failure"); err != nil {
		t.Fatal(err)
	}
	want := append([]reconcile.Action(nil), plan.Actions[1:]...)
	if got := reconcile.RetryActions(plan, receipt); !reflect.DeepEqual(got, want) {
		t.Fatalf("RetryActions() = %#v, want %#v", got, want)
	}
}

func TestParseReceipt_RoundTripsForRetryAndRejectsNonAtomicShape(t *testing.T) {
	plan, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	receipt := reconcile.NewReceipt(testDesiredState(t), plan)
	if err := receipt.Record(plan.Actions[0].ID, reconcile.EffectSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reconcile.ParseReceipt(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := loaded.Checkpoints(), receipt.Checkpoints(); !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded checkpoints = %#v, want %#v", got, want)
	}
	if got, want := reconcile.RetryActions(plan, loaded), plan.Actions[1:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("retry after load = %#v, want %#v", got, want)
	}
	reencoded, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, data) {
		t.Fatalf("receipt round trip changed bytes:\n%s\n%s", data, reencoded)
	}

	invalid := append([]byte(nil), data[:len(data)-1]...)
	if _, err := reconcile.ParseReceipt(invalid); err == nil {
		t.Fatal("ParseReceipt accepted truncated receipt")
	}
	withExtra := bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":1,"secret":"leak"`), 1)
	if _, err := reconcile.ParseReceipt(withExtra); err == nil {
		t.Fatal("ParseReceipt accepted an extra field")
	}
}

func TestReceiptJSON_RejectsDuplicateObjectKeysForParseAndUnmarshal(t *testing.T) {
	plan, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(reconcile.NewReceipt(testDesiredState(t), plan))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		from []byte
		to   []byte
	}{
		{"top level", []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`)},
		{"desired", []byte(`"project":"aisuz"`), []byte(`"project":"aisuz","project":"aisuz"`)},
		{"checkpoint", []byte(`"action_id":"10-prepare-workspace"`), []byte(`"action_id":"10-prepare-workspace","action_id":"10-prepare-workspace"`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if bytes.Count(data, test.from) != 1 {
				t.Fatalf("fixture occurrence count for %q = %d", test.from, bytes.Count(data, test.from))
			}
			duplicate := bytes.Replace(data, test.from, test.to, 1)
			if _, err := reconcile.ParseReceipt(duplicate); err == nil || !strings.Contains(err.Error(), "RECEIPT_INVALID") {
				t.Fatalf("ParseReceipt() error = %v", err)
			}
			var standalone reconcile.Receipt
			if err := json.Unmarshal(duplicate, &standalone); err == nil || !strings.Contains(err.Error(), "RECEIPT_INVALID") {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
		})
	}
}

func TestParseReceipt_RedactsRegisteredSecretsBeforeDiagnosticsAreExposed(t *testing.T) {
	canary := "TEAMKIT_PARSED_SECRET_CANARY_b61d"
	plan, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	receipt := reconcile.NewReceipt(testDesiredState(t), plan)
	if err := receipt.Record(plan.Actions[0].ID, reconcile.EffectFailed, "failure: "+canary); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reconcile.ParseReceipt(data, canary)
	if err != nil {
		t.Fatal(err)
	}
	diagnostic := loaded.Checkpoints()[0].Diagnostic
	if strings.Contains(diagnostic, canary) || diagnostic != "failure: [REDACTED]" {
		t.Fatalf("parsed diagnostic was not redacted: %q", diagnostic)
	}
}

func TestRetryActions_RejectsCheckpointSequenceThatDoesNotMatchPlan(t *testing.T) {
	plan, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	receipt := reconcile.NewReceipt(testDesiredState(t), plan)
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(data, []byte(plan.Actions[0].ID), []byte("10-substituted-action"), 1)
	loaded, err := reconcile.ParseReceipt(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got := reconcile.RetryActions(plan, loaded); got != nil {
		t.Fatalf("RetryActions accepted mismatched checkpoint sequence: %#v", got)
	}
}

func TestRetryActionsChecked_DistinguishesCompleteFromMismatchedReceipt(t *testing.T) {
	desired := testDesiredState(t)
	plan, err := reconcile.Plan(desired, reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	complete := reconcile.NewReceipt(desired, plan)
	for _, action := range plan.Actions {
		if err := complete.Record(action.ID, reconcile.EffectSucceeded, ""); err != nil {
			t.Fatal(err)
		}
	}
	actions, err := reconcile.RetryActionsChecked(plan, complete)
	if err != nil || len(actions) != 0 {
		t.Fatalf("complete actions=%#v err=%v", actions, err)
	}

	other := reconcile.OperationPlan{Actions: append([]reconcile.Action(nil), plan.Actions...)}
	other.Actions[0].ID = "10-different-action"
	if _, err := reconcile.RetryActionsChecked(other, complete); err == nil || !strings.Contains(err.Error(), "RECEIPT_PLAN_MISMATCH") {
		t.Fatalf("mismatched error=%v", err)
	}
}
