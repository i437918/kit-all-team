package reconcile

import (
	"fmt"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

// UpdateChoice is the approved update scope for a nonempty workspace.
type UpdateChoice string

const (
	UpdateNone     UpdateChoice = "none"
	UpdateContent  UpdateChoice = "content"
	UpdateDatabase UpdateChoice = "database"
	UpdateBoth     UpdateChoice = "both"
)

// ObservedState describes effect completion without containing credentials.
type ObservedState struct {
	WorkspaceReady    bool
	ContentReady      bool
	DatabaseReady     bool
	ToolchainReady    bool
	ApplicationReady  bool
	NonemptyWorkspace bool
	Update            UpdateChoice
	LocalChanges      bool
}

// OperationPlan is an ordered deterministic set of effects.
type OperationPlan struct {
	ContractHash string   `json:"contract_hash"`
	Actions      []Action `json:"actions"`
}

// PlanStatus is a read-only summary of a deterministic plan.
type PlanStatus string

const (
	StatusReady      PlanStatus = "ready"
	StatusNeedsApply PlanStatus = "needs_apply"
)

// AllowedUpdateChoices returns the four approved choices for nonempty workspaces.
func AllowedUpdateChoices(nonempty bool) []UpdateChoice {
	if !nonempty {
		return nil
	}
	return []UpdateChoice{UpdateNone, UpdateContent, UpdateDatabase, UpdateBoth}
}

// Plan compares desired and observed state without performing effects.
func Plan(desired domain.DesiredState, observed ObservedState) (OperationPlan, error) {
	if desired.Project() == "" {
		return OperationPlan{}, domain.NewValidationError(domain.ProjectUnknown, "project", "")
	}
	if err := validateUpdateChoice(observed); err != nil {
		return OperationPlan{}, err
	}

	refreshContent := observed.NonemptyWorkspace && (observed.Update == UpdateContent || observed.Update == UpdateBoth)
	refreshDatabase := observed.NonemptyWorkspace && (observed.Update == UpdateDatabase || observed.Update == UpdateBoth)
	if observed.LocalChanges && ((!observed.ContentReady || refreshContent) || (!observed.DatabaseReady || refreshDatabase)) {
		return OperationPlan{}, domain.NewValidationError(domain.LocalChangesDetected, "workspace", "")
	}

	actions := make([]Action, 0, 6)
	if !observed.WorkspaceReady {
		actions = append(actions, action("10-prepare-workspace", ActionPrepareWorkspace))
	}
	if !observed.ContentReady || refreshContent {
		actions = append(actions, action("20-sync-content", ActionSyncContent))
	}
	if !observed.DatabaseReady || refreshDatabase {
		actions = append(actions, action("30-sync-database", ActionSyncDatabase))
	}
	if !observed.ToolchainReady {
		actions = append(actions, action("40-install-toolchain", ActionInstallToolchain))
	}
	if !observed.ApplicationReady {
		actions = append(actions, action("50-configure-application", ActionConfigureApplication))
	}
	if len(actions) > 0 {
		actions = append(actions, action("90-verify-state", ActionVerifyState))
	}
	return OperationPlan{Actions: actions}, nil
}

// Status calculates the current read-only status from a plan.
func Status(plan OperationPlan) PlanStatus {
	if len(plan.Actions) == 0 {
		return StatusReady
	}
	return StatusNeedsApply
}

func validateUpdateChoice(observed ObservedState) error {
	if !observed.NonemptyWorkspace && observed.Update != "" && observed.Update != UpdateNone {
		return fmt.Errorf("UPDATE_CHOICE_NOT_APPLICABLE: %q", observed.Update)
	}
	if observed.Update == "" {
		return nil
	}
	for _, allowed := range []UpdateChoice{UpdateNone, UpdateContent, UpdateDatabase, UpdateBoth} {
		if observed.Update == allowed {
			return nil
		}
	}
	return fmt.Errorf("UPDATE_CHOICE_UNKNOWN: %q", observed.Update)
}
