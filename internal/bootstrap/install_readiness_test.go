package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

func TestEffects_ObserveRequiresManagedHermesInstallForApplicationReadiness(t *testing.T) {
	home := testutil.TempDir(t)
	desired := desiredInstalled(t, home, false)
	managedReady := false
	effects := &Effects{ManagedInstallReady: func(string) (bool, error) { return managedReady, nil }, OfficeCLI: officeCLIFixture(desired)}
	if err := effects.Apply(context.Background(), desired, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	writeReadyHermesProfile(t, desired)
	archive, digest := certificateFixture(t)
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := hermes.ExtractCertificates(bytes.NewReader(data), int64(len(data)), desired.HermesHome())
	if err != nil {
		t.Fatal(err)
	}
	effects.CertificateSHA256 = digest
	writeCertificateEnvironmentFixture(t, filepath.Join(desired.HermesHome(), ".env"), bundle)
	writeCertificateEnvironmentFixture(t, filepath.Join(profileDirectory(desired), ".env"), bundle)
	if err := os.MkdirAll(filepath.Dir(installedMarker(desired)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedMarker(desired), []byte("installed-by-teamkit\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	observed, err := effects.Observe(context.Background(), desired, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ApplicationReady {
		t.Fatal("marker-only Hermes installation was reported application-ready")
	}

	writeBootstrapManagedInstallFixture(t, desired.HermesHome(), hermes.HermesSourceCommit+"\n", true)
	managedReady = true
	observed, err = effects.Observe(context.Background(), desired, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if !observed.ApplicationReady {
		t.Fatal("exact managed Hermes installation was not reported application-ready")
	}

	providerKey := []byte(hermes.CustomLLMProvider().APIKeyEnvironment + "=provider-test-value\n")
	profileEnv := filepath.Join(profileDirectory(desired), ".env")
	profileData, err := os.ReadFile(profileEnv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profileEnv, bytes.ReplaceAll(profileData, providerKey, []byte(hermes.CustomLLMProvider().APIKeyEnvironment+"=   \n")), 0o600); err != nil {
		t.Fatal(err)
	}
	assertHermesConfigureReplanned(t, effects, desired)

	writeCertificateEnvironmentFixture(t, profileEnv, bundle)
	globalEnv := filepath.Join(desired.HermesHome(), ".env")
	globalData, err := os.ReadFile(globalEnv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalEnv, bytes.ReplaceAll(globalData, providerKey, nil), 0o600); err != nil {
		t.Fatal(err)
	}
	assertHermesConfigureReplanned(t, effects, desired)
}

func assertHermesConfigureReplanned(t *testing.T, effects *Effects, desired domain.DesiredState) {
	t.Helper()
	observed, err := effects.Observe(context.Background(), desired, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ApplicationReady {
		t.Fatal("Hermes environment without a nonblank provider key was reported application-ready")
	}
	plan, err := reconcile.Plan(desired, observed)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		if action.Kind == reconcile.ActionConfigureApplication {
			return
		}
	}
	t.Fatalf("plan did not restore Hermes configuration: %#v", plan.Actions)
}

func TestEffects_StaleInstallMarkerDoesNotSkipInstaller(t *testing.T) {
	home := testutil.TempDir(t)
	desired := desiredInstalled(t, home, false)
	if err := (&Effects{}).Apply(context.Background(), desired, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(installedMarker(desired)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedMarker(desired), []byte("installed-by-teamkit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	managedReady := false
	archive, digest := certificateFixture(t)
	effects := &Effects{
		ManagedInstallReady: func(string) (bool, error) { return managedReady, nil },
		InstallerPath:       filepath.Join(home, "hermes-installer"),
		Installer: InstallerPortFunc(func(context.Context, string) error {
			calls++
			managedReady = true
			writeBootstrapManagedInstallFixture(t, desired.HermesHome(), hermes.HermesSourceCommit+"\n", true)
			return nil
		}),
		Profile: profileFixture(home), OfficeCLI: officeCLIFixture(desired), CertificateArchive: archive, CertificateSHA256: digest,
		Secrets:        &capturingSecrets{path: filepath.Join(desired.HermesHome(), ".env")},
		ProfileSecrets: &capturingSecrets{path: filepath.Join(profileDirectory(desired), ".env")},
	}
	setTestRuntime(t, effects, 34)

	if err := effects.Apply(context.Background(), desired, reconcile.Action{Kind: reconcile.ActionConfigureApplication}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("installer calls = %d, want 1", calls)
	}
}

func TestEffects_InstallerSuccessWithoutPinnedLayoutLeavesNoReadinessMarker(t *testing.T) {
	home := testutil.TempDir(t)
	desired := desiredInstalled(t, home, false)
	if err := (&Effects{}).Apply(context.Background(), desired, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	effects := &Effects{
		ManagedInstallReady: func(string) (bool, error) { return false, nil },
		InstallerPath:       filepath.Join(home, "hermes-installer"),
		Installer:           InstallerPortFunc(func(context.Context, string) error { return nil }),
		Profile:             profileFixture(home),
	}

	err := effects.Apply(context.Background(), desired, reconcile.Action{Kind: reconcile.ActionConfigureApplication})
	if !errors.Is(err, hermes.ErrInstallLayout) {
		t.Fatalf("ConfigureApplication error = %v, want ErrInstallLayout", err)
	}
	if _, statErr := os.Lstat(installedMarker(desired)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unverified installer wrote readiness marker: %v", statErr)
	}
}

func TestEffects_AppInstalledHermesRequiresVerifiedAbsoluteExecutable(t *testing.T) {
	home := testutil.TempDir(t)
	desired := desiredInstalled(t, home, true)
	if err := (&Effects{}).Apply(context.Background(), desired, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	effects := &Effects{Profile: profileFixture(home)}

	err := effects.Apply(context.Background(), desired, reconcile.Action{Kind: reconcile.ActionConfigureApplication})
	if !errors.Is(err, hermes.ErrExecutableUnverified) {
		t.Fatalf("ConfigureApplication error = %v, want ErrExecutableUnverified", err)
	}
}

func TestEffects_ObserveIncludesOfficeCLIReadinessAndExactProfile(t *testing.T) {
	home := testutil.TempDir(t)
	desired := desiredInstalled(t, home, true)
	if err := (&Effects{}).Apply(context.Background(), desired, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	officePath := filepath.Join(desired.HermesHome(), ".teamkit", "officecli", "1.0.144", "officecli")
	writeReadyHermesProfileWithOfficeCLI(t, desired, officePath)
	archive, digest := certificateFixture(t)
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := hermes.ExtractCertificates(bytes.NewReader(data), int64(len(data)), desired.HermesHome())
	if err != nil {
		t.Fatal(err)
	}
	writeCertificateEnvironmentFixture(t, filepath.Join(desired.HermesHome(), ".env"), bundle)
	writeCertificateEnvironmentFixture(t, filepath.Join(profileDirectory(desired), ".env"), bundle)
	officeReady := false
	officeErr := error(nil)
	effects := &Effects{
		HermesExecutable:  testHermesExecutable(t),
		CertificateSHA256: digest,
		OfficeCLI: &fakeOfficeCLI{path: officePath, ready: func(context.Context) (bool, error) {
			return officeReady, officeErr
		}},
	}
	setTestRuntime(t, effects, 34)

	observed, err := effects.Observe(context.Background(), desired, reconcile.UpdateNone)
	if err != nil || observed.ApplicationReady {
		t.Fatalf("missing OfficeCLI readiness = %#v, %v; want not ready", observed, err)
	}
	officeReady = true
	observed, err = effects.Observe(context.Background(), desired, reconcile.UpdateNone)
	if err != nil || !observed.ApplicationReady {
		t.Fatalf("valid OfficeCLI readiness = %#v, %v; want ready", observed, err)
	}
	officeErr = errors.New("OFFICECLI_PATH_SECURITY_FAILURE")
	if _, err = effects.Observe(context.Background(), desired, reconcile.UpdateNone); !errors.Is(err, officeErr) {
		t.Fatalf("OfficeCLI security error = %v, want exact %v", err, officeErr)
	}
}

func TestProfileConfigReady_RequiresExactOfficeCLIProfileBytes(t *testing.T) {
	desired := desiredInstalled(t, testutil.TempDir(t), true)
	legacy, err := hermes.ProfileFromDesired(desired)
	if err != nil {
		t.Fatal(err)
	}
	legacyData, err := legacy.RenderForSchema(hermes.CustomLLMProvider(), 34)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(profileDirectory(desired), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath(desired), legacyData, 0o600); err != nil {
		t.Fatal(err)
	}
	officePath := officeCLIFixture(desired).Path()
	ready, err := profileConfigReady(desired, 34, officePath)
	if err != nil || ready {
		t.Fatalf("legacy three-MCP profile ready=%v err=%v; want false", ready, err)
	}
	withOfficeCLI, err := legacy.WithOfficeCLI(officePath)
	if err != nil {
		t.Fatal(err)
	}
	exactData, err := withOfficeCLI.RenderForSchema(hermes.CustomLLMProvider(), 34)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath(desired), exactData, 0o600); err != nil {
		t.Fatal(err)
	}
	ready, err = profileConfigReady(desired, 34, officePath)
	if err != nil || !ready {
		t.Fatalf("exact OfficeCLI profile ready=%v err=%v; want true", ready, err)
	}
}

func TestEffects_ObserveHermesRequiresOfficeCLIPort(t *testing.T) {
	desired := desiredInstalled(t, testutil.TempDir(t), true)
	if err := (&Effects{}).Apply(context.Background(), desired, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	effects := &Effects{HermesExecutable: testHermesExecutable(t)}
	setTestRuntime(t, effects, 34)
	_, err := effects.Observe(context.Background(), desired, reconcile.UpdateNone)
	if err == nil || err.Error() != "OFFICECLI_REQUIRED" {
		t.Fatalf("Observe error = %v, want OFFICECLI_REQUIRED", err)
	}
}

func writeReadyHermesProfile(t *testing.T, desired domain.DesiredState) {
	t.Helper()
	if err := os.MkdirAll(profileDirectory(desired), 0o700); err != nil {
		t.Fatal(err)
	}
	profile, err := hermes.ProfileFromDesired(desired)
	if err != nil {
		t.Fatal(err)
	}
	profile, err = profile.WithOfficeCLI(officeCLIFixture(desired).Path())
	if err != nil {
		t.Fatal(err)
	}
	data, err := profile.Render(hermes.CustomLLMProvider())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath(desired), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(profileOwnerPath(desired)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profileOwnerPath(desired), []byte(profileIdentity(desired)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeReadyHermesProfileWithOfficeCLI(t *testing.T, desired domain.DesiredState, officeCLIPath string) {
	t.Helper()
	if err := os.MkdirAll(profileDirectory(desired), 0o700); err != nil {
		t.Fatal(err)
	}
	profile, err := hermes.ProfileFromDesired(desired)
	if err != nil {
		t.Fatal(err)
	}
	profile, err = profile.WithOfficeCLI(officeCLIPath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := profile.Render(hermes.CustomLLMProvider())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath(desired), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(profileOwnerPath(desired)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profileOwnerPath(desired), []byte(profileIdentity(desired)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeBootstrapManagedInstallFixture(t *testing.T, home, head string, executable bool) {
	t.Helper()
	checkout := filepath.Join(home, ".teamkit", "hermes-agent-source")
	if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".git", "HEAD"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
	if !executable {
		return
	}
	executablePath := filepath.Join(checkout, "venv", "bin", "hermes")
	if err := os.MkdirAll(filepath.Dir(executablePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executablePath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}
