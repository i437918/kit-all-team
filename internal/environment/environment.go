// Package environment verifies existing Team Kit roots without mutating them.
package environment

import (
	"context"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

// InspectionState classifies the result of inspecting an existing root.
type InspectionState uint8

const (
	Ready InspectionState = iota
	RetryRequired
	Foreign
	InspectionFailed
)

// String returns the stable public operational code for a state.
func (s InspectionState) String() string {
	switch s {
	case Ready:
		return "READY"
	case RetryRequired:
		return "RETRY_REQUIRED"
	case Foreign:
		return "FOREIGN_WORKSPACE"
	case InspectionFailed:
		return "WORKSPACE_INSPECTION_FAILED"
	default:
		return "WORKSPACE_INSPECTION_FAILED"
	}
}

// AddState classifies a root requested for a new environment.
type AddState uint8

const (
	AddTargetReady AddState = iota
	AddWorkspaceExists
)

// VerifiedEnvironment contains public state read from a verified root.
type VerifiedEnvironment struct {
	Home    string
	Desired domain.DesiredState
	Pending bool
}

// Inspector verifies existing roots and classifies add targets without writes.
type Inspector interface {
	Inspect(context.Context, string) (VerifiedEnvironment, InspectionState, error)
	ClassifyAdd(context.Context, string) (AddState, error)
}

// Error carries the typed inspection state used by orchestration.
type Error struct {
	State  InspectionState
	Detail string
	Cause  error
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.State.String()
	}
	return e.State.String() + ": " + e.Detail
}

// Unwrap exposes the underlying filesystem or parse error when present.
func (e *Error) Unwrap() error { return e.Cause }

func inspectionError(state InspectionState, detail string, cause error) error {
	return &Error{State: state, Detail: detail, Cause: cause}
}
