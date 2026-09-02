package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrAuthenticodeUnsupported reports that Authenticode is unavailable off Windows.
var ErrAuthenticodeUnsupported = errors.New("AUTHENTICODE_UNSUPPORTED")

// ErrAuthenticodeOutput reports invalid or oversized PowerShell verification data.
var ErrAuthenticodeOutput = errors.New("AUTHENTICODE_OUTPUT_INVALID")

const maxAuthenticodeJSONBytes = 16 * 1024

// SignerMetadata is the minimal safe result of a signature check.
type SignerMetadata struct {
	Subject string
	Trusted bool
}

// AuthenticodeVerifier verifies a Windows executable signature without launching it.
type AuthenticodeVerifier interface {
	Verify(path string) (SignerMetadata, error)
}

// PowerShellRunner runs a fixed PowerShell argument vector.
type PowerShellRunner interface {
	Run(ctx context.Context, name string, args []string) ([]byte, error)
}

// PowerShellRunnerFunc adapts a function to PowerShellRunner.
type PowerShellRunnerFunc func(ctx context.Context, name string, args []string) ([]byte, error)

// Run calls f(ctx, name, args).
func (f PowerShellRunnerFunc) Run(ctx context.Context, name string, args []string) ([]byte, error) {
	return f(ctx, name, args)
}

func parseAuthenticodeJSON(data []byte) (SignerMetadata, error) {
	if len(data) == 0 || len(data) > maxAuthenticodeJSONBytes {
		return SignerMetadata{}, ErrAuthenticodeOutput
	}
	var result struct {
		Subject string `json:"subject"`
		Trusted bool   `json:"trusted"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Subject == "" {
		if err != nil {
			return SignerMetadata{}, fmt.Errorf("%w: %v", ErrAuthenticodeOutput, err)
		}
		return SignerMetadata{}, ErrAuthenticodeOutput
	}
	return SignerMetadata{Subject: result.Subject, Trusted: result.Trusted}, nil
}
