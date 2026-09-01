// Package config converts validated desired state to and from strict public
// workspace dotenv values.
package config

// ErrorCode is a stable structural configuration failure code.
type ErrorCode string

const (
	KeyUnknown          ErrorCode = "CONFIG_KEY_UNKNOWN"
	KeyDuplicate        ErrorCode = "CONFIG_KEY_DUPLICATE"
	KeyMissing          ErrorCode = "CONFIG_KEY_MISSING"
	SecretKeyForbidden  ErrorCode = "CONFIG_SECRET_KEY_FORBIDDEN"
	BooleanInvalid      ErrorCode = "CONFIG_BOOLEAN_INVALID"
	ValueInvalid        ErrorCode = "CONFIG_VALUE_INVALID"
	LineInvalid         ErrorCode = "CONFIG_LINE_INVALID"
	HermesHomeForbidden ErrorCode = "CONFIG_HERMES_HOME_FORBIDDEN"
)

// Error reports a structural dotenv failure without including values.
type Error struct {
	Code ErrorCode
}

// Error implements error.
func (e *Error) Error() string { return string(e.Code) }

func configError(code ErrorCode) error { return &Error{Code: code} }
