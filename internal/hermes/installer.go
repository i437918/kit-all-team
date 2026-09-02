package hermes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrInstallerChecksum reports an executable whose digest differs from its pin.
var ErrInstallerChecksum = errors.New("Hermes installer checksum mismatch")

// ErrInstallerSigner reports missing or untrusted executable signer metadata.
var ErrInstallerSigner = errors.New("Hermes installer signer is not trusted")

// SignerMetadata is the minimum result required from a platform signature check.
type SignerMetadata struct {
	Subject string
	Trusted bool
}

// SignerVerifier verifies the Authenticode metadata of an installer.
type SignerVerifier interface {
	Verify(path string) (SignerMetadata, error)
}

// SignerVerifierFunc adapts a function to SignerVerifier.
type SignerVerifierFunc func(path string) (SignerMetadata, error)

// Verify calls f(path).
func (f SignerVerifierFunc) Verify(path string) (SignerMetadata, error) { return f(path) }

// Installer is the narrow process port used only after verification succeeds.
type Installer interface {
	Run(ctx context.Context, path string) error
}

// InstallerFunc adapts a function to Installer.
type InstallerFunc func(ctx context.Context, path string) error

// Run calls f(ctx, path).
func (f InstallerFunc) Run(ctx context.Context, path string) error { return f(ctx, path) }

// WindowsInstaller checks the pinned executable before delegating installation to
// an injected port. It never starts an installer executable itself.
type WindowsInstaller struct {
	ExpectedSHA256 string
	ExpectedSigner string
	Verifier       SignerVerifier
	Install        Installer
}

// Apply verifies an installer and delegates it to the injected installer port.
func (w WindowsInstaller) Apply(ctx context.Context, path string) error {
	if w.Verifier == nil || w.Install == nil || len(w.ExpectedSHA256) != sha256.Size*2 || strings.TrimSpace(w.ExpectedSigner) == "" {
		return ErrInstallerSigner
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(contents)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), w.ExpectedSHA256) {
		return ErrInstallerChecksum
	}
	signer, err := w.Verifier.Verify(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInstallerSigner, err)
	}
	if !signer.Trusted || signer.Subject == "" || !strings.Contains(strings.ToLower(signer.Subject), strings.ToLower(w.ExpectedSigner)) {
		return ErrInstallerSigner
	}
	return w.Install.Run(ctx, path)
}

// InstallStatus describes the next safe action for a platform.
type InstallStatus struct {
	Code     string
	Guidance string
}

// PlatformInstallStatus describes the platform-specific installation path.
func PlatformInstallStatus(platform string) InstallStatus {
	if platform == "windows" {
		return InstallStatus{Code: "HERMES_INSTALL_READY", Guidance: "Verify the pinned local Hermes-Setup.exe before installation."}
	}
	return InstallStatus{
		Code:     "HERMES_INSTALL_MANUAL",
		Guidance: "Install Hermes with the pinned NousResearch installer script for this platform; automatic parity is not claimed.",
	}
}
