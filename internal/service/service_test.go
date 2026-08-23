package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mi1man-cmd/kit-all-team/internal/apps"
	"github.com/mi1man-cmd/kit-all-team/internal/bootstrap"
	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/cli"
	"github.com/mi1man-cmd/kit-all-team/internal/config"
	"github.com/mi1man-cmd/kit-all-team/internal/credentials"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/engine"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/gitx"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/operationlock"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/platform"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/secrets"
	"github.com/mi1man-cmd/kit-all-team/internal/state"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

func TestService_PlanAndStatusAreReadOnlyAndDoNotOpenSecrets(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppCursor, true, "")
	opened, privateHomeAccessed := 0, 0
	svc := New(Options{
		GitRunner: readySourceObservationRunner(t, desired, ""),
		ApplicationHome: func(domain.DesiredState) (string, error) {
			privateHomeAccessed++
			return "", errors.New("plan must not resolve private application home")
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			opened++
			return nil, errors.New("must not open")
		},
		AskPass: func(string, gitx.Credentials) (AskPassSession, error) {
			t.Fatal("plan must not create askpass")
			return nil, nil
		},
		Downloader: DownloadFunc(func(context.Context, string) ([]byte, error) {
			t.Fatal("plan must not download")
			return nil, nil
		}),
		Process: platform.ProcessRunnerFunc(func(context.Context, string, []string) error {
			t.Fatal("plan must not start a process")
			return nil
		}),
	})
	if _, err := svc.Plan(context.Background(), desired, reconcile.UpdateNone); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	prepareConvergedWorkspace(t, desired)
	writeDesired(t, desired)
	if _, err := os.Lstat(filepath.Join(desired.KitHome(), ".git")); err != nil {
		t.Fatalf("content marker fixture: %v", err)
	}
	status, plan, err := svc.Status(context.Background(), desired.KitHome())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status != reconcile.StatusReady || len(plan.Actions) != 0 {
		t.Fatalf("status=%q plan=%#v", status, plan)
	}
	if opened != 0 {
		t.Fatalf("secret store opened %d times", opened)
	}
	if privateHomeAccessed != 0 {
		t.Fatalf("private application home accessed %d times", privateHomeAccessed)
	}
}

func TestResolveOfficeCLIAsset_UsesFixedSupportedPlatformMatrix(t *testing.T) {
	tests := []struct {
		name         string
		family       domain.OSFamily
		architecture string
		wantOS       domain.OSFamily
		wantArch     string
		wantErr      error
	}{
		{name: "windows ignores host arm", family: domain.OSWindows, architecture: "arm64", wantOS: domain.OSWindows, wantArch: "amd64"},
		{name: "linux ignores host arm", family: domain.OSLinux, architecture: "arm64", wantOS: domain.OSLinux, wantArch: "amd64"},
		{name: "alt uses linux amd64", family: domain.OSALTLinux, architecture: "arm64", wantOS: domain.OSLinux, wantArch: "amd64"},
		{name: "mac amd64", family: domain.OSMacOS, architecture: "amd64", wantOS: domain.OSMacOS, wantArch: "amd64"},
		{name: "mac arm64", family: domain.OSMacOS, architecture: "arm64", wantOS: domain.OSMacOS, wantArch: "arm64"},
		{name: "mac unsupported", family: domain.OSMacOS, architecture: "386", wantErr: catalog.ErrOfficeCLIPlatformUnsupported},
		{name: "unknown", family: domain.OSFamily("other"), architecture: "amd64", wantErr: catalog.ErrOfficeCLIPlatformUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset, err := resolveOfficeCLIAsset(test.family, test.architecture)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("resolveOfficeCLIAsset() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil || asset.OS != test.wantOS || asset.Architecture != test.wantArch {
				t.Fatalf("resolveOfficeCLIAsset() = %#v, %v; want OS=%s arch=%s", asset, err, test.wantOS, test.wantArch)
			}
		})
	}
}

func TestService_PlanOfficeCLIReadinessUsesNoMutationAdapters(t *testing.T) {
	originalHomeResolver := officeCLIUserHomeResolver
	t.Cleanup(func() { officeCLIUserHomeResolver = originalHomeResolver })
	for _, validAsset := range []bool{false, true} {
		name := "missing asset"
		if validAsset {
			name = "valid asset"
		}
		t.Run(name, func(t *testing.T) {
			kitHome := filepath.Join(testutil.TempDir(t), "kit")
			hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
			desired := testDesired(t, kitHome, domain.AppHermes, true, hermesHome)
			executable := filepath.Join(testutil.TempDir(t), "hermes")
			if err := os.WriteFile(executable, []byte("verified Hermes"), 0o700); err != nil {
				t.Fatal(err)
			}
			userHome := testutil.TempDir(t)
			officeCLIUserHomeResolver = func() (string, error) { return userHome, nil }
			asset, err := resolveOfficeCLIAsset(desired.OS(), "arm64")
			if err != nil {
				t.Fatal(err)
			}
			if validAsset {
				path, err := officeCLIManagedPath(hermesHome, asset.Version)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o700)
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate(asset.Size); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				writeOfficeCLIConfig(t, filepath.Join(userHome, ".officecli", "config.json"), false, false)
			}
			verifyCalls := 0
			svc := New(Options{
				ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
					return hermes.DiscoveryResult{Installed: true, Home: hermesHome, Executable: executable, Version: "0.20.1"}, nil
				},
				Downloader: DownloadFunc(func(context.Context, string) ([]byte, error) {
					t.Fatal("Plan must not download OfficeCLI")
					return nil, nil
				}),
				WritePrivate: func(string, []byte) error {
					t.Fatal("Plan must not write OfficeCLI")
					return nil
				},
				Process: platform.ProcessRunnerFunc(func(context.Context, string, []string) error {
					t.Fatal("Plan must not start OfficeCLI")
					return nil
				}),
				VerifyDigest: func([]byte, string) bool { verifyCalls++; return true },
			})
			if _, err := svc.Plan(context.Background(), desired, reconcile.UpdateNone); err != nil {
				t.Fatalf("Plan: %v", err)
			}
			wantVerifyCalls := 0
			if validAsset {
				wantVerifyCalls = 1
			}
			if verifyCalls != wantVerifyCalls {
				t.Fatalf("digest verification calls = %d, want %d", verifyCalls, wantVerifyCalls)
			}
		})
	}
}

func TestService_ApplyRejectsRedirectedOfficeCLIPathBeforeMutationState(t *testing.T) {
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	if err := os.MkdirAll(filepath.Join(hermesHome, ".teamkit"), 0o700); err != nil {
		t.Fatal(err)
	}
	redirectOfficeCLIPath(t, filepath.Join(hermesHome, ".teamkit", "officecli"), testutil.TempDir(t))
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "kit"), domain.AppHermes, true, hermesHome)
	opened := 0
	originalHomeResolver := officeCLIUserHomeResolver
	officeCLIUserHomeResolver = func() (string, error) {
		t.Fatal("redirected managed path reached OfficeCLI config resolution")
		return "", nil
	}
	t.Cleanup(func() { officeCLIUserHomeResolver = originalHomeResolver })
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) { opened++; return testutil.TempDir(t), nil },
		SecretStore: func(string) (credentials.SecretStore, error) {
			opened++
			return nil, errors.New("must not open")
		},
		StateStore: func(string) (engine.Store, error) {
			opened++
			return nil, errors.New("must not prepare")
		},
		Downloader:   DownloadFunc(func(context.Context, string) ([]byte, error) { opened++; return nil, nil }),
		WritePrivate: func(string, []byte) error { opened++; return nil },
		Process:      platform.ProcessRunnerFunc(func(context.Context, string, []string) error { opened++; return nil }),
		Effects:      func(EffectInputs) engine.Effects { opened++; return failingEffects{} },
	})
	_, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{})
	if !errors.Is(err, bootstrap.ErrForeignProfile) {
		t.Fatalf("Apply error = %v, want ErrForeignProfile", err)
	}
	if opened != 0 {
		t.Fatalf("redirected OfficeCLI path reached mutation adapters: %d calls", opened)
	}
}

func TestValidateHermesServicePaths_RejectsUnsafeOfficeCLIComponents(t *testing.T) {
	asset, err := resolveOfficeCLIAsset(domain.OSLinux, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "redirected root", setup: func(t *testing.T, home string) {
			if err := os.MkdirAll(filepath.Join(home, ".teamkit"), 0o700); err != nil {
				t.Fatal(err)
			}
			redirectOfficeCLIPath(t, filepath.Join(home, ".teamkit", "officecli"), testutil.TempDir(t))
		}},
		{name: "version is regular file", setup: func(t *testing.T, home string) {
			path := filepath.Join(home, ".teamkit", "officecli", asset.Version)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "executable is directory", setup: func(t *testing.T, home string) {
			path := filepath.Join(home, ".teamkit", "officecli", asset.Version, officeCLIExecutableName())
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := filepath.Join(testutil.TempDir(t), "hermes")
			test.setup(t, home)
			if err := validateHermesServicePaths(home); !errors.Is(err, bootstrap.ErrForeignProfile) {
				t.Fatalf("validateHermesServicePaths() error = %v, want ErrForeignProfile", err)
			}
		})
	}
}

func TestService_ApplyNoOpDoesNotOpenSecretsOrCreateAskPass(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppCursor, true, "")
	prepareConvergedWorkspace(t, desired)
	opened, askPass := 0, 0
	svc := New(Options{
		GitRunner: readySourceObservationRunner(t, desired, ""),
		SecretStore: func(string) (credentials.SecretStore, error) {
			opened++
			return nil, errors.New("must not open")
		},
		AskPass: func(string, gitx.Credentials) (AskPassSession, error) {
			askPass++
			return nil, errors.New("must not create")
		},
	})
	plan, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{Secrets: map[string]string{credentials.GitLabToken: "TEAMKIT_NOOP_CANARY"}})
	if err != nil || len(plan.Actions) != 0 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if opened != 0 || askPass != 0 {
		t.Fatalf("secret stores=%d askpass=%d", opened, askPass)
	}
}

func TestService_ApplyRejectsConcurrentWorkspaceMutation(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCodex, true, "")
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	held, err := operationlock.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	var applied domain.DesiredState
	svc := New(Options{
		ApplicationLookPath: func(string) (string, error) { return filepath.Join(testutil.TempDir(t), "codex"), nil },
		ApplicationHome:     func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		SecretStore:         func(string) (credentials.SecretStore, error) { return &recordingSecretStore{}, nil },
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			return &recordingAskPass{credentials: input}, nil
		},
		Effects:  func(EffectInputs) engine.Effects { return captureEffects{desired: &applied} },
		TempRoot: testutil.TempDir(t),
	})
	_, err = svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{Secrets: map[string]string{
		credentials.GitLabUsername: "user", credentials.GitLabToken: "token",
	}})
	if !errors.Is(err, operationlock.ErrOperationInProgress) {
		t.Fatalf("Apply error = %v, want ErrOperationInProgress", err)
	}
	if applied.Project() != "" {
		t.Fatalf("concurrent Apply reached effects with desired %#v", applied)
	}
}

func TestService_PlanDetectsDirtyWorktreeBeforeAnySecretAccess(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppCursor, true, "")
	prepareConvergedWorkspace(t, desired)
	opened := 0
	svc := New(Options{
		GitRunner: readySourceObservationRunner(t, desired, desired.KitHome()),
		SecretStore: func(string) (credentials.SecretStore, error) {
			opened++
			return nil, errors.New("must not open")
		},
	})
	_, err := svc.Plan(context.Background(), desired, reconcile.UpdateNone)
	if gitx.ErrorCode(err) != "LOCAL_CHANGES_DETECTED" {
		t.Fatalf("error=%v code=%q", err, gitx.ErrorCode(err))
	}
	if opened != 0 {
		t.Fatalf("opened secret stores=%d", opened)
	}
}

func TestService_StatusDetectsDirtyWorktree(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppCursor, true, "")
	prepareConvergedWorkspace(t, desired)
	writeDesired(t, desired)
	svc := New(Options{GitRunner: readySourceObservationRunner(t, desired, desired.KitHome())})
	_, _, err := svc.Status(context.Background(), desired.KitHome())
	if gitx.ErrorCode(err) != "LOCAL_CHANGES_DETECTED" {
		t.Fatalf("error=%v code=%q", err, gitx.ErrorCode(err))
	}
}

