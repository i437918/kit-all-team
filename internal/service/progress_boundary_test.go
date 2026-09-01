package service

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/credentials"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/engine"
	"github.com/mi1man-cmd/kit-all-team/internal/gitx"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/state"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

type progressRecordingEffects struct {
	observed reconcile.ObservedState
	actions  *[]reconcile.Action
}

func (effects progressRecordingEffects) Observe(context.Context, domain.DesiredState, reconcile.UpdateChoice) (reconcile.ObservedState, error) {
	return effects.observed, nil
}

func (effects progressRecordingEffects) Apply(_ context.Context, _ domain.DesiredState, action reconcile.Action) error {
	*effects.actions = append(*effects.actions, action)
	return nil
}

func TestServiceProgress_UpdateUsesSelectedActionSubset(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCursor, true, "")
	prepareConvergedWorkspace(t, desired)
	writeDesired(t, desired)
	store := &recordingSecretStore{loaded: map[string]string{
		credentials.GitLabUsername: "user", credentials.GitLabToken: "token", GitCAFile: "ca.pem",
	}}
	var applied []reconcile.Action
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		SecretStore:     func(string) (credentials.SecretStore, error) { return store, nil },
		GitRunner:       readySourceObservationRunner(t, desired, ""),
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			return &recordingAskPass{credentials: input}, nil
		},
		Effects: func(EffectInputs) engine.Effects {
			return progressRecordingEffects{observed: reconcile.ObservedState{
				WorkspaceReady: true, ContentReady: true, DatabaseReady: true,
				ToolchainReady: true, ApplicationReady: true, NonemptyWorkspace: true,
			}, actions: &applied}
		},
		TempRoot: testutil.TempDir(t),
	})
	ctx, progress := serviceProgressContext(context.Background())
	plan, err := svc.Update(ctx, root, reconcile.UpdateContent)
	if err != nil {
		t.Fatal(err)
	}
	wantActions := []reconcile.Action{
		{ID: "20-sync-content", Kind: reconcile.ActionSyncContent, Idempotent: true},
		{ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true},
	}
	if !reflect.DeepEqual(plan.Actions, wantActions) || !reflect.DeepEqual(applied, wantActions) {
		t.Fatalf("plan=%v applied=%v want=%v", plan.Actions, applied, wantActions)
	}
	if want := serviceProgressPairs(wantActions); !reflect.DeepEqual(*progress, want) {
		t.Fatalf("progress=%v want=%v", *progress, want)
	}
}

func TestServiceProgress_RetryUsesIncompleteActionsOnly(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	public, bound := interruptedHermesStates(t, root, hermesHome, "0.20.1")
	plan, err := reconcile.Plan(bound, reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	plan.ContractHash, err = defaultOperationContract(bound)
	if err != nil {
		t.Fatal(err)
	}
	writeInterruptedCurrentOperation(t, public, bound, plan)
	receipt := reconcile.NewReceipt(bound, plan)
	if err := receipt.Record(plan.Actions[0].ID, reconcile.EffectSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	if err := receipt.Record(plan.Actions[1].ID, reconcile.EffectSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	if err := receipt.Record(plan.Actions[2].ID, reconcile.EffectFailed, "interrupted"); err != nil {
		t.Fatal(err)
	}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, receipt); err != nil {
		t.Fatal(err)
	}

	secretStore := &recordingSecretStore{loaded: map[string]string{
		credentials.GitLabUsername: "user", credentials.GitLabToken: "token",
		credentials.PublicProviderAPIKey: "provider", credentials.JiraToken: "jira", credentials.ConfluenceToken: "confluence",
	}}
	var applied []reconcile.Action
	runtimeCalls := 0
	svc := New(Options{
		ResolveHermesRuntime: stableHermesRuntime(hermesHome, "0.20.1", &runtimeCalls),
		ApplicationHome:      func(domain.DesiredState) (string, error) { return hermesHome, nil },
		SecretStore:          func(string) (credentials.SecretStore, error) { return secretStore, nil },
		ManagedCertificateBundle: func(string, string) (string, bool, error) {
			return filepath.Join(hermesHome, "certs", "ca-bundle.pem"), true, nil
		},
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			return &recordingAskPass{credentials: input}, nil
		},
		Effects: func(EffectInputs) engine.Effects {
			return progressRecordingEffects{actions: &applied}
		},
	})
	ctx, progress := serviceProgressContext(context.Background())
	if err := svc.Retry(ctx, root); err != nil {
		t.Fatal(err)
	}
	wantActions := plan.Actions[2:]
	if !reflect.DeepEqual(applied, wantActions) {
		t.Fatalf("applied=%v want=%v", applied, wantActions)
	}
	if want := serviceProgressPairs(wantActions); !reflect.DeepEqual(*progress, want) {
		t.Fatalf("progress=%v want=%v", *progress, want)
	}
}

func serviceProgressContext(ctx context.Context) (context.Context, *[]string) {
	events := []string{}
	ctx = reconcile.WithProgressObserver(ctx, func(event reconcile.ProgressEvent) {
		if event.Target == reconcile.ProgressAction {
			events = append(events, string(event.Phase)+":"+string(event.Action))
		}
	})
	return ctx, &events
}

func serviceProgressPairs(actions []reconcile.Action) []string {
	events := make([]string, 0, len(actions)*2)
	for _, action := range actions {
		events = append(events, "started:"+string(action.Kind), "completed:"+string(action.Kind))
	}
	return events
}
