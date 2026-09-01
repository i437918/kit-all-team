package engine

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

func TestExecutePreparedRejectsDifferentCurrentContractBeforeEffects(t *testing.T) {
	desired := testDesired(t)
	oldContract := strings.Repeat("a", sha256.Size*2)
	currentContract := strings.Repeat("b", sha256.Size*2)
	plan := reconcile.OperationPlan{
		ContractHash: oldContract,
		Actions: []reconcile.Action{{
			ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true,
		}},
	}
	store := &memoryStore{}
	if err := (Engine{Store: store, ContractHash: oldContract}).Prepare(desired, plan); err != nil {
		t.Fatal(err)
	}
	effects := &fakeEffects{}

	err := (Engine{Store: store, Effects: effects, ContractHash: currentContract}).ExecutePrepared(context.Background(), desired, plan)
	if err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
		t.Fatalf("ExecutePrepared error = %v, want OPERATION_CONTRACT_MISMATCH", err)
	}
	if len(effects.applied) != 0 {
		t.Fatalf("contract mismatch applied effects: %v", effects.applied)
	}
}