func TestService_PlanRejectsReadyContentSourceDriftBeforeSecrets(t *testing.T) {
	project, err := catalog.LookupProject(domain.ProjectWMS)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		marker      string
		origin      string
		branch      string
		status      string
		symbolicErr error
		wantCode    string
	}{
		{name: "remote", marker: project.ContentBranch, origin: "https://attacker.invalid/content.git", branch: project.ContentBranch, wantCode: "GIT_REMOTE_MISMATCH"},
		{name: "stale_marker_remote", marker: "content-other", origin: "https://attacker.invalid/content.git", branch: project.ContentBranch, wantCode: "GIT_REMOTE_MISMATCH"},
		{name: "branch", marker: project.ContentBranch, origin: project.ContentRepository, branch: "content-other", wantCode: "GIT_BRANCH_MISMATCH"},
		{name: "detached", marker: project.ContentBranch, origin: project.ContentRepository, symbolicErr: errors.New("detached HEAD"), wantCode: "GIT_BRANCH_MISMATCH"},
		{name: "dirty", marker: project.ContentBranch, origin: project.ContentRepository, branch: project.ContentBranch, status: " M tracked.txt\n", wantCode: "LOCAL_CHANGES_DETECTED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppCursor, true, "")
			prepareConvergedWorkspace(t, desired)
			if err := os.WriteFile(filepath.Join(desired.KitHome(), ".teamkit", "content.ready"), []byte(test.marker+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(desired.KitHome(), ".teamkit", "database.ready")); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			opened := 0
			var commands []gitx.Command
			runner := gitRunnerFunc(func(_ context.Context, command gitx.Command) (gitx.Result, error) {
				commands = append(commands, command)
				joined := strings.Join(command.Args, " ")
				switch {
				case strings.Contains(joined, "config --local"):
					return gitx.Result{}, nil
				case strings.Contains(joined, "config --get remote.origin.url"):
					return gitx.Result{Stdout: test.origin + "\n"}, nil
				case strings.Contains(joined, "symbolic-ref"):
					if test.symbolicErr != nil {
						return gitx.Result{}, test.symbolicErr
					}
					return gitx.Result{Stdout: test.branch + "\n"}, nil
				case strings.Contains(joined, "status --porcelain"):
					return gitx.Result{Stdout: test.status}, nil
				default:
					t.Fatalf("unexpected Git observation: %#v", command)
					return gitx.Result{}, nil
				}
			})
			svc := New(Options{
				GitRunner: runner,
				SecretStore: func(string) (credentials.SecretStore, error) {
					opened++
					return nil, errors.New("must not open")
				},
			})
			_, err := svc.Plan(context.Background(), desired, reconcile.UpdateNone)
			if gitx.ErrorCode(err) != test.wantCode {
				t.Fatalf("error=%v code=%q commands=%#v", err, gitx.ErrorCode(err), commands)
			}
			if opened != 0 {
				t.Fatalf("opened secret stores=%d", opened)
			}
			if len(commands) == 0 || !strings.Contains(strings.Join(commands[0].Args, " "), "config --local") {
				t.Fatalf("local config was not the first Git check: %#v", commands)
			}
		})
	}
}

func TestService_PlanRejectsReadyDatabaseSourceDriftAgainstCatalog(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppCursor, true, "")
	prepareConvergedWorkspace(t, desired)
	if err := os.Remove(filepath.Join(desired.KitHome(), ".teamkit", "content.ready")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desired.KitHome(), ".teamkit", "database.ready"), []byte("develop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := catalog.LookupProject(desired.Project())
	if err != nil {
		t.Fatal(err)
	}
	var commands []gitx.Command
	svc := New(Options{GitRunner: gitRunnerFunc(func(_ context.Context, command gitx.Command) (gitx.Result, error) {
		commands = append(commands, command)
		joined := strings.Join(command.Args, " ")
		switch {
		case strings.Contains(joined, "config --local"):
			return gitx.Result{}, nil
		case strings.Contains(joined, "remote.origin.url"):
			return gitx.Result{Stdout: project.DatabaseRepository + "\n"}, nil
		case strings.Contains(joined, "symbolic-ref"):
			return gitx.Result{Stdout: "main\n"}, nil
		default:
			t.Fatalf("unexpected Git observation: %#v", command)
			return gitx.Result{}, nil
		}
	})})
	_, err = svc.Plan(context.Background(), desired, reconcile.UpdateNone)
	if gitx.ErrorCode(err) != "GIT_BRANCH_MISMATCH" {
		t.Fatalf("error=%v commands=%#v", err, commands)
	}
	if len(commands) < 3 || !slices.Contains(commands[2].Args, "symbolic-ref") {
		t.Fatalf("database source was not verified: %#v", commands)
	}
}

func TestService_PlanRejectsExistingHermesToolchainHeadDriftBeforeSecrets(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes"))
	toolchain, err := catalog.LookupToolchain(desired.Toolchain())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(desired.HermesHome(), "profiles", hermesProfileIdentity(desired), ".teamkit", "toolchain-source")
	if err := os.MkdirAll(filepath.Join(source, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	opened := 0
	var commands []gitx.Command
	svc := New(Options{
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			return hermes.DiscoveryResult{Installed: true, Home: desired.HermesHome(), Executable: filepath.Join(testutil.TempDir(t), "hermes"), Version: "0.20.1"}, nil
		},
		GitRunner: gitRunnerFunc(func(_ context.Context, command gitx.Command) (gitx.Result, error) {
			commands = append(commands, command)
			joined := strings.Join(command.Args, " ")
			switch {
			case strings.Contains(joined, "config --local"):
				return gitx.Result{}, nil
			case strings.Contains(joined, "remote.origin.url"):
				return gitx.Result{Stdout: toolchain.Origin + "\n"}, nil
			case strings.Contains(joined, "rev-parse"):
				return gitx.Result{Stdout: strings.Repeat("a", 40) + "\n"}, nil
			default:
				t.Fatalf("unexpected Git observation: %#v", command)
				return gitx.Result{}, nil
			}
		}),
		SecretStore: func(string) (credentials.SecretStore, error) {
			opened++
			return nil, errors.New("must not open")
		},
	})
	_, err = svc.Plan(context.Background(), desired, reconcile.UpdateNone)
	if gitx.ErrorCode(err) != "GIT_PIN_UNVERIFIED" {
		t.Fatalf("error=%v code=%q commands=%#v", err, gitx.ErrorCode(err), commands)
	}
	if opened != 0 {
		t.Fatalf("opened secret stores=%d", opened)
	}
}

func TestService_HermesRuntimeRejectsObservedDrift(t *testing.T) {
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true, KitHome: filepath.Join(testutil.TempDir(t), "kit"), HermesHome: filepath.Join(testutil.TempDir(t), "hermes"), HermesVersion: "0.20.2", Project: domain.ProjectAPA, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills})
	if err != nil {
		t.Fatal(err)
	}
	for name, result := range map[string]hermes.DiscoveryResult{
		"home":          {Installed: true, Home: filepath.Join(testutil.TempDir(t), "other"), Executable: "/hermes", Version: "0.20.2"},
		"version":       {Installed: true, Home: desired.HermesHome(), Executable: "/hermes", Version: "0.20.3"},
		"not installed": {Installed: false, Home: desired.HermesHome()},
	} {
		t.Run(name, func(t *testing.T) {
			svc := New(Options{ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) { return result, nil }})
			if _, err := svc.hermesExecutable(context.Background(), desired); err == nil {
				t.Fatal("runtime drift accepted")
			}
		})
	}
}

func TestService_HermesRuntimeAcceptsMatchingAndLegacyVersion(t *testing.T) {
	for _, version := range []string{"0.20.2", ""} {
		desired, err := domain.NewDesiredState(domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true, KitHome: filepath.Join(testutil.TempDir(t), "kit"), HermesHome: filepath.Join(testutil.TempDir(t), "hermes"), HermesVersion: version, Project: domain.ProjectAPA, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills})
		if err != nil {
			t.Fatal(err)
		}
		svc := New(Options{ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			return hermes.DiscoveryResult{Installed: true, Home: desired.HermesHome(), Executable: "/verified/hermes", Version: "0.20.2"}, nil
		}})
		got, err := svc.hermesExecutable(context.Background(), desired)
		if err != nil || got != "/verified/hermes" {
			t.Fatalf("got=%q err=%v", got, err)
		}
	}
}

func TestService_ManagedInstallSkipsExternalRuntimeResolver(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "kit"), domain.AppHermes, false, filepath.Join(testutil.TempDir(t), "hermes"))
	svc := New(Options{ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
		t.Fatal("resolver called")
		return hermes.DiscoveryResult{}, nil
	}})
	got, err := svc.hermesExecutable(context.Background(), desired)
	if err != nil || got != "" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestService_PublicPlanAndApplyRejectRuntimeBeforePrivateAdapters(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "kit"), domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes"))
	private := 0
	svc := New(Options{ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
		return hermes.DiscoveryResult{}, errors.New("HERMES_EXECUTABLE_UNVERIFIED: probe")
	}, SecretStore: func(string) (credentials.SecretStore, error) { private++; return nil, errors.New("must not open") }, Effects: func(EffectInputs) engine.Effects { private++; return failingEffects{} }})
	if _, err := svc.Plan(context.Background(), desired, reconcile.UpdateNone); err == nil {
		t.Fatal("Plan accepted runtime failure")
	}
	if _, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{}); err == nil {
		t.Fatal("Apply accepted runtime failure")
	}
	if private != 0 {
		t.Fatalf("private adapters=%d", private)
	}
}

func TestService_PublicRetryRejectsRuntimeBeforePrivateAdapters(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	contract, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	plan := reconcile.OperationPlan{ContractHash: contract, Actions: []reconcile.Action{{ID: "50-configure-application", Kind: reconcile.ActionConfigureApplication, Idempotent: true}}}
	persisted, _ := state.New(root)
	if err := persisted.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil {
		t.Fatal(err)
	}
	private := 0
	svc := New(Options{ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
		return hermes.DiscoveryResult{}, errors.New("HERMES_EXECUTABLE_UNVERIFIED: capability probe")
	}, SecretStore: func(string) (credentials.SecretStore, error) { private++; return nil, errors.New("must not open") }, Effects: func(EffectInputs) engine.Effects { private++; return failingEffects{} }})
	if err := svc.Retry(context.Background(), root); err == nil {
		t.Fatal("Retry accepted runtime failure")
	}
	if private != 0 {
		t.Fatalf("private adapters=%d", private)
	}
}

func TestService_ApplyUpsertsSecretsUsesEnvironmentAuthAndCheckpoints(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCursor, true, "")
	canary := "TEAMKIT_SECRET_CANARY_apply"
	store := &recordingSecretStore{}
	session := &recordingAskPass{}
	var commands []gitx.Command
	runner := gitRunnerFunc(func(_ context.Context, command gitx.Command) (gitx.Result, error) {
		commands = append(commands, command)
		if len(command.Args) >= 3 && command.Args[0] == "-C" && command.Args[2] == "init" {
			if err := os.MkdirAll(filepath.Join(command.Args[1], ".git"), 0o700); err != nil {
				return gitx.Result{}, err
			}
		}
		if len(command.Args) > 0 && command.Args[0] == "clone" {
			destination := command.Args[len(command.Args)-1]
			if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
				return gitx.Result{}, err
			}
		}
		return gitx.Result{}, nil
	})
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) { return filepath.Join(testutil.TempDir(t), "cursor"), nil },
		SecretStore:     func(string) (credentials.SecretStore, error) { return store, nil },
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			session.credentials = input
			session.credentials.AskPassPath = filepath.Join(testutil.TempDir(t), "askpass")
			return session, nil
		},
		GitRunner: runner,
		TempRoot:  testutil.TempDir(t),
	})
	secrets := map[string]string{
		credentials.GitLabUsername: "teamkit-user",
		credentials.GitLabToken:    canary,
		GitCAFile:                  filepath.Join(testutil.TempDir(t), "ca.pem"),
	}
	plan, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{Secrets: secrets})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !reflect.DeepEqual(store.saved, secrets) {
		t.Fatalf("saved=%#v want %#v", store.saved, secrets)
	}
	if !session.closed {
		t.Fatal("askpass session was not cleaned")
	}
	if session.credentials.Username != "teamkit-user" || session.credentials.Token != canary || session.credentials.CAFile != secrets[GitCAFile] {
		t.Fatalf("credentials=%#v", session.credentials)
	}
	if len(commands) == 0 {
		t.Fatal("no authenticated Git command was issued")
	}
	for _, command := range commands {
		if strings.Contains(strings.Join(command.Args, " "), canary) {
			t.Fatalf("secret in argv: %#v", command.Args)
		}
	}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := persisted.LoadReceipt(canary)
	if err != nil {
		t.Fatalf("LoadReceipt: %v", err)
	}
	if got := receipt.Checkpoints(); len(got) != len(plan.Actions) || got[len(got)-1].Status != reconcile.EffectSucceeded {
		t.Fatalf("checkpoints=%#v plan=%#v", got, plan)
	}
}

func TestService_ApplyRedactsFailureAndAlwaysClosesAskPass(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppCursor, true, "")
	canary := "TEAMKIT_SECRET_CANARY_failure"
	session := &recordingAskPass{}
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		SecretStore:     func(string) (credentials.SecretStore, error) { return &recordingSecretStore{}, nil },
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			session.credentials = input
			return session, nil
		},
		Effects: func(EffectInputs) engine.Effects {
			return failingEffects{canary: canary}
		},
		TempRoot: testutil.TempDir(t),
	})
	_, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{Secrets: map[string]string{
		credentials.GitLabUsername: "user", credentials.GitLabToken: canary,
	}})
	if err == nil || strings.Contains(err.Error(), canary) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error was not safely redacted: %v", err)
	}
	if !session.closed {
		t.Fatal("askpass session leaked on failure")
	}
}

