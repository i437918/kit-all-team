package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

type fakeEffects struct {
	observed reconcile.ObservedState
	applied  []string
	failID   string
	onApply  func()
}

func (f *fakeEffects) Observe(context.Context, domain.DesiredState, reconcile.UpdateChoice) (reconcile.ObservedState, error) {
	return f.observed, nil
}

func (f *fakeEffects) Apply(_ context.Context, _ domain.DesiredState, action reconcile.Action) error {
	f.applied = append(f.applied, action.ID)
	if f.onApply != nil {
		f.onApply()
	}
	if action.ID == f.failID {
		return errors.New("injected effect failure")
	}
	return nil
}

type memoryStore struct {
	plan     reconcile.OperationPlan
	receipt  *reconcile.Receipt
	saveRuns int
	onSave   func()
	failSave int
}

func (s *memoryStore) SavePlan(plan reconcile.OperationPlan) error {
	s.plan = plan
	return nil
}

func (s *memoryStore) LoadPlan() (reconcile.OperationPlan, error) { return s.plan, nil }

func (s *memoryStore) SaveReceipt(receipt *reconcile.Receipt) error {
	s.receipt = receipt
	s.saveRuns++
	if s.onSave != nil {
		s.onSave()
	}
	if s.failSave == s.saveRuns {
		return errors.New("injected checkpoint failure")
	}
	return nil
}

func (s *memoryStore) LoadReceipt(...string) (*reconcile.Receipt, error) { return s.receipt, nil }

func TestApplyPersistsEveryCheckpointInPlanOrder(t *testing.T) {
	desired := testDesired(t)
	effects := &fakeEffects{}
	store := &memoryStore{}
	runner := Engine{Effects: effects, Store: store}

	plan, err := runner.Apply(context.Background(), desired, reconcile.UpdateNone)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Fatal("Apply() returned an empty first-run plan")
	}
	want := actionIDs(plan.Actions)
	if !reflect.DeepEqual(effects.applied, want) {
		t.Fatalf("effect order = %v, want %v", effects.applied, want)
	}
	if store.saveRuns != len(plan.Actions)+1 {
		t.Fatalf("receipt saves = %d, want initial plus every action (%d)", store.saveRuns, len(plan.Actions)+1)
	}
	for _, checkpoint := range store.receipt.Checkpoints() {
		if checkpoint.Status != reconcile.EffectSucceeded {
			t.Fatalf("checkpoint = %+v, want succeeded", checkpoint)
		}
	}
}

func TestExecutePreparedReportsStartedCheckpointCompleted(t *testing.T) {
	desired := testDesired(t)
	plan := reconcile.OperationPlan{Actions: []reconcile.Action{{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true}}}
	sequence := []string{}
	store := &memoryStore{}
	runner := Engine{Effects: &fakeEffects{onApply: func() { sequence = append(sequence, "effect") }}, Store: store}
	if err := runner.Prepare(desired, plan); err != nil {
		t.Fatal(err)
	}
	store.onSave = func() { sequence = append(sequence, "checkpoint") }
	ctx := reconcile.WithProgressObserver(context.Background(), func(event reconcile.ProgressEvent) {
		sequence = append(sequence, string(event.Phase))
	})
	if err := runner.ExecutePrepared(ctx, desired, plan); err != nil {
		t.Fatal(err)
	}
	want := []string{"started", "effect", "checkpoint", "completed"}
	if !reflect.DeepEqual(sequence, want) {
		t.Fatalf("sequence=%v want=%v", sequence, want)
	}
}

func TestExecutePreparedReportsFailedInsteadOfCompletedOnEffectOrCheckpointError(t *testing.T) {
	for _, test := range []struct {
		name       string
		failEffect bool
		failSave   int
	}{
		{name: "effect", failEffect: true},
		{name: "checkpoint", failSave: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			desired := testDesired(t)
			plan := reconcile.OperationPlan{Actions: []reconcile.Action{{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true}}}
			effects := &fakeEffects{}
			store := &memoryStore{}
			runner := Engine{Effects: effects, Store: store}
			if err := runner.Prepare(desired, plan); err != nil {
				t.Fatal(err)
			}
			if test.failEffect {
				effects.failID = plan.Actions[0].ID
			}
			store.failSave = test.failSave
			phases := []reconcile.ProgressPhase{}
			ctx := reconcile.WithProgressObserver(context.Background(), func(event reconcile.ProgressEvent) {
				phases = append(phases, event.Phase)
			})
			if err := runner.ExecutePrepared(ctx, desired, plan); err == nil {
				t.Fatal("ExecutePrepared() error=nil")
			}
			want := []reconcile.ProgressPhase{reconcile.ProgressStarted, reconcile.ProgressFailed}
			if !reflect.DeepEqual(phases, want) {
				t.Fatalf("phases=%v want=%v", phases, want)
			}
		})
	}
}

