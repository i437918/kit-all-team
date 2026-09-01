package reconcile_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

func TestPlan_EqualDesiredAndCurrentStateIsNoOp(t *testing.T) {
	plan, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{
		WorkspaceReady:   true,
		ContentReady:     true,
		DatabaseReady:    true,
		ToolchainReady:   true,
		ApplicationReady: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("Actions = %#v, want no-op", plan.Actions)
	}
	if got := reconcile.Status(plan); got != reconcile.StatusReady {
		t.Fatalf("Status = %q, want %q", got, reconcile.StatusReady)
	}
}

func TestPlan_PartialStateProducesOrderedMinimalActions(t *testing.T) {
	plan, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{
		WorkspaceReady: true,
		ContentReady:   false,
		DatabaseReady:  true,
		ToolchainReady: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []reconcile.Action{
		{ID: "20-sync-content", Kind: reconcile.ActionSyncContent, Idempotent: true},
		{ID: "40-install-toolchain", Kind: reconcile.ActionInstallToolchain, Idempotent: true},
		{ID: "50-configure-application", Kind: reconcile.ActionConfigureApplication, Idempotent: true},
		{ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true},
	}
	if !reflect.DeepEqual(plan.Actions, want) {
		t.Fatalf("Actions = %#v, want %#v", plan.Actions, want)
	}
	second, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{
		WorkspaceReady: true,
		ContentReady:   false,
		DatabaseReady:  true,
		ToolchainReady: false,
	})
	if err != nil || !reflect.DeepEqual(plan, second) {
		t.Fatalf("equal inputs produced unequal plans: %#v, %#v, %v", plan, second, err)
	}
	if got := reconcile.Status(plan); got != reconcile.StatusNeedsApply {
		t.Fatalf("Status = %q, want %q", got, reconcile.StatusNeedsApply)
	}
}

func TestAllowedUpdateChoices_NonemptyWorkspaceHasExactlyFourChoices(t *testing.T) {
	want := []reconcile.UpdateChoice{
		reconcile.UpdateNone,
		reconcile.UpdateContent,
		reconcile.UpdateDatabase,
		reconcile.UpdateBoth,
	}
	if got := reconcile.AllowedUpdateChoices(true); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedUpdateChoices(true) = %#v, want %#v", got, want)
	}
	if got := reconcile.AllowedUpdateChoices(false); got != nil {
		t.Fatalf("AllowedUpdateChoices(false) = %#v, want nil", got)
	}
}

func TestPlan_SelectiveUpdateAddsOnlySelectedReadySource(t *testing.T) {
	observed := reconcile.ObservedState{
		WorkspaceReady:    true,
		ContentReady:      true,
		DatabaseReady:     true,
		ToolchainReady:    true,
		ApplicationReady:  true,
		NonemptyWorkspace: true,
		Update:            reconcile.UpdateDatabase,
	}
	plan, err := reconcile.Plan(testDesiredState(t), observed)
	if err != nil {
		t.Fatal(err)
	}
	want := []reconcile.Action{
		{ID: "30-sync-database", Kind: reconcile.ActionSyncDatabase, Idempotent: true},
		{ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true},
	}
	if !reflect.DeepEqual(plan.Actions, want) {
		t.Fatalf("Actions = %#v, want %#v", plan.Actions, want)
	}
}

func TestPlan_UpdateWithLocalChangesFailsClosed(t *testing.T) {
	_, err := reconcile.Plan(testDesiredState(t), reconcile.ObservedState{
		WorkspaceReady:    true,
		ContentReady:      true,
		DatabaseReady:     true,
		ToolchainReady:    true,
		ApplicationReady:  true,
		NonemptyWorkspace: true,
		Update:            reconcile.UpdateBoth,
		LocalChanges:      true,
	})
	var validationErr *domain.ValidationError
	if !errors.As(err, &validationErr) || validationErr.Code != domain.LocalChangesDetected {
		t.Fatalf("error = %v, want code %q", err, domain.LocalChangesDetected)
	}
}

func testDesiredState(t *testing.T) domain.DesiredState {
	t.Helper()
	state, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS:           domain.OSLinux,
		Application:  domain.AppHermes,
		AppInstalled: true,
		KitHome:      "/srv/teamkit",
		HermesHome:   "/srv/hermes",
		Project:      domain.ProjectAISUZ,
		Role:         domain.RoleDeveloper,
		Toolchain:    domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}