func TestGitFailureDiagnosticRemainsBoundedAndRedactedInReceipt(t *testing.T) {
	root := testutil.TempDir(t)
	desired := testDesired(t, root, domain.AppCursor, true, "")
	auth := gitx.Credentials{
		Username: "receipt-user-canary",
		Token:    "receipt-user-canary-token-secret",
		CAFile:   "C:/private/receipt-ca-canary.pem",
	}
	runner := gitRunnerFunc(func(context.Context, gitx.Command) (gitx.Result, error) {
		return gitx.Result{Stderr: "fatal: TLS handshake failed for " + auth.Username + " using " + auth.Token + " and " + auth.CAFile + "\n" + strings.Repeat("detail ", 600)}, errors.New("exit status 128")
	})
	gitErr := gitx.NewRepository(runner).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", filepath.Join(root, "content"), auth)
	if gitErr == nil {
		t.Fatal("Git failure fixture unexpectedly succeeded")
	}
	plan := reconcile.OperationPlan{Actions: []reconcile.Action{{
		ID: "20-sync-content", Kind: reconcile.ActionSyncContent, Idempotent: true,
	}}}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	operation := engine.Engine{
		Effects: receiptFailureEffects{err: gitErr}, Store: persisted,
		Secrets: []string{auth.Username, auth.Token, auth.CAFile},
	}
	if err := operation.Prepare(desired, plan); err != nil {
		t.Fatal(err)
	}
	if err := operation.ExecutePrepared(context.Background(), desired, plan); err == nil {
		t.Fatal("ExecutePrepared unexpectedly succeeded")
	}
	receipt, err := persisted.LoadReceipt(auth.Username, auth.Token, auth.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	diagnostic := receipt.Checkpoints()[0].Diagnostic
	if !strings.Contains(diagnostic, "fatal: TLS handshake failed") || !strings.Contains(diagnostic, "[REDACTED]") || len(diagnostic) > 2300 {
		t.Fatalf("receipt lost bounded actionable Git detail: len=%d diagnostic=%q", len(diagnostic), diagnostic)
	}
	for _, canary := range []string{auth.Username, auth.Token, auth.CAFile} {
		if strings.Contains(diagnostic, canary) {
			t.Fatalf("receipt leaked %q: %q", canary, diagnostic)
		}
	}
}

func TestService_ApplyPersistsPublicDesiredStateBeforeFirstEffect(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCursor, true, "")
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		SecretStore:     func(string) (credentials.SecretStore, error) { return &recordingSecretStore{}, nil },
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			return &recordingAskPass{credentials: input}, nil
		},
		Effects:  func(EffectInputs) engine.Effects { return failingEffects{canary: "first-effect"} },
		TempRoot: testutil.TempDir(t),
	})
	if _, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{Secrets: map[string]string{
		credentials.GitLabUsername: "user", credentials.GitLabToken: "token",
	}}); err == nil {
		t.Fatal("Apply unexpectedly succeeded")
	}
	reloaded, err := svc.loadDesired(root)
	if err != nil {
		t.Fatalf("desired state was unavailable for retry: %v", err)
	}
	if reloaded.Project() != desired.Project() || reloaded.Application() != desired.Application() {
		t.Fatalf("reloaded desired=%#v want=%#v", reloaded, desired)
	}
}

func TestService_ApplyPersistsOperationBeforeOpeningMutationAdapters(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCursor, true, "")
	applicationHome := filepath.Join(testutil.TempDir(t), "cursor")
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) {
			return applicationHome, nil
		},
		SecretStore: func(string) (credentials.SecretStore, error) { return failingSecretStore{}, nil },
	})
	plan, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{Secrets: map[string]string{credentials.GitLabToken: "canary"}})
	if err == nil || !strings.Contains(err.Error(), "INJECTED_SECRET_SAVE_FAILURE") {
		t.Fatalf("Apply plan=%#v error=%v", plan, err)
	}
	persisted, stateErr := state.New(root)
	if stateErr != nil {
		t.Fatal(stateErr)
	}
	loadedPlan, receipt, stateErr := persisted.LoadOperation()
	if stateErr != nil {
		t.Fatalf("durable operation missing after pre-effect failure: %v", stateErr)
	}
	if !reflect.DeepEqual(loadedPlan, plan) || len(receipt.Checkpoints()) != len(plan.Actions) {
		t.Fatalf("operation plan=%#v receipt=%#v want=%#v", loadedPlan, receipt.Checkpoints(), plan)
	}
}

func TestService_HermesEffectInputsScopeThreePersonalKeysToNamedProfile(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes"))
	archive := writeTestCertificateArchive(t)
	var captured EffectInputs
	store := &recordingSecretStore{}
	svc := New(Options{
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			return hermes.DiscoveryResult{Installed: true, Home: desired.HermesHome(), Executable: filepath.Join(testutil.TempDir(t), "hermes"), Version: "0.20.1"}, nil
		},
		ApplicationHome: func(domain.DesiredState) (string, error) { return desired.HermesHome(), nil },
		SecretStore:     func(string) (credentials.SecretStore, error) { return store, nil },
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			return &recordingAskPass{credentials: input}, nil
		},
		HermesProfile: bootstrap.ProfilePortFuncs{
			CreateFunc: func(context.Context, string) error { return nil },
			DoctorFunc: func(context.Context, string) error { return nil },
		},
		VerifyDigest: func([]byte, string) bool { return true },
		Effects: func(input EffectInputs) engine.Effects {
			captured = input
			return failingEffects{canary: "expected-test-stop"}
		},
		TempRoot: testutil.TempDir(t),
	})
	_, _ = svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{CertificateArchive: archive, Secrets: map[string]string{
		credentials.GitLabUsername:   "git-user",
		credentials.GitLabToken:      "git-secret",
		credentials.PublicProviderAPIKey: "provider-secret",
		"TEAMKIT_PUBLIC_ISSUES_KEY":                 "jira-secret",
		"TEAMKIT_PUBLIC_WIKI_KEY":           "confluence-secret",
	}})
	if captured.Profile == nil || captured.ProfileSecrets == nil || captured.OfficeCLI == nil {
		t.Fatalf("Hermes profile dependencies missing: %#v", captured)
	}
	if !filepath.IsAbs(captured.HermesExecutable) || filepath.Base(captured.HermesExecutable) != "hermes" {
		t.Fatalf("Hermes executable = %q", captured.HermesExecutable)
	}
	want := map[string]string{
		credentials.PublicProviderAPIKey: "provider-secret", "TEAMKIT_PUBLIC_ISSUES_KEY": "jira-secret", "TEAMKIT_PUBLIC_WIKI_KEY": "confluence-secret",
	}
	if !reflect.DeepEqual(captured.ProfileEnvironment, want) {
		t.Fatalf("profile environment = %#v, want %#v", captured.ProfileEnvironment, want)
	}
}

func TestService_PlanFailsClosedWhenFirstPATHHermesIsIncompatible(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes-home"))
	opened := 0
	svc := New(Options{
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			return hermes.DiscoveryResult{}, hermes.ErrExecutableUnverified
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			opened++
			return nil, errors.New("must not open secrets")
		},
	})

	_, err := svc.Plan(context.Background(), desired, reconcile.UpdateNone)
	if !errors.Is(err, hermes.ErrExecutableUnverified) {
		t.Fatalf("Plan error = %v, want ErrExecutableUnverified", err)
	}
	if opened != 0 {
		t.Fatalf("secret stores opened = %d", opened)
	}
}

func TestService_ApplyPreflightsForeignWorkspaceBeforeSecretsOrAskPass(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "foreign")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("foreign\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, ".git"), filepath.Join(root, ".teamkit")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".teamkit", "content.ready"), []byte("content-wms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	desired := testDesired(t, root, domain.AppCursor, true, "")
	store := &recordingSecretStore{}
	askPassCalls := 0
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		SecretStore:     func(string) (credentials.SecretStore, error) { return store, nil },
		AskPass: func(string, gitx.Credentials) (AskPassSession, error) {
			askPassCalls++
			return &recordingAskPass{}, nil
		},
	})
	_, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{Secrets: map[string]string{credentials.GitLabToken: "TEAMKIT_SECRET_CANARY_foreign"}})
	if !errors.Is(err, bootstrap.ErrForeignWorkspace) {
		t.Fatalf("Apply error=%v", err)
	}
	if store.saved != nil || askPassCalls != 0 {
		t.Fatalf("foreign preflight mutated secrets=%#v askpass calls=%d", store.saved, askPassCalls)
	}
}

func TestService_ApplyRejectsMissingAlternativeApplicationBeforeAnyMutation(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCursor, false, "")
	storeCalls := 0
	svc := New(Options{SecretStore: func(string) (credentials.SecretStore, error) {
		storeCalls++
		return &recordingSecretStore{}, nil
	}})
	_, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{Secrets: map[string]string{
		credentials.GitLabToken: "TEAMKIT_MISSING_APP_CANARY",
	}})
	if !errors.Is(err, apps.ErrApplicationRequired) {
		t.Fatalf("Apply error=%v", err)
	}
	if storeCalls != 0 {
		t.Fatalf("secret store opened %d times", storeCalls)
	}
	if _, statErr := os.Lstat(root); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("workspace mutated: %v", statErr)
	}
}

func TestService_PlanRejectsClaimedAlternativeWhenExecutableIsAbsent(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppCursor, true, "")
	workspaceObservations := 0
	svc := New(Options{
		ApplicationLookPath: func(string) (string, error) { return "", exec.ErrNotFound },
		GitRunner: gitRunnerFunc(func(context.Context, gitx.Command) (gitx.Result, error) {
			workspaceObservations++
			return gitx.Result{}, errors.New("workspace observation must not run")
		}),
		ApplicationHome: func(domain.DesiredState) (string, error) {
			t.Fatal("application home must not be resolved")
			return "", nil
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			t.Fatal("secret store must not be opened")
			return nil, nil
		},
	})

	_, err := svc.Plan(context.Background(), desired, reconcile.UpdateNone)
	if apps.Code(err) != "AI_APP_REQUIRED" {
		t.Fatalf("Plan error=%v code=%q", err, apps.Code(err))
	}
	if workspaceObservations != 0 {
		t.Fatalf("workspace observed %d times", workspaceObservations)
	}
}

func TestService_ApplyRejectsClaimedAlternativeLookupFailureBeforeMutation(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCodex, true, "")
	stateStores, secretStores, effectFactories := 0, 0, 0
	svc := New(Options{
		ApplicationLookPath: func(string) (string, error) { return "", errors.New("LOOKUP_DENIED") },
		ApplicationHome: func(domain.DesiredState) (string, error) {
			t.Fatal("application home must not be resolved")
			return "", nil
		},
		StateStore: func(string) (engine.Store, error) {
			stateStores++
			return nil, errors.New("must not open state")
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			secretStores++
			return nil, errors.New("must not open secrets")
		},
		Effects: func(EffectInputs) engine.Effects {
			effectFactories++
			return failingEffects{canary: "TEAMKIT_APP_LOOKUP_CANARY"}
		},
		WritePrivate: func(string, []byte) error {
			t.Fatal("private writer must not run")
			return nil
		},
	})

	_, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{Secrets: map[string]string{
		credentials.GitLabToken: "TEAMKIT_APP_LOOKUP_CANARY",
	}})
	if apps.Code(err) != "AI_APP_REQUIRED" {
		t.Fatalf("Apply error=%v code=%q", err, apps.Code(err))
	}
	if strings.Contains(err.Error(), "TEAMKIT_APP_LOOKUP_CANARY") {
		t.Fatalf("Apply leaked secret canary: %v", err)
	}
	if stateStores != 0 || secretStores != 0 || effectFactories != 0 {
		t.Fatalf("mutation adapters opened: state=%d secrets=%d effects=%d", stateStores, secretStores, effectFactories)
	}
	if _, statErr := os.Lstat(root); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("workspace mutated: %v", statErr)
	}
}

func TestService_PlanAcceptsClaimedAlternativeResolvedByLookPath(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppCodex, true, "")
	svc := New(Options{
		ApplicationLookPath: func(name string) (string, error) {
			if name != "codex" {
				return "", exec.ErrNotFound
			}
			return filepath.Join("test-bin", name), nil
		},
	})

	plan, err := svc.Plan(context.Background(), desired, reconcile.UpdateNone)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Fatal("Plan returned no actions for an empty workspace")
	}
}

func TestService_RetryRechecksAlternativeAfterAtomicOperationBeforeCredentialAccess(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCodex, true, "")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	contractHash, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	plan := reconcile.OperationPlan{ContractHash: contractHash, Actions: []reconcile.Action{{ID: "50-configure-application", Kind: reconcile.ActionConfigureApplication, Idempotent: true}}}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil {
		t.Fatal(err)
	}
	secretStores := 0
	svc := New(Options{
		ApplicationLookPath: func(string) (string, error) { return "", exec.ErrNotFound },
		SecretStore: func(string) (credentials.SecretStore, error) {
			secretStores++
			return nil, errors.New("must not open secrets")
		},
	})

	err = svc.Retry(context.Background(), root)
	if apps.Code(err) != "AI_APP_REQUIRED" {
		t.Fatalf("Retry error=%v code=%q", err, apps.Code(err))
	}
	if secretStores != 0 {
		t.Fatalf("secret stores opened: %d", secretStores)
	}
}

