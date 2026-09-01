//go:build windows

package platform

import "context"

const fixedAuthenticodeScript = "$targetPath = $args[0]; $signature = Get-AuthenticodeSignature -LiteralPath $targetPath -ErrorAction Stop; [pscustomobject]@{subject=[string]$signature.SignerCertificate.Subject; trusted=($signature.Status -eq 'Valid')} | ConvertTo-Json -Compress"

type windowsAuthenticodeVerifier struct {
	runner PowerShellRunner
}

// NewWindowsAuthenticodeVerifier creates a fixed-script Authenticode adapter.
func NewWindowsAuthenticodeVerifier(runner PowerShellRunner) AuthenticodeVerifier {
	return windowsAuthenticodeVerifier{runner: runner}
}

func (v windowsAuthenticodeVerifier) Verify(path string) (SignerMetadata, error) {
	if v.runner == nil {
		return SignerMetadata{}, ErrAuthenticodeOutput
	}
	output, err := v.runner.Run(context.Background(), "powershell.exe", []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", fixedAuthenticodeScript, path,
	})
	if err != nil {
		return SignerMetadata{}, err
	}
	return parseAuthenticodeJSON(output)
}
