package cli

import (
	"context"
	"errors"

	"github.com/mi1man-cmd/kit-all-team/internal/apps"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/gitx"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

type operationalCode string

const (
	codeInputRequired             operationalCode = "INPUT_REQUIRED"
	codeWorkspaceExistsUseUpdate  operationalCode = "WORKSPACE_EXISTS_USE_UPDATE"
	codeForeignWorkspace          operationalCode = "FOREIGN_WORKSPACE"
	codeRetryRequired             operationalCode = "RETRY_REQUIRED"
	codeUpdateChoiceNotApplicable operationalCode = "UPDATE_CHOICE_NOT_APPLICABLE"
	codeWorkspaceInspectionFailed operationalCode = "WORKSPACE_INSPECTION_FAILED"
	codeAIAppInspectionFailed     operationalCode = "AI_APP_INSPECTION_FAILED"
)

type operationalError struct {
	Code   operationalCode
	Detail string
	Cause  error
}

type publicQueryError struct {
	Code    operationalCode
	Message string
	Cause   error
}

func (e *publicQueryError) Error() string { return e.Message }

func (e *publicQueryError) Unwrap() error { return e.Cause }

func (e *operationalError) Error() string {
	if e.Detail == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Detail
}

func (e *operationalError) Unwrap() error { return e.Cause }

func newOperationalError(code operationalCode, detail string, cause error) error {
	return &operationalError{Code: code, Detail: detail, Cause: cause}
}

func errorIdentity(err error) (string, int) {
	if errors.Is(err, context.Canceled) {
		return "INTERRUPTED", ExitInterrupted
	}
	if errors.Is(err, workspace.ErrChanged) {
		return "WORKSPACE_CHANGED", ExitFailure
	}
	if errors.Is(err, hermes.ErrConfigSchemaUnsupported) {
		return "HERMES_CONFIG_SCHEMA_UNSUPPORTED", ExitFailure
	}
	var operational *operationalError
	if errors.As(err, &operational) {
		switch operational.Code {
		case codeInputRequired, codeUpdateChoiceNotApplicable:
			return string(operational.Code), ExitUsage
		case codeWorkspaceExistsUseUpdate, codeForeignWorkspace, codeRetryRequired, codeWorkspaceInspectionFailed, codeAIAppInspectionFailed:
			return string(operational.Code), ExitFailure
		default:
			return string(operational.Code), ExitFailure
		}
	}
	var public *publicQueryError
	if errors.As(err, &public) {
		return string(public.Code), ExitFailure
	}
	if code := apps.Code(err); code != "" {
		return code, ExitApplicationRequired
	}
	if code, ok := domain.CodeOf(err); ok {
		if code == domain.AIAppRequired {
			return string(code), ExitApplicationRequired
		}
		if code == domain.LocalChangesDetected {
			return string(code), ExitLocalChanges
		}
		return string(code), ExitUsage
	}
	if code := gitx.ErrorCode(err); code == "LOCAL_CHANGES_DETECTED" {
		return code, ExitLocalChanges
	}
	return "TEAMKIT_FAILED", ExitFailure
}