func TestService_DefaultDownloaderHasFiniteTimeout(t *testing.T) {
	downloader, ok := New(Options{}).downloader(maxInstallerBytes, "HERMES_INSTALLER_TOO_LARGE").(httpDownloader)
	if !ok {
		t.Fatalf("default downloader type=%T", New(Options{}).downloader(maxInstallerBytes, "HERMES_INSTALLER_TOO_LARGE"))
	}
	if downloader.client == nil || downloader.client.Timeout <= 0 || downloader.client.Timeout > 2*time.Minute {
		t.Fatalf("default HTTP timeout=%v; want a finite timeout no greater than two minutes", downloader.client.Timeout)
	}
	if downloader.maxBytes != 4<<20 {
		t.Fatalf("default downloader maxBytes=%d; want %d", downloader.maxBytes, 4<<20)
	}
	if downloader.tooLargeError != "HERMES_INSTALLER_TOO_LARGE" {
		t.Fatalf("default downloader oversize error=%q; want HERMES_INSTALLER_TOO_LARGE", downloader.tooLargeError)
	}
	if downloader.client.Timeout != 30*time.Second {
		t.Fatalf("default HTTP timeout=%v; want 30s", downloader.client.Timeout)
	}
}

func TestPreflightOwnership_RejectsSymlinkedTeamKitDirectory(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	if err := os.WriteFile(filepath.Join(external, "owner"), []byte("wms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, ".teamkit")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	desired := testDesired(t, root, domain.AppCursor, true, "")
	if err := preflightOwnership(desired); !errors.Is(err, bootstrap.ErrForeignWorkspace) {
		t.Fatalf("preflight error=%v", err)
	}
}

func TestService_LoadDesiredRejectsSymlinkedKitHomeBeforeReadingEnv(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	link := filepath.Join(root, "kit")
	desired := testDesired(t, link, domain.AppCursor, true, "")
	writeDesiredAt(t, external, desired)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	reads := 0
	_, err := New(Options{ReadFile: func(string) ([]byte, error) {
		reads++
		return nil, errors.New("unsafe alias must be rejected before reading")
	}}).loadDesired(link)
	if !errors.Is(err, bootstrap.ErrForeignWorkspace) || !strings.Contains(err.Error(), "unsafe filesystem path") {
		t.Fatalf("loadDesired() error = %v, want ErrForeignWorkspace", err)
	}
	if reads != 0 {
		t.Fatalf("unsafe alias reached ReadFile: calls=%d", reads)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
}

func TestService_CertificateCacheRejectsSymlinkedParentBeforePrivateWriter(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "kit"), domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes"))
	applicationHome := testutil.TempDir(t)
	metadata := filepath.Join(applicationHome, ".teamkit")
	if err := os.Mkdir(metadata, 0o700); err != nil {
		t.Fatal(err)
	}
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(metadata, "cache")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	source := filepath.Join(testutil.TempDir(t), "certs.zip")
	if err := os.WriteFile(source, []byte("verified fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	svc := New(Options{
		VerifyDigest: func([]byte, string) bool { return true },
		WritePrivate: func(path string, data []byte) error {
			writes++
			return os.WriteFile(path, data, 0o600)
		},
	})

	_, err := svc.certificateFor(desired, source, applicationHome)
	if !errors.Is(err, bootstrap.ErrForeignProfile) {
		t.Fatalf("certificateFor() error = %v, want ErrForeignProfile", err)
	}
	if writes != 0 {
		t.Fatalf("private writer crossed symlink: calls=%d", writes)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
}

func TestService_ApplyRejectsKitOverlapBeforeFilesystemOrStoreMutation(t *testing.T) {
	for _, test := range []struct {
		name       string
		app        domain.AIApplication
		kitPath    func(string) string
		hermesPath func(string) string
		appPath    func(string) string
	}{
		{
			name: "same Hermes home", app: domain.AppHermes,
			kitPath:    func(root string) string { return filepath.Join(root, "same") },
			hermesPath: func(root string) string { return filepath.Join(root, "same") },
			appPath:    func(root string) string { return filepath.Join(root, "application") },
		},
		{
			name: "Hermes nested under kit", app: domain.AppHermes,
			kitPath:    func(root string) string { return filepath.Join(root, "kit") },
			hermesPath: func(root string) string { return filepath.Join(root, "kit", "hermes") },
			appPath:    func(root string) string { return filepath.Join(root, "application") },
		},
		{
			name: "kit nested under Hermes", app: domain.AppHermes,
			kitPath:    func(root string) string { return filepath.Join(root, "hermes", "kit") },
			hermesPath: func(root string) string { return filepath.Join(root, "hermes") },
			appPath:    func(root string) string { return filepath.Join(root, "application") },
		},
		{
			name: "same application home", app: domain.AppCursor,
			kitPath: func(root string) string { return filepath.Join(root, "same") },
			appPath: func(root string) string { return filepath.Join(root, "same") },
		},
		{
			name: "application home nested under kit", app: domain.AppCursor,
			kitPath: func(root string) string { return filepath.Join(root, "kit") },
			appPath: func(root string) string { return filepath.Join(root, "kit", "application") },
		},
		{
			name: "kit nested under application home", app: domain.AppCursor,
			kitPath: func(root string) string { return filepath.Join(root, "application", "kit") },
			appPath: func(root string) string { return filepath.Join(root, "application") },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := testutil.TempDir(t)
			kitHome := test.kitPath(base)
			hermesHome := ""
			if test.hermesPath != nil {
				hermesHome = test.hermesPath(base)
			}
			applicationHome := test.appPath(base)
			desired := testDesired(t, kitHome, test.app, true, hermesHome)
			storeCalls, stateCalls, effectCalls := 0, 0, 0
			svc := New(Options{
				ApplicationHome: func(domain.DesiredState) (string, error) { return applicationHome, nil },
				SecretStore: func(string) (credentials.SecretStore, error) {
					storeCalls++
					return &recordingSecretStore{}, nil
				},
				StateStore: func(string) (engine.Store, error) {
					stateCalls++
					return nil, errors.New("state store canary")
				},
				Effects: func(EffectInputs) engine.Effects {
					effectCalls++
					return captureEffects{}
				},
			})

			_, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{
				Secrets: map[string]string{credentials.GitLabToken: "TEAMKIT_OVERLAP_CANARY"},
			})
			if !errors.Is(err, ErrHomeOverlap) {
				t.Fatalf("Apply() error = %v, want ErrHomeOverlap", err)
			}
			if storeCalls != 0 || stateCalls != 0 || effectCalls != 0 {
				t.Fatalf("overlap opened adapters: secret=%d state=%d effects=%d", storeCalls, stateCalls, effectCalls)
			}
			entries, readErr := os.ReadDir(base)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("overlap mutated filesystem: %#v", entries)
			}
		})
	}
}

func TestService_PlanDefersApplicationHomeOverlapToMutationWithoutOpeningAdapters(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "kit")
	desired := testDesired(t, root, domain.AppCursor, true, "")
	adapterCalls, homeCalls := 0, 0
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) {
			homeCalls++
			return root, nil
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			adapterCalls++
			return &recordingSecretStore{}, nil
		},
		GitRunner: gitRunnerFunc(func(context.Context, gitx.Command) (gitx.Result, error) {
			adapterCalls++
			return gitx.Result{}, nil
		}),
	})

	if _, err := svc.Plan(context.Background(), desired, reconcile.UpdateNone); err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if adapterCalls != 0 || homeCalls != 0 {
		t.Fatalf("Plan opened adapters=%d or resolved application home=%d", adapterCalls, homeCalls)
	}
	if _, statErr := os.Lstat(root); !os.IsNotExist(statErr) {
		t.Fatalf("Plan mutated KIT home: %v", statErr)
	}
}

func TestService_ApplyRejectsSymlinkedApplicationHomeBeforeFilesystemMutation(t *testing.T) {
	base := testutil.TempDir(t)
	kitHome := filepath.Join(base, "kit")
	external := testutil.TempDir(t)
	applicationHome := filepath.Join(base, "application")
	if err := os.Symlink(external, applicationHome); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	desired := testDesired(t, kitHome, domain.AppCursor, true, "")
	storeCalls := 0
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) { return applicationHome, nil },
		SecretStore: func(string) (credentials.SecretStore, error) {
			storeCalls++
			return &recordingSecretStore{}, nil
		},
	})

	_, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{})
	if !errors.Is(err, bootstrap.ErrForeignProfile) {
		t.Fatalf("Apply() error = %v, want ErrForeignProfile", err)
	}
	if storeCalls != 0 {
		t.Fatalf("secret store opened through application symlink: %d", storeCalls)
	}
	if _, statErr := os.Lstat(kitHome); !os.IsNotExist(statErr) {
		t.Fatalf("Apply mutated KIT home before rejection: %v", statErr)
	}
}

