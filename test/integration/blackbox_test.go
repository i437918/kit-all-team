package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mi1man-cmd/kit-all-team/internal/apps"
	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/config"
	"github.com/mi1man-cmd/kit-all-team/internal/credentials"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/gitx"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/registry"
	"github.com/mi1man-cmd/kit-all-team/internal/secrets"
	"github.com/mi1man-cmd/kit-all-team/internal/state"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

func TestMain(m *testing.M) {
	os.Exit(runIntegrationTests(m))
}

func runIntegrationTests(m *testing.M) (result int) {
	if strings.TrimSpace(os.Getenv("TEAMKIT_TEST_BINARY")) != "" {
		return m.Run()
	}
	repository, err := filepath.Abs("../..")
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve integration repository: %v\n", err)
		return 1
	}
	buildDir, err := os.MkdirTemp("", "teamkit-integration-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create integration build directory: %v\n", err)
		return 1
	}
	defer func() {
		result = finishIntegrationRun(result, buildDir, os.RemoveAll, os.Stderr)
	}()

	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(buildDir, "teamkit"+suffix)
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go"+suffix)
	build := exec.Command(goTool, "build", "-mod=vendor", "-o", binary, "./cmd/teamkit")
	build.Dir = repository
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "build integration Team Kit binary: %v\n%s", buildErr, output)
		return 1
	}
	if err := os.Setenv("TEAMKIT_TEST_BINARY", binary); err != nil {
		fmt.Fprintf(os.Stderr, "configure integration Team Kit binary: %v\n", err)
		return 1
	}
	return m.Run()
}

func finishIntegrationRun(result int, buildDir string, removeAll func(string) error, stderr io.Writer) int {
	if err := removeAll(buildDir); err != nil {
		fmt.Fprintf(stderr, "remove integration build directory: %v\n", err)
		if result == 0 {
			return 1
		}
	}
	return result
}

func TestIntegrationBuildCleanup_FailureForcesNonzero(t *testing.T) {
	var stderr bytes.Buffer
	removeCalls := 0
	buildDir := filepath.Join("fixture", "integration-build")
	result := finishIntegrationRun(0, buildDir, func(path string) error {
		removeCalls++
		if path != buildDir {
			t.Fatalf("cleanup path=%q want=%q", path, buildDir)
		}
		return errors.New("cleanup denied")
	}, &stderr)
	if result == 0 || removeCalls != 1 {
		t.Fatalf("result=%d remove calls=%d; want nonzero/1", result, removeCalls)
	}
	if output := stderr.String(); !strings.Contains(output, "remove integration build directory") || !strings.Contains(output, "cleanup denied") {
		t.Fatalf("cleanup stderr=%q", output)
	}
}

func TestBinary_VersionIsRunnableWithoutConfiguration(t *testing.T) {
	output, err := teamkitRun(t, "version")
	if err != nil || !strings.Contains(string(output), "version") {
		t.Fatalf("version: %v: %s", err, output)
	}
}

func TestBinary_HelpUsesCLIContract(t *testing.T) {
	output, err := teamkitRun(t, "--help")
	if err != nil {
		t.Fatalf("help: %v: %s", err, output)
	}
	if !strings.Contains(output, "teamkit plan|apply|status|retry|update|version") {
		t.Fatalf("help output=%q", output)
	}
}

func TestBinary_PlanDispatchesThroughProductionService(t *testing.T) {
	kit := filepath.Join(testutil.TempDir(t), "kit")
	output, err := teamkitRun(t,
		"plan", "--non-interactive", "--json", "--os", "linux", "--app", "codex",
		"--app-installed=true", "--kit-home", kit, "--project", "wms",
		"--role", "developer", "--toolchain", "ai_rules_1c",
	)
	if err != nil {
		t.Fatalf("plan: %v: %s", err, output)
	}
	if !strings.Contains(output, `"command":"plan"`) {
		t.Fatalf("plan output=%q", output)
	}
}

func TestBinary_Hermes0202AutoDetectsWithoutInstalledOrHomeFlags(t *testing.T) {
	kit := filepath.Join(testutil.TempDir(t), "kit")
	result := runJSONCommand(t, "plan", "--non-interactive", "--json", "--os", nativeTeamKitOS(), "--app", "hermes", "--kit-home", kit, "--project", "wms", "--role", "developer", "--toolchain", "ai_rules_1c")
	wantHome := filepath.Join(filepath.Dir(kit), ".test-user", ".hermes")
	if !result.Hermes.Installed || result.Hermes.Home != wantHome || result.Hermes.Version != "0.20.2" {
		t.Fatalf("Hermes metadata=%#v want home=%q/version=0.20.2", result.Hermes, wantHome)
	}
}

