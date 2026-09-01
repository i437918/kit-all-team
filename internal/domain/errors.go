// Package domain defines validated, effect-free Team Kit values.
package domain

import "fmt"

// ErrorCode is a stable machine-readable failure identifier.
type ErrorCode string

const (
	ProjectUnknown       ErrorCode = "PROJECT_UNKNOWN"
	RoleUnknown          ErrorCode = "ROLE_UNKNOWN"
	ToolchainUnknown     ErrorCode = "TOOLCHAIN_UNKNOWN"
	OSUnknown            ErrorCode = "OS_UNKNOWN"
	ApplicationUnknown   ErrorCode = "APPLICATION_UNKNOWN"
	KitHomeRequired      ErrorCode = "KIT_HOME_REQUIRED"
	HermesHomeRequired   ErrorCode = "HERMES_HOME_REQUIRED"
	LocalChangesDetected ErrorCode = "LOCAL_CHANGES_DETECTED"
	AIAppRequired        ErrorCode = "AI_APP_REQUIRED"
)

// ValidationError reports one invalid desired-state field without wrapping
// implementation-specific errors.
type ValidationError struct {
	Code  ErrorCode
	Field string
	Value string
}

// Error implements error.
func (e *ValidationError) Error() string {
	if e.Value == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Field)
	}
	return fmt.Sprintf("%s: %s=%q", e.Code, e.Field, e.Value)
}

// NewValidationError constructs a stable validation failure.
func NewValidationError(code ErrorCode, field, value string) error {
	return &ValidationError{Code: code, Field: field, Value: value}
}

// CodeOf returns a stable code from a validation error.
func CodeOf(err error) (ErrorCode, bool) {
	validationErr, ok := err.(*ValidationError)
	if !ok {
		return "", false
	}
	return validationErr.Code, true
}