func TestService_RetryReloadsDesiredAndOnlyIncompleteActionSecrets(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	desired := testDesired(t, root, domain.AppHermes, true, hermesHome)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	contractHash, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	plan := reconcile.OperationPlan{ContractHash: contractHash, Actions: []reconcile.Action{{ID: "50-configure-application", Kind: reconcile.ActionConfigureApplication, Idempotent: true}}}
	persisted, _ := state.New(root)
	if err := persisted.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := &recordingSecretStore{loaded: map[string]string{
		credentials.PublicProviderAPIKey: "TEAMKIT$SECRET$CANARY$retry", "TEAMKIT_PUBLIC_ISSUES_KEY": "jira-retry-secret", "TEAMKIT_PUBLIC_WIKI_KEY": "confluence-retry-secret",
	}}
	var applied domain.DesiredState
	var captured EffectInputs
	resolvedBeforeSecrets := false
	bundle := filepath.Join(testutil.TempDir(t), "ca-bundle.pem")
	if err := os.WriteFile(bundle, []byte("test CA"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := New(Options{
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			resolvedBeforeSecrets = true
			return hermes.DiscoveryResult{Installed: true, Home: desired.HermesHome(), Executable: filepath.Join(testutil.TempDir(t), "hermes"), Version: "0.20.1"}, nil
		},
		ManagedCertificateBundle: func(string, string) (string, bool, error) {
			return bundle, true, nil
		},
		ApplicationHome: func(got domain.DesiredState) (string, error) {
			if got.Project() != desired.Project() {
				t.Fatalf("desired state was not reloaded: %#v", got)
			}
			return hermesHome, nil
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			if !resolvedBeforeSecrets {
				t.Fatal("Hermes executable was not verified before opening secrets")
			}
			return store, nil
		},
		GitRunner: gitRunnerFunc(func(context.Context, gitx.Command) (gitx.Result, error) {
			return gitx.Result{}, errors.New("retry must use the persisted plan before worktree observation")
		}),
		Effects: func(input EffectInputs) engine.Effects { captured = input; return captureEffects{desired: &applied} },
	})
	if err := svc.Retry(context.Background(), root); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if !reflect.DeepEqual(store.loadKeys, []string{credentials.PublicProviderAPIKey, "TEAMKIT_PUBLIC_ISSUES_KEY", "TEAMKIT_PUBLIC_WIKI_KEY"}) {
		t.Fatalf("loaded keys=%#v", store.loadKeys)
	}
	wantEnvironment := map[string]string{
		credentials.PublicProviderAPIKey: "TEAMKIT$SECRET$CANARY$retry", "TEAMKIT_PUBLIC_ISSUES_KEY": "jira-retry-secret", "TEAMKIT_PUBLIC_WIKI_KEY": "confluence-retry-secret",
	}
	if !reflect.DeepEqual(captured.ProfileEnvironment, wantEnvironment) {
		t.Fatalf("retry profile environment=%#v, want %#v", captured.ProfileEnvironment, wantEnvironment)
	}
	if applied.Project() != desired.Project() {
		t.Fatalf("applied desired=%#v", applied)
	}
}

func TestServiceRetry_LegacyDACLFailureRepairsOwnedProfileAndCompletesAction50(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	desired := testDesired(t, root, domain.AppHermes, true, hermesHome)
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)

	identity := hermesProfileIdentity(desired)
	profileRoot := filepath.Join(hermesHome, "profiles", identity)
	if err := os.MkdirAll(filepath.Join(profileRoot, "skills", "learned-user"), 0o700); err != nil {
		t.Fatal(err)
	}
	learnedPath := filepath.Join(profileRoot, "skills", "learned-user", "SKILL.md")
	if err := os.WriteFile(learnedPath, []byte("learned-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(profileRoot, ".no-bundled-skills")
	markerBody := "This profile opted out of bundled-skill seeding (`hermes profile create --no-skills`).\nDelete this file to re-enable sync on the next `hermes update`.\n"
	if err := os.WriteFile(marker, []byte(markerBody), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := filepath.Join(profileRoot, ".env")
	if err := os.WriteFile(environment, []byte(credentials.PublicProviderAPIKey+"=old-value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(environment, 0o644); err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(hermesHome, ".teamkit", "profiles", identity+".owner")
	if err := workspace.WriteFileAtomic(ownerPath, []byte(identity+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	pin, err := catalog.LookupToolchain(desired.Toolchain())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(testutil.TempDir(t), "toolchain")
	if err := os.MkdirAll(filepath.Join(source, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".git", "HEAD"), []byte(pin.Commit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(source, ".claude", "skills", "fixture")
	if err := os.MkdirAll(skill, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := hermes.MaterializeToolchain(source, profileRoot, pin); err != nil {
		t.Fatal(err)
	}

	executable := filepath.Join(testutil.TempDir(t), "hermes")
	if err := os.WriteFile(executable, []byte("test hermes executable\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeContract := hermes.RuntimeContract{
		Info:         hermes.RuntimeInfo{Executable: executable, InstallDir: filepath.Dir(executable), Version: "0.20.1"},
		Identity:     hermes.RuntimeIdentity{InstallRootKey: "runtime-root", ExecutableKey: "runtime-executable"},
		ConfigSchema: 37, BundledSkills: []string{"hermes-default"}, BundledInventorySHA256: strings.Repeat("a", 64),
	}
	archive := writeTestCertificateArchive(t)
	archiveData, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	archiveHash := sha256.Sum256(archiveData)
	archiveDigest := hex.EncodeToString(archiveHash[:])
	releaseDir := testutil.TempDir(t)
	if err := os.WriteFile(filepath.Join(releaseDir, "certs.zip"), archiveData, 0o600); err != nil {
		t.Fatal(err)
	}

	const operationContract = "v0.1.1-dacl-retry-contract"
	plan := reconcile.OperationPlan{ContractHash: operationContract, Actions: []reconcile.Action{{ID: "50-configure-application", Kind: reconcile.ActionConfigureApplication, Idempotent: true}}}
	receipt := reconcile.NewReceipt(desired, plan)
	if err := receipt.Record("50-configure-application", reconcile.EffectFailed, "SECRET_FILE_PERMISSIONS_UNSAFE"); err != nil {
		t.Fatal(err)
	}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, receipt); err != nil {
		t.Fatal(err)
	}

	optInCalls := 0
	appStore := &recordingSecretStore{loaded: map[string]string{
		credentials.PublicProviderAPIKey: "new-provider-value", credentials.JiraToken: "new-jira-value", credentials.ConfluenceToken: "new-confluence-value",
	}}
	svc := New(Options{
		OperationContract: func(domain.DesiredState) (string, error) { return operationContract, nil },
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			return hermes.DiscoveryResult{Installed: true, Home: hermesHome, Executable: executable, Version: "0.20.1", Contract: runtimeContract}, nil
		},
		RuntimeProbe:    func(context.Context, string) (hermes.RuntimeContract, error) { return runtimeContract, nil },
		ApplicationHome: func(domain.DesiredState) (string, error) { return hermesHome, nil },
		SecretStore: func(home string) (credentials.SecretStore, error) {
			if filepath.Clean(home) == filepath.Clean(profileRoot) {
				return secrets.NewStore(home)
			}
			return appStore, nil
		},
		HermesProfile: bootstrap.ProfilePortFuncs{
			OptInBundledSkillsFunc: func(context.Context, string) error {
				optInCalls++
				if err := os.Remove(marker); err != nil {
					return err
				}
				return os.MkdirAll(filepath.Join(profileRoot, "skills", "hermes-default"), 0o700)
			},
			DoctorFunc: func(context.Context, string) error { return nil },
		},
		VerifyDigest: func(data []byte, _ string) bool {
			got := sha256.Sum256(data)
			return hex.EncodeToString(got[:]) == archiveDigest
		},
		WritePrivate: func(path string, data []byte) error { return workspace.WriteFileAtomic(path, data, 0o600) },
		ReleaseDir:   releaseDir,
		Effects: func(input EffectInputs) engine.Effects {
			officeCLI, ok := input.OfficeCLI.(*officeCLIProvisioner)
			if !ok {
				t.Fatalf("OfficeCLI input = %T, want *officeCLIProvisioner", input.OfficeCLI)
			}
			payload := []byte("verified OfficeCLI retry fixture")
			digest := sha256.Sum256(payload)
			officeCLI.asset.Size = int64(len(payload))
			officeCLI.asset.SHA256 = hex.EncodeToString(digest[:])
			officeCLI.verify = func(data []byte, expected string) bool {
				got := sha256.Sum256(data)
				return hex.EncodeToString(got[:]) == expected
			}
			if err := os.MkdirAll(filepath.Dir(officeCLI.Path()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(officeCLI.Path(), payload, 0o700); err != nil {
				t.Fatal(err)
			}
			writeOfficeCLIConfig(t, officeCLI.configPath, false, false)
			officeCLI.run = platform.ProcessRunnerFunc(func(context.Context, string, []string) error { return nil })
			officeCLI.capture = func(context.Context, string, []string) ([]byte, []byte, error) {
				return []byte("false\n"), nil, nil
			}
			return &bootstrap.Effects{
				Git: input.Git, Installer: input.Installer, InstallerPath: input.InstallerPath,
				CertificateArchive: input.CertificateArchive, CertificateSHA256: archiveDigest, Secrets: input.Secrets,
				ProfileSecrets: input.ProfileSecrets, ProfileEnvironment: input.ProfileEnvironment, Profile: input.Profile,
				OfficeCLI:        input.OfficeCLI,
				HermesExecutable: input.HermesExecutable,
				RuntimeContract:  input.RuntimeContract, RuntimeProbe: input.RuntimeProbe,
			}
		},
	})
	if err := svc.Retry(context.Background(), root); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if optInCalls != 1 {
		t.Fatalf("opt-in calls=%d want=1", optInCalls)
	}
	if err := privatefile.Validate(environment); err != nil {
		t.Fatalf("profile environment remains unsafe: %v", err)
	}
	learned, err := os.ReadFile(learnedPath)
	if err != nil || string(learned) != "learned-sentinel" {
		t.Fatalf("Learned skill changed: %q, %v", learned, err)
	}
	config, err := os.ReadFile(filepath.Join(profileRoot, "config.yaml"))
	if err != nil || !bytes.Contains(config, []byte("_config_version: 37")) || !bytes.Contains(config, []byte("v8std:")) {
		t.Fatalf("config is incomplete: %q, %v", config, err)
	}
}

func TestService_RetryRecoversPendingFirstRunBeforeOwnerAndPublicEnv(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	applicationHome := filepath.Join(testutil.TempDir(t), "codex")
	desired := testDesired(t, root, domain.AppCodex, true, "")
	contractHash, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	plan := reconcile.OperationPlan{ContractHash: contractHash, Actions: []reconcile.Action{{
		ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true,
	}}}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".env")); !os.IsNotExist(err) {
		t.Fatalf("fixture unexpectedly has public env: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".teamkit", "owner")); !os.IsNotExist(err) {
		t.Fatalf("fixture unexpectedly has owner: %v", err)
	}

	var applied domain.DesiredState
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) { return applicationHome, nil },
		SecretStore:     func(string) (credentials.SecretStore, error) { return &recordingSecretStore{}, nil },
		Effects:         func(EffectInputs) engine.Effects { return captureEffects{desired: &applied} },
	})
	if err := svc.Retry(context.Background(), root); err != nil {
		t.Fatalf("Retry pending first run: %v", err)
	}
	if applied.Project() != desired.Project() {
		t.Fatalf("applied desired=%#v, want project %q", applied, desired.Project())
	}
	if _, err := os.Stat(filepath.Join(root, ".env")); err != nil {
		t.Fatalf("public env not recovered: %v", err)
	}
	owner, err := os.ReadFile(filepath.Join(root, ".teamkit", "owner"))
	if err != nil || strings.TrimSpace(string(owner)) != string(desired.Project()) {
		t.Fatalf("owner not recovered: %q, %v", owner, err)
	}
}

func TestService_StatusReportsExactIncompleteUpdateInsteadOfReadyObservedMarkers(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCodex, true, "")
	prepareConvergedWorkspace(t, desired)
	writeDesired(t, desired)
	contractHash, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	operationPlan := reconcile.OperationPlan{ContractHash: contractHash, Actions: []reconcile.Action{
		{ID: "20-sync-content", Kind: reconcile.ActionSyncContent, Idempotent: true},
		{ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true},
	}}
	receipt := reconcile.NewReceipt(desired, operationPlan)
	if err := receipt.Record("20-sync-content", reconcile.EffectSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	if err := receipt.Record("90-verify-state", reconcile.EffectFailed, "verification interrupted"); err != nil {
		t.Fatal(err)
	}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(operationPlan, receipt); err != nil {
		t.Fatal(err)
	}

	svc := New(Options{
		ApplicationLookPath: func(string) (string, error) { return filepath.Join(testutil.TempDir(t), "codex"), nil },
		GitRunner:           readySourceObservationRunner(t, desired, ""),
	})
	status, plan, err := svc.Status(context.Background(), root)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status != reconcile.StatusNeedsApply {
		t.Fatalf("Status = %q, want %q", status, reconcile.StatusNeedsApply)
	}
	want := reconcile.OperationPlan{ContractHash: contractHash, Actions: []reconcile.Action{
		{ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true},
	}}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("Status plan = %#v, want exact incomplete operation %#v", plan, want)
	}
}

func TestService_StatusReportsPendingFirstRunBeforeOwnerAndPublicEnv(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCodex, true, "")
	contractHash, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	operationPlan := reconcile.OperationPlan{ContractHash: contractHash, Actions: []reconcile.Action{
		{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true},
		{ID: "20-sync-content", Kind: reconcile.ActionSyncContent, Idempotent: true},
	}}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(operationPlan, reconcile.NewReceipt(desired, operationPlan)); err != nil {
		t.Fatal(err)
	}

	svc := New(Options{
		ApplicationLookPath: func(string) (string, error) { return filepath.Join(testutil.TempDir(t), "codex"), nil },
		GitRunner: gitRunnerFunc(func(context.Context, gitx.Command) (gitx.Result, error) {
			t.Fatal("pending status must not observe a partially owned worktree")
			return gitx.Result{}, nil
		}),
	})
	status, plan, err := svc.Status(context.Background(), root)
	if err != nil {
		t.Fatalf("Status pending first run: %v", err)
	}
	if status != reconcile.StatusNeedsApply || !reflect.DeepEqual(plan, operationPlan) {
		t.Fatalf("Status = %q, plan %#v; want needs_apply and %#v", status, plan, operationPlan)
	}
	if _, err := os.Lstat(filepath.Join(root, ".teamkit", "owner")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only status claimed ownership: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".env")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only status published .env: %v", err)
	}
}

func TestService_StatusRejectsChangedOperationContractBeforeObservation(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCodex, true, "")
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	operationPlan := reconcile.OperationPlan{ContractHash: "old-contract", Actions: []reconcile.Action{{
		ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true,
	}}}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(operationPlan, reconcile.NewReceipt(desired, operationPlan)); err != nil {
		t.Fatal(err)
	}

	svc := New(Options{
		OperationContract: func(domain.DesiredState) (string, error) { return "new-contract", nil },
		GitRunner: gitRunnerFunc(func(context.Context, gitx.Command) (gitx.Result, error) {
			t.Fatal("contract-mismatched status must not observe repositories")
			return gitx.Result{}, nil
		}),
	})
	if _, _, err := svc.Status(context.Background(), root); err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
		t.Fatalf("Status error = %v, want OPERATION_CONTRACT_MISMATCH", err)
	}
}

func TestService_StatusRejectsReceiptForDifferentDesiredStateBeforeObservation(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCodex, true, "")
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	other, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppCodex, AppInstalled: true, KitHome: root,
		Project: domain.ProjectWMS, Role: domain.RoleAnalyst, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	contractHash, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	operationPlan := reconcile.OperationPlan{ContractHash: contractHash, Actions: []reconcile.Action{{
		ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true,
	}}}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(operationPlan, reconcile.NewReceipt(other, operationPlan)); err != nil {
		t.Fatal(err)
	}

	svc := New(Options{GitRunner: gitRunnerFunc(func(context.Context, gitx.Command) (gitx.Result, error) {
		t.Fatal("desired-mismatched status must not observe repositories")
		return gitx.Result{}, nil
	})})
	if _, _, err := svc.Status(context.Background(), root); err == nil || err.Error() != "RECEIPT_DESIRED_MISMATCH" {
		t.Fatalf("Status error = %v, want RECEIPT_DESIRED_MISMATCH", err)
	}
}

func TestService_RetryRejectsEmptyConfigureCredentialBeforeAskPassOrEffects(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	desired := testDesired(t, root, domain.AppHermes, true, hermesHome)
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)

	contractHash, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	plan := reconcile.OperationPlan{ContractHash: contractHash, Actions: []reconcile.Action{{
		ID: "50-configure-application", Kind: reconcile.ActionConfigureApplication, Idempotent: true,
	}}}
	receipt := reconcile.NewReceipt(desired, plan)
	if err := receipt.Record(plan.Actions[0].ID, reconcile.EffectFailed, "previous configure failure"); err != nil {
		t.Fatal(err)
	}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, receipt); err != nil {
		t.Fatal(err)
	}

	store := &recordingSecretStore{loaded: map[string]string{
		credentials.PublicProviderAPIKey: " \t ",
	}}
	askPassCalls, effectCalls := 0, 0
	svc := New(Options{
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			return hermes.DiscoveryResult{Installed: true, Home: desired.HermesHome(), Executable: filepath.Join(testutil.TempDir(t), "hermes"), Version: "0.20.1"}, nil
		},
		ApplicationHome: func(domain.DesiredState) (string, error) { return hermesHome, nil },
		SecretStore:     func(string) (credentials.SecretStore, error) { return store, nil },
		AskPass: func(string, gitx.Credentials) (AskPassSession, error) {
			askPassCalls++
			return &recordingAskPass{}, nil
		},
		Effects: func(EffectInputs) engine.Effects {
			effectCalls++
			return failingEffects{canary: "TEAMKIT_MISSING_PROVIDER_CANARY"}
		},
	})

	err = svc.Retry(context.Background(), root)
	want := "CREDENTIALS_REQUIRED: " + credentials.PublicProviderAPIKey + ",TEAMKIT_PUBLIC_ISSUES_KEY,TEAMKIT_PUBLIC_WIKI_KEY"
	if err == nil || err.Error() != want {
		t.Fatalf("Retry() error = %v, want %q", err, want)
	}
	if askPassCalls != 0 || effectCalls != 0 {
		t.Fatalf("missing credential opened mutation adapters: askpass=%d effects=%d", askPassCalls, effectCalls)
	}
}