func TestBinary_RejectsClaimedApplicationMissingFromPathBeforeWorkspaceCreation(t *testing.T) {
	kit := filepath.Join(testutil.TempDir(t), "kit")
	output, err := teamkitRunWithoutApplication(t,
		"plan", "--non-interactive", "--json", "--os", "linux", "--app", "codex",
		"--app-installed=true", "--kit-home", kit, "--project", "wms",
		"--role", "developer", "--toolchain", "ai_rules_1c",
	)
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("plan error=%v output=%q; want process failure", err, output)
	}
	if os.Getenv("TEAMKIT_TEST_BINARY") != "" && exitError.ExitCode() != 3 {
		t.Fatalf("exact binary exit code=%d output=%q; want 3", exitError.ExitCode(), output)
	}
	if os.Getenv("TEAMKIT_TEST_BINARY") == "" && !strings.Contains(output, "exit status 3") {
		t.Fatalf("go run output=%q; want wrapped exit status 3", output)
	}
	if !strings.Contains(output, "AI_APP_REQUIRED") {
		t.Fatalf("plan output=%q; want AI_APP_REQUIRED", output)
	}
	if _, statErr := os.Lstat(kit); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("workspace created before application preflight: %v", statErr)
	}
}

func TestBinary_RejectsSecretFlagWithoutEchoingValue(t *testing.T) {
	for _, test := range []struct {
		name  string
		flag  string
		value string
	}{
		{name: "GitLab", flag: "--gitlab-token", value: "TEAMKIT_MAIN_SECRET_CANARY"},
		{name: "Jira", flag: "--jira-token", value: "jira-personal-canary-7xQ2mN9pL4vK8dR6"},
		{name: "Confluence", flag: "--confluence-token", value: "confluence-personal-canary-3wF8sT5yH2cJ9nM7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := teamkitRun(t, "plan", test.flag, test.value)
			if err == nil {
				t.Fatal("secret flag unexpectedly accepted")
			}
			if strings.Contains(output, test.value) {
				t.Fatalf("secret leaked in output=%q", output)
			}
		})
	}
}

func TestBinary_PendingApplicationLifecycleUsesNoNetworkOrSecrets(t *testing.T) {
	kit := prepareReadyWorkspace(t)
	if err := os.Remove(filepath.Join(kit, ".teamkit", "handoff.txt")); err != nil {
		t.Fatal(err)
	}
	selectors := []string{
		"--non-interactive", "--json", "--os", nativeTeamKitOS(), "--app", "codex",
		"--app-installed=true", "--kit-home", kit, "--project", "wms",
		"--role", "developer", "--toolchain", "ai_rules_1c", "--update", "none",
	}

	plan := runJSONCommand(t, append([]string{"plan"}, selectors...)...)
	assertCommandStatus(t, plan, "plan", "needs_apply")
	if len(plan.Plan.Actions) != 2 || plan.Plan.Actions[0].ID != "50-configure-application" || plan.Plan.Actions[1].ID != "90-verify-state" {
		t.Fatalf("plan actions=%#v", plan.Plan.Actions)
	}

	pending := runJSONCommand(t, "status", "--json", "--kit-home", kit)
	assertCommandStatus(t, pending, "status", "needs_apply")

	applied := runJSONCommand(t, append([]string{"apply"}, selectors...)...)
	assertCommandStatus(t, applied, "apply", "ready")
	if applied.Handoff == "" || !strings.Contains(applied.Handoff, "In codex, configure exactly one toolchain") {
		t.Fatalf("apply handoff=%q", applied.Handoff)
	}
	handoff, err := os.ReadFile(filepath.Join(kit, ".teamkit", "handoff.txt"))
	if err != nil || strings.TrimSpace(string(handoff)) != applied.Handoff {
		t.Fatalf("persisted handoff=%q err=%v; response=%q", handoff, err, applied.Handoff)
	}

	ready := runJSONCommand(t, "status", "--json", "--kit-home", kit)
	assertCommandStatus(t, ready, "status", "ready")
	if len(ready.Plan.Actions) != 0 {
		t.Fatalf("ready actions=%#v", ready.Plan.Actions)
	}

	retried := runJSONCommand(t, "retry", "--json", "--kit-home", kit)
	assertCommandStatus(t, retried, "retry", "ready")
	updated := runJSONCommand(t, "update", "--json", "--kit-home", kit, "--target", "none")
	assertCommandStatus(t, updated, "update", "ready")
}

