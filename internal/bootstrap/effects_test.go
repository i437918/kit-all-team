package bootstrap

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/apps"
	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/gitx"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

func TestEffects_ApplyRejectsForeignWorkspace(t *testing.T) {
	home := testutil.TempDir(t)
	if err := os.WriteFile(filepath.Join(home, "foreign.txt"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	effects := Effects{}
	err := effects.Apply(context.Background(), desired(t, home), reconcile.Action{Kind: reconcile.ActionPrepareWorkspace})
	if !errors.Is(err, ErrForeignWorkspace) {
		t.Fatalf("Apply() error = %v", err)
	}
}

func TestEffects_ObserveRequiresPublicDesiredStateForWorkspaceReadiness(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := workspace.EnsureOwner(home, string(state.Project())); err != nil {
		t.Fatal(err)
	}
	observed, err := (&Effects{OfficeCLI: officeCLIFixture(state)}).Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if observed.WorkspaceReady {
		t.Fatal("owner marker without public .env was reported ready")
	}
}

func TestEffects_FinalizeAddsRequiredRootEntriesAndPreservesLocalExclude(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "kit")
	if err := os.MkdirAll(filepath.Join(home, "db", ".git", "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, home, "init")
	gitCommand(t, home, "config", "user.name", "Team Kit Test")
	gitCommand(t, home, "config", "user.email", "teamkit@example.invalid")
	const upstream = "dist/\n"
	if err := os.WriteFile(filepath.Join(home, ".gitignore"), []byte(upstream), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, home, "add", ".gitignore")
	gitCommand(t, home, "commit", "-m", "fixture")
	const localRules = "# local developer rules\n*.scratch\n"
	if err := os.WriteFile(filepath.Join(home, ".git", "info", "exclude"), []byte(localRules), 0o600); err != nil {
		t.Fatal(err)
	}

	state := desired(t, home)
	effects := Effects{InstallHooks: func(string) error { return nil }}
	if err := effects.finalize(state); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"dist/", ".env", "/db/", "/.teamkit/"} {
		if !strings.Contains(string(got), entry+"\n") {
			t.Fatalf("root .gitignore missing %q: %q", entry, got)
		}
	}
	status := strings.TrimSpace(gitCommand(t, home, "status", "--porcelain"))
	if status != "M .gitignore" && status != " M .gitignore" {
		t.Fatalf("Team Kit left unexpected managed delta: %q", status)
	}
	exclude, err := os.ReadFile(filepath.Join(home, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(exclude), localRules) {
		t.Fatalf("local exclude lost safe rules: %q", exclude)
	}
	for _, entry := range []string{".env", "/db/", "/.teamkit/"} {
		if !strings.Contains(string(exclude), entry+"\n") {
			t.Fatalf("local exclude missing %q: %q", entry, exclude)
		}
	}
}

func TestEffects_FinalizeCreatesIgnoredRootGitignoreWhenContentHasNone(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "kit")
	if err := os.MkdirAll(filepath.Join(home, "db", ".git", "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, home, "init")
	gitCommand(t, home, "config", "user.name", "Team Kit Test")
	gitCommand(t, home, "config", "user.email", "teamkit@example.invalid")
	gitCommand(t, home, "commit", "--allow-empty", "-m", "fixture")
	if err := (&Effects{InstallHooks: func(string) error { return nil }}).finalize(desired(t, home)); err != nil {
		t.Fatal(err)
	}
	rootIgnore, err := os.ReadFile(filepath.Join(home, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{".env", "/db/", "/.teamkit/"} {
		if !strings.Contains(string(rootIgnore), entry+"\n") {
			t.Fatalf("root .gitignore missing %q: %q", entry, rootIgnore)
		}
	}
	if status := gitCommand(t, home, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Fatalf("generated .gitignore is not locally ignored: %q", status)
	}
}

func gitCommand(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func TestEffects_ApplySyncsCatalogPinnedToolchainThroughPort(t *testing.T) {
	home := testutil.TempDir(t)
	var gotRemote, gotCommit, gotDestination string
	effects := Effects{HermesExecutable: testHermesExecutable(t), Git: GitPortFunc{SyncPinnedFunc: func(ctx context.Context, remote, commit, destination string) error {
		gotRemote, gotCommit, gotDestination = remote, commit, destination
		return writePinnedSkillFixture(destination, commit, domain.ToolchainCC1CSkills)
	}}, Profile: profileFixture(home)}
	state := desired(t, home)
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain}); err != nil {
		t.Fatal(err)
	}
	if gotRemote == "" || gotCommit == "" || gotDestination != filepath.Join(state.HermesHome(), "profiles", "1c-aisuz-developer-cc_1c_skills", ".teamkit", "toolchain-source") {
		t.Fatalf("pin call = %q %q %q", gotRemote, gotCommit, gotDestination)
	}
	if _, err := os.Stat(filepath.Join(state.HermesHome(), "profiles", "1c-aisuz-developer-cc_1c_skills", "skills", "fixture", "SKILL.md")); err != nil {
		t.Fatalf("materialized skill: %v", err)
	}
}

func TestEffects_ContentCloneTargetsKitRoot(t *testing.T) {
	home := testutil.TempDir(t)
	var destination string
	effects := Effects{Git: GitPortFunc{CloneContentFunc: func(ctx context.Context, remote, branch, dest string) error {
		destination = dest
		return os.MkdirAll(filepath.Join(dest, ".git"), 0o700)
	}}}
	if err := effects.Apply(context.Background(), desired(t, home), reconcile.Action{Kind: reconcile.ActionSyncContent}); err != nil {
		t.Fatal(err)
	}
	if destination != home {
		t.Fatalf("content destination = %q, want KIT root %q", destination, home)
	}
	marker, err := os.ReadFile(filepath.Join(home, ".teamkit", "content.ready"))
	if err != nil {
		t.Fatalf("content marker: %v", err)
	}
	if string(marker) != "content-aisuz\n" {
		t.Fatalf("content marker = %q", marker)
	}
}

func TestEffects_PartialDatabaseInitResumesCloneInsteadOfUpdate(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "db", ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	cloneCalls, updateCalls := 0, 0
	effects := &Effects{Git: GitPortFunc{
		CloneDatabaseFunc:  func(context.Context, string, string) error { cloneCalls++; return nil },
		UpdateDatabaseFunc: func(context.Context, string, string) error { updateCalls++; return nil },
	}}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionSyncDatabase}); err != nil {
		t.Fatal(err)
	}
	if cloneCalls != 1 || updateCalls != 0 {
		t.Fatalf("clone=%d update=%d", cloneCalls, updateCalls)
	}
}

func TestEffects_DatabaseGitWithoutReadyMarkerIsNotReady(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{filepath.Join(home, ".git"), filepath.Join(home, "db", ".git")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(contentMarkerPath(home), []byte("content-aisuz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := (&Effects{OfficeCLI: officeCLIFixture(state)}).Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if observed.DatabaseReady {
		t.Fatal("partial database .git was reported ready")
	}
}

func TestEffects_DatabaseReadinessRequiresExactManagedHooks(t *testing.T) {
	home := testutil.TempDir(t)
	state, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppCodex, AppInstalled: true,
		KitHome: home, Project: domain.ProjectAISUZ, Role: domain.RoleDeveloper,
		Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	effects := &Effects{}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{filepath.Join(home, ".git"), filepath.Join(home, "db", ".git", "hooks")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, value := range map[string]string{
		contentMarkerPath(home):  "content-aisuz\n",
		databaseMarkerPath(home): "develop\n",
	} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionConfigureApplication}); err != nil {
		t.Fatal(err)
	}
	observed, err := effects.Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if observed.DatabaseReady {
		t.Fatal("database without managed hooks was reported ready")
	}
	if err := gitx.InstallHooks(filepath.Join(home, "db", ".git", "hooks")); err != nil {
		t.Fatal(err)
	}
	observed, err = effects.Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil || !observed.DatabaseReady {
		t.Fatalf("database with exact hooks ready=%v err=%v", observed.DatabaseReady, err)
	}
}

type capturingSecrets struct {
	values map[string]string
	path   string
}

func (s *capturingSecrets) Save(values map[string]string) (string, error) {
	if s.values == nil {
		s.values = map[string]string{}
	}
	for key, value := range values {
		s.values[key] = value
	}
	if s.path != "" {
		keys := make([]string, 0, len(s.values))
		for key := range s.values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		lines := make([]string, 0, len(keys))
		for _, key := range keys {
			lines = append(lines, key+"="+s.values[key])
		}
		if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
			return "", err
		}
		if err := privatefile.WriteAtomic(s.path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			return "", err
		}
		return s.path, nil
	}
	return "app/.env", nil
}

func TestEffects_FirstRunFinalizesReadyStateAndSecondObservationIsNoOp(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	archive, digest := certificateFixture(t)
	secretStore := &capturingSecrets{
		values: map[string]string{hermes.PublicProviderProvider().APIKeyEnvironment: "provider-test-value"},
		path:   filepath.Join(state.HermesHome(), ".env"),
	}
	profileStore := &capturingSecrets{path: filepath.Join(profileDirectory(state), ".env")}
	var hookPath string
	effects := &Effects{
		Git: GitPortFunc{
			CloneContentFunc: func(_ context.Context, _, _, destination string) error {
				return os.MkdirAll(filepath.Join(destination, ".git"), 0o700)
			},
			CloneDatabaseFunc: func(_ context.Context, _, workspaceRoot string) error {
				return os.MkdirAll(filepath.Join(workspaceRoot, "db", ".git"), 0o700)
			},
			SyncPinnedFunc: func(_ context.Context, _, _, destination string) error {
				pin, _ := catalog.LookupToolchain(state.Toolchain())
				return writePinnedSkillFixture(destination, pin.Commit, state.Toolchain())
			},
		},
		Profile:            profileFixture(home),
		OfficeCLI:          officeCLIFixture(state),
		CertificateArchive: archive, CertificateSHA256: digest, Secrets: secretStore,
		ProfileSecrets: profileStore, ProfileEnvironment: map[string]string{
			hermes.PublicProviderProvider().APIKeyEnvironment: "provider-test-value",
		}, HermesExecutable: testHermesExecutable(t),
		InstallHooks: func(path string) error { hookPath = path; return gitx.InstallHooks(path) },
	}
	setTestRuntime(t, effects, 34)
	plan, err := reconcile.Plan(state, reconcile.ObservedState{})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range plan.Actions {
		if err := effects.Apply(context.Background(), state, action); err != nil {
			t.Fatalf("Apply(%s) error = %v", action.ID, err)
		}
	}
	observed, err := effects.Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reconcile.Plan(state, observed)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Actions) != 0 {
		t.Fatalf("second plan = %+v, want no-op", second)
	}
	if hookPath != filepath.Join(home, "db", ".git", "hooks") {
		t.Fatalf("hook path = %q", hookPath)
	}
	wantCA := hermes.ApplicationCAEnvironment(filepath.Join(state.HermesHome(), "certs", "ca-bundle.pem"))
	wantCA[hermes.PublicProviderProvider().APIKeyEnvironment] = "provider-test-value"
	if !reflect.DeepEqual(secretStore.values, wantCA) {
		t.Fatalf("CA values = %#v, want %#v", secretStore.values, wantCA)
	}
	if err := os.WriteFile(profilePath(state), []byte("tampered: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tampered, err := effects.Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if tampered.ApplicationReady {
		t.Fatal("tampered Hermes config was reported ready")
	}
}

func TestEffects_HermesInstallerRunsOnlyWhenApplicationIsMissing(t *testing.T) {
	home := testutil.TempDir(t)
	missing := desiredInstalled(t, home, false)
	called := 0
	archive, digest := certificateFixture(t)
	managedReady := false
	effects := &Effects{InstallerPath: filepath.Join(home, "Hermes-Setup.exe"), Installer: InstallerPortFunc(func(context.Context, string) error {
		called++
		managedReady = true
		writeBootstrapManagedInstallFixture(t, missing.HermesHome(), hermes.HermesSourceCommit+"\n", true)
		return nil
	}), Profile: profileFixture(home), OfficeCLI: officeCLIFixture(missing), ManagedInstallReady: func(string) (bool, error) { return managedReady, nil },
		CertificateArchive: archive, CertificateSHA256: digest,
		Secrets:        &capturingSecrets{path: filepath.Join(missing.HermesHome(), ".env")},
		ProfileSecrets: &capturingSecrets{path: filepath.Join(profileDirectory(missing), ".env")}}
	setTestRuntime(t, effects, 34)
	if err := effects.Apply(context.Background(), missing, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	if err := effects.Apply(context.Background(), missing, reconcile.Action{Kind: reconcile.ActionConfigureApplication}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("installer calls = %d, want 1", called)
	}
	installed := desiredInstalled(t, home, true)
	effects.HermesExecutable = testHermesExecutable(t)
	if err := effects.Apply(context.Background(), installed, reconcile.Action{Kind: reconcile.ActionConfigureApplication}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("installer ran for installed Hermes: calls=%d", called)
	}
}

func TestEffects_RejectsCertificateArchiveHashBeforeExtraction(t *testing.T) {
	home := testutil.TempDir(t)
	archive, _ := certificateFixture(t)
	effects := &Effects{CertificateArchive: archive, CertificateSHA256: strings.Repeat("0", 64), Secrets: &capturingSecrets{}, ProfileSecrets: &capturingSecrets{}, Profile: profileFixture(home), OfficeCLI: officeCLIFixture(desired(t, home)), HermesExecutable: testHermesExecutable(t)}
	setTestRuntime(t, effects, 34)
	err := effects.Apply(context.Background(), desired(t, home), reconcile.Action{Kind: reconcile.ActionConfigureApplication})
	if !errors.Is(err, ErrCertificateChecksum) {
		t.Fatalf("error = %v, want ErrCertificateChecksum", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "hermes", "certs")); !os.IsNotExist(statErr) {
		t.Fatalf("certificate directory exists after rejected archive: %v", statErr)
	}
}

func TestEffects_ConfigureHermesRequiresCertificateArchiveOrVerifiedManagedBundle(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	effects := &Effects{
		Profile:          profileFixture(home),
		OfficeCLI:        officeCLIFixture(state),
		Secrets:          &capturingSecrets{},
		ProfileSecrets:   &capturingSecrets{},
		HermesExecutable: testHermesExecutable(t),
	}
	setTestRuntime(t, effects, 34)
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}

	err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionConfigureApplication})
	if !errors.Is(err, ErrCertificateRequired) {
		t.Fatalf("ConfigureApplication error = %v, want ErrCertificateRequired", err)
	}
}

func TestEffects_ObserveRequiresManagedCertificateAndBothCAEnvironments(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	writeReadyHermesProfile(t, state)
	archive, digest := certificateFixture(t)
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := hermes.ExtractCertificates(bytes.NewReader(data), int64(len(data)), state.HermesHome())
	if err != nil {
		t.Fatal(err)
	}
	effects := &Effects{CertificateSHA256: digest, HermesExecutable: testHermesExecutable(t), OfficeCLI: officeCLIFixture(state)}

	observed, err := effects.Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ApplicationReady {
		t.Fatal("Hermes without mandatory CA environments was reported ready")
	}
	writeCertificateEnvironmentFixture(t, filepath.Join(state.HermesHome(), ".env"), bundle)
	writeCertificateEnvironmentFixture(t, filepath.Join(profileDirectory(state), ".env"), bundle)
	observed, err = effects.Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil || !observed.ApplicationReady {
		t.Fatalf("Hermes with managed CA state ready=%v err=%v", observed.ApplicationReady, err)
	}
	if err := os.WriteFile(bundle, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err = effects.Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ApplicationReady {
		t.Fatal("Hermes with tampered CA bundle was reported ready")
	}
}

func TestEffects_NonHermesDoesNotUseHermesToolchainPath(t *testing.T) {
	home := testutil.TempDir(t)
	state, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppCodex, AppInstalled: true,
		KitHome: home, Project: domain.ProjectAISUZ, Role: domain.RoleDeveloper,
		Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := (&Effects{}).Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if !observed.ToolchainReady {
		t.Fatal("non-Hermes toolchain should be delegated in the handoff")
	}
}

func TestEffects_AlternativeHandoffReadinessBindsApplicationAndToolchain(t *testing.T) {
	home := testutil.TempDir(t)
	makeState := func(toolchain domain.Toolchain) domain.DesiredState {
		state, err := domain.NewDesiredState(domain.DesiredStateInput{
			OS: domain.OSLinux, Application: domain.AppCodex, AppInstalled: true,
			KitHome: home, Project: domain.ProjectAISUZ, Role: domain.RoleDeveloper, Toolchain: toolchain,
		})
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	configured := makeState(domain.ToolchainCC1CSkills)
	effects := &Effects{}
	if err := effects.Apply(context.Background(), configured, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "db", ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".teamkit", "content.ready"), []byte("content-aisuz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := effects.Apply(context.Background(), configured, reconcile.Action{Kind: reconcile.ActionConfigureApplication}); err != nil {
		t.Fatal(err)
	}
	observed, err := effects.Observe(context.Background(), makeState(domain.ToolchainAIRules1C), reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ApplicationReady {
		t.Fatal("handoff for a different toolchain was accepted as ready")
	}
	missing, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppCodex, AppInstalled: false,
		KitHome: home, Project: domain.ProjectAISUZ, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	observed, err = effects.Observe(context.Background(), missing, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ApplicationReady {
		t.Fatal("handoff was accepted when the selected application is absent")
	}
}

func desiredAlternative(t *testing.T, home string, application domain.AIApplication, toolchain domain.Toolchain) domain.DesiredState {
	t.Helper()
	state, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: application, AppInstalled: true, KitHome: home,
		Project: domain.ProjectAISUZ, Role: domain.RoleDeveloper, Toolchain: toolchain,
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestEffects_AlternativeHandoffPersistsExactlyOnePinnedToolchain(t *testing.T) {
	applications := apps.SupportedApplications()
	toolchains := catalog.Toolchains()
	if len(applications) != 10 || len(toolchains) != 2 {
		t.Fatalf("matrix dimensions = %d applications x %d toolchains, want 10 x 2", len(applications), len(toolchains))
	}
	for _, application := range applications {
		for _, selected := range toolchains {
			t.Run(string(application)+"/"+string(selected.ID), func(t *testing.T) {
				home := testutil.TempDir(t)
				state := desiredAlternative(t, home, application, selected.ID)
				installerCalls, profileCalls := 0, 0
				effects := &Effects{
					Installer: InstallerPortFunc(func(context.Context, string) error { installerCalls++; return nil }),
					Profile: ProfilePortFuncs{CreateFunc: func(context.Context, string) error {
						profileCalls++
						return nil
					}},
					InstallHooks: func(string) error { return nil },
				}
				if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
					t.Fatal(err)
				}
				if state.Toolchain() != selected.ID {
					t.Fatalf("desired toolchain=%q want stable ID %q", state.Toolchain(), selected.ID)
				}
				if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionConfigureApplication}); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(home, "db", ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := effects.finalize(state); err != nil {
					t.Fatal(err)
				}
				publicEnv, err := os.ReadFile(filepath.Join(home, ".env"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(publicEnv), "TOOLCHAIN="+string(selected.ID)+"\n") {
					t.Fatalf("public .env does not persist stable toolchain ID %q: %q", selected.ID, publicEnv)
				}
				body, err := os.ReadFile(filepath.Join(home, ".teamkit", "handoff.txt"))
				if err != nil {
					t.Fatal(err)
				}
				wantHandoff := fmt.Sprintf("In %s, configure exactly one toolchain from %s pinned to commit %s, then configure the separate v8std MCP endpoint %s.\n", application, selected.Origin, selected.Commit, catalog.V8StdMCP().Endpoint)
				if string(body) != wantHandoff {
					t.Fatalf("handoff=%q want=%q", body, wantHandoff)
				}
				if installerCalls != 0 || profileCalls != 0 {
					t.Fatalf("handoff=%q installer=%d profile=%d", body, installerCalls, profileCalls)
				}
			})
		}
	}
}

func TestEffects_HermesProfilesAllRolesStatesAndToolchainsRemainExclusive(t *testing.T) {
	roles := []domain.Role{domain.RoleAnalyst, domain.RoleDeveloper, domain.RoleArchitect}
	toolchains := catalog.Toolchains()
	if len(toolchains) != 2 {
		t.Fatalf("toolchain count = %d, want 2", len(toolchains))
	}
	for _, role := range roles {
		for _, existing := range []bool{false, true} {
			for _, selected := range toolchains {
				name := fmt.Sprintf("%s/existing=%t/%s", role, existing, selected.ID)
				t.Run(name, func(t *testing.T) {
					kitHome := filepath.Join(testutil.TempDir(t), "kit")
					hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
					state, err := domain.NewDesiredState(domain.DesiredStateInput{
						OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
						KitHome: kitHome, HermesHome: hermesHome, HermesVersion: "0.20.2",
						Project: domain.ProjectAISUZ, Role: role, Toolchain: selected.ID,
					})
					if err != nil {
						t.Fatal(err)
					}
					if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
						t.Fatal(err)
					}
					profileRoot := profileDirectory(state)
					if existing {
						if err := os.MkdirAll(profileRoot, 0o700); err != nil {
							t.Fatal(err)
						}
						if err := workspace.WriteFileAtomic(profileOwnerPath(state), []byte(profileIdentity(state)+"\n"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					creates := 0
					type syncCall struct {
						remote string
						commit string
					}
					var syncCalls []syncCall
					effects := &Effects{
						HermesExecutable: testHermesExecutable(t),
						Profile: ProfilePortFuncs{CreateFunc: func(_ context.Context, identity string) error {
							creates++
							return os.MkdirAll(filepath.Join(hermesHome, "profiles", identity), 0o700)
						}, DoctorFunc: func(context.Context, string) error { return nil }},
						Git: GitPortFunc{SyncPinnedFunc: func(_ context.Context, remote, commit, destination string) error {
							syncCalls = append(syncCalls, syncCall{remote: remote, commit: commit})
							return writePinnedSkillFixture(destination, commit, selected.ID)
						}},
					}
					action := reconcile.Action{Kind: reconcile.ActionInstallToolchain}
					if err := effects.Apply(context.Background(), state, action); err != nil {
						t.Fatal(err)
					}
					if err := effects.Apply(context.Background(), state, action); err != nil {
						t.Fatalf("second install: %v", err)
					}
					wantCreates := 1
					if existing {
						wantCreates = 0
					}
					if creates != wantCreates {
						t.Fatalf("profile creates=%d want=%d", creates, wantCreates)
					}
					if len(syncCalls) != 2 {
						t.Fatalf("SyncPinned calls=%d want=2: %#v", len(syncCalls), syncCalls)
					}
					for index, call := range syncCalls {
						if call.remote != selected.Origin || call.commit != selected.Commit {
							t.Fatalf("SyncPinned call[%d]=%#v want origin=%q commit=%q", index, call, selected.Origin, selected.Commit)
						}
					}
					lockPath := filepath.Join(profileRoot, "external", string(selected.ID)+".json")
					lockData, err := os.ReadFile(lockPath)
					if err != nil {
						t.Fatal(err)
					}
					var lock hermes.ToolchainLock
					if err := json.Unmarshal(lockData, &lock); err != nil {
						t.Fatal(err)
					}
					if lock.Toolchain != selected.ID || lock.Commit != selected.Commit {
						t.Fatalf("lock=%#v", lock)
					}
					for _, other := range toolchains {
						if other.ID == selected.ID {
							continue
						}
						if _, err := os.Lstat(filepath.Join(profileRoot, "external", string(other.ID)+".json")); !errors.Is(err, os.ErrNotExist) {
							t.Fatalf("unselected lock exists: %v", err)
						}
					}
					installed, err := hermes.ToolchainInstalled(profileRoot, selected)
					if err != nil || !installed {
						t.Fatalf("installed=%t err=%v", installed, err)
					}
				})
			}
		}
	}
}

func TestEffects_VerifyStateRejectsAReceiptReadyWorkspaceWithoutContentGit(t *testing.T) {
	home := testutil.TempDir(t)
	state, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppCodex, AppInstalled: true,
		KitHome: home, Project: domain.ProjectAISUZ, Role: domain.RoleDeveloper,
		Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "db", ".git", "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".teamkit"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(home, ".teamkit", "owner"):         "aisuz\n",
		filepath.Join(home, ".teamkit", "content.ready"): "content-aisuz\n",
		filepath.Join(home, ".teamkit", "handoff.txt"):   "ready\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	effects := &Effects{}
	err = effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionVerifyState})
	if !errors.Is(err, ErrStateVerification) {
		t.Fatalf("Verify error = %v, want ErrStateVerification", err)
	}
}

func TestEffects_HermesLifecycleCreatesProfileBeforeSkillsAndDoctorsAfterConfiguration(t *testing.T) {
	home := testutil.TempDir(t)
	state := desiredInstalled(t, home, false)
	archive, digest := certificateFixture(t)
	managedReady := false
	sequence := []string{}
	profileSecrets := &capturingSecrets{path: filepath.Join(profileDirectory(state), ".env")}
	profile := ProfilePortFuncs{
		CreateFunc: func(_ context.Context, identity string) error {
			sequence = append(sequence, "profile-create")
			return os.MkdirAll(filepath.Join(state.HermesHome(), "profiles", identity), 0o700)
		},
		DoctorFunc: func(_ context.Context, identity string) error {
			soul, err := os.ReadFile(filepath.Join(state.HermesHome(), "profiles", identity, "SOUL.md"))
			if err != nil {
				return fmt.Errorf("read SOUL.md before Doctor: %w", err)
			}
			if !strings.Contains(string(soul), "# Роль: Программист 1С") {
				return fmt.Errorf("SOUL.md does not match developer role: %q", soul)
			}
			sequence = append(sequence, "doctor")
			return nil
		},
	}
	effects := &Effects{
		ManagedInstallReady: func(string) (bool, error) { return managedReady, nil },
		InstallerPath:       filepath.Join(home, "Hermes-Setup.exe"),
		Installer: InstallerPortFunc(func(context.Context, string) error {
			sequence = append(sequence, "install")
			managedReady = true
			writeBootstrapManagedInstallFixture(t, state.HermesHome(), hermes.HermesSourceCommit+"\n", true)
			return nil
		}),
		Profile: profile,
		OfficeCLI: &fakeOfficeCLI{
			path: filepath.Join(state.HermesHome(), ".teamkit", "officecli", "1.0.144", "officecli"),
			ensure: func(context.Context) error {
				sequence = append(sequence, "officecli")
				return nil
			},
		},
		ProfileSecrets: profileSecrets,
		Secrets: &capturingSecrets{
			values: map[string]string{hermes.PublicProviderProvider().APIKeyEnvironment: "provider-test-value"},
			path:   filepath.Join(state.HermesHome(), ".env"),
		},
		CertificateArchive: archive, CertificateSHA256: digest,
		ProfileEnvironment: map[string]string{
			"TEAMKIT_PUBLIC_PROVIDER_API_KEY": "provider-test-value",
		},
		HermesEnvironment: func(home string) error {
			if home != state.HermesHome() {
				t.Fatalf("HERMES_HOME = %q", home)
			}
			sequence = append(sequence, "environment")
			return nil
		},
		Git: GitPortFunc{SyncPinnedFunc: func(_ context.Context, _, commit, destination string) error {
			sequence = append(sequence, "sync-skills")
			return writePinnedSkillFixture(destination, commit, state.Toolchain())
		}},
	}
	setTestRuntime(t, effects, 34)
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(sequence, ",") != "environment,install,profile-create,sync-skills" {
		t.Fatalf("lifecycle sequence = %v", sequence)
	}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionConfigureApplication}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sequence, ","); !strings.HasSuffix(got, "environment,environment,officecli") {
		t.Fatalf("configure lifecycle sequence = %s", got)
	}
	if _, err := os.Stat(filepath.Join(state.HermesHome(), "profiles", profileIdentity(state), "config.yaml")); err != nil {
		t.Fatalf("Hermes config: %v", err)
	}
	if profileSecrets.values["TEAMKIT_PUBLIC_PROVIDER_API_KEY"] != "provider-test-value" || len(profileSecrets.values) != 7 {
		t.Fatalf("profile secrets = %#v", profileSecrets.values)
	}
	if err := os.MkdirAll(filepath.Join(home, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "db", ".git", "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".teamkit"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".teamkit", "owner"), []byte("aisuz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".teamkit", "content.ready"), []byte("content-aisuz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databaseMarkerPath(home), []byte("develop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profileSoulPath(state), []byte("generic Hermes persona\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	effects.InstallHooks = gitx.InstallHooks
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionVerifyState}); err != nil {
		t.Fatal(err)
	}
	if sequence[len(sequence)-1] != "doctor" {
		t.Fatalf("doctor was not last: %v", sequence)
	}
}

func TestEffects_HermesLifecycleOfficeCLIErrorPrecedesProfileSecretsCertificatesAndYAML(t *testing.T) {
	home := testutil.TempDir(t)
	state := desiredInstalled(t, home, true)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	officeErr := errors.New("injected OfficeCLI failure")
	profileSecrets := &capturingSecrets{path: filepath.Join(profileDirectory(state), ".env")}
	globalSecrets := &capturingSecrets{path: filepath.Join(state.HermesHome(), ".env")}
	effects := &Effects{
		HermesExecutable: testHermesExecutable(t),
		Profile:          profileFixture(home),
		OfficeCLI: &fakeOfficeCLI{
			path:   filepath.Join(state.HermesHome(), ".teamkit", "officecli", "1.0.144", "officecli"),
			ensure: func(context.Context) error { return officeErr },
		},
		ProfileSecrets:     profileSecrets,
		ProfileEnvironment: map[string]string{hermes.PublicProviderProvider().APIKeyEnvironment: "must-not-save"},
		Secrets:            globalSecrets,
	}
	setTestRuntime(t, effects, 34)

	err := effects.configure(context.Background(), state)
	if !errors.Is(err, officeErr) {
		t.Fatalf("configure error = %v, want OfficeCLI error", err)
	}
	if len(profileSecrets.values) != 0 || len(globalSecrets.values) != 0 {
		t.Fatalf("later secret stages ran: profile=%#v global=%#v", profileSecrets.values, globalSecrets.values)
	}
	for _, path := range []string{profilePath(state), filepath.Join(state.HermesHome(), "certs", "ca-bundle.pem")} {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("later lifecycle path %q exists: %v", path, statErr)
		}
	}
}

func TestEffects_FailedHermesProfileCreateLeavesNoReadinessMarker(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	effects := &Effects{
		HermesExecutable: testHermesExecutable(t),
		OfficeCLI:        officeCLIFixture(state),
		Git:              GitPortFunc{},
		Profile: ProfilePortFuncs{CreateFunc: func(_ context.Context, identity string) error {
			profile := filepath.Join(state.HermesHome(), "profiles", identity)
			if err := os.MkdirAll(profile, 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(profile, "config.yaml"), []byte("partial\n"), 0o600); err != nil {
				return err
			}
			return errors.New("profile create canary failure")
		}},
	}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain})
	if err == nil || !strings.Contains(err.Error(), "profile create canary failure") {
		t.Fatalf("InstallToolchain error = %v", err)
	}
	if _, err := os.Lstat(profileOwnerPath(state)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed create left readiness marker: %v", err)
	}
	if _, err := os.Lstat(profileDirectory(state)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed create left a non-retriable partial profile: %v", err)
	}
	observed, err := effects.Observe(context.Background(), state, reconcile.UpdateNone)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ApplicationReady {
		t.Fatal("partially created profile was reported ready")
	}
}

func TestEffects_ProfileRecoveryAdoptsNonceBoundPublishedProfile(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	claim := profileClaim{Identity: profileIdentity(state), StagingIdentity: "teamkit-0123456789abcdef0123456789abcdef", Nonce: "0123456789abcdef0123456789abcdef"}
	profile := profileDirectory(state)
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeProfileAdoptionProof(profile, claim); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(profileCreatingPath(state)), 0o700); err != nil {
		t.Fatal(err)
	}
	claimData, err := json.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profileCreatingPath(state), claimData, 0o600); err != nil {
		t.Fatal(err)
	}
	created := 0
	effects := &Effects{
		HermesExecutable: testHermesExecutable(t),
		Profile: ProfilePortFuncs{CreateFunc: func(context.Context, string) error {
			created++
			return errors.New("published profile must be adopted without recreate")
		}},
		Git: GitPortFunc{SyncPinnedFunc: func(_ context.Context, _, commit, destination string) error {
			return writePinnedSkillFixture(destination, commit, state.Toolchain())
		}},
	}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain}); err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("profile create calls=%d", created)
	}
	if ok, err := profileOwnerMatches(state); err != nil || !ok {
		t.Fatalf("owner ready=%v err=%v", ok, err)
	}
	if _, err := os.Lstat(profileCreatingPath(state)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("creating claim remains after success: %v", err)
	}
}

