package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"gopkg.in/yaml.v3"
)

func TestEffects_ContinuationMaterializesSchema39HermesBootstrapEvidence(t *testing.T) {
	home := testutil.TempDir(t)
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
		KitHome: filepath.Join(home, "kit"), HermesHome: filepath.Join(home, "hermes"),
		Project: domain.ProjectAISUZ, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeBootstrapManagedInstallFixture(t, desired.HermesHome(), hermes.HermesSourceCommit+"\n", true)
	executable := standardHermesExecutable(desired)
	runtimeContract, err := hermes.VerifyRuntimeContract(context.Background(), executable, nil)
	if err != nil {
		t.Fatalf("verify continuation runtime: %v", err)
	}
	if !desired.AppInstalled() || runtimeContract.ConfigSchema != 39 {
		t.Fatalf("continuation state: app installed=%t schema=%d", desired.AppInstalled(), runtimeContract.ConfigSchema)
	}
	archive, digest := certificateFixture(t)
	installerCalls := 0
	officeEnsures := 0
	office := officeCLIFixture(desired)
	providerEnvironmentKey := "TEAMKIT_PUBLIC_PROVIDER_API_KEY"
	fixtureValue := t.Name()
	effects := &Effects{
		InstallerPath: filepath.Join(home, "hermes-installer"),
		Installer: InstallerPortFunc(func(context.Context, string) error {
			installerCalls++
			return errors.New("continuation invoked managed installer")
		}),
		HermesExecutable: executable,
		RuntimeContract:  runtimeContract,
		RuntimeProbe: func(ctx context.Context, path string) (hermes.RuntimeContract, error) {
			return hermes.VerifyRuntimeContract(ctx, path, nil)
		},
		Git: GitPortFunc{SyncPinnedFunc: func(_ context.Context, _, commit, destination string) error {
			return writePinnedSkillFixture(destination, commit, desired.Toolchain())
		}},
		Profile: profileFixture(home),
		OfficeCLI: &fakeOfficeCLI{
			path: office.Path(),
			ensure: func(context.Context) error {
				officeEnsures++
				return nil
			},
		},
		CertificateArchive: archive,
		CertificateSHA256:  digest,
		Secrets: &capturingSecrets{
			values: map[string]string{providerEnvironmentKey: fixtureValue},
			path:   filepath.Join(desired.HermesHome(), ".env"),
		},
		ProfileSecrets: &capturingSecrets{path: filepath.Join(profileDirectory(desired), ".env")},
		ProfileEnvironment: map[string]string{
			providerEnvironmentKey: fixtureValue,
		},
	}

	for _, action := range []reconcile.ActionKind{
		reconcile.ActionPrepareWorkspace,
		reconcile.ActionInstallToolchain,
		reconcile.ActionConfigureApplication,
	} {
		if err := effects.Apply(context.Background(), desired, reconcile.Action{Kind: action}); err != nil {
			t.Fatalf("Apply(%s): %v", action, err)
		}
	}

	if effects.RuntimeContract.ConfigSchema != 39 {
		t.Fatalf("discovered runtime schema = %d, want 39", effects.RuntimeContract.ConfigSchema)
	}
	if installerCalls != 0 {
		t.Fatalf("managed installer calls = %d, want 0", installerCalls)
	}
	configData, err := os.ReadFile(profilePath(desired))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		ConfigVersion int `yaml:"_config_version"`
		Model         struct {
			Default string `yaml:"default"`
			APIKey  string `yaml:"api_key"`
		} `yaml:"model"`
		Providers map[string]struct {
			KeyEnv       string `yaml:"key_env"`
			DefaultModel string `yaml:"default_model"`
		} `yaml:"providers"`
		MCPServers map[string]struct {
			Command string `yaml:"command"`
			Enabled bool   `yaml:"enabled"`
		} `yaml:"mcp_servers"`
	}
	if err := yaml.Unmarshal(configData, &config); err != nil {
		t.Fatalf("materialized profile config: %v", err)
	}
	if config.ConfigVersion != 39 {
		t.Fatalf("profile schema = %d, want 39", config.ConfigVersion)
	}
	provider, ok := config.Providers["public-provider"]
	if !ok || config.Model.Default != "public-development" || provider.DefaultModel != "public-development" {
		t.Fatal("materialized profile does not select public-development")
	}
	if config.Model.APIKey != "${TEAMKIT_PUBLIC_PROVIDER_API_KEY}" || provider.KeyEnv != providerEnvironmentKey {
		t.Fatal("materialized profile does not reference TEAMKIT_PUBLIC_PROVIDER_API_KEY")
	}
	for _, id := range []string{"v8std", "public-provider-issues", "public-provider-wiki", "officecli"} {
		server, exists := config.MCPServers[id]
		if !exists || !server.Enabled {
			t.Fatalf("materialized profile does not enable MCP %q", id)
		}
	}
	if len(config.MCPServers) != 4 || config.MCPServers["officecli"].Command != office.Path() || officeEnsures != 1 {
		t.Fatalf("OfficeCLI evidence: MCP count=%d ensure calls=%d", len(config.MCPServers), officeEnsures)
	}

	pin, err := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := hermes.VerifiedToolchainLock(profileDirectory(desired), pin)
	if err != nil {
		t.Fatalf("selected toolchain artifact: %v", err)
	}
	if lock.Toolchain != domain.ToolchainCC1CSkills {
		t.Fatalf("selected toolchain = %q, want %q", lock.Toolchain, domain.ToolchainCC1CSkills)
	}

	profileEnvironment := filepath.Join(profileDirectory(desired), ".env")
	if err := privatefile.Validate(profileEnvironment); err != nil {
		t.Fatalf("profile .env: %v", err)
	}
	environmentData, err := os.ReadFile(profileEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	keyPresent := false
	for _, line := range bytes.Split(environmentData, []byte("\n")) {
		key, _, present := bytes.Cut(line, []byte("="))
		if present && string(key) == providerEnvironmentKey {
			keyPresent = true
			break
		}
	}
	if !keyPresent {
		t.Fatal("profile .env lacks TEAMKIT_PUBLIC_PROVIDER_API_KEY")
	}

	observed, err := effects.Observe(context.Background(), desired, reconcile.UpdateNone)
	if err != nil || !observed.ToolchainReady || !observed.ApplicationReady {
		t.Fatalf("continuation bootstrap readiness: toolchain=%t application=%t err=%v", observed.ToolchainReady, observed.ApplicationReady, err)
	}
}

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

	providerKey := []byte(hermes.PublicProviderProvider().APIKeyEnvironment + "=provider-test-value\n")
	profileEnv := filepath.Join(profileDirectory(desired), ".env")
	profileData, err := os.ReadFile(profileEnv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profileEnv, bytes.ReplaceAll(profileData, providerKey, []byte(hermes.PublicProviderProvider().APIKeyEnvironment+"=   \n")), 0o600); err != nil {
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
	legacyData, err := legacy.RenderForSchema(hermes.PublicProviderProvider(), 34)
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
	exactData, err := withOfficeCLI.RenderForSchema(hermes.PublicProviderProvider(), 34)
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
	data, err := profile.Render(hermes.PublicProviderProvider())
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
	data, err := profile.Render(hermes.PublicProviderProvider())
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
	checkout := filepath.Join(home, "hermes-agent")
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
	if runtime.GOOS == "windows" {
		executablePath = filepath.Join(checkout, "venv", "Scripts", "hermes.exe")
	}
	if err := os.MkdirAll(filepath.Dir(executablePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executablePath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(checkout, "hermes_cli", "config_defaults.py")
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("DEFAULT_CONFIG = {\n    \"_config_version\": 39,\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
