//go:build !windows

package platform

import (
	"errors"
	"testing"
)

func TestWindowsAuthenticodeVerifier_IsUnsupportedOffWindows(t *testing.T) {
	_, err := NewWindowsAuthenticodeVerifier(nil).Verify("Hermes-Setup.exe")
	if !errors.Is(err, ErrAuthenticodeUnsupported) {
		t.Fatalf("Verify() error = %v, want ErrAuthenticodeUnsupported", err)
	}
}
