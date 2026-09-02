//go:build windows

package platform

import (
	"context"
	"strings"
	"testing"
)

func TestWindowsAuthenticodeVerifier_UsesFixedScriptAndPathArgument(t *testing.T) {
	var gotName string
	var gotArgs []string
	verifier := NewWindowsAuthenticodeVerifier(PowerShellRunnerFunc(func(ctx context.Context, name string, args []string) ([]byte, error) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return []byte(`{"subject":"CN=NousResearch","trusted":true}`), nil
	}))
	metadata, err := verifier.Verify(`C:\\installers\\Hermes-Setup.exe`)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !metadata.Trusted || metadata.Subject != "CN=NousResearch" {
		t.Fatalf("metadata = %#v", metadata)
	}
	if gotName != "powershell.exe" || len(gotArgs) < 2 || gotArgs[len(gotArgs)-1] != `C:\\installers\\Hermes-Setup.exe` {
		t.Fatalf("PowerShell argv = %q %#v", gotName, gotArgs)
	}
	script := strings.Join(gotArgs[:len(gotArgs)-1], " ")
	if !strings.Contains(script, "$args[0]") || strings.Contains(script, "$Path") {
		t.Fatalf("fixed script does not bind the final path argument safely: %q", script)
	}
	for _, argument := range gotArgs[:len(gotArgs)-1] {
		if strings.Contains(argument, `C:\\installers\\Hermes-Setup.exe`) {
			t.Fatalf("installer path was interpolated into fixed argument %q", argument)
		}
	}
}

func TestWindowsAuthenticodeVerifier_RejectsOversizedOutput(t *testing.T) {
	verifier := NewWindowsAuthenticodeVerifier(PowerShellRunnerFunc(func(context.Context, string, []string) ([]byte, error) {
		return make([]byte, maxAuthenticodeJSONBytes+1), nil
	}))
	if _, err := verifier.Verify(`C:\\installers\\Hermes-Setup.exe`); err == nil {
		t.Fatal("Verify() accepted oversized JSON")
	}
}