func TestService_RetryCompleteReceiptDoesNotOpenMutationAdapters(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	contractHash, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	plan := reconcile.OperationPlan{ContractHash: contractHash, Actions: []reconcile.Action{{ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true}}}
	receipt := reconcile.NewReceipt(desired, plan)
	if err := receipt.Record(plan.Actions[0].ID, reconcile.EffectSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, receipt); err != nil {
		t.Fatal(err)
	}
	adapterCalls := 0
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) {
			adapterCalls++
			return "", errors.New("must not resolve application home")
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			adapterCalls++
			return nil, errors.New("must not open secrets")
		},
		AskPass: func(string, gitx.Credentials) (AskPassSession, error) {
			adapterCalls++
			return nil, errors.New("must not create askpass")
		},
		Effects: func(EffectInputs) engine.Effects { adapterCalls++; return failingEffects{} },
		Downloader: DownloadFunc(func(context.Context, string) ([]byte, error) {
			adapterCalls++
			return nil, errors.New("must not download")
		}),
	})
	if err := svc.Retry(context.Background(), root); err != nil {
		t.Fatalf("Retry complete receipt: %v", err)
	}
	if adapterCalls != 0 {
		t.Fatalf("complete retry opened %d mutation adapters", adapterCalls)
	}
}

func TestService_RetryRejectsConcurrentWorkspaceMutation(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCodex, true, "")
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	contractHash, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	plan := reconcile.OperationPlan{ContractHash: contractHash, Actions: []reconcile.Action{{
		ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true,
	}}}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil {
		t.Fatal(err)
	}
	held, err := operationlock.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	var applied domain.DesiredState
	svc := New(Options{
		ApplicationLookPath: func(string) (string, error) { return filepath.Join(testutil.TempDir(t), "codex"), nil },
		ApplicationHome:     func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		SecretStore:         func(string) (credentials.SecretStore, error) { return &recordingSecretStore{}, nil },
		Effects:             func(EffectInputs) engine.Effects { return captureEffects{desired: &applied} },
	})
	err = svc.Retry(context.Background(), root)
	if !errors.Is(err, operationlock.ErrOperationInProgress) {
		t.Fatalf("Retry error = %v, want ErrOperationInProgress", err)
	}
	if applied.Project() != "" {
		t.Fatalf("concurrent Retry reached effects with desired %#v", applied)
	}
}

func TestService_UpdateReloadsDesiredAndLoadsGitKeysOnly(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCursor, true, "")
	prepareConvergedWorkspace(t, desired)
	writeDesired(t, desired)
	store := &recordingSecretStore{loaded: map[string]string{
		credentials.GitLabUsername: "user", credentials.GitLabToken: "token", GitCAFile: "ca.pem",
	}}
	var applied domain.DesiredState
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		SecretStore:     func(string) (credentials.SecretStore, error) { return store, nil },
		GitRunner:       readySourceObservationRunner(t, desired, ""),
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			return &recordingAskPass{credentials: input}, nil
		},
		Effects:  func(EffectInputs) engine.Effects { return captureEffects{desired: &applied, nonempty: true} },
		TempRoot: testutil.TempDir(t),
	})
	if _, err := svc.Update(context.Background(), root, reconcile.UpdateContent); err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := []string{credentials.GitLabUsername, credentials.GitLabToken, GitCAFile}
	if !reflect.DeepEqual(store.loadKeys, want) {
		t.Fatalf("loaded keys=%#v want %#v", store.loadKeys, want)
	}
	if applied.Project() != desired.Project() {
		t.Fatalf("applied desired=%#v", applied)
	}
}

func TestService_UpdateVerifiedRejectsEverySelectedIdentityChangeBeforeAdapters(t *testing.T) {
	tests := []struct {
		name   string
		hermes bool
		mutate func(domain.DesiredStateInput) domain.DesiredStateInput
	}{
		{"os", false, func(input domain.DesiredStateInput) domain.DesiredStateInput {
			input.OS = domain.OSWindows
			return input
		}},
		{"application", false, func(input domain.DesiredStateInput) domain.DesiredStateInput {
			input.Application = domain.AppCodex
			return input
		}},
		{"installed", false, func(input domain.DesiredStateInput) domain.DesiredStateInput {
			input.AppInstalled = false
			return input
		}},
		{"kit home", false, func(input domain.DesiredStateInput) domain.DesiredStateInput {
			input.KitHome = filepath.Join(testutil.TempDir(t), "other")
			return input
		}},
		{"Hermes home", true, func(input domain.DesiredStateInput) domain.DesiredStateInput {
			input.HermesHome = filepath.Join(testutil.TempDir(t), "other-hermes")
			return input
		}},
		{"Hermes version", true, func(input domain.DesiredStateInput) domain.DesiredStateInput {
			input.HermesVersion = "0.20.3"
			return input
		}},
		{"project", false, func(input domain.DesiredStateInput) domain.DesiredStateInput {
			input.Project = domain.ProjectAPA
			return input
		}},
		{"role", false, func(input domain.DesiredStateInput) domain.DesiredStateInput {
			input.Role = domain.RoleAnalyst
			return input
		}},
		{"toolchain", false, func(input domain.DesiredStateInput) domain.DesiredStateInput {
			input.Toolchain = domain.ToolchainAIRules1C
			return input
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			expectedInput := domain.DesiredStateInput{
				OS: domain.OSLinux, Application: domain.AppCursor, AppInstalled: true, KitHome: root,
				Project: domain.ProjectWMS, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
			}
			if test.hermes {
				expectedInput.Application = domain.AppHermes
				expectedInput.HermesHome = filepath.Join(testutil.TempDir(t), "hermes")
				expectedInput.HermesVersion = "0.20.2"
			}
			expected := mustDesiredState(t, expectedInput)
			actual := mustDesiredState(t, test.mutate(expectedInput))
			prepareUpdateIdentityFixture(t, expected)
			writeDesiredAt(t, root, actual)

			adapterCalls := 0
			svc := New(Options{
				ApplicationLookPath: func(string) (string, error) { adapterCalls++; return "", nil },
				ApplicationHome:     func(domain.DesiredState) (string, error) { adapterCalls++; return "", nil },
				SecretStore:         func(string) (credentials.SecretStore, error) { adapterCalls++; return &recordingSecretStore{}, nil },
				StateStore:          func(string) (engine.Store, error) { adapterCalls++; return nil, nil },
				AskPass: func(string, gitx.Credentials) (AskPassSession, error) {
					adapterCalls++
					return &recordingAskPass{}, nil
				},
				GitRunner: gitRunnerFunc(func(context.Context, gitx.Command) (gitx.Result, error) { adapterCalls++; return gitx.Result{}, nil }),
				Effects:   func(EffectInputs) engine.Effects { adapterCalls++; return captureEffects{} },
				ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
					adapterCalls++
					return hermes.DiscoveryResult{}, nil
				},
			})

			_, err := svc.UpdateVerified(context.Background(), environment.VerifiedEnvironment{Home: root, Desired: expected}, reconcile.UpdateContent)
			if !errors.Is(err, workspace.ErrChanged) {
				t.Fatalf("UpdateVerified() error = %v, want WORKSPACE_CHANGED", err)
			}
			if adapterCalls != 0 {
				t.Fatalf("identity mismatch opened adapters/process/effects: %d", adapterCalls)
			}
		})
	}
}

func TestService_UpdateBindsExpectedStateAcrossMutationLock(t *testing.T) {
	for _, verified := range []bool{false, true} {
		name := "direct"
		if verified {
			name = "verified"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			expected := testDesired(t, root, domain.AppCursor, true, "")
			changedInput := desiredStateInput(expected)
			changedInput.Project = domain.ProjectAPA
			changed := mustDesiredState(t, changedInput)
			prepareUpdateIdentityFixture(t, expected)
			writeDesired(t, expected)
			expectedBytes := desiredStateBytes(t, expected)
			changedBytes := desiredStateBytes(t, changed)
			reads, adapterCalls := 0, 0
			svc := New(Options{
				ReadFile: func(string) ([]byte, error) {
					reads++
					if reads == 1 {
						return append([]byte(nil), expectedBytes...), nil
					}
					return append([]byte(nil), changedBytes...), nil
				},
				ApplicationLookPath: func(string) (string, error) { adapterCalls++; return "", nil },
				ApplicationHome:     func(domain.DesiredState) (string, error) { adapterCalls++; return "", nil },
				SecretStore:         func(string) (credentials.SecretStore, error) { adapterCalls++; return &recordingSecretStore{}, nil },
				StateStore:          func(string) (engine.Store, error) { adapterCalls++; return nil, nil },
				GitRunner:           gitRunnerFunc(func(context.Context, gitx.Command) (gitx.Result, error) { adapterCalls++; return gitx.Result{}, nil }),
				Effects:             func(EffectInputs) engine.Effects { adapterCalls++; return captureEffects{} },
			})
			var err error
			if verified {
				_, err = svc.UpdateVerified(context.Background(), environment.VerifiedEnvironment{Home: root, Desired: expected}, reconcile.UpdateContent)
			} else {
				_, err = svc.Update(context.Background(), root, reconcile.UpdateContent)
			}
			if !errors.Is(err, workspace.ErrChanged) {
				t.Fatalf("Update() error = %v, want WORKSPACE_CHANGED", err)
			}
			if reads != 2 || adapterCalls != 0 {
				t.Fatalf("reads=%d adapters/process/effects=%d", reads, adapterCalls)
			}
		})
	}
}

func TestService_UpdateVerifiedMapsEveryPreLockPublicLoadFailureToWorkspaceChanged(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		err  error
	}{
		{"malformed UTF-8", []byte("KIT_ALL_TEAM_HOME=C:\xff\xfe\n"), nil},
		{"oversize", bytes.Repeat([]byte("A"), maxDesiredStateBytes+1), nil},
		{"deleted", nil, os.ErrNotExist},
		{"unsafe", nil, pathsafe.ErrUnsafe},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			expected := testDesired(t, root, domain.AppCursor, true, "")
			prepareUpdateIdentityFixture(t, expected)
			adapterCalls := 0
			svc := New(updateFailureOptions(func(string) ([]byte, error) {
				return append([]byte(nil), test.data...), test.err
			}, &adapterCalls))

			_, err := svc.UpdateVerified(context.Background(), environment.VerifiedEnvironment{Home: root, Desired: expected}, reconcile.UpdateContent)
			if !errors.Is(err, workspace.ErrChanged) {
				t.Fatalf("UpdateVerified() error = %v, want WORKSPACE_CHANGED", err)
			}
			if adapterCalls != 0 {
				t.Fatalf("public-state failure opened adapters/process/effects: %d", adapterCalls)
			}
		})
	}
}

func TestService_UpdateVerifiedMapsUnderLockPublicLoadFailureToWorkspaceChanged(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	expected := testDesired(t, root, domain.AppCursor, true, "")
	prepareUpdateIdentityFixture(t, expected)
	expectedBytes := desiredStateBytes(t, expected)
	reads, adapterCalls := 0, 0
	svc := New(updateFailureOptions(func(string) ([]byte, error) {
		reads++
		if reads == 1 {
			return append([]byte(nil), expectedBytes...), nil
		}
		return []byte("KIT_ALL_TEAM_HOME=C:\xff\xfe\n"), nil
	}, &adapterCalls))

	_, err := svc.UpdateVerified(context.Background(), environment.VerifiedEnvironment{Home: root, Desired: expected}, reconcile.UpdateContent)
	if !errors.Is(err, workspace.ErrChanged) {
		t.Fatalf("UpdateVerified() error = %v, want WORKSPACE_CHANGED", err)
	}
	if reads != 2 || adapterCalls != 0 {
		t.Fatalf("reads=%d adapters/process/effects=%d", reads, adapterCalls)
	}
}

func TestService_UpdateDirectInitialInvalidPreservesOriginalFailure(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	adapterCalls := 0
	svc := New(updateFailureOptions(func(string) ([]byte, error) {
		return []byte("KIT_ALL_TEAM_HOME=C:\xff\xfe\n"), nil
	}, &adapterCalls))

	_, err := svc.Update(context.Background(), root, reconcile.UpdateContent)
	if !errors.Is(err, bootstrap.ErrForeignWorkspace) || errors.Is(err, workspace.ErrChanged) {
		t.Fatalf("Update() error = %v, want original ErrForeignWorkspace only", err)
	}
	if adapterCalls != 0 {
		t.Fatalf("initial public-state failure opened adapters/process/effects: %d", adapterCalls)
	}
}