func TestEffects_ProfileRecoveryNeverDeletesUnownedFinalProfile(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	profile := profileDirectory(state)
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(profile, "user-data.sentinel")
	if err := os.WriteFile(sentinel, []byte("do not delete"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(profileCreatingPath(state)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profileCreatingPath(state), []byte(profileIdentity(state)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := 0
	effects := &Effects{
		HermesExecutable: testHermesExecutable(t),
		Profile:          ProfilePortFuncs{CreateFunc: func(context.Context, string) error { created++; return nil }},
		Git:              GitPortFunc{SyncPinnedFunc: func(context.Context, string, string, string) error { return nil }},
	}

	err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain})
	if !errors.Is(err, ErrForeignProfile) {
		t.Fatalf("InstallToolchain error = %v, want ErrForeignProfile", err)
	}
	if created != 0 {
		t.Fatalf("profile create calls=%d", created)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "do not delete" {
		t.Fatalf("user sentinel = %q, %v", contents, readErr)
	}
}

func TestEffects_ProfilePublishPreservesConcurrentFinalSentinel(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	final := profileDirectory(state)
	sentinel := filepath.Join(final, "concurrent-user.sentinel")
	effects := &Effects{HermesExecutable: testHermesExecutable(t), Git: GitPortFunc{SyncPinnedFunc: func(context.Context, string, string, string) error { return nil }}, Profile: ProfilePortFuncs{CreateFunc: func(_ context.Context, stagingIdentity string) error {
		staging := filepath.Join(state.HermesHome(), "profiles", stagingIdentity)
		if err := os.MkdirAll(staging, 0o700); err != nil {
			return err
		}
		if err := os.MkdirAll(final, 0o700); err != nil {
			return err
		}
		return os.WriteFile(sentinel, []byte("concurrent user data"), 0o600)
	}}}

	err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain})
	if !errors.Is(err, ErrForeignProfile) {
		t.Fatalf("InstallToolchain error = %v, want ErrForeignProfile", err)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "concurrent user data" {
		t.Fatalf("concurrent sentinel = %q, %v", contents, readErr)
	}
}

func TestEffects_HermesProfileEnvironmentIsNormalizedBeforePublish(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	secret := []byte("TEAMKIT_PUBLIC_PROVIDER_API_KEY=sentinel\n")
	effects := &Effects{
		HermesExecutable: testHermesExecutable(t),
		Git: GitPortFunc{SyncPinnedFunc: func(_ context.Context, _, commit, destination string) error {
			return writePinnedSkillFixture(destination, commit, state.Toolchain())
		}},
		Profile: ProfilePortFuncs{CreateFunc: func(_ context.Context, stagingIdentity string) error {
			staging := filepath.Join(state.HermesHome(), "profiles", stagingIdentity)
			if err := os.MkdirAll(staging, 0o700); err != nil {
				return err
			}
			path := filepath.Join(staging, ".env")
			if err := os.WriteFile(path, secret, 0o644); err != nil {
				return err
			}
			return os.Chmod(path, 0o644)
		}},
	}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(profileDirectory(state), ".env")
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("profile environment bytes changed: len=%d err=%v", len(got), err)
	}
	if err := privatefile.Validate(path); err != nil {
		t.Fatalf("published profile environment is unsafe: %v", err)
	}
}

func TestEffects_HermesCompatibilityRepairsOwnedLegacyEnvironmentAndMigratesOnce(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	profileRoot := profileDirectory(state)
	if err := os.MkdirAll(profileRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteFileAtomic(profileOwnerPath(state), []byte(profileIdentity(state)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := filepath.Join(profileRoot, ".env")
	secret := []byte("TEAMKIT_PUBLIC_PROVIDER_API_KEY=legacy-sentinel\n")
	if err := os.WriteFile(environment, secret, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(environment, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(profileRoot, ".no-bundled-skills")
	markerBody := "This profile opted out of bundled-skill seeding (`hermes profile create --no-skills`).\nDelete this file to re-enable sync on the next `hermes update`.\n"
	if err := os.WriteFile(marker, []byte(markerBody), 0o600); err != nil {
		t.Fatal(err)
	}
	materializeSelectedFixture(t, state, profileRoot)
	executable := testHermesExecutable(t)
	contract := testRuntimeContract(executable, 37)
	optInCalls := 0
	effects := &Effects{
		HermesExecutable: executable,
		OfficeCLI:        officeCLIFixture(state),
		RuntimeContract:  contract,
		RuntimeProbe: func(context.Context, string) (hermes.RuntimeContract, error) {
			return contract, nil
		},
		Profile: ProfilePortFuncs{
			OptInBundledSkillsFunc: func(_ context.Context, identity string) error {
				optInCalls++
				if identity != profileIdentity(state) {
					t.Fatalf("opt-in identity=%q", identity)
				}
				if err := os.Remove(marker); err != nil {
					return err
				}
				return os.MkdirAll(filepath.Join(profileRoot, "skills", "hermes-default"), 0o700)
			},
		},
	}
	if err := effects.ensureHermesCompatibility(context.Background(), state); err != nil {
		t.Fatalf("first compatibility: %v", err)
	}
	if err := effects.ensureHermesCompatibility(context.Background(), state); err != nil {
		t.Fatalf("second compatibility: %v", err)
	}
	if optInCalls != 1 {
		t.Fatalf("opt-in calls=%d want=1", optInCalls)
	}
	if err := privatefile.Validate(environment); err != nil {
		t.Fatalf("legacy environment remains unsafe: %v", err)
	}
	got, err := os.ReadFile(environment)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("legacy environment changed: %q, %v", got, err)
	}
}

func TestEffects_HermesCompatibilityRejectsBundledCollisionBeforeACLOrOptIn(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	profileRoot := profileDirectory(state)
	if err := os.MkdirAll(filepath.Join(profileRoot, "skills", "user-sentinel"), 0o700); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(profileRoot, "skills", "user-sentinel", "SKILL.md")
	if err := os.WriteFile(userPath, []byte("user-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(profileRoot, "skills", "hermes-default"), 0o700); err != nil {
		t.Fatal(err)
	}
	bundledPath := filepath.Join(profileRoot, "skills", "hermes-default", "SKILL.md")
	if err := os.WriteFile(bundledPath, []byte("bundled-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteFileAtomic(profileOwnerPath(state), []byte(profileIdentity(state)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := filepath.Join(profileRoot, ".env")
	if err := os.WriteFile(environment, []byte("secret-sentinel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(environment, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(profileRoot, ".no-bundled-skills")
	markerBody := "This profile opted out of bundled-skill seeding (`hermes profile create --no-skills`).\nDelete this file to re-enable sync on the next `hermes update`.\n"
	if err := os.WriteFile(marker, []byte(markerBody), 0o600); err != nil {
		t.Fatal(err)
	}
	materializeSelectedFixture(t, state, profileRoot)
	lockPath := filepath.Join(profileRoot, "external", string(state.Toolchain())+".json")
	externalPath := filepath.Join(profileRoot, "skills", "fixture", "SKILL.md")
	beforeLock, _ := os.ReadFile(lockPath)
	beforeExternal, _ := os.ReadFile(externalPath)
	beforeUser, _ := os.ReadFile(userPath)
	beforeBundled, _ := os.ReadFile(bundledPath)

	executable := testHermesExecutable(t)
	contract := testRuntimeContract(executable, 37)
	contract.BundledSkills = []string{"fixture"}
	optInCalls := 0
	effects := &Effects{
		HermesExecutable: executable,
		OfficeCLI:        officeCLIFixture(state),
		RuntimeContract:  contract,
		RuntimeProbe:     func(context.Context, string) (hermes.RuntimeContract, error) { return contract, nil },
		Profile: ProfilePortFuncs{OptInBundledSkillsFunc: func(context.Context, string) error {
			optInCalls++
			return nil
		}},
	}
	err := effects.ensureHermesCompatibility(context.Background(), state)
	if !errors.Is(err, hermes.ErrToolchainCollision) {
		t.Fatalf("compatibility error=%v want ErrToolchainCollision", err)
	}
	if optInCalls != 0 {
		t.Fatalf("opt-in calls=%d want=0", optInCalls)
	}
	if err := privatefile.Validate(environment); err == nil {
		t.Fatal("collision normalized the legacy environment before failing")
	}
	for path, before := range map[string][]byte{lockPath: beforeLock, externalPath: beforeExternal, userPath: beforeUser, bundledPath: beforeBundled} {
		after, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(after, before) {
			t.Fatalf("collision changed %s: before=%q after=%q err=%v", path, before, after, readErr)
		}
	}
}

func TestEffects_HermesCompatibilityRejectsExternalToolchainDriftAfterOptIn(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	profileRoot := profileDirectory(state)
	if err := os.MkdirAll(profileRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteFileAtomic(profileOwnerPath(state), []byte(profileIdentity(state)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileRoot, ".env"), []byte("secret-sentinel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(profileRoot, ".no-bundled-skills")
	markerBody := "This profile opted out of bundled-skill seeding (`hermes profile create --no-skills`).\nDelete this file to re-enable sync on the next `hermes update`.\n"
	if err := os.WriteFile(marker, []byte(markerBody), 0o600); err != nil {
		t.Fatal(err)
	}
	materializeSelectedFixture(t, state, profileRoot)
	externalPath := filepath.Join(profileRoot, "skills", "fixture", "SKILL.md")
	executable := testHermesExecutable(t)
	contract := testRuntimeContract(executable, 37)
	effects := &Effects{
		HermesExecutable: executable,
		RuntimeContract:  contract,
		RuntimeProbe:     func(context.Context, string) (hermes.RuntimeContract, error) { return contract, nil },
		Profile: ProfilePortFuncs{OptInBundledSkillsFunc: func(context.Context, string) error {
			if err := os.Remove(marker); err != nil {
				return err
			}
			return os.Remove(externalPath)
		}},
	}
	err := effects.ensureHermesCompatibility(context.Background(), state)
	if !errors.Is(err, hermes.ErrBundledSkillsMigrationFailed) && !errors.Is(err, hermes.ErrManagedInvariant) {
		t.Fatalf("compatibility error=%v want migration or managed invariant failure", err)
	}
}

func TestEffects_HermesNewProfileKeepsBundledSkillsAndWritesSchema37V8Std(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	archive, digest := certificateFixture(t)
	executable := testHermesExecutable(t)
	contract := testRuntimeContract(executable, 37)
	profileStore := &capturingSecrets{path: filepath.Join(profileDirectory(state), ".env")}
	effects := &Effects{
		HermesExecutable: executable,
		OfficeCLI:        officeCLIFixture(state),
		RuntimeContract:  contract,
		RuntimeProbe: func(context.Context, string) (hermes.RuntimeContract, error) {
			return contract, nil
		},
		Git: GitPortFunc{SyncPinnedFunc: func(_ context.Context, _, commit, destination string) error {
			return writePinnedSkillFixture(destination, commit, state.Toolchain())
		}},
		Profile: ProfilePortFuncs{CreateFunc: func(_ context.Context, identity string) error {
			root := filepath.Join(state.HermesHome(), "profiles", identity)
			if err := os.MkdirAll(filepath.Join(root, "skills", "hermes-default"), 0o700); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(root, "skills", "learned-user"), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(root, "skills", "learned-user", "SKILL.md"), []byte("learned-sentinel"), 0o600); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(root, ".env"), []byte("TEAMKIT_PUBLIC_PROVIDER_API_KEY=seed\n"), 0o644)
		}},
		ProfileSecrets: profileStore,
		Secrets:        &capturingSecrets{path: filepath.Join(state.HermesHome(), ".env")},
		ProfileEnvironment: map[string]string{
			hermes.PublicProviderProvider().APIKeyEnvironment: "provider-test-value",
		},
		CertificateArchive: archive,
		CertificateSHA256:  digest,
	}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain}); err != nil {
		t.Fatalf("install toolchain: %v", err)
	}
	if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionConfigureApplication}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	profileRoot := profileDirectory(state)
	for _, name := range []string{"hermes-default", "learned-user", "fixture"} {
		if info, err := os.Stat(filepath.Join(profileRoot, "skills", name)); err != nil || !info.IsDir() {
			t.Fatalf("skill %q missing: %v", name, err)
		}
	}
	learned, err := os.ReadFile(filepath.Join(profileRoot, "skills", "learned-user", "SKILL.md"))
	if err != nil || string(learned) != "learned-sentinel" {
		t.Fatalf("Learned skill changed: %q, %v", learned, err)
	}
	if _, err := os.Stat(filepath.Join(profileRoot, "external", string(domain.ToolchainAIRules1C)+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second toolchain lock exists: %v", err)
	}
	config, err := os.ReadFile(profilePath(state))
	if err != nil || !bytes.Contains(config, []byte("_config_version: 37")) || !bytes.Contains(config, []byte("v8std:")) || !bytes.Contains(config, []byte(catalog.V8StdMCP().Endpoint)) {
		t.Fatalf("config does not contain schema 37 v8std: %q, %v", config, err)
	}
	if err := privatefile.Validate(filepath.Join(profileRoot, ".env")); err != nil {
		t.Fatalf("profile environment is unsafe: %v", err)
	}
}

func TestNormalizeStagingProfileEnvironment_RejectsUnprovenOrForeignProfile(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	claim := profileClaim{Identity: profileIdentity(state), StagingIdentity: "teamkit-0123456789abcdef0123456789abcdef", Nonce: "0123456789abcdef0123456789abcdef"}
	for _, test := range []struct {
		name     string
		mutate   func(t *testing.T, staging string)
		callPath func(staging string) string
	}{
		{name: "wrong proof", mutate: func(t *testing.T, staging string) {
			wrong := claim
			wrong.Nonce = "abcdef0123456789abcdef0123456789"
			if err := writeProfileAdoptionProof(staging, wrong); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "changed staging identity", mutate: func(t *testing.T, staging string) {
			if err := writeProfileAdoptionProof(staging, claim); err != nil {
				t.Fatal(err)
			}
		}, callPath: func(staging string) string {
			return filepath.Join(filepath.Dir(staging), "teamkit-abcdef0123456789abcdef0123456789")
		}},
		{name: "existing final profile", mutate: func(t *testing.T, staging string) {
			if err := writeProfileAdoptionProof(staging, claim); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(filepath.Dir(staging), claim.Identity), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			profiles := filepath.Join(testutil.TempDir(t), "profiles")
			staging := filepath.Join(profiles, claim.StagingIdentity)
			if err := os.MkdirAll(staging, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(staging, ".env")
			if err := os.WriteFile(path, []byte("sentinel"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, staging)
			callPath := staging
			if test.callPath != nil {
				callPath = test.callPath(staging)
			}
			err := normalizeStagingProfileEnvironment(callPath, claim)
			if !errors.Is(err, ErrForeignProfile) {
				t.Fatalf("normalize error = %v, want ErrForeignProfile", err)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != "sentinel" {
				t.Fatalf("foreign environment changed: body=%q err=%v", got, readErr)
			}
		})
	}
}

func TestEffects_HermesProfileRejectsSymlinkedParent(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state.HermesHome(), 0o700); err != nil {
		t.Fatal(err)
	}
	target := testutil.TempDir(t)
	if err := os.Symlink(target, filepath.Join(state.HermesHome(), "profiles")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	createCalls := 0
	effects := &Effects{
		Git: GitPortFunc{},
		Profile: ProfilePortFuncs{CreateFunc: func(context.Context, string) error {
			createCalls++
			return nil
		}},
	}
	err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain})
	if !errors.Is(err, ErrForeignProfile) {
		t.Fatalf("InstallToolchain error = %v; want ErrForeignProfile", err)
	}
	if createCalls != 0 {
		t.Fatalf("profile create crossed symlinked parent: calls=%d", createCalls)
	}
}

func TestEffects_DatabaseRejectsSymlinkedDirectoryBeforeGit(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(home, "db")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	gitCalls := 0
	effects := &Effects{Git: GitPortFunc{
		CloneDatabaseFunc:  func(context.Context, string, string) error { gitCalls++; return nil },
		UpdateDatabaseFunc: func(context.Context, string, string) error { gitCalls++; return nil },
	}}

	err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionSyncDatabase})
	if !errors.Is(err, ErrForeignWorkspace) {
		t.Fatalf("SyncDatabase error = %v, want ErrForeignWorkspace", err)
	}
	if gitCalls != 0 {
		t.Fatalf("Git crossed symlinked db: calls=%d", gitCalls)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
}

func TestEffects_HermesProfileRejectsSymlinkedMetadataBeforeGit(t *testing.T) {
	home := testutil.TempDir(t)
	state := desired(t, home)
	if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil {
		t.Fatal(err)
	}
	profile := profileDirectory(state)
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	external := testutil.TempDir(t)
	if err := os.Symlink(external, filepath.Join(profile, ".teamkit")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(profileOwnerPath(state)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profileOwnerPath(state), []byte(profileIdentity(state)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCalls := 0
	effects := &Effects{Git: GitPortFunc{SyncPinnedFunc: func(context.Context, string, string, string) error {
		gitCalls++
		return nil
	}}}

	err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionInstallToolchain})
	if !errors.Is(err, ErrForeignProfile) {
		t.Fatalf("InstallToolchain error = %v, want ErrForeignProfile", err)
	}
	if gitCalls != 0 {
		t.Fatalf("Git crossed symlinked profile metadata: calls=%d", gitCalls)
	}
}

func TestEffects_ObserveRejectsUnownedGeneratedProfile(t *testing.T) {
	home := testutil.TempDir(t)
	state, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
		KitHome: home, HermesHome: filepath.Join(testutil.TempDir(t), "hermes"),
		Project: domain.ProjectAISUZ, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := profileDirectory(state)
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "foreign.txt"), []byte("foreign\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = (&Effects{}).Observe(context.Background(), state, reconcile.UpdateNone)
	if !errors.Is(err, ErrForeignProfile) {
		t.Fatalf("Observe error=%v, want ErrForeignProfile", err)
	}
}

func desired(t *testing.T, home string) domain.DesiredState {
	return desiredInstalled(t, home, true)
}

func desiredInstalled(t *testing.T, home string, installed bool) domain.DesiredState {
	t.Helper()
	state, err := domain.NewDesiredState(domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: installed, KitHome: home, HermesHome: filepath.Join(home, "hermes"), Project: domain.ProjectAISUZ, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func certificateFixture(t *testing.T) (string, string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("ca-bundle.pem")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("test certificate bundle")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(testutil.TempDir(t), "certs.zip")
	if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(buffer.Bytes())
	return path, hex.EncodeToString(digest[:])
}

func profileFixture(home string) ProfilePortFuncs {
	return ProfilePortFuncs{
		CreateFunc: func(_ context.Context, identity string) error {
			return os.MkdirAll(filepath.Join(home, "hermes", "profiles", identity), 0o700)
		},
		DoctorFunc: func(context.Context, string) error { return nil },
	}
}

func writeCertificateEnvironmentFixture(t *testing.T, path, bundle string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	values := hermes.ApplicationCAEnvironment(bundle)
	values[hermes.PublicProviderProvider().APIKeyEnvironment] = "provider-test-value"
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var lines []string
	for _, key := range keys {
		lines = append(lines, key+"="+values[key])
	}
	if err := privatefile.WriteAtomic(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testHermesExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(testutil.TempDir(t), "hermes")
	if err := os.WriteFile(path, []byte("test Hermes executable\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func testRuntimeContract(executable string, schema int) hermes.RuntimeContract {
	return hermes.RuntimeContract{
		Info: hermes.RuntimeInfo{
			Executable: executable,
			InstallDir: filepath.Dir(executable),
			Version:    "0.20.1",
		},
		Identity:               hermes.RuntimeIdentity{InstallRootKey: "test-runtime-root", ExecutableKey: "test-runtime-executable"},
		ConfigSchema:           schema,
		BundledSkills:          []string{"hermes-default"},
		BundledInventorySHA256: strings.Repeat("a", 64),
	}
}

func setTestRuntime(t *testing.T, effects *Effects, schema int) {
	t.Helper()
	if effects.HermesExecutable == "" {
		effects.HermesExecutable = testHermesExecutable(t)
	}
	contract := testRuntimeContract(effects.HermesExecutable, schema)
	effects.RuntimeContract = contract
	effects.RuntimeProbe = func(context.Context, string) (hermes.RuntimeContract, error) { return contract, nil }
}

type fakeOfficeCLI struct {
	path   string
	ensure func(context.Context) error
	ready  func(context.Context) (bool, error)
}

func officeCLIFixture(desired domain.DesiredState) *fakeOfficeCLI {
	return &fakeOfficeCLI{path: filepath.Join(desired.HermesHome(), ".teamkit", "officecli", "1.0.144", "officecli.exe")}
}

func (f *fakeOfficeCLI) Path() string { return f.path }
func (f *fakeOfficeCLI) Ensure(ctx context.Context) error {
	if f.ensure != nil {
		return f.ensure(ctx)
	}
	return nil
}
func (f *fakeOfficeCLI) Ready(ctx context.Context) (bool, error) {
	if f.ready != nil {
		return f.ready(ctx)
	}
	return true, nil
}

func materializeSelectedFixture(t *testing.T, state domain.DesiredState, profileRoot string) {
	t.Helper()
	pin, err := catalog.LookupToolchain(state.Toolchain())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(testutil.TempDir(t), "toolchain")
	if err := writePinnedSkillFixture(source, pin.Commit, state.Toolchain()); err != nil {
		t.Fatal(err)
	}
	if err := hermes.MaterializeToolchain(source, profileRoot, pin); err != nil {
		t.Fatal(err)
	}
}

func writePinnedSkillFixture(destination, commit string, toolchain domain.Toolchain) error {
	if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(destination, ".git", "HEAD"), []byte(commit+"\n"), 0o600); err != nil {
		return err
	}
	subpath := filepath.Join(".claude", "skills")
	if toolchain == domain.ToolchainAIRules1C {
		subpath = filepath.Join("content", "skills")
	}
	skill := filepath.Join(destination, subpath, "fixture")
	if err := os.MkdirAll(skill, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# fixture\n"), 0o600)
}