func TestRunReportsFailedWhenReceiptRecordRejectsAction(t *testing.T) {
	desired := testDesired(t)
	plan, err := reconcile.Plan(desired, reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	receipt := reconcile.NewReceipt(desired, plan)
	phases := []reconcile.ProgressPhase{}
	ctx := reconcile.WithProgressObserver(context.Background(), func(event reconcile.ProgressEvent) {
		phases = append(phases, event.Phase)
	})
	runner := Engine{Effects: &fakeEffects{}, Store: &memoryStore{}}
	if err := runner.run(ctx, desired, plan, []reconcile.Action{plan.Actions[1]}, receipt); err == nil || err.Error() != `CHECKPOINT_OUT_OF_ORDER: "20-sync-content"` {
		t.Fatalf("run() error=%v", err)
	}
	want := []reconcile.ProgressPhase{reconcile.ProgressStarted, reconcile.ProgressFailed}
	if !reflect.DeepEqual(phases, want) {
		t.Fatalf("phases=%v want=%v", phases, want)
	}
}

func TestProgressActionsFollowApplyUpdateAndRetryExecutionBoundaries(t *testing.T) {
	desired := testDesired(t)
	fullKinds := []reconcile.ActionKind{
		reconcile.ActionPrepareWorkspace,
		reconcile.ActionSyncContent,
		reconcile.ActionSyncDatabase,
		reconcile.ActionInstallToolchain,
		reconcile.ActionConfigureApplication,
		reconcile.ActionVerifyState,
	}

	t.Run("apply full plan order", func(t *testing.T) {
		runner := Engine{Effects: &fakeEffects{}, Store: &memoryStore{}}
		got, err := captureProgressActions(context.Background(), func(ctx context.Context) error {
			_, err := runner.Apply(ctx, desired, reconcile.UpdateNone)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		want := progressActionPairs(fullKinds)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("progress=%v want=%v", got, want)
		}
	})

	t.Run("update selected database subset", func(t *testing.T) {
		effects := &fakeEffects{observed: reconcile.ObservedState{
			WorkspaceReady: true, ContentReady: true, DatabaseReady: true,
			ToolchainReady: true, ApplicationReady: true, NonemptyWorkspace: true,
		}}
		runner := Engine{Effects: effects, Store: &memoryStore{}}
		got, err := captureProgressActions(context.Background(), func(ctx context.Context) error {
			_, err := runner.Apply(ctx, desired, reconcile.UpdateDatabase)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		want := progressActionPairs([]reconcile.ActionKind{reconcile.ActionSyncDatabase, reconcile.ActionVerifyState})
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("progress=%v want=%v", got, want)
		}
	})

	t.Run("retry incomplete actions only", func(t *testing.T) {
		effects := &fakeEffects{failID: "30-sync-database"}
		store := &memoryStore{}
		runner := Engine{Effects: effects, Store: store}
		if _, err := runner.Apply(context.Background(), desired, reconcile.UpdateNone); err == nil {
			t.Fatal("Apply() error=nil want injected failure")
		}
		effects.failID = ""
		effects.applied = nil
		got, err := captureProgressActions(context.Background(), func(ctx context.Context) error {
			return runner.Retry(ctx, desired)
		})
		if err != nil {
			t.Fatal(err)
		}
		wantKinds := []reconcile.ActionKind{
			reconcile.ActionSyncDatabase,
			reconcile.ActionInstallToolchain,
			reconcile.ActionConfigureApplication,
			reconcile.ActionVerifyState,
		}
		if want := progressActionPairs(wantKinds); !reflect.DeepEqual(got, want) {
			t.Fatalf("progress=%v want=%v", got, want)
		}
		wantIDs := []string{"30-sync-database", "40-install-toolchain", "50-configure-application", "90-verify-state"}
		if !reflect.DeepEqual(effects.applied, wantIDs) {
			t.Fatalf("effects=%v want=%v", effects.applied, wantIDs)
		}
	})
}

func captureProgressActions(ctx context.Context, run func(context.Context) error) ([]string, error) {
	events := []string{}
	ctx = reconcile.WithProgressObserver(ctx, func(event reconcile.ProgressEvent) {
		if event.Target == reconcile.ProgressAction {
			events = append(events, string(event.Phase)+":"+string(event.Action))
		}
	})
	return events, run(ctx)
}

func progressActionPairs(kinds []reconcile.ActionKind) []string {
	events := make([]string, 0, len(kinds)*2)
	for _, kind := range kinds {
		events = append(events, "started:"+string(kind), "completed:"+string(kind))
	}
	return events
}

func TestPreparedPlanIsDurableBeforeAdaptersAndExecutesWithoutReplanning(t *testing.T) {
	desired := testDesired(t)
	plan, err := reconcile.Plan(desired, reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{}
	preparer := Engine{Store: store}
	if err := preparer.Prepare(desired, plan); err != nil {
		t.Fatal(err)
	}
	if store.receipt == nil || !reflect.DeepEqual(store.plan, plan) {
		t.Fatalf("prepared plan=%#v receipt=%#v", store.plan, store.receipt)
	}
	effects := &fakeEffects{observed: reconcile.ObservedState{
		WorkspaceReady: true, ContentReady: true, DatabaseReady: true,
		ToolchainReady: true, ApplicationReady: true,
	}}
	if err := (Engine{Store: store, Effects: effects}).ExecutePrepared(context.Background(), desired, plan); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(effects.applied, actionIDs(plan.Actions)) {
		t.Fatalf("prepared execution applied=%#v want=%#v", effects.applied, actionIDs(plan.Actions))
	}
}

func TestRetryRunsOnlyFailedAndFollowingIdempotentEffects(t *testing.T) {
	desired := testDesired(t)
	effects := &fakeEffects{failID: "30-sync-database"}
	store := &memoryStore{}
	runner := Engine{Effects: effects, Store: store}

	plan, err := runner.Apply(context.Background(), desired, reconcile.UpdateNone)
	if err == nil {
		t.Fatal("Apply() error = nil, want injected failure")
	}
	if got := effects.applied[len(effects.applied)-1]; got != effects.failID {
		t.Fatalf("last attempted action = %q, want %q", got, effects.failID)
	}

	effects.applied = nil
	effects.failID = ""
	if err := runner.Retry(context.Background(), desired); err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	want := actionIDs(reconcile.RetryActions(plan, store.receipt))
	if len(want) != 0 {
		t.Fatalf("receipt remains incomplete after retry: %v", want)
	}
	if len(effects.applied) == 0 || effects.applied[0] != "30-sync-database" {
		t.Fatalf("retry actions = %v, want failed action first", effects.applied)
	}
}

func TestRetryRejectsDesiredStateDifferentFromReceipt(t *testing.T) {
	desired := testDesired(t)
	effects := &fakeEffects{failID: "20-sync-content"}
	store := &memoryStore{}
	runner := Engine{Effects: effects, Store: store}
	if _, err := runner.Apply(context.Background(), desired, reconcile.UpdateNone); err == nil {
		t.Fatal("Apply unexpectedly succeeded")
	}
	other, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
		KitHome: "/tmp/teamkit", HermesHome: "/tmp/hermes",
		Project: domain.ProjectWMS, Role: domain.RoleArchitect,
		Toolchain: domain.ToolchainAIRules1C,
	})
	if err != nil {
		t.Fatal(err)
	}
	effects.applied = nil
	effects.failID = ""
	err = runner.Retry(context.Background(), other)
	if err == nil || err.Error() != "RECEIPT_DESIRED_MISMATCH" {
		t.Fatalf("Retry error=%v", err)
	}
	if len(effects.applied) != 0 {
		t.Fatalf("mismatched retry applied effects=%v", effects.applied)
	}
}

func TestStatusIsReadOnly(t *testing.T) {
	effects := &fakeEffects{observed: reconcile.ObservedState{
		WorkspaceReady: true, ContentReady: true, DatabaseReady: true,
		ToolchainReady: true, ApplicationReady: true,
	}}
	store := &memoryStore{}
	runner := Engine{Effects: effects, Store: store}

	status, plan, err := runner.Status(context.Background(), testDesired(t))
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status != reconcile.StatusReady || len(plan.Actions) != 0 {
		t.Fatalf("status=%q plan=%+v", status, plan)
	}
	if len(effects.applied) != 0 || store.saveRuns != 0 {
		t.Fatalf("status mutated state: effects=%v saves=%d", effects.applied, store.saveRuns)
	}
}

func testDesired(t *testing.T) domain.DesiredState {
	t.Helper()
	state, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
		KitHome: "/tmp/teamkit", HermesHome: "/tmp/hermes",
		Project: domain.ProjectWMS, Role: domain.RoleDeveloper,
		Toolchain: domain.ToolchainAIRules1C,
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func actionIDs(actions []reconcile.Action) []string {
	ids := make([]string, len(actions))
	for i, action := range actions {
		ids[i] = action.ID
	}
	return ids
}