func TestService_UpdateDirectMapsUnderLockDisappearanceToWorkspaceChanged(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	expected := testDesired(t, root, domain.AppCursor, true, "")
	prepareUpdateIdentityFixture(t, expected)
	expectedBytes := desiredStateBytes(t, expected)
	reads, adapterCalls := 0, 0
	svc := New(updateFailureOptions(func(string) ([]byte, error) {
		reads++
		if reads == 1 {
			return append([]byte(nil), expectedBytes...), nil
		}
		return nil, os.ErrNotExist
	}, &adapterCalls))

	_, err := svc.Update(context.Background(), root, reconcile.UpdateContent)
	if !errors.Is(err, workspace.ErrChanged) {
		t.Fatalf("Update() error = %v, want WORKSPACE_CHANGED", err)
	}
	if reads != 2 || adapterCalls != 0 {
		t.Fatalf("reads=%d adapters/process/effects=%d", reads, adapterCalls)
	}
}

func TestService_LoadDesiredRejectsOversizeAndInvalidUTF8BeforeStateStore(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"oversize", bytes.Repeat([]byte("A"), (64<<10)+1)},
		{"invalid UTF-8", []byte("KIT_ALL_TEAM_HOME=C:\xff\xfe\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			if err := os.MkdirAll(filepath.Join(root, ".teamkit"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".env"), test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			stateCalls := 0
			_, err := New(Options{StateStore: func(string) (engine.Store, error) { stateCalls++; return nil, nil }}).loadDesired(root)
			if !errors.Is(err, bootstrap.ErrForeignWorkspace) {
				t.Fatalf("loadDesired() error = %v, want ErrForeignWorkspace", err)
			}
			if stateCalls != 0 {
				t.Fatalf("invalid public env opened state store: %d", stateCalls)
			}
		})
	}
}

func TestService_UpdateRejectsConcurrentWorkspaceMutation(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	desired := testDesired(t, root, domain.AppCodex, true, "")
	prepareConvergedWorkspace(t, desired)
	writeDesired(t, desired)
	held, err := operationlock.Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	store := &recordingSecretStore{loaded: map[string]string{
		credentials.GitLabUsername: "user", credentials.GitLabToken: "token",
	}}
	var applied domain.DesiredState
	svc := New(Options{
		ApplicationLookPath: func(string) (string, error) { return filepath.Join(testutil.TempDir(t), "codex"), nil },
		ApplicationHome:     func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		SecretStore:         func(string) (credentials.SecretStore, error) { return store, nil },
		GitRunner:           readySourceObservationRunner(t, desired, ""),
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			return &recordingAskPass{credentials: input}, nil
		},
		Effects:  func(EffectInputs) engine.Effects { return captureEffects{desired: &applied, nonempty: true} },
		TempRoot: testutil.TempDir(t),
	})
	_, err = svc.Update(context.Background(), root, reconcile.UpdateContent)
	if !errors.Is(err, operationlock.ErrOperationInProgress) {
		t.Fatalf("Update error = %v, want ErrOperationInProgress", err)
	}
	if applied.Project() != "" {
		t.Fatalf("concurrent Update reached effects with desired %#v", applied)
	}
}

func TestService_UpdateRejectsMissingGitCredentialsBeforeAskPassOrEffects(t *testing.T) {
	for _, test := range []struct {
		name   string
		loaded map[string]string
		want   string
	}{
		{
			name:   "missing",
			loaded: map[string]string{},
			want:   "CREDENTIALS_REQUIRED: " + credentials.GitLabUsername + "," + credentials.GitLabToken,
		},
		{
			name: "empty value with optional CA absent",
			loaded: map[string]string{
				credentials.GitLabUsername: " \t ",
				credentials.GitLabToken:    "token",
			},
			want: "CREDENTIALS_REQUIRED: " + credentials.GitLabUsername,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			desired := testDesired(t, root, domain.AppCursor, true, "")
			prepareConvergedWorkspace(t, desired)
			writeDesired(t, desired)
			store := &recordingSecretStore{loaded: test.loaded}
			askPassCalls, effectCalls := 0, 0
			svc := New(Options{
				ApplicationHome: func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
				SecretStore:     func(string) (credentials.SecretStore, error) { return store, nil },
				GitRunner:       readySourceObservationRunner(t, desired, ""),
				AskPass: func(string, gitx.Credentials) (AskPassSession, error) {
					askPassCalls++
					return &recordingAskPass{}, nil
				},
				Effects: func(EffectInputs) engine.Effects {
					effectCalls++
					return failingEffects{canary: "TEAMKIT_MISSING_GIT_CANARY"}
				},
			})

			plan, err := svc.Update(context.Background(), root, reconcile.UpdateContent)
			if len(plan.Actions) == 0 {
				t.Fatal("Update() did not produce a credential-requiring plan")
			}
			if err == nil || err.Error() != test.want {
				t.Fatalf("Update() error = %v, want %q", err, test.want)
			}
			if askPassCalls != 0 || effectCalls != 0 {
				t.Fatalf("missing credentials opened mutation adapters: askpass=%d effects=%d", askPassCalls, effectCalls)
			}
		})
	}
}

func TestService_POSIXInstallerPinsFixedInvocation(t *testing.T) {
	desiredPOSIX := testDesired(t, filepath.Join(testutil.TempDir(t), "linux-kit"), domain.AppHermes, false, filepath.Join(testutil.TempDir(t), "linux-hermes"))
	var downloadedURL, verifiedHash, processName string
	var processArgs []string
	svc := New(Options{
		ManagedInstallReady: func(string) (bool, error) { return true, nil },
		Downloader: DownloadFunc(func(_ context.Context, url string) ([]byte, error) {
			downloadedURL = url
			return []byte("pinned script fixture"), nil
		}),
		VerifyDigest: func(_ []byte, expected string) bool { verifiedHash = expected; return true },
		WritePrivate: func(path string, _ []byte) error { return workspace.WriteFileAtomic(path, []byte("fixture"), 0o600) },
		Process: platform.ProcessRunnerFunc(func(_ context.Context, name string, args []string) error {
			processName, processArgs = name, append([]string(nil), args...)
			checkout := filepath.Join(desiredPOSIX.HermesHome(), ".teamkit", "hermes-agent-source")
			if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(checkout, ".git", "HEAD"), []byte(POSIXInstallerCommit+"\n"), 0o600); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(checkout, "venv", "bin"), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(checkout, "venv", "bin", "hermes"), []byte("#!/bin/sh\n"), 0o700); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(desiredPOSIX.HermesHome(), "skills", "bundled"), 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(desiredPOSIX.HermesHome(), "skills", "bundled", "SKILL.md"), []byte("bundled fixture\n"), 0o600)
		}),
	})
	installer, path, err := svc.installerFor(desiredPOSIX, cli.ApplyInputs{}, testutil.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := installer.Apply(context.Background(), path); err != nil {
		t.Fatalf("POSIX installer: %v", err)
	}
	if downloadedURL != POSIXInstallerURL || verifiedHash != POSIXInstallerSHA256 {
		t.Fatalf("url=%q hash=%q", downloadedURL, verifiedHash)
	}
	wantArgs := []string{
		path,
		"--dir", filepath.Join(desiredPOSIX.HermesHome(), ".teamkit", "hermes-agent-source"),
		"--hermes-home", desiredPOSIX.HermesHome(),
		"--commit", POSIXInstallerCommit, "--force-commit",
		"--skip-setup", "--non-interactive",
	}
	if processName != "/bin/bash" || !reflect.DeepEqual(processArgs, wantArgs) {
		t.Fatalf("process=%q args=%#v", processName, processArgs)
	}
	joinedArgs := "\x00" + strings.Join(processArgs, "\x00") + "\x00"
	if strings.Contains(joinedArgs, "\x00--no-skills\x00") || !strings.Contains(joinedArgs, "\x00--skip-setup\x00") || !strings.Contains(joinedArgs, "\x00--non-interactive\x00") {
		t.Fatalf("installer arguments = %#v", processArgs)
	}
	if _, err := os.Stat(filepath.Join(desiredPOSIX.HermesHome(), "skills", "bundled", "SKILL.md")); err != nil {
		t.Fatalf("bundled skills are absent: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(desiredPOSIX.HermesHome(), ".no-bundled-skills")); !os.IsNotExist(err) {
		t.Fatalf("unexpected bundled-skills opt-out marker: %v", err)
	}
}

func TestService_POSIXInstallerRejectsUnverifiedSuccessfulProcess(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "kit"), domain.AppHermes, false, filepath.Join(testutil.TempDir(t), "hermes"))
	svc := New(Options{
		Downloader:   DownloadFunc(func(context.Context, string) ([]byte, error) { return []byte("fixture"), nil }),
		VerifyDigest: func([]byte, string) bool { return true },
		WritePrivate: func(path string, data []byte) error { return workspace.WriteFileAtomic(path, data, 0o600) },
		Process:      platform.ProcessRunnerFunc(func(context.Context, string, []string) error { return nil }),
	})
	installer, path, err := svc.installerFor(desired, cli.ApplyInputs{}, desired.HermesHome())
	if err != nil {
		t.Fatal(err)
	}
	err = installer.Apply(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "HERMES_INSTALL_VERIFICATION_FAILED") {
		t.Fatalf("installer error=%v", err)
	}
}

func TestService_WindowsMissingHermesFailsClosedUntilInstallDirectoryIsVerified(t *testing.T) {
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSWindows, Application: domain.AppHermes, AppInstalled: false,
		KitHome: filepath.Join(testutil.TempDir(t), "kit"), HermesHome: filepath.Join(testutil.TempDir(t), "hermes"),
		Project: domain.ProjectWMS, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	installer, path, err := New(Options{}).installerFor(desired, cli.ApplyInputs{
		HermesInstaller: filepath.Join(testutil.TempDir(t), "Hermes-Setup.exe"),
	}, desired.HermesHome())
	if !errors.Is(err, ErrWindowsHermesInstallUnverified) {
		t.Fatalf("installerFor() = %#v, %q, %v; want fail-closed error", installer, path, err)
	}
	if !strings.Contains(err.Error(), "install Hermes manually") || !strings.Contains(err.Error(), "--app-installed=true") {
		t.Fatalf("error is not actionable: %v", err)
	}
}

func TestService_POSIXInstallerCopiesExplicitInputToPrivateCache(t *testing.T) {
	desiredPOSIX := testDesired(t, filepath.Join(testutil.TempDir(t), "linux-kit"), domain.AppHermes, false, filepath.Join(testutil.TempDir(t), "linux-hermes"))
	source := filepath.Join(testutil.TempDir(t), "install.sh")
	if err := os.WriteFile(source, []byte("verified local fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	var written, invoked string
	svc := New(Options{
		ManagedInstallReady: func(string) (bool, error) { return true, nil },
		VerifyDigest:        func([]byte, string) bool { return true },
		WritePrivate:        func(path string, _ []byte) error { written = path; return nil },
		Process: platform.ProcessRunnerFunc(func(_ context.Context, _ string, args []string) error {
			invoked = args[0]
			checkout := filepath.Join(desiredPOSIX.HermesHome(), ".teamkit", "hermes-agent-source")
			if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(checkout, ".git", "HEAD"), []byte(POSIXInstallerCommit+"\n"), 0o600); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(checkout, "venv", "bin"), 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(checkout, "venv", "bin", "hermes"), []byte("#!/bin/sh\n"), 0o700)
		}),
	})
	installer, cache, err := svc.installerFor(desiredPOSIX, cli.ApplyInputs{HermesInstaller: source}, desiredPOSIX.HermesHome())
	if err != nil {
		t.Fatal(err)
	}
	if cache == source || !strings.HasPrefix(cache, desiredPOSIX.HermesHome()+string(os.PathSeparator)) {
		t.Fatalf("unsafe POSIX cache=%q source=%q", cache, source)
	}
	if err := installer.Apply(context.Background(), cache); err != nil {
		t.Fatal(err)
	}
	if written != cache || invoked != cache {
		t.Fatalf("written=%q invoked=%q cache=%q", written, invoked, cache)
	}
}

func TestService_CertificateArchiveUsesVerifiedApplicationLocalCache(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "kit"), domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes"))
	source := filepath.Join(testutil.TempDir(t), "certs.zip")
	if err := os.WriteFile(source, []byte("certificate archive fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := New(Options{
		VerifyDigest: func(_ []byte, expected string) bool {
			return expected == bootstrap.DefaultCertificateSHA256
		},
		WritePrivate: func(path string, data []byte) error {
			return workspace.WriteFileAtomic(path, data, 0o600)
		},
	})
	cache, err := svc.certificateFor(desired, source, desired.HermesHome())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(desired.HermesHome(), ".teamkit", "cache", "certs.zip")
	if cache != want || cache == source {
		t.Fatalf("cache = %q, want %q", cache, want)
	}
	retryCache, err := svc.certificateFor(desired, "", desired.HermesHome())
	if err != nil || retryCache != cache {
		t.Fatalf("retry cache = %q, %v", retryCache, err)
	}
}

func TestService_HermesCertificateArchiveIsMandatoryWithoutVerifiedManagedBundle(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes"))
	svc := New(Options{})

	_, err := svc.certificateFor(desired, "", desired.HermesHome())
	if !errors.Is(err, bootstrap.ErrCertificateRequired) {
		t.Fatalf("certificateFor() error = %v, want ErrCertificateRequired", err)
	}
}

