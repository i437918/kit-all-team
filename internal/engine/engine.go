// Package engine coordinates deterministic planning with idempotent side effects.
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

// Effects is the imperative shell used by the reconciliation engine.
type Effects interface {
	Observe(context.Context, domain.DesiredState, reconcile.UpdateChoice) (reconcile.ObservedState, error)
	Apply(context.Context, domain.DesiredState, reconcile.Action) error
}

// Store persists only the non-secret plan and redacted receipt.
type Store interface {
	SavePlan(reconcile.OperationPlan) error
	LoadPlan() (reconcile.OperationPlan, error)
	SaveReceipt(*reconcile.Receipt) error
	LoadReceipt(secrets ...string) (*reconcile.Receipt, error)
}

type operationStore interface {
	SaveOperation(reconcile.OperationPlan, *reconcile.Receipt) error
	LoadOperation(secrets ...string) (reconcile.OperationPlan, *reconcile.Receipt, error)
}

// Engine backs plan, apply, status, retry, and update with one state machine.
type Engine struct {
	Effects      Effects
	Store        Store
	Secrets      []string
	ContractHash string
}

// Plan observes current state and returns its deterministic reconciliation plan.
func (e Engine) Plan(ctx context.Context, desired domain.DesiredState, update reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	if e.Effects == nil {
		return reconcile.OperationPlan{}, fmt.Errorf("EFFECTS_REQUIRED")
	}
	observed, err := e.Effects.Observe(ctx, desired, update)
	if err != nil {
		return reconcile.OperationPlan{}, err
	}
	observed.Update = update
	return reconcile.Plan(desired, observed)
}

// Apply executes a newly observed plan and checkpoints every attempted effect.
func (e Engine) Apply(ctx context.Context, desired domain.DesiredState, update reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	if e.Store == nil {
		return reconcile.OperationPlan{}, fmt.Errorf("STATE_STORE_REQUIRED")
	}
	plan, err := e.Plan(ctx, desired, update)
	if err != nil {
		return reconcile.OperationPlan{}, err
	}
	if err := e.Prepare(desired, plan); err != nil {
		return plan, err
	}
	if err := e.ExecutePrepared(ctx, desired, plan); err != nil {
		return plan, err
	}
	return plan, nil
}

// Prepare durably records one exact plan and its pending receipt before any
// mutation adapter is opened. Stores that support operationStore publish both
// values as one atomic generation.
func (e Engine) Prepare(desired domain.DesiredState, plan reconcile.OperationPlan) error {
	if e.Store == nil {
		return fmt.Errorf("STATE_STORE_REQUIRED")
	}
	if err := e.validateContract(plan); err != nil {
		return err
	}
	receipt := reconcile.NewReceipt(desired, plan, e.Secrets...)
	if store, ok := e.Store.(operationStore); ok {
		return store.SaveOperation(plan, receipt)
	}
	if err := e.Store.SavePlan(plan); err != nil {
		return err
	}
	return e.Store.SaveReceipt(receipt)
}

// ExecutePrepared loads and validates the durable operation, then executes its
// incomplete effects without observing or replacing the plan.
func (e Engine) ExecutePrepared(ctx context.Context, desired domain.DesiredState, expected reconcile.OperationPlan) error {
	if e.Store == nil {
		return fmt.Errorf("STATE_STORE_REQUIRED")
	}
	if err := e.validateContract(expected); err != nil {
		return err
	}
	plan, receipt, err := e.loadOperation()
	if err != nil {
		return err
	}
	if err := e.validateContract(plan); err != nil {
		return err
	}
	if !receipt.MatchesDesired(desired) {
		return fmt.Errorf("RECEIPT_DESIRED_MISMATCH")
	}
	actions, err := reconcile.RetryActionsChecked(expected, receipt)
	if err != nil {
		return fmt.Errorf("RECEIPT_PLAN_MISMATCH")
	}
	return e.run(ctx, desired, plan, actions, receipt)
}

// Status is a read-only view of current convergence.
func (e Engine) Status(ctx context.Context, desired domain.DesiredState) (reconcile.PlanStatus, reconcile.OperationPlan, error) {
	plan, err := e.Plan(ctx, desired, reconcile.UpdateNone)
	if err != nil {
		return "", reconcile.OperationPlan{}, err
	}
	return reconcile.Status(plan), plan, nil
}

// Retry replays only incomplete idempotent effects from the persisted execution.
func (e Engine) Retry(ctx context.Context, desired domain.DesiredState) error {
	if e.Store == nil {
		return fmt.Errorf("STATE_STORE_REQUIRED")
	}
	plan, receipt, err := e.loadOperation()
	if err != nil {
		return err
	}
	if err := e.validateContract(plan); err != nil {
		return err
	}
	if !receipt.MatchesDesired(desired) {
		return fmt.Errorf("RECEIPT_DESIRED_MISMATCH")
	}
	actions, err := reconcile.RetryActionsChecked(plan, receipt)
	if err != nil {
		return err
	}
	return e.run(ctx, desired, plan, actions, receipt)
}

func (e Engine) validateContract(plan reconcile.OperationPlan) error {
	if e.ContractHash != "" && plan.ContractHash != e.ContractHash {
		return fmt.Errorf("OPERATION_CONTRACT_MISMATCH")
	}
	return nil
}

func (e Engine) run(ctx context.Context, desired domain.DesiredState, plan reconcile.OperationPlan, actions []reconcile.Action, receipt *reconcile.Receipt) error {
	for _, action := range actions {
		e.report(ctx, desired, action, reconcile.ProgressStarted)
		err := e.Effects.Apply(ctx, desired, action)
		status := reconcile.EffectSucceeded
		diagnostic := ""
		if err != nil {
			status = reconcile.EffectFailed
			diagnostic = err.Error()
		}
		if recordErr := receipt.Record(action.ID, status, diagnostic); recordErr != nil {
			e.report(ctx, desired, action, reconcile.ProgressFailed)
			return recordErr
		}
		if saveErr := e.saveOperation(plan, receipt); saveErr != nil {
			e.report(ctx, desired, action, reconcile.ProgressFailed)
			return saveErr
		}
		if err != nil {
			e.report(ctx, desired, action, reconcile.ProgressFailed)
			return fmt.Errorf("ACTION_FAILED %s: %w", action.ID, err)
		}
		e.report(ctx, desired, action, reconcile.ProgressCompleted)
	}
	return nil
}

func (e Engine) report(ctx context.Context, desired domain.DesiredState, action reconcile.Action, phase reconcile.ProgressPhase) {
	reconcile.ReportProgress(ctx, reconcile.ProgressEvent{
		Target: reconcile.ProgressAction, Phase: phase, Action: action.Kind, Application: string(desired.Application()),
	})
}

func (e Engine) saveOperation(plan reconcile.OperationPlan, receipt *reconcile.Receipt) error {
	if store, ok := e.Store.(operationStore); ok {
		return store.SaveOperation(plan, receipt)
	}
	return e.Store.SaveReceipt(receipt)
}

func (e Engine) loadOperation() (reconcile.OperationPlan, *reconcile.Receipt, error) {
	if store, ok := e.Store.(operationStore); ok {
		plan, receipt, err := store.LoadOperation(e.Secrets...)
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			return plan, receipt, err
		}
	}
	plan, err := e.Store.LoadPlan()
	if err != nil {
		return reconcile.OperationPlan{}, nil, err
	}
	receipt, err := e.Store.LoadReceipt(e.Secrets...)
	return plan, receipt, err
}
