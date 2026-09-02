//go:build !windows

package platform

type unsupportedAuthenticodeVerifier struct{}

// NewWindowsAuthenticodeVerifier returns a stable unsupported adapter off Windows.
func NewWindowsAuthenticodeVerifier(PowerShellRunner) AuthenticodeVerifier {
	return unsupportedAuthenticodeVerifier{}
}

func (unsupportedAuthenticodeVerifier) Verify(string) (SignerMetadata, error) {
	return SignerMetadata{}, ErrAuthenticodeUnsupported
}