func TestService_MutationMaterializesVerifiedCAForFirstGitAction(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "kit"), domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes"))
	archive := filepath.Join(testutil.TempDir(t), "certs.zip")
	var payload bytes.Buffer
	zw := zip.NewWriter(&payload)
	entry, err := zw.Create("ca-bundle.pem")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("test CA")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, payload.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	var askPassInput gitx.Credentials
	svc := New(Options{
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			return hermes.DiscoveryResult{Installed: true, Home: desired.HermesHome(), Executable: filepath.Join(testutil.TempDir(t), "hermes"), Version: "0.20.1"}, nil
		},
		ApplicationHome: func(domain.DesiredState) (string, error) { return desired.HermesHome(), nil },
		SecretStore:     func(string) (credentials.SecretStore, error) { return &recordingSecretStore{}, nil },
		VerifyDigest:    func([]byte, string) bool { return true },
		WritePrivate: func(path string, data []byte) error {
			return workspace.WriteFileAtomic(path, data, 0o600)
		},
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			askPassInput = input
			return &recordingAskPass{credentials: input}, nil
		},
		TempRoot: testutil.TempDir(t),
	})
	values := map[string]string{
		credentials.GitLabUsername: "teamkit-user",
		credentials.GitLabToken:    "TEAMKIT_FIRST_GIT_CANARY",
	}
	_, runtimeContract, err := svc.bindHermesRuntime(context.Background(), desired)
	if err != nil {
		t.Fatalf("verify Hermes executable: %v", err)
	}
	_, cleanup, err := svc.mutationWithStoreExecutable(desired, cli.ApplyInputs{CertificateArchive: archive}, values, &recordingSecretStore{}, false, runtimeContract)
	if err != nil {
		t.Fatalf("mutationWithStore: %v", err)
	}
	defer cleanup()
	wantCA := filepath.Join(desired.HermesHome(), "certs", "ca-bundle.pem")
	if askPassInput.CAFile != wantCA {
		t.Fatalf("first Git CA = %q, want %q", askPassInput.CAFile, wantCA)
	}
	if info, err := os.Lstat(wantCA); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("materialized CA is not a safe regular file: info=%v err=%v", info, err)
	}
}

func TestAuthenticatedGit_SyncPinnedUsesOnlyApplicationLocalCA(t *testing.T) {
	runner := &fakeServiceGitRunner{results: []gitx.Result{{}, {Stdout: strings.Repeat("a", 40) + "\n"}, {}}}
	destination := filepath.Join(testutil.TempDir(t), "toolchain")
	git := authenticatedGit{
		repository: gitx.NewRepository(runner),
		credentials: gitx.Credentials{
			AskPassPath: "C:/private/askpass.exe",
			Username:    "gitlab-user",
			Token:       "TEAMKIT_CROSS_HOST_CANARY",
			CAFile:      "C:/private/ca-bundle.pem",
		},
	}
	if err := git.SyncPinned(context.Background(), "https://github.com/example/toolchain.git", strings.Repeat("a", 40), destination); err != nil {
		t.Fatalf("SyncPinned: %v", err)
	}
	for _, command := range runner.commands {
		joined := strings.Join(command.Env, "\n")
		network := false
		for _, argument := range command.Args {
			if argument == "clone" || argument == "fetch" {
				network = true
			}
		}
		hasCA := strings.Contains(joined, "GIT_SSL_CAINFO=C:/private/ca-bundle.pem")
		if network != hasCA {
			t.Fatalf("network=%v CA=%v command=%#v", network, hasCA, command)
		}
		if strings.Contains(joined, "TEAMKIT_CROSS_HOST_CANARY") || strings.Contains(joined, "gitlab-user") || strings.Contains(joined, "askpass.exe") {
			t.Fatalf("GitLab credentials crossed host boundary: %#v", command.Env)
		}
	}
}

type fakeServiceGitRunner struct {
	commands []gitx.Command
	results  []gitx.Result
}

func (r *fakeServiceGitRunner) Run(_ context.Context, command gitx.Command) (gitx.Result, error) {
	r.commands = append(r.commands, command)
	if slices.Contains(command.Args, "clone") && len(command.Args) > 0 {
		if err := os.MkdirAll(filepath.Join(command.Args[len(command.Args)-1], ".git"), 0o700); err != nil {
			return gitx.Result{}, err
		}
	}
	if len(r.results) == 0 {
		return gitx.Result{}, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result, nil
}

type recordingSecretStore struct {
	saved    map[string]string
	loaded   map[string]string
	loadKeys []string
}

type failingSecretStore struct{}

func (failingSecretStore) Save(map[string]string) (string, error) {
	return "", errors.New("INJECTED_SECRET_SAVE_FAILURE")
}

func (failingSecretStore) Load(...string) (map[string]string, error) {
	return nil, errors.New("INJECTED_SECRET_LOAD_FAILURE")
}

type gitRunnerFunc func(context.Context, gitx.Command) (gitx.Result, error)

func (f gitRunnerFunc) Run(ctx context.Context, command gitx.Command) (gitx.Result, error) {
	return f(ctx, command)
}

func readySourceObservationRunner(t *testing.T, desired domain.DesiredState, dirtyDirectory string) gitx.Runner {
	t.Helper()
	project, err := catalog.LookupProject(desired.Project())
	if err != nil {
		t.Fatal(err)
	}
	pin, err := catalog.LookupToolchain(desired.Toolchain())
	if err != nil {
		t.Fatal(err)
	}
	return gitRunnerFunc(func(_ context.Context, command gitx.Command) (gitx.Result, error) {
		joined := strings.Join(command.Args, " ")
		directory := ""
		for index, argument := range command.Args {
			if argument == "-C" && index+1 < len(command.Args) {
				directory = filepath.Clean(command.Args[index+1])
				break
			}
		}
		switch {
		case strings.Contains(joined, "config --local"):
			return gitx.Result{}, nil
		case strings.Contains(joined, "config --get remote.origin.url"):
			switch directory {
			case filepath.Clean(desired.KitHome()):
				return gitx.Result{Stdout: project.ContentRepository + "\n"}, nil
			case filepath.Clean(filepath.Join(desired.KitHome(), "db")):
				return gitx.Result{Stdout: project.DatabaseRepository + "\n"}, nil
			default:
				return gitx.Result{Stdout: pin.Origin + "\n"}, nil
			}
		case strings.Contains(joined, "symbolic-ref"):
			if directory == filepath.Clean(desired.KitHome()) {
				return gitx.Result{Stdout: project.ContentBranch + "\n"}, nil
			}
			return gitx.Result{Stdout: project.DatabaseBranch + "\n"}, nil
		case strings.Contains(joined, "rev-parse"):
			return gitx.Result{Stdout: pin.Commit + "\n"}, nil
		case strings.Contains(joined, "status --porcelain"):
			if dirtyDirectory != "" && directory == filepath.Clean(dirtyDirectory) {
				return gitx.Result{Stdout: " M tracked.txt\n"}, nil
			}
			return gitx.Result{}, nil
		default:
			t.Fatalf("unexpected Git observation: %#v", command)
			return gitx.Result{}, nil
		}
	})
}

func (s *recordingSecretStore) Save(values map[string]string) (string, error) {
	s.saved = cloneMap(values)
	return "secret.env", nil
}
func (s *recordingSecretStore) Load(keys ...string) (map[string]string, error) {
	s.loadKeys = append([]string(nil), keys...)
	return cloneMap(s.loaded), nil
}

type recordingAskPass struct {
	credentials gitx.Credentials
	closed      bool
}

func (s *recordingAskPass) Credentials() gitx.Credentials { return s.credentials }
func (s *recordingAskPass) Close() error                  { s.closed = true; return nil }

type failingEffects struct{ canary string }

type receiptFailureEffects struct{ err error }

func (f receiptFailureEffects) Observe(context.Context, domain.DesiredState, reconcile.UpdateChoice) (reconcile.ObservedState, error) {
	return reconcile.ObservedState{}, nil
}
func (f receiptFailureEffects) Apply(context.Context, domain.DesiredState, reconcile.Action) error {
	return f.err
}

func (f failingEffects) Observe(context.Context, domain.DesiredState, reconcile.UpdateChoice) (reconcile.ObservedState, error) {
	return reconcile.ObservedState{}, nil
}
func (f failingEffects) Apply(context.Context, domain.DesiredState, reconcile.Action) error {
	return errors.New("provider rejected " + f.canary)
}

type captureEffects struct {
	desired  *domain.DesiredState
	nonempty bool
}

func (f captureEffects) Observe(_ context.Context, _ domain.DesiredState, update reconcile.UpdateChoice) (reconcile.ObservedState, error) {
	return reconcile.ObservedState{WorkspaceReady: true, ContentReady: true, DatabaseReady: true, ToolchainReady: true, ApplicationReady: true, NonemptyWorkspace: f.nonempty, Update: update}, nil
}
func (f captureEffects) Apply(_ context.Context, desired domain.DesiredState, _ reconcile.Action) error {
	*f.desired = desired
	return nil
}

func testDesired(t *testing.T, root string, app domain.AIApplication, installed bool, hermesHome string) domain.DesiredState {
	t.Helper()
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: app, AppInstalled: installed, KitHome: root, HermesHome: hermesHome,
		Project: domain.ProjectWMS, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	return desired
}

func desiredStateInput(desired domain.DesiredState) domain.DesiredStateInput {
	return domain.DesiredStateInput{
		OS: desired.OS(), Application: desired.Application(), AppInstalled: desired.AppInstalled(),
		KitHome: desired.KitHome(), HermesHome: desired.HermesHome(), HermesVersion: desired.HermesVersion(),
		Project: desired.Project(), Role: desired.Role(), Toolchain: desired.Toolchain(),
	}
}

func mustDesiredState(t *testing.T, input domain.DesiredStateInput) domain.DesiredState {
	t.Helper()
	desired, err := domain.NewDesiredState(input)
	if err != nil {
		t.Fatal(err)
	}
	return desired
}

func desiredStateBytes(t *testing.T, desired domain.DesiredState) []byte {
	t.Helper()
	path := filepath.Join(testutil.TempDir(t), ".env")
	if err := workspace.WritePublicEnv(path, config.Encode(desired)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func updateFailureOptions(read func(string) ([]byte, error), adapterCalls *int) Options {
	recordAdapter := func() { *adapterCalls = *adapterCalls + 1 }
	return Options{
		ReadFile: read,
		ApplicationLookPath: func(string) (string, error) {
			recordAdapter()
			return "", nil
		},
		ApplicationHome: func(domain.DesiredState) (string, error) {
			recordAdapter()
			return "", nil
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			recordAdapter()
			return nil, errors.New("secret store must not open")
		},
		StateStore: func(string) (engine.Store, error) {
			recordAdapter()
			return nil, errors.New("state store must not open")
		},
		AskPass: func(string, gitx.Credentials) (AskPassSession, error) {
			recordAdapter()
			return nil, errors.New("askpass must not open")
		},
		GitRunner: gitRunnerFunc(func(context.Context, gitx.Command) (gitx.Result, error) {
			recordAdapter()
			return gitx.Result{}, errors.New("git must not run")
		}),
		Effects: func(EffectInputs) engine.Effects {
			recordAdapter()
			return captureEffects{}
		},
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			recordAdapter()
			return hermes.DiscoveryResult{}, errors.New("Hermes must not run")
		},
	}
}

func prepareConvergedWorkspace(t *testing.T, desired domain.DesiredState) {
	t.Helper()
	for _, path := range []string{filepath.Join(desired.KitHome(), ".git"), filepath.Join(desired.KitHome(), "db", ".git"), filepath.Join(desired.KitHome(), ".teamkit")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := gitx.InstallHooks(filepath.Join(desired.KitHome(), "db", ".git", "hooks")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desired.KitHome(), ".teamkit", "owner"), []byte(string(desired.Project())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	toolchain, err := apps.PinnedToolchain(desired.Toolchain())
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := apps.PrepareHandoff(apps.Application{ID: string(desired.Application()), Installed: true}, apps.HandoffRequest{Toolchain: toolchain})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desired.KitHome(), ".teamkit", "handoff.txt"), []byte(handoff.Command+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desired.KitHome(), ".teamkit", "content.ready"), []byte("content-wms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desired.KitHome(), ".teamkit", "database.ready"), []byte("develop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func prepareUpdateIdentityFixture(t *testing.T, desired domain.DesiredState) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(desired.KitHome(), ".teamkit"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desired.KitHome(), ".teamkit", "owner"), []byte(string(desired.Project())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
}

func writeDesired(t *testing.T, desired domain.DesiredState) {
	t.Helper()
	writeDesiredAt(t, desired.KitHome(), desired)
}

func writeDesiredAt(t *testing.T, root string, desired domain.DesiredState) {
	t.Helper()
	if err := workspace.WritePublicEnv(filepath.Join(root, ".env"), config.Encode(desired)); err != nil {
		t.Fatal(err)
	}
}

func cloneMap(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func writeTestCertificateArchive(t *testing.T) string {
	t.Helper()
	archive := filepath.Join(testutil.TempDir(t), "certs.zip")
	var payload bytes.Buffer
	writer := zip.NewWriter(&payload)
	entry, err := writer.Create("ca-bundle.pem")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("test CA")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, payload.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return archive
}