type jsonResult struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Plan    struct {
		Actions []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"actions"`
	} `json:"plan"`
	Handoff string `json:"handoff"`
	Hermes  struct {
		Installed bool   `json:"installed"`
		Home      string `json:"home"`
		Version   string `json:"version"`
	} `json:"hermes"`
}

func runJSONCommand(t *testing.T, args ...string) jsonResult {
	t.Helper()
	output, err := teamkitRun(t, args...)
	if err != nil {
		t.Fatalf("%s: %v: %s", strings.Join(args, " "), err, output)
	}
	if strings.Contains(output, "TEAMKIT_MAIN_SECRET_CANARY") {
		t.Fatalf("secret canary in output=%q", output)
	}
	var result jsonResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid JSON schema: %v: %q", err, output)
	}
	return result
}

func assertCommandStatus(t *testing.T, result jsonResult, command, status string) {
	t.Helper()
	if result.Command != command || result.Status != status {
		t.Fatalf("result command=%q status=%q; want %q/%q", result.Command, result.Status, command, status)
	}
}

func teamkitRun(t *testing.T, args ...string) (string, error) {
	return teamkitRunConfigured(t, true, args...)
}

func teamkitRunWithoutApplication(t *testing.T, args ...string) (string, error) {
	return teamkitRunConfigured(t, false, args...)
}

func teamkitRunConfigured(t *testing.T, installApplication bool, args ...string) (string, error) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var cmd *exec.Cmd
	moduleCache := strings.TrimSpace(os.Getenv("GOMODCACHE"))
	if candidate := strings.TrimSpace(os.Getenv("TEAMKIT_TEST_BINARY")); candidate != "" {
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(root, candidate)
		}
		cmd = exec.Command(candidate, args...)
	} else {
		suffix := ""
		if runtime.GOOS == "windows" {
			suffix = ".exe"
		}
		goTool := filepath.Join(runtime.GOROOT(), "bin", "go"+suffix)
		if moduleCache == "" {
			envCommand := exec.Command(goTool, "env", "GOMODCACHE")
			envCommand.Dir = root
			value, envErr := envCommand.Output()
			if envErr != nil {
				t.Fatalf("resolve Go module cache: %v", envErr)
			}
			moduleCache = strings.TrimSpace(string(value))
		}
		cmd = exec.Command(goTool, append([]string{"run", "./cmd/teamkit"}, args...)...)
	}
	cmd.Dir = root
	var extraEnvironment []string
	for index, argument := range args {
		if argument != "--kit-home" || index+1 >= len(args) {
			continue
		}
		userHome := filepath.Join(filepath.Dir(args[index+1]), ".test-user")
		configHome := filepath.Join(userHome, "config")
		if err := os.MkdirAll(configHome, 0o700); err != nil {
			t.Fatal(err)
		}
		appBin := filepath.Join(userHome, "bin")
		if err := os.MkdirAll(appBin, 0o700); err != nil {
			t.Fatal(err)
		}
		pathValue := appBin
		if installApplication {
			selectedApp := ""
			for i, arg := range args {
				if arg == "--app" && i+1 < len(args) {
					selectedApp = args[i+1]
					break
				}
			}
			if selectedApp == "hermes" {
				hermesHome := filepath.Join(userHome, ".hermes")
				installFakeHermes(t, hermesHome, moduleCache)
				pathValue = filepath.Join(hermesHome, "hermes-agent", "venv", "bin") + string(os.PathListSeparator) + os.Getenv("PATH")
				if runtime.GOOS == "windows" {
					pathValue = filepath.Join(hermesHome, "hermes-agent", "venv", "Scripts") + string(os.PathListSeparator) + os.Getenv("PATH")
				}
				extraEnvironment = append(extraEnvironment, "HERMES_HOME="+hermesHome)
			} else {
				launcher := filepath.Join(appBin, "codex")
				contents := []byte("#!/bin/sh\nexit 0\n")
				if runtime.GOOS == "windows" {
					launcher += ".cmd"
					contents = []byte("@exit /b 0\r\n")
				}
				if err := os.WriteFile(launcher, contents, 0o700); err != nil {
					t.Fatal(err)
				}
				pathValue += string(os.PathListSeparator) + os.Getenv("PATH")
			}
		}
		environment := os.Environ()
		if moduleCache != "" {
			environment = append(environment, "GOMODCACHE="+moduleCache)
		}
		cmd.Env = append(environment,
			"USERPROFILE="+userHome,
			"HOME="+userHome,
			"APPDATA="+configHome,
			"LOCALAPPDATA="+configHome,
			"XDG_CONFIG_HOME="+configHome,
			"PATH="+pathValue,
		)
		cmd.Env = append(cmd.Env, extraEnvironment...)
		break
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err = cmd.Run()
	return output.String(), err
}

func nativeTeamKitOS() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return "linux"
}

func installFakeHermes(t *testing.T, home, moduleCache string) {
	t.Helper()
	install := filepath.Join(home, "hermes-agent")
	bin := filepath.Join(install, "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		bin = filepath.Join(install, "venv", "Scripts", "hermes.exe")
	}
	if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(install, "hermes_cli"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "hermes_cli", "config_defaults.py"), []byte("DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(install, "skills", "github"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "skills", "github", "SKILL.md"), []byte("---\nname: github\n---\n# github\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(testutil.TempDir(t), "fake-hermes.go")
	program := fmt.Sprintf(`package main
import("fmt";"os";"strings")
func main(){a:=strings.Join(os.Args[1:]," ");switch a{case "--version":fmt.Printf("Hermes Agent v0.20.2 (2026.8.13)\nInstall directory: %s\nPython: 3.11.15\nOpenAI SDK: 2.24.0\nRun 'hermes version' for update status.\n");case "profile --help":fmt.Println("create");case "profile create --help":fmt.Println("--no-alias --no-skills");case "skills opt-in --help":fmt.Println("--sync");case "doctor --help":fmt.Println("doctor");default:os.Exit(1)}}`, filepath.ToSlash(install))
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go"+suffix)
	build := exec.Command(goTool, "build", "-o", bin, source)
	if moduleCache != "" {
		build.Env = append(os.Environ(), "GOMODCACHE="+moduleCache)
	}
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake Hermes: %v: %s", err, output)
	}
	if err := exec.Command(bin, "unknown-command").Run(); err == nil {
		t.Fatal("fake Hermes accepted an unknown command")
	}
}

func prepareReadyWorkspace(t *testing.T) string {
	t.Helper()
	return prepareReadyWorkspaceAt(t, filepath.Join(testutil.TempDir(t), "kit"), domain.ProjectWMS)
}

func prepareReadyWorkspaceAt(t *testing.T, kit string, projectID domain.ProjectID) string {
	t.Helper()
	project, err := catalog.LookupProject(projectID)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS:           domain.OSFamily(nativeTeamKitOS()),
		Application:  domain.AppCodex,
		AppInstalled: true,
		KitHome:      kit,
		Project:      projectID,
		Role:         domain.RoleDeveloper,
		Toolchain:    domain.ToolchainAIRules1C,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{filepath.Join(kit, ".teamkit"), filepath.Join(kit, "db")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(kit, ".teamkit", "owner"):          string(projectID) + "\n",
		filepath.Join(kit, ".teamkit", "content.ready"):  project.ContentBranch + "\n",
		filepath.Join(kit, ".teamkit", "database.ready"): "develop\n",
		filepath.Join(kit, ".teamkit", "handoff.txt"):    mustHandoff(t, desired) + "\n",
		filepath.Join(kit, ".gitignore"):                 ".env\n/db/\n/.teamkit/\n",
	}
	for path, contents := range files {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := workspace.WritePublicEnv(filepath.Join(kit, ".env"), config.Encode(desired)); err != nil {
		t.Fatal(err)
	}
	gitRun(t, kit, "init")
	gitRun(t, kit, "checkout", "-b", project.ContentBranch)
	gitRun(t, kit, "config", "user.name", "Team Kit Test")
	gitRun(t, kit, "config", "user.email", "teamkit@example.invalid")
	gitRun(t, kit, "remote", "add", "origin", project.ContentRepository)
	gitRun(t, kit, "add", ".gitignore")
	gitRun(t, kit, "commit", "-m", "fixture")
	gitRun(t, filepath.Join(kit, "db"), "init")
	gitRun(t, filepath.Join(kit, "db"), "checkout", "-b", "develop")
	gitRun(t, filepath.Join(kit, "db"), "remote", "add", "origin", project.DatabaseRepository)
	if err := gitx.InstallHooks(filepath.Join(kit, "db", ".git", "hooks")); err != nil {
		t.Fatal(err)
	}
	return kit
}

func mustHandoff(t *testing.T, desired domain.DesiredState) string {
	t.Helper()
	toolchain, err := apps.PinnedToolchain(desired.Toolchain())
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := apps.PrepareHandoff(apps.Application{ID: string(desired.Application()), Installed: true}, apps.HandoffRequest{Toolchain: toolchain, V8StdEndpoint: catalog.V8StdMCP().Endpoint})
	if err != nil {
		t.Fatal(err)
	}
	return handoff.Command
}

func teamkitProcess(t *testing.T, isolatedRoot, stdin string, args ...string) (string, string, int) {
	t.Helper()
	return teamkitProcessWithEnv(t, isolatedRoot, stdin, nil, args...)
}

func teamkitProcessWithEnv(t *testing.T, isolatedRoot, stdin string, extra map[string]string, args ...string) (string, string, int) {
	t.Helper()
	command := teamkitCommandWithEnv(t, isolatedRoot, extra, args...)
	command.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	runErr := command.Run()
	if runErr == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	}
	t.Fatalf("run teamkit: %v", runErr)
	return "", "", -1
}

func teamkitCommandWithEnv(t *testing.T, isolatedRoot string, extra map[string]string, args ...string) *exec.Cmd {
	t.Helper()
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var command *exec.Cmd
	if binary := strings.TrimSpace(os.Getenv("TEAMKIT_TEST_BINARY")); binary != "" {
		if !filepath.IsAbs(binary) {
			binary = filepath.Join(repository, binary)
		}
		command = exec.Command(binary, args...)
	} else {
		suffix := ""
		if runtime.GOOS == "windows" {
			suffix = ".exe"
		}
		goTool := filepath.Join(runtime.GOROOT(), "bin", "go"+suffix)
		command = exec.Command(goTool, append([]string{"run", "./cmd/teamkit"}, args...)...)
	}
	userHome := filepath.Join(isolatedRoot, "home")
	localAppData := filepath.Join(isolatedRoot, "local")
	appData := filepath.Join(isolatedRoot, "roaming")
	xdg := filepath.Join(isolatedRoot, "config")
	bin := filepath.Join(isolatedRoot, "bin")
	for _, directory := range []string{userHome, localAppData, appData, xdg, bin} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	launcher := filepath.Join(bin, "codex")
	launcherBody := []byte("#!/bin/sh\nexit 0\n")
	if runtime.GOOS == "windows" {
		launcher += ".cmd"
		launcherBody = []byte("@exit /b 0\r\n")
	}
	if err := os.WriteFile(launcher, launcherBody, 0o700); err != nil {
		t.Fatal(err)
	}
	installIsolatedGitWrapper(t, bin)
	environment := append([]string(nil), os.Environ()...)
	fixed := map[string]string{
		"USERPROFILE": userHome, "HOME": userHome, "LOCALAPPDATA": localAppData,
		"APPDATA": appData, "XDG_CONFIG_HOME": xdg, "KIT_ALL_TEAM_HOME": "",
		"PATH": bin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	for key, value := range extra {
		fixed[key] = value
	}
	filtered := environment[:0]
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := fixed[key]; !overridden {
			filtered = append(filtered, entry)
		}
	}
	environment = filtered
	for key, value := range fixed {
		environment = append(environment, key+"="+value)
	}
	command.Dir, command.Env = repository, environment
	return command
}

func isolatedRegistryPath(root string) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(root, "local", "TeamKit", "environments.json")
	case "darwin":
		return filepath.Join(root, "home", "Library", "Application Support", "TeamKit", "environments.json")
	default:
		return filepath.Join(root, "config", "teamkit", "environments.json")
	}
}

func writeStrictRegistry(t *testing.T, path string, homes []string) {
	t.Helper()
	store := registry.New(path)
	for index := len(homes) - 1; index >= 0; index-- {
		if err := store.Promote(context.Background(), homes[index]); err != nil {
			t.Fatal(err)
		}
	}
}

type fileSnapshot struct {
	SHA256  [32]byte
	Size    int64
	ModTime int64
}

func snapshotRegularTree(t *testing.T, root string) map[string]fileSnapshot {
	t.Helper()
	result := map[string]fileSnapshot{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in fixture: %s", path)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[relative] = fileSnapshot{SHA256: sha256.Sum256(data), Size: info.Size(), ModTime: info.ModTime().UnixNano()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func createBareRemote(t *testing.T, root, branch string) string {
	t.Helper()
	work, bare := root+"-work", root+".git"
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "init")
	gitRun(t, work, "checkout", "-b", branch)
	gitRun(t, work, "config", "user.name", "Team Kit Test")
	gitRun(t, work, "config", "user.email", "teamkit@example.invalid")
	if err := os.WriteFile(filepath.Join(work, "fixture.txt"), []byte(branch+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "add", "fixture.txt")
	gitRun(t, work, "commit", "-m", "fixture")
	if output, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v: %s", err, output)
	}
	gitRun(t, work, "remote", "add", "fixture", bare)
	gitRun(t, work, "push", "fixture", branch)
	return bare
}

func fileURL(path string) string {
	slash := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}

func localProjectRewriteEnvironment(t *testing.T, root string) map[string]string {
	t.Helper()
	content := createBareRemote(t, filepath.Join(root, "content-remote"), "content-wms")
	database := createBareRemote(t, filepath.Join(root, "database-remote"), "develop")
	return map[string]string{
		"TEAMKIT_TEST_CONTENT_REMOTE":  content,
		"TEAMKIT_TEST_DATABASE_REMOTE": database,
	}
}

func installIsolatedGitWrapper(t *testing.T, bin string) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(testutil.TempDir(t), "isolated-git.go")
	program := fmt.Sprintf(`package main
import("fmt";"os";"os/exec";"strings")
func main(){
	if ledger:=os.Getenv("TEAMKIT_TEST_GIT_LEDGER");ledger!=""{file,err:=os.OpenFile(ledger,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600);if err!=nil{fmt.Fprintln(os.Stderr,"TEAMKIT_TEST_LEDGER_FAILED");os.Exit(96)};fmt.Fprintln(file,strings.Join(os.Args[1:],"\x1f"));if err:=file.Close();err!=nil{os.Exit(96)}}
	args:=append([]string(nil),os.Args[1:]...)
	fetch:=false
	for _,arg:=range args{if arg=="fetch"{fetch=true}}
	mapped:=false
	if fetch{
		for index,arg:=range args{
			switch arg{
			case "https://gitlab.example.invalid/1c/aisuz/ai.git":args[index]=os.Getenv("TEAMKIT_TEST_CONTENT_REMOTE");mapped=true
			case "https://gitlab.example.invalid/1c/fulfillment/wms.git", "https://gitlab.example.invalid/1c/fulfillment/wms":
				args[index]=os.Getenv("TEAMKIT_TEST_DATABASE_REMOTE");mapped=true
			default:
				lower:=strings.ToLower(arg)
				if strings.HasPrefix(lower,"https://")||strings.HasPrefix(lower,"http://"){fmt.Fprintln(os.Stderr,"TEAMKIT_TEST_NETWORK_REMOTE_REJECTED");os.Exit(97)}
			}
		}
	}
	if mapped{
		filtered:=make([]string,0,len(args))
		for index:=0;index<len(args);index++{if args[index]=="-c"&&index+1<len(args)&&args[index+1]=="protocol.file.allow=never"{index++;continue};filtered=append(filtered,args[index])}
		args=append([]string{"-c","protocol.file.allow=always"},filtered...)
	}
	command:=exec.Command(%q,args...);command.Stdin=os.Stdin;command.Stdout=os.Stdout;command.Stderr=os.Stderr
	if err:=command.Run();err!=nil{if exit,ok:=err.(*exec.ExitError);ok{os.Exit(exit.ExitCode())};fmt.Fprintln(os.Stderr,err);os.Exit(98)}
}`, realGit)
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	wrapper := filepath.Join(bin, name)
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go"+suffix)
	build := exec.Command(goTool, "build", "-o", wrapper, source)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build isolated git wrapper: %v: %s", err, output)
	}
	return wrapper
}

func TestBlackBox_IsolatedGitRejectsUnknownRemote(t *testing.T) {
	isolated := testutil.TempDir(t)
	bin := filepath.Join(isolated, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	wrapper := installIsolatedGitWrapper(t, bin)
	command := exec.Command(wrapper, "fetch", "https://example.invalid/no-network")
	command.Env = append(os.Environ(),
		"TEAMKIT_TEST_CONTENT_REMOTE="+filepath.Join(isolated, "content.git"),
		"TEAMKIT_TEST_DATABASE_REMOTE="+filepath.Join(isolated, "database.git"),
	)
	output, err := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 97 || !bytes.Contains(output, []byte("TEAMKIT_TEST_NETWORK_REMOTE_REJECTED")) {
		t.Fatalf("unknown remote was not rejected locally: err=%v output=%q", err, output)
	}
}

func pendingWorkspaceFixture(t *testing.T, home string) string {
	t.Helper()
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS:           domain.OSFamily(nativeTeamKitOS()),
		Application:  domain.AppCodex,
		AppInstalled: true,
		KitHome:      home,
		Project:      domain.ProjectWMS,
		Role:         domain.RoleDeveloper,
		Toolchain:    domain.ToolchainAIRules1C,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := reconcile.OperationPlan{ContractHash: "blackbox-contract", Actions: []reconcile.Action{{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true}}}
	store, err := state.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestBlackBox_InteractiveAddAndRegistry(t *testing.T) {
	const secretCanary = "TEAMKIT_SECRET_CANARY"
	isolated := testutil.TempDir(t)
	kit := filepath.Join(isolated, "kit")
	input := strings.Join([]string{"1", "3", "4", "1", kit, "11", "2", "1", "fixture-user", secretCanary, ""}, "\n")
	stdout, stderr, exit := teamkitProcessWithEnv(t, isolated, input, localProjectRewriteEnvironment(t, isolated), "apply")
	if exit != 0 || !strings.HasPrefix(stdout, "Что вы хотите сделать:") || !strings.Contains(stdout, "apply: ready") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
	if strings.Contains(stdout, secretCanary) || strings.Contains(stderr, secretCanary) {
		t.Fatalf("secret canary leaked to process output: stdout=%q stderr=%q", stdout, stderr)
	}
	raw, err := os.ReadFile(isolatedRegistryPath(isolated))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("trailing JSON: %v", err)
	}
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var version int
	var homes []string
	if err := json.Unmarshal(document["schema_version"], &version); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(document["homes"], &homes); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keys, []string{"homes", "schema_version"}) || version != 1 || !reflect.DeepEqual(homes, []string{filepath.Clean(kit)}) {
		t.Fatalf("keys=%v version=%d homes=%v", keys, version, homes)
	}
	for _, forbidden := range []string{"PROJECT", "ROLE", "TOOLCHAIN", "TOKEN", "aisuz", "apa", "wms", "TEAMKIT_SECRET_CANARY"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("registry contains %q: %q", forbidden, raw)
		}
	}
	assertBoundedTreeExcludes(t, kit, []byte(secretCanary))
	secretStore := filepath.Join(isolated, "home", ".codex", ".env")
	if err := privatefile.Validate(secretStore); err != nil {
		t.Fatalf("Codex secret store is not protected: %v", err)
	}
	protected, err := os.ReadFile(secretStore)
	if err != nil || !bytes.Contains(protected, []byte(secretCanary)) {
		t.Fatalf("test canary did not reach the protected application store %q: err=%v", secretStore, err)
	}
}

func assertBoundedTreeExcludes(t *testing.T, root string, forbidden []byte) {
	t.Helper()
	const maxFileBytes = 1 << 20
	const maxTreeBytes = 16 << 20
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("public tree contains symlink: %s", path)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > maxFileBytes {
			return fmt.Errorf("public file exceeds bounded scan limit: %s", path)
		}
		total += info.Size()
		if total > maxTreeBytes {
			return fmt.Errorf("public tree exceeds bounded scan limit")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, forbidden) {
			return fmt.Errorf("secret canary found in public file: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBlackBox_InteractiveUpdateSelectionAndNoOp(t *testing.T) {
	isolated := testutil.TempDir(t)
	apa := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "apa"), domain.ProjectAPA)
	wms := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "wms"), domain.ProjectWMS)
	writeStrictRegistry(t, isolatedRegistryPath(isolated), []string{wms, apa})
	beforeTree := snapshotRegularTree(t, wms)
	beforeRegistry, err := os.ReadFile(isolatedRegistryPath(isolated))
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exit := teamkitProcess(t, isolated, "2\n1\n1\n", "apply")
	if exit != 0 || !strings.Contains(stdout, "1. wms — "+environment.DisplayPath(wms)) || !strings.Contains(stdout, "2. apa — "+environment.DisplayPath(apa)) || !strings.Contains(stdout, "3. Указать другой путь") || !strings.Contains(stdout, "KIT_ALL_TEAM_HOME: "+environment.DisplayPath(wms)) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
	afterRegistry, err := os.ReadFile(isolatedRegistryPath(isolated))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeTree, snapshotRegularTree(t, wms)) || !bytes.Equal(beforeRegistry, afterRegistry) {
		t.Fatal("none changed workspace or registry")
	}
}

func TestBlackBox_InteractiveUpdateAutoSelectsSingleRegistryHome(t *testing.T) {
	isolated := testutil.TempDir(t)
	kit := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "kit"), domain.ProjectWMS)
	writeStrictRegistry(t, isolatedRegistryPath(isolated), []string{kit})
	stdout, stderr, exit := teamkitProcess(t, isolated, "2\n1\n", "apply")
	if exit != 0 || strings.Contains(stdout, "Выберите окружение:") || !strings.Contains(stdout, "KIT_ALL_TEAM_HOME: "+environment.DisplayPath(kit)) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
}

func TestBlackBox_InteractiveUpdateManualSelectionDrivesUpdate(t *testing.T) {
	isolated := testutil.TempDir(t)
	registryWMS := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "registered-wms"), domain.ProjectWMS)
	registryAPA := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "registered-apa"), domain.ProjectAPA)
	manual := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "manual-wms"), domain.ProjectWMS)
	writeStrictRegistry(t, isolatedRegistryPath(isolated), []string{registryWMS, registryAPA})
	writeCodexSecrets(t, isolated, "fixture-user", "TEAMKIT_MANUAL_TOKEN_CANARY")
	extra := localProjectRewriteEnvironmentFromReadyWorkspace(t, isolated, manual)
	input := strings.Join([]string{"2", "3", manual, "2", ""}, "\n")
	stdout, stderr, exit := teamkitProcessWithEnv(t, isolated, input, extra, "apply")
	if exit != 0 || !strings.Contains(stdout, "3. Указать другой путь") || !strings.Contains(stdout, "KIT_ALL_TEAM_HOME: "+environment.DisplayPath(manual)) || !strings.Contains(stdout, "update: ready") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
	loaded, state, err := registry.New(isolatedRegistryPath(isolated)).Load(context.Background())
	if err != nil || state != registry.LoadValid || len(loaded.Homes) == 0 || loaded.Homes[0] != filepath.Clean(manual) {
		t.Fatalf("manual update did not promote selected root: state=%v homes=%v err=%v", state, loaded.Homes, err)
	}
}

func writeCodexSecrets(t *testing.T, isolatedRoot, username, token string) {
	t.Helper()
	store, err := secrets.NewStore(filepath.Join(isolatedRoot, "home", ".codex"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(map[string]string{credentials.GitLabUsername: username, credentials.GitLabToken: token}); err != nil {
		t.Fatal(err)
	}
}

func localProjectRewriteEnvironmentFromReadyWorkspace(t *testing.T, root, workspaceRoot string) map[string]string {
	t.Helper()
	contentBare := filepath.Join(root, "matching-content.git")
	databaseBare := filepath.Join(root, "matching-database.git")
	gitRun(t, root, "clone", "--bare", workspaceRoot, contentBare)
	gitRun(t, root, "clone", "--bare", filepath.Join(workspaceRoot, "db"), databaseBare)
	return map[string]string{
		"TEAMKIT_TEST_CONTENT_REMOTE":  contentBare,
		"TEAMKIT_TEST_DATABASE_REMOTE": databaseBare,
	}
}

func TestBlackBox_CorruptRegistryWarnsOnceFallsBackAndDoesNotRewrite(t *testing.T) {
	isolated := testutil.TempDir(t)
	kit := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "kit"), domain.ProjectWMS)
	registryPath := isolatedRegistryPath(isolated)
	writeStrictRegistry(t, registryPath, []string{kit})
	original := []byte("{not-json\nTEAMKIT_CORRUPT_REGISTRY_CANARY\n")
	if err := os.WriteFile(registryPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	loadedBefore, stateBefore, loadErr := registry.New(registryPath).Load(context.Background())
	if loadErr == nil || stateBefore != registry.LoadCorrupt || len(loadedBefore.Homes) != 0 {
		t.Fatalf("fixture is not a protected corrupt registry: state=%v homes=%v err=%v", stateBefore, loadedBefore.Homes, loadErr)
	}
	stdout, stderr, exit := teamkitProcessWithEnv(t, isolated, "2\n1\n", map[string]string{"KIT_ALL_TEAM_HOME": kit}, "apply")
	const warning = "Предупреждение: локальный реестр Team Kit повреждён, недоступен или имеет неподдерживаемый формат и будет проигнорирован."
	if exit != 0 || strings.Count(stderr, warning) != 1 || !strings.Contains(stdout, "KIT_ALL_TEAM_HOME: "+environment.DisplayPath(kit)) {
		t.Fatalf("exit=%d warning-count=%d stdout=%q stderr=%q", exit, strings.Count(stderr, warning), stdout, stderr)
	}
	after, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) {
		t.Fatalf("corrupt registry was rewritten: before=%q after=%q", original, after)
	}
}

type synchronizedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type startedProcess struct {
	once    sync.Once
	kill    func() error
	wait    func() error
	waitErr error
}

func newStartedProcess(kill, wait func() error) *startedProcess {
	return &startedProcess{kill: kill, wait: wait}
}

func (p *startedProcess) Wait() error {
	p.once.Do(func() {
		p.waitErr = p.wait()
	})
	return p.waitErr
}

func (p *startedProcess) KillAndWait() {
	p.once.Do(func() {
		_ = p.kill()
		p.waitErr = p.wait()
	})
}

func TestStartedProcess_KillAndWaitReapsExactlyOnce(t *testing.T) {
	killCalls, waitCalls := 0, 0
	process := newStartedProcess(func() error {
		killCalls++
		return errors.New("already exited")
	}, func() error {
		waitCalls++
		return nil
	})

	process.KillAndWait()
	process.KillAndWait()
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	if killCalls != 1 || waitCalls != 1 {
		t.Fatalf("kill calls=%d wait calls=%d; want 1/1", killCalls, waitCalls)
	}
}

func TestStartedProcess_WaitThenCleanupDoesNotKillOrWaitAgain(t *testing.T) {
	killCalls, waitCalls := 0, 0
	process := newStartedProcess(func() error {
		killCalls++
		return nil
	}, func() error {
		waitCalls++
		return nil
	})

	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	process.KillAndWait()
	if killCalls != 0 || waitCalls != 1 {
		t.Fatalf("kill calls=%d wait calls=%d; want 0/1", killCalls, waitCalls)
	}
}

func TestBlackBox_InteractiveUpdateNoOpStartsNoChildAfterChoice(t *testing.T) {
	isolated := testutil.TempDir(t)
	kit := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "kit"), domain.ProjectWMS)
	writeStrictRegistry(t, isolatedRegistryPath(isolated), []string{kit})
	ledger := filepath.Join(isolated, "git-invocations.log")
	if err := os.WriteFile(ledger, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	command := teamkitCommandWithEnv(t, isolated, map[string]string{"TEAMKIT_TEST_GIT_LEDGER": ledger}, "apply")
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goTool += ".exe"
	}
	if strings.EqualFold(filepath.Clean(command.Path), filepath.Clean(goTool)) {
		t.Fatal("interactive no-op fixture must start a prebuilt Team Kit binary, not compile through go run")
	}
	wrapperName := "git"
	if runtime.GOOS == "windows" {
		wrapperName += ".exe"
	}
	probe := exec.Command(filepath.Join(isolated, "bin", wrapperName), "--version")
	probe.Env = command.Env
	if output, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("process-ledger canary failed: %v output=%q", err, output)
	}
	seededLedger, err := os.ReadFile(ledger)
	if err != nil || !bytes.Contains(seededLedger, []byte("--version")) {
		t.Fatalf("process-ledger canary was not recorded: bytes=%q err=%v", seededLedger, err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr synchronizedBuffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := newStartedProcess(command.Process.Kill, command.Wait)
	t.Cleanup(process.KillAndWait)
	if _, err := io.WriteString(stdin, "2\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(stdout.String(), "Что обновить в существующем окружении:") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(stdout.String(), "Что обновить в существующем окружении:") {
		t.Fatalf("update prompt not reached: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	before, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatalf("read process ledger before no-op choice: %v", err)
	}
	if !bytes.Equal(seededLedger, before) {
		t.Fatalf("unexpected child process before no-op choice: seed=%q before=%q", seededLedger, before)
	}
	if _, err := io.WriteString(stdin, "1\n"); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("no-op process: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	after, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("no-op started a Git child after choice: before=%q after=%q", before, after)
	}
}

func TestBlackBox_RetryRequiredLeavesWorkspaceAndRegistryUnchanged(t *testing.T) {
	isolated := testutil.TempDir(t)
	home := pendingWorkspaceFixture(t, filepath.Join(isolated, "pending"))
	registryPath := isolatedRegistryPath(isolated)
	writeStrictRegistry(t, registryPath, []string{home})
	before, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	_, stderr, exit := teamkitProcess(t, isolated, "2\n", "apply")
	if exit == 0 || !strings.Contains(stderr, "RETRY_REQUIRED") || !strings.Contains(stderr, home) {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	after, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("retry-required flow rewrote registry")
	}
	for _, path := range []string{filepath.Join(home, ".teamkit", "owner"), filepath.Join(home, ".env")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected metadata %s: %v", path, err)
		}
	}
}

func TestBlackBox_NonInteractiveApplyNeverPrintsMode(t *testing.T) {
	isolated := testutil.TempDir(t)
	kit := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "kit"), domain.ProjectWMS)
	stdout, stderr, exit := teamkitProcess(t, isolated, "", "apply", "--non-interactive", "--os", nativeTeamKitOS(), "--app", "codex", "--app-installed=true", "--kit-home", kit, "--project", "wms", "--role", "developer", "--toolchain", "ai_rules_1c", "--update", "none")
	if exit != 0 || strings.Contains(stdout, "Что вы хотите сделать:") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
}

func gitRun(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
