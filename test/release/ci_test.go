package release_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPublicSource_ContainsNoCorporateInfrastructureReferences(t *testing.T) {
	root := repositoryRoot(t)
	forbidden := []string{
		"russian" + "post",
		"pochta" + "tech",
		"JIRA" + "_TOKEN",
		"CONFLUENCE" + "_TOKEN",
		"litellm" + ".tools",
		"confluence" + ".tools",
	}
	command := exec.Command("git", "-C", root, "ls-files", "-z")
	tracked, err := command.Output()
	if err != nil {
		t.Fatalf("list tracked public files: %v", err)
	}
	for _, relative := range strings.Split(string(tracked), "\x00") {
		if relative == "" {
			continue
		}
		path := filepath.Join(root, relative)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatalf("stat tracked public file %s: %v", relative, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read tracked public file %s: %v", relative, err)
		}
		lower := strings.ToLower(string(data))
		for _, value := range forbidden {
			if strings.Contains(lower, strings.ToLower(value)) {
				t.Errorf("public source retains forbidden corporate reference %q in %s", value, filepath.ToSlash(relative))
			}
		}
	}
}

func TestActiveVersionContracts_UseFinalV015Exclusively(t *testing.T) {
	root := repositoryRoot(t)
	files := []string{
		".gitlab-ci.yml",
		"scripts/build.ps1",
		"scripts/build.sh",
		".github/workflows/ci.yml",
		".github/workflows/nightly.yml",
		".github/workflows/alt-native.yml",
		".github/workflows/hermes-windows-e2e.yml",
		".github/workflows/release.yml",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			contract := readContractFile(t, root, name)
			if !strings.Contains(contract, "v0.1.5") {
				t.Error("active version contract does not contain v0.1.5")
			}
			for _, obsolete := range []string{"v0.1.4", "v0.1.2", "v0.1.0-rc.1", "v0.1.0-rc.2", "VERSION: v0.1.0", `Version = "v0.1.0"`, "VERSION=${1:-v0.1.0}"} {
				if strings.Contains(contract, obsolete) {
					t.Errorf("active version contract still contains %s", obsolete)
				}
			}
		})
	}
}

func TestReleaseValidationArtifacts_UseExactV015PlatformNames(t *testing.T) {
	artifacts := []string{
		"teamkit-v0.1.5-windows-amd64.exe",
		"teamkit-v0.1.5-linux-amd64",
		"teamkit-v0.1.5-darwin-amd64",
		"teamkit-v0.1.5-darwin-arm64",
	}
	for _, path := range []string{
		".github/workflows/ci.yml",
		".github/workflows/release.yml",
	} {
		t.Run(path, func(t *testing.T) {
			workflow := readContractFile(t, repositoryRoot(t), path)
			for _, artifact := range artifacts {
				if !strings.Contains(workflow, artifact) {
					t.Errorf("release workflow does not contain exact v0.1.5 artifact %q", artifact)
				}
			}
			for _, obsolete := range []string{
				"teamkit-v0.1.4-windows-amd64.exe",
				"teamkit-v0.1.4-linux-amd64",
				"teamkit-v0.1.4-darwin-amd64",
				"teamkit-v0.1.4-darwin-arm64",
				"teamkit-v0.1.2-windows-amd64.exe",
				"teamkit-v0.1.2-linux-amd64",
				"teamkit-v0.1.2-darwin-amd64",
				"teamkit-v0.1.2-darwin-arm64",
			} {
				if strings.Contains(workflow, obsolete) {
					t.Errorf("release workflow retains obsolete active artifact %q", obsolete)
				}
			}
		})
	}
}

func TestReleaseValidationArtifacts_RequireExactV015PlatformSets(t *testing.T) {
	ci := strings.ReplaceAll(readContractFile(t, repositoryRoot(t), ".github/workflows/ci.yml"), "\r\n", "\n")
	matrixStart := strings.Index(ci, "        include:\n")
	matrixEnd := -1
	if matrixStart >= 0 {
		matrixEnd = strings.Index(ci[matrixStart:], "    runs-on:")
		if matrixEnd >= 0 {
			matrixEnd += matrixStart
		}
	}
	if matrixStart < 0 || matrixEnd <= matrixStart {
		t.Fatalf("cannot isolate native artifact matrix: start=%d end=%d", matrixStart, matrixEnd)
	}
	matrix := ci[matrixStart:matrixEnd]
	assertExactV015PlatformArtifacts(t, "native artifact matrix", matrix)

	release := strings.ReplaceAll(readContractFile(t, repositoryRoot(t), ".github/workflows/release.yml"), "\r\n", "\n")
	const listStart = "          for asset in \\\n"
	const listEnd = "            SHA256SUMS SECURITY-AUDIT.json; do"
	for index, start := 0, 0; ; index++ {
		next := strings.Index(release[start:], listStart)
		if next < 0 {
			break
		}
		listStartIndex := start + next
		listEndIndex := strings.Index(release[listStartIndex:], listEnd)
		if listEndIndex < 0 {
			t.Fatalf("cannot isolate final validation artifact list %d", index+1)
		}
		listEndIndex += listStartIndex
		assertExactV015PlatformArtifacts(t, "final validation artifact list", release[listStartIndex:listEndIndex])
		start = listEndIndex + len(listEnd)
	}
	if strings.Count(release, listStart) != 2 {
		t.Fatalf("final validation must contain exactly two artifact lists, got %d", strings.Count(release, listStart))
	}
}

func assertExactV015PlatformArtifacts(t *testing.T, scope, text string) {
	t.Helper()
	want := map[string]int{
		"teamkit-v0.1.5-windows-amd64.exe": 1,
		"teamkit-v0.1.5-linux-amd64":       1,
		"teamkit-v0.1.5-darwin-amd64":      1,
		"teamkit-v0.1.5-darwin-arm64":      1,
	}
	got := make(map[string]int)
	for _, line := range strings.Split(text, "\n") {
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "\\"))
		name = strings.TrimPrefix(name, "binary: ")
		if strings.HasPrefix(name, "teamkit-v0.1.5-") {
			got[name]++
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s has %d v0.1.5 platform artifacts, want %d: got=%v", scope, len(got), len(want), got)
	}
	for name, count := range want {
		if got[name] != count {
			t.Errorf("%s has %d occurrences of %q, want %d", scope, got[name], name, count)
		}
	}
	for name, count := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s has unexpected v0.1.5 platform artifact %q (%d occurrences)", scope, name, count)
		}
	}
}

func TestCIUsesPinnedNativeAndALTInputs(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	files := []string{
		".github/workflows/ci.yml",
		".github/workflows/nightly.yml",
		"scripts/alt-container-smoke.sh",
		"scripts/alt-qemu-smoke.sh",
	}
	var joined strings.Builder
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		joined.Write(data)
	}

	want := []string{
		"windows-2025",
		"ubuntu-24.04",
		"macos-15-intel",
		"macos-15",
		"macos-26",
		"actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803",
		"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
		"actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f",
		"actions/download-artifact@37930b1c2abaa49bbe596cd826c3c89aef350131",
		"registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea",
		"alt-p11-cloud-x86_64.qcow2",
		"d5db0d26addcd2fceed5f045d8eb7f3227afb29e6ec4986bd2edf2addfada1ee",
		"17F112840DE94827C9C109FD3E2B30EA57EF33CE",
		"SHA256SUM.asc",
		"alt-cloud-signing-key.asc",
	}
	text := joined.String()
	for _, fragment := range want {
		if !strings.Contains(text, fragment) {
			t.Errorf("CI contract does not contain %q", fragment)
		}
	}
}

func TestFinalReleaseWorkflow_IsReadOnlyValidationForExactCandidate(t *testing.T) {
	root := repositoryRoot(t)
	text := readContractFile(t, root, ".github/workflows/release.yml")
	for _, fragment := range []string{
		"name: final-release-validation",
		"workflow_dispatch:",
		"ci_run_id:",
		"candidate_digest:",
		"actions: read",
		"contents: read",
		"VERSION: v0.1.5",
		"gitlab_sha256s:",
		"GITLAB_SHA256S: ${{ inputs.gitlab_sha256s }}",
		"DUAL_CI_BYTE_COMPARE_OK",
		"evidence/native/native/$runner-tests.json",
		"evidence/native/native/$runner-candidate.txt",
		"evidence/native/native/security-audit-$runner.json",
		"actions/download-artifact@37930b1c2abaa49bbe596cd826c3c89aef350131",
		"actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f",
		"test \"$(gh api repos/\"$GITHUB_REPOSITORY\" --jq '.private')\" = false",
		"SHA256SUMS",
		"RELEASE-EVIDENCE.md",
		"RELEASE-EVIDENCE.tar.gz",
		"Classification: final (non-prerelease), unsigned internal release",
		"Trusted corporate-network evidence: unavailable for this exact commit; not a validation gate",
		"ALT evidence: pinned p11 userspace only; native ALT and QEMU/VM are unverified and are not validation gates",
		"Publication: GitHub validation only; GitLab is the only publisher",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("release workflow does not contain %q", fragment)
		}
	}

	inputStart := strings.Index(text, "    inputs:")
	permissionsStart := strings.Index(text, "\npermissions:")
	if inputStart < 0 || permissionsStart <= inputStart {
		t.Fatal("cannot isolate workflow_dispatch inputs")
	}
	inputs := text[inputStart:permissionsStart]
	for _, forbidden := range []string{"qemu_run_id:", "network_probe_run_id:", "publish:", "payload_release:"} {
		if strings.Contains(inputs, forbidden) {
			t.Errorf("final validation retains forbidden input %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"contents: write",
		"gh release ",
		"git/refs",
		"refs/tags",
		"--prerelease",
		"--draft",
		"RELEASE_TAG_ALREADY_EXISTS",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("read-only GitHub validation contains publication mutation %q", forbidden)
		}
	}
}

func TestGitLabReleaseHandoff_StoresOneKeptExactCandidateSet(t *testing.T) {
	root := repositoryRoot(t)
	text := readContractFile(t, root, ".gitlab-ci.yml") + "\n" + readContractFile(t, root, "scripts/release/handoff/main.go")
	for _, fragment := range []string{
		"release-handoff:",
		"- handoff/",
		"MANIFEST.json",
		"expire_in: never",
		"teamkit-v0.1.5-windows-amd64.exe",
		"teamkit-v0.1.5-linux-amd64",
		"teamkit-v0.1.5-darwin-amd64",
		"teamkit-v0.1.5-darwin-arm64",
		"SHA256SUMS",
		"SECURITY-AUDIT.json",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("GitLab handoff does not contain %q", fragment)
		}
	}
}

func TestGitLabReleaseHandoff_UsesBundledGoTransportWithoutPackageInstall(t *testing.T) {
	root := repositoryRoot(t)
	text := readContractFile(t, root, ".gitlab-ci.yml")
	handoffStart := strings.Index(text, "release-handoff:")
	artifactsStart := strings.Index(text[handoffStart:], "  artifacts:")
	if handoffStart < 0 || artifactsStart < 0 {
		t.Fatal("cannot isolate release-handoff job")
	}
	handoff := text[handoffStart : handoffStart+artifactsStart]
	for _, required := range []string{
		"image: golang:1.26.6",
		"go run ./scripts/release/handoff",
	} {
		if !strings.Contains(handoff, required) {
			t.Errorf("release-handoff does not use %q", required)
		}
	}
	for _, forbidden := range []string{"apk add", "curl ", "unzip "} {
		if strings.Contains(handoff, forbidden) {
			t.Errorf("release-handoff depends on runner package installation %q", forbidden)
		}
	}
}

func TestALTQualificationImage_UsesNewGitHubOwnerAndDigest(t *testing.T) {
	root := repositoryRoot(t)
	const image = "ghcr.io/i437918/kit-all-team/alt-p11-officecli@sha256:5ee493c6c7edbdb8d68fb0ab9af2847bae855c9042bc5f13f5fd6b3d0965a825"
	for _, path := range []string{
		".github/workflows/ci.yml",
		"scripts/alt-container-smoke.sh",
		"internal/service/officecli_live_test.go",
		"docs/TEST-MATRIX.md",
	} {
		if !strings.Contains(readContractFile(t, root, path), image) {
			t.Errorf("working ALT qualification pin %s does not use the migrated GHCR image", path)
		}
	}
}

func TestBlackboxHarness_UsesExactCandidateOrPortableGoTool(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "test", "integration", "blackbox_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"filepath.Join(root, \".tools\")", "go.exe\"), \"run\""} {
		if strings.Contains(text, forbidden) {
			t.Errorf("blackbox harness contains clean-checkout-incompatible fragment %q", forbidden)
		}
	}
	for _, required := range []string{"TEAMKIT_TEST_BINARY", "runtime.GOROOT()", "GOMODCACHE="} {
		if !strings.Contains(text, required) {
			t.Errorf("blackbox harness does not contain %q", required)
		}
	}
}

func TestALTUserspaceCandidateIsExecutableByTheContainerUser(t *testing.T) {
	root := repositoryRoot(t)
	workflow := readContractFile(t, root, ".github/workflows/ci.yml")
	if !strings.Contains(workflow, "chmod 0755 dist/teamkit-v0.1.5-linux-amd64") {
		t.Fatal("ALT userspace candidate is not executable by the unprivileged container user")
	}
	if strings.Contains(workflow, "chmod 700 dist/teamkit-v0.1.5-linux-amd64") {
		t.Fatal("ALT userspace candidate is owner-only and cannot run under the container UID")
	}
}

func TestOfficeCLILiveWorkflow_IsExactDispatchOnlyRuntimeGate(t *testing.T) {
	root := repositoryRoot(t)
	workflow := strings.ReplaceAll(readContractFile(t, root, ".github/workflows/ci.yml"), "\r\n", "\n")
	liveTest := readContractFile(t, root, "internal/service/officecli_live_test.go")
	provisioner := readContractFile(t, root, "internal/service/officecli.go")
	catalogContract := readContractFile(t, root, "internal/catalog/catalog.go")
	script := readContractFile(t, root, "scripts/alt-container-smoke.sh")
	operationContract := readContractFile(t, root, "internal/service/operation_contract.go")
	runtimeContract := liveTest + provisioner

	for _, required := range []string{
		"expected_sha:",
		"required: true",
		`github.event_name == 'workflow_dispatch'`,
		`GITHUB_SHA`,
		`EXPECTED_SHA: ${{ inputs.expected_sha }}`,
		`test -n "$EXPECTED_SHA"`,
		`test "$GITHUB_SHA" = "$EXPECTED_SHA"`,
		"go test -tags officecli_live ./internal/service -run TestOfficeCLILive_QualifiedAssetAndMCPHandshake -count=1 -timeout=3m",
		"CGO_ENABLED=0 go test -c -tags officecli_live -o dist/evidence/officecli/officecli-live.test ./internal/service",
		`TEAMKIT_OFFICECLI_RUNNER_ENVIRONMENT: ${{ runner.environment }}`,
		"officecli-win-x64.exe",
		"officecli-linux-x64",
		"officecli-mac-x64",
		"officecli-mac-arm64",
		"codesign --verify --strict --verbose=2",
		"spctl --assess --type execute --verbose=4",
		"OFFICECLI_ALT_USERSPACE_COMPATIBLE",
	} {
		if !strings.Contains(workflow+script, required) {
			t.Errorf("OfficeCLI live CI contract does not contain %q", required)
		}
	}
	evidenceMode := strings.Index(workflow, `chmod 0755 "$officecli_asset"`)
	evidenceHash := -1
	if evidenceMode >= 0 {
		evidenceHash = strings.Index(workflow[evidenceMode:], `sha256sum --check --strict`)
	}
	if evidenceMode < 0 || evidenceHash < 0 {
		t.Fatalf("ALT evidence copy must be re-hashed after mode 0755: chmod=%d hash=%d", evidenceMode, evidenceHash)
	}
	setPolicy := strings.Index(provisioner, "p.setAutoUpdate(ctx)")
	readPolicy := strings.Index(provisioner, "p.readAutoUpdate(ctx)")
	ensure := strings.Index(liveTest, "provisioner.Ensure(context.Background())")
	firstMCP := strings.Index(liveTest, "officeCLIMCPHandshake(t, provisioner.Path())")
	postMCPDigest := strings.LastIndex(liveTest, "assertOfficeCLIAsset(t, service, provisioner.Path(), asset)")
	if setPolicy < 0 || readPolicy <= setPolicy || ensure < 0 || firstMCP <= ensure || postMCPDigest <= firstMCP {
		t.Fatalf("OfficeCLI config/read-back/MCP/post-SHA ordering is unsafe: set=%d read=%d ensure=%d mcp=%d sha=%d", setPolicy, readPolicy, ensure, firstMCP, postMCPDigest)
	}
	for _, forbidden := range []string{"pull_request:\n    paths:", "push:\n    paths:", `branches: [master, main, "feat/**", "codex/**"]`} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("OfficeCLI smoke expands permanent PR/push trigger surface via %q", forbidden)
		}
	}

	for _, required := range []string{
		"//go:build officecli_live",
		"TestOfficeCLILive_QualifiedAssetAndMCPHandshake",
		"resolveOfficeCLIAsset",
		"service.officeCLIProvisioner",
		"provisioner.Ensure",
		`[]string{"config", "autoUpdate", "false"}`,
		`[]string{"config", "autoUpdate"}`,
		`[]string{"--version"}`,
		`[]string{"mcp"}`,
		`"protocolVersion":"2024-11-05"`,
		`"method":"notifications/initialized"`,
		`"method":"tools/list"`,
		"TEAMKIT_OFFICECLI_EXISTING_PATH",
		"TEAMKIT_OFFICECLI_RUNNER_ENVIRONMENT",
		"TEAMKIT_OFFICECLI_EXEC_ROOT",
		"1 << 20",
		"10 * time.Second",
		"30 * time.Second",
		".update.partial",
		"autoUpdate",
		"SKILL.md",
	} {
		if !strings.Contains(runtimeContract, required) {
			t.Errorf("OfficeCLI live test does not contain required policy %q", required)
		}
	}
	for _, forbidden := range []string{
		"releases/latest", "install.sh", "install.ps1", "officecli install",
		"officecli skills install", "officecli skill install", "OFFICECLI_SKIP_UPDATE",
		"curl |", "curl -s |", "curl -fsSL |",
		"http.Get(", "http.DefaultClient", "exec.Command(",
	} {
		if strings.Contains(workflow+script+liveTest+provisioner+operationContract+catalogContract, forbidden) {
			t.Errorf("OfficeCLI runtime contract contains forbidden flow %q", forbidden)
		}
	}
	if strings.Contains(liveTest+workflow+script, "https://github.com/iOfficeAI/OfficeCLI/releases/") {
		t.Error("OfficeCLI live flow bypasses the production catalog with an inline initial URL")
	}
	for _, exactURL := range []string{
		"https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-win-x64.exe",
		"https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-linux-x64",
		"https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-mac-x64",
		"https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-mac-arm64",
	} {
		if strings.Count(catalogContract, exactURL) != 1 {
			t.Errorf("production catalog does not contain exactly one qualified initial URL %q", exactURL)
		}
	}
	if strings.Contains(script, "config autoUpdate") || strings.Contains(script, "tools/list") {
		t.Error("ALT script duplicates the Go config/protocol implementation")
	}
}

func TestALTOfficeCLIQualificationPublication(t *testing.T) {
	workflowText := readContractFile(t, repositoryRoot(t), ".github/workflows/publish-alt-p11-officecli.yml")
	var workflow altOfficeCLIQualificationWorkflow
	if err := yaml.Unmarshal([]byte(workflowText), &workflow); err != nil {
		t.Fatalf("parse ALT OfficeCLI publication workflow: %v", err)
	}
	input, ok := workflow.On.WorkflowDispatch.Inputs["expected_sha"]
	if !ok || !input.Required || input.Type != "string" {
		t.Fatalf("expected_sha dispatch input = %#v", input)
	}
	publish, ok := workflow.Jobs["publish"]
	if !ok || publish.Permissions["packages"] != "write" || workflow.Permissions["packages"] != "" {
		t.Fatalf("packages write must be publish-job-only: workflow=%#v publish=%#v", workflow.Permissions, publish.Permissions)
	}
	discover, ok := workflow.Jobs["discover"]
	if !ok {
		t.Fatal("ALT OfficeCLI publication workflow has no discover job")
	}
	verify := altWorkflowStep(t, publish.Steps, "Verify checked out commit and exact RPM evidence")
	verifyRun := executableShell(verify.Run)
	for _, required := range []string{`test "$(git rev-parse HEAD)" = "$EXPECTED_SHA"`, `[[ "$ALT_LIBRT_PACKAGE" =~ ^`} {
		if !strings.Contains(verifyRun, required) {
			t.Errorf("publish source/package verification is missing %q:\n%s", required, verifyRun)
		}
	}
	discovery := altWorkflowStep(t, discover.Steps, "Discover official runtime providers")
	discoveryRun := executableShell(discovery.Run)
	for _, required := range []string{
		"docker run --rm",
		"ALT_LIBRT_DISCOVERY_CANDIDATE=glibc-pthread",
		"owner=\"$(rpm -qf /lib64/librt.so.1)\"",
		"ALT_LIBRT_OWNER=\"$(rpm -q --qf",
		"ALT_LIBRT_PACKAGE=\"$ALT_LIBRT_OWNER\"",
		"test \"$ALT_LIBRT_OWNER\" = \"$ALT_LIBRT_PACKAGE\"",
		"ALT_LIBRT_NAME=\"$(rpm -q --qf",
		"ALT_LIBRT_EVR=\"$(rpm -q --qf",
		"ALT_LIBRT_APT_SPEC=\"$ALT_LIBRT_NAME=$ALT_LIBRT_EVR\"",
		"ALT_LIBRT_APT_SPEC=%s",
		"ALT_LIBRT_PATH=/lib64/librt.so.1",
	} {
		if !strings.Contains(discoveryRun, required) {
			t.Errorf("discovery executable contract is missing %q:\n%s", required, discoveryRun)
		}
	}
	if strings.Contains(discoveryRun, `test "$owner" = "$ALT_LIBRT_PACKAGE"`) || strings.Contains(discoveryRun, `test "$(rpm -qf /lib64/librt.so.1)" =`) || strings.Contains(discoveryRun, "apt-cache show") || strings.Contains(discoveryRun, "apt_versions") {
		t.Errorf("discovery confuses canonical RPM evidence with an apt-cache selector:\n%s", discoveryRun)
	}
	auth := altWorkflowStep(t, publish.Steps, "Authenticate the private registry")
	if discover.Outputs["alt-librt-package"] != "${{ steps.discover.outputs.alt_librt_package }}" || discover.Outputs["alt-librt-apt-spec"] != "${{ steps.discover.outputs.alt_librt_apt_spec }}" {
		t.Fatalf("discovery output wiring = %#v", discover.Outputs)
	}
	if strings.Contains(discoveryRun, "ALT_LIBRT_APT_SPEC=\"$ALT_LIBRT_PACKAGE\"") {
		t.Errorf("discovery reuses the RPM NEVRA as an apt-rpm selector:\n%s", discoveryRun)
	}
	if verify.Env["ALT_LIBRT_APT_SPEC"] != "${{ needs.discover.outputs.alt-librt-apt-spec }}" {
		t.Fatalf("publish apt selector wiring = %#v", verify.Env)
	}
	for _, required := range []string{`[[ "$ALT_LIBRT_APT_SPEC" =~ ^[A-Za-z0-9][A-Za-z0-9+._-]*=[^[:space:]=]+$ ]]`} {
		if !strings.Contains(verifyRun, required) {
			t.Errorf("publish RPM-to-apt selector verification is missing %q:\n%s", required, verifyRun)
		}
	}
	if auth.Env["GITHUB_TOKEN"] != "${{ github.token }}" {
		t.Fatalf("registry authentication token wiring = %#v", auth.Env)
	}
	for _, step := range publish.Steps {
		if step.Name != auth.Name && strings.Contains(step.Env["GITHUB_TOKEN"], "github.token") {
			t.Fatalf("github.token is exposed outside registry authentication in %q", step.Name)
		}
	}
	qualify := altWorkflowStep(t, publish.Steps, "Qualify actual OfficeCLI in candidate image")
	if qualify.Env["TEAMKIT_OFFICECLI_KEEP_PATH"] != "${{ github.workspace }}/dist/evidence/officecli" {
		t.Fatalf("OfficeCLI evidence path wiring = %#v", qualify.Env)
	}
	qualifyRun := executableShell(qualify.Run)
	for _, required := range []string{
		"go test -tags officecli_live ./internal/service -run TestOfficeCLILive_QualifiedAssetAndMCPHandshake",
		"35316133",
		"32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8",
		"sha256sum --check --strict",
		"CGO_ENABLED=0 go test -c -tags officecli_live",
		"scripts/qualify-alt-officecli-image.sh",
	} {
		if !strings.Contains(qualifyRun, required) {
			t.Errorf("candidate qualification executable contract is missing %q:\n%s", required, qualifyRun)
		}
	}
	for _, forbidden := range []string{"apt-get", "apt-rpm", "rpm -i", "dnf", "yum", "ln -s"} {
		if strings.Contains(qualifyRun, forbidden) {
			t.Errorf("candidate qualification must not install or synthesize runtime dependencies via %q:\n%s", forbidden, qualifyRun)
		}
	}
	if workflow.Env["ALT_OFFICECLI_IMAGE"] != "ghcr.io/i437918/kit-all-team/alt-p11-officecli" {
		t.Fatalf("GHCR qualification image wiring = %#v", workflow.Env)
	}
	push := altWorkflowStep(t, publish.Steps, "Push verified qualification image")
	pushRun := executableShell(push.Run)
	for _, required := range []string{
		"qualification_image=\"$ALT_OFFICECLI_IMAGE:qualification-$EXPECTED_SHA\"",
		"docker push \"$qualification_image\"",
		"push-metadata.txt",
		"scripts/docker-push-digest.sh < push-metadata.txt",
	} {
		if !strings.Contains(pushRun, required) {
			t.Errorf("published image executable contract is missing %q:\n%s", required, pushRun)
		}
	}
	for _, forbidden := range []string{"docker buildx build", "--push", "image-metadata.json"} {
		if strings.Contains(pushRun, forbidden) {
			t.Errorf("verified artifact push must not rebuild via %q:\n%s", forbidden, pushRun)
		}
	}
	for _, forbidden := range []string{"gh release", "git tag", "git push --tags", "--build-arg GITHUB_TOKEN", "--build-arg GH_TOKEN", "imagetools inspect"} {
		if strings.Contains(executableShellFromWorkflow(workflow), forbidden) {
			t.Errorf("ALT OfficeCLI publication workflow contains forbidden executable %q", forbidden)
		}
	}
}

func TestALTOfficeCLIQualificationPublication_CarriesIndependentLDDPackageEvidence(t *testing.T) {
	workflowText := readContractFile(t, repositoryRoot(t), ".github/workflows/publish-alt-p11-officecli.yml")
	var workflow altOfficeCLIQualificationWorkflow
	if err := yaml.Unmarshal([]byte(workflowText), &workflow); err != nil {
		t.Fatalf("parse ALT OfficeCLI publication workflow: %v", err)
	}
	input, ok := workflow.On.WorkflowDispatch.Inputs["alt_ldd_package"]
	if !ok || input.Required || input.Type != "string" {
		t.Fatalf("alt_ldd_package dispatch input = %#v", input)
	}
	discover := workflow.Jobs["discover"]
	publish := workflow.Jobs["publish"]
	discovery := altWorkflowStep(t, discover.Steps, "Discover official runtime providers")
	discoveryRun := executableShell(discovery.Run)
	for _, required := range []string{
		"ALT_LDD_DISCOVERY_CANDIDATE=glibc-utils",
		`apt-get install -y "$ALT_LIBRT_DISCOVERY_CANDIDATE" "$ALT_LDD_DISCOVERY_CANDIDATE" "$ALT_ICU_DISCOVERY_CANDIDATE"`,
		`ldd_owner="$(rpm -qf /usr/bin/ldd)"`,
		"ALT_LDD_OWNER=\"$(rpm -q --qf",
		`ALT_LDD_PACKAGE="$ALT_LDD_OWNER"`,
		`test "$ALT_LDD_OWNER" = "$ALT_LDD_PACKAGE"`,
		"ALT_LDD_NAME=\"$(rpm -q --qf",
		"ALT_LDD_EVR=\"$(rpm -q --qf",
		`ALT_LDD_APT_SPEC="$ALT_LDD_NAME=$ALT_LDD_EVR"`,
		"ALT_LDD_APT_SPEC=%s",
		"ALT_LDD_PATH=/usr/bin/ldd",
	} {
		if !strings.Contains(discoveryRun, required) {
			t.Errorf("independent ldd discovery contract is missing %q:\n%s", required, discoveryRun)
		}
	}
	if discover.Outputs["alt-ldd-package"] != "${{ steps.discover.outputs.alt_ldd_package }}" || discover.Outputs["alt-ldd-apt-spec"] != "${{ steps.discover.outputs.alt_ldd_apt_spec }}" {
		t.Fatalf("ldd discovery output wiring = %#v", discover.Outputs)
	}
	for _, forbidden := range []string{`ALT_LDD_EVR="$ALT_LIBRT_EVR"`, `ALT_LDD_PACKAGE="$ALT_LIBRT_PACKAGE"`, "apt-cache show"} {
		if strings.Contains(discoveryRun, forbidden) {
			t.Errorf("ldd discovery is not independently evidenced; found %q:\n%s", forbidden, discoveryRun)
		}
	}
	verify := altWorkflowStep(t, publish.Steps, "Verify checked out commit and exact RPM evidence")
	if verify.Env["ALT_LDD_PACKAGE"] != "${{ needs.discover.outputs.alt-ldd-package }}" || verify.Env["ALT_LDD_APT_SPEC"] != "${{ needs.discover.outputs.alt-ldd-apt-spec }}" || verify.Env["REQUESTED_ALT_LDD_PACKAGE"] != "${{ inputs.alt_ldd_package }}" {
		t.Fatalf("publish ldd evidence wiring = %#v", verify.Env)
	}
	verifyRun := executableShell(verify.Run)
	for _, required := range []string{`[[ "$ALT_LDD_PACKAGE" =~ ^`, `[[ "$ALT_LDD_APT_SPEC" =~ ^[A-Za-z0-9][A-Za-z0-9+._-]*=[^[:space:]=]+$ ]]`, `test "$REQUESTED_ALT_LDD_PACKAGE" = "$ALT_LDD_PACKAGE"`} {
		if !strings.Contains(verifyRun, required) {
			t.Errorf("publish ldd verification is missing %q:\n%s", required, verifyRun)
		}
	}
	candidate := altWorkflowStep(t, publish.Steps, "Build unpushed qualification candidate")
	if candidate.Env["ALT_LDD_PACKAGE"] != "${{ needs.discover.outputs.alt-ldd-package }}" || candidate.Env["ALT_LDD_APT_SPEC"] != "${{ needs.discover.outputs.alt-ldd-apt-spec }}" {
		t.Fatalf("candidate ldd build wiring = %#v", candidate.Env)
	}
	candidateRun := executableShell(candidate.Run)
	for _, required := range []string{`--build-arg ALT_LDD_PACKAGE="$ALT_LDD_PACKAGE"`, `--build-arg ALT_LDD_APT_SPEC="$ALT_LDD_APT_SPEC"`} {
		if !strings.Contains(candidateRun, required) {
			t.Errorf("candidate ldd build argument is missing %q:\n%s", required, candidateRun)
		}
	}
	local := altWorkflowStep(t, publish.Steps, "Qualify actual OfficeCLI in candidate image")
	if local.Env["ALT_LDD_PACKAGE"] != "${{ needs.discover.outputs.alt-ldd-package }}" || !strings.Contains(local.Run, `"$ALT_LDD_PACKAGE"`) {
		t.Fatalf("local ldd qualification wiring = env=%#v run=%q", local.Env, local.Run)
	}
	remote := altWorkflowStep(t, publish.Steps, "Qualify pushed immutable digest")
	if remote.Env["ALT_LDD_PACKAGE"] != "${{ needs.discover.outputs.alt-ldd-package }}" || !strings.Contains(remote.Run, `"$ALT_LDD_PACKAGE"`) {
		t.Fatalf("remote ldd qualification wiring = env=%#v run=%q", remote.Env, remote.Run)
	}
	report := altWorkflowStep(t, publish.Steps, "Report immutable image digest")
	if report.Env["ALT_LIBRT_PACKAGE"] != "${{ needs.discover.outputs.alt-librt-package }}" || report.Env["ALT_LDD_PACKAGE"] != "${{ needs.discover.outputs.alt-ldd-package }}" {
		t.Fatalf("immutable report package evidence wiring = %#v", report.Env)
	}
	reportRun := executableShell(report.Run)
	for _, required := range []string{"ALT_LIBRT_PACKAGE=", "ALT_LDD_PACKAGE=", "IMAGE_DIGEST="} {
		if !strings.Contains(reportRun, required) {
			t.Errorf("immutable report is missing %q:\n%s", required, reportRun)
		}
	}
}

func TestALTOfficeCLIQualificationPublication_CarriesIndependentICURuntimeEvidence(t *testing.T) {
	root := repositoryRoot(t)
	workflowText := readContractFile(t, root, ".github/workflows/publish-alt-p11-officecli.yml")
	var workflow altOfficeCLIQualificationWorkflow
	if err := yaml.Unmarshal([]byte(workflowText), &workflow); err != nil {
		t.Fatalf("parse ALT OfficeCLI publication workflow: %v", err)
	}
	input, ok := workflow.On.WorkflowDispatch.Inputs["alt_icu_package"]
	if !ok || input.Required || input.Type != "string" {
		t.Fatalf("alt_icu_package dispatch input = %#v", input)
	}
	discover := workflow.Jobs["discover"]
	publish := workflow.Jobs["publish"]
	discoveryRun := executableShell(altWorkflowStep(t, discover.Steps, "Discover official runtime providers").Run)
	for _, required := range []string{
		"ALT_ICU_DISCOVERY_CANDIDATE=libicu74",
		`apt-get install -y "$ALT_LIBRT_DISCOVERY_CANDIDATE" "$ALT_LDD_DISCOVERY_CANDIDATE" "$ALT_ICU_DISCOVERY_CANDIDATE"`,
		`icu_owner="$(rpm -qf /usr/lib64/libicuuc.so.74)"`,
		"ALT_ICU_OWNER=\"$(rpm -q --qf",
		`ALT_ICU_PACKAGE="$ALT_ICU_OWNER"`,
		`test "$ALT_ICU_OWNER" = "$ALT_ICU_PACKAGE"`,
		"ALT_ICU_NAME=\"$(rpm -q --qf",
		"ALT_ICU_EVR=\"$(rpm -q --qf",
		`ALT_ICU_APT_SPEC="$ALT_ICU_NAME=$ALT_ICU_EVR"`,
		"ALT_ICU_APT_SPEC=%s",
		"ALT_ICU_PATH=/usr/lib64/libicuuc.so.74",
		"ALT_ICU_DATA_PATH=/usr/lib64/libicudata.so.74", `test -r /usr/lib64/libicuuc.so.74`, `test -r /usr/lib64/libicudata.so.74`, `icu_data_owner="$(rpm -qf /usr/lib64/libicudata.so.74)"`, `ALT_ICU_DATA_OWNER="$(rpm -q --qf`, `test "$ALT_ICU_DATA_OWNER" = "$ALT_ICU_PACKAGE"`,
	} {
		if !strings.Contains(discoveryRun, required) {
			t.Errorf("independent ICU discovery contract is missing %q:\n%s", required, discoveryRun)
		}
	}
	if discover.Outputs["alt-icu-package"] != "${{ steps.discover.outputs.alt_icu_package }}" || discover.Outputs["alt-icu-apt-spec"] != "${{ steps.discover.outputs.alt_icu_apt_spec }}" {
		t.Fatalf("ICU discovery output wiring = %#v", discover.Outputs)
	}
	for _, forbidden := range []string{`ALT_ICU_EVR="$ALT_LIBRT_EVR"`, `ALT_ICU_PACKAGE="$ALT_LIBRT_PACKAGE"`, `ALT_ICU_PACKAGE="$ALT_LDD_PACKAGE"`, "apt-cache show"} {
		if strings.Contains(discoveryRun, forbidden) {
			t.Errorf("ICU discovery is not independently evidenced; found %q:\n%s", forbidden, discoveryRun)
		}
	}
	verify := altWorkflowStep(t, publish.Steps, "Verify checked out commit and exact RPM evidence")
	if verify.Env["ALT_ICU_PACKAGE"] != "${{ needs.discover.outputs.alt-icu-package }}" || verify.Env["ALT_ICU_APT_SPEC"] != "${{ needs.discover.outputs.alt-icu-apt-spec }}" || verify.Env["REQUESTED_ALT_ICU_PACKAGE"] != "${{ inputs.alt_icu_package }}" {
		t.Fatalf("publish ICU evidence wiring = %#v", verify.Env)
	}
	verifyRun := executableShell(verify.Run)
	for _, required := range []string{`[[ "$ALT_ICU_PACKAGE" =~ ^`, `[[ "$ALT_ICU_APT_SPEC" =~ ^[A-Za-z0-9][A-Za-z0-9+._-]*=[^[:space:]=]+$ ]]`, `test "$REQUESTED_ALT_ICU_PACKAGE" = "$ALT_ICU_PACKAGE"`} {
		if !strings.Contains(verifyRun, required) {
			t.Errorf("publish ICU verification is missing %q:\n%s", required, verifyRun)
		}
	}
	candidate := altWorkflowStep(t, publish.Steps, "Build unpushed qualification candidate")
	if candidate.Env["ALT_ICU_PACKAGE"] != "${{ needs.discover.outputs.alt-icu-package }}" || candidate.Env["ALT_ICU_APT_SPEC"] != "${{ needs.discover.outputs.alt-icu-apt-spec }}" {
		t.Fatalf("candidate ICU build wiring = %#v", candidate.Env)
	}
	for _, required := range []string{`--build-arg ALT_ICU_PACKAGE="$ALT_ICU_PACKAGE"`, `--build-arg ALT_ICU_APT_SPEC="$ALT_ICU_APT_SPEC"`} {
		if !strings.Contains(candidate.Run, required) {
			t.Errorf("candidate ICU build argument is missing %q:\n%s", required, candidate.Run)
		}
	}
	for _, stepName := range []string{"Qualify actual OfficeCLI in candidate image", "Qualify pushed immutable digest"} {
		step := altWorkflowStep(t, publish.Steps, stepName)
		if step.Env["ALT_ICU_PACKAGE"] != "${{ needs.discover.outputs.alt-icu-package }}" || !strings.Contains(step.Run, `"$ALT_ICU_PACKAGE"`) {
			t.Errorf("%s ICU qualification wiring = env=%#v run=%q", stepName, step.Env, step.Run)
		}
	}
	report := altWorkflowStep(t, publish.Steps, "Report immutable image digest")
	if report.Env["ALT_ICU_PACKAGE"] != "${{ needs.discover.outputs.alt-icu-package }}" || !strings.Contains(report.Run, "ALT_ICU_PACKAGE=") {
		t.Fatalf("immutable report ICU evidence wiring = env=%#v run=%q", report.Env, report.Run)
	}

	dockerfile := readContractFile(t, root, "docker/alt-p11-officecli/Dockerfile")
	for _, required := range []string{
		"ARG ALT_ICU_PACKAGE",
		"ARG ALT_ICU_APT_SPEC",
		`apt-get install -y "$ALT_LIBRT_APT_SPEC" "$ALT_LDD_APT_SPEC" "$ALT_ICU_APT_SPEC"`,
		`icu_owner="$(rpm -qf /usr/lib64/libicuuc.so.74)"`,
		`test "$icu_owner_nevra" = "$ALT_ICU_PACKAGE"`,
		`test -r /usr/lib64/libicuuc.so.74`, `test -r /usr/lib64/libicudata.so.74`, `icu_data_owner="$(rpm -qf /usr/lib64/libicudata.so.74)"`, `test "$icu_data_owner_nevra" = "$ALT_ICU_PACKAGE"`,
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("qualification image ICU contract is missing %q:\n%s", required, dockerfile)
		}
	}
	if strings.Count(dockerfile, "apt-get install -y") != 1 {
		t.Errorf("qualification image must install all exact runtime providers in one transaction:\n%s", dockerfile)
	}

	qualification := readContractFile(t, root, "scripts/qualify-alt-officecli-image.sh")
	for _, required := range []string{
		"EXACT_ICU_NEVRA",
		`ALT_ICU_PACKAGE=${4:?exact ICU provider NEVRA is required}`,
		`icu_owner="$(rpm -qf /usr/lib64/libicuuc.so.74)"`,
		`test "$icu_owner_nevra" = "$ALT_ICU_PACKAGE"`,
		`test -r /usr/lib64/libicuuc.so.74`, `test -r /usr/lib64/libicudata.so.74`, `icu_data_owner="$(rpm -qf /usr/lib64/libicudata.so.74)"`, `test "$icu_data_owner_nevra" = "$ALT_ICU_PACKAGE"`,
	} {
		if !strings.Contains(qualification, required) {
			t.Errorf("locked-down qualification ICU proof is missing %q:\n%s", required, qualification)
		}
	}
	for _, forbidden := range []string{"apt-get", "apt-rpm", "rpm -i", "dnf", "yum", "ln -s"} {
		if strings.Contains(qualification, forbidden) {
			t.Errorf("locked-down qualification must not install or synthesize ICU via %q:\n%s", forbidden, qualification)
		}
	}
}

func TestALTOfficeCLIQualificationPublication_UsesOneLocallyAndRemotelyQualifiedImage(t *testing.T) {
	workflowText := readContractFile(t, repositoryRoot(t), ".github/workflows/publish-alt-p11-officecli.yml")
	var triggers struct {
		On map[string]yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal([]byte(workflowText), &triggers); err != nil {
		t.Fatalf("parse ALT OfficeCLI publication triggers: %v", err)
	}
	if len(triggers.On) != 1 {
		t.Fatalf("publication triggers = %#v, want workflow_dispatch only", triggers.On)
	}
	if _, ok := triggers.On["workflow_dispatch"]; !ok {
		t.Fatalf("publication workflow is missing sole workflow_dispatch trigger: %#v", triggers.On)
	}
	var workflow altOfficeCLIQualificationWorkflow
	if err := yaml.Unmarshal([]byte(workflowText), &workflow); err != nil {
		t.Fatalf("parse ALT OfficeCLI publication workflow: %v", err)
	}
	for jobName, job := range workflow.Jobs {
		if strings.Contains(job.If, "github.event_name") && job.If != "github.event_name == 'workflow_dispatch'" {
			t.Fatalf("publication job %q has a non-dispatch event condition %q", jobName, job.If)
		}
		for stepIndex, step := range job.Steps {
			if strings.Contains(step.If, "github.event_name") && step.If != "github.event_name == 'workflow_dispatch'" {
				t.Fatalf("publication job %q step %d has a non-dispatch event condition %q", jobName, stepIndex, step.If)
			}
		}
	}
	discover := workflow.Jobs["discover"]
	publish := workflow.Jobs["publish"]
	const dockerConfig = "${{ runner.temp }}/alt-p11-officecli-docker-config-${{ github.run_id }}-${{ github.run_attempt }}"
	if _, ok := publish.Env["DOCKER_CONFIG"]; ok {
		t.Fatalf("publish job must not use runner.temp in job-level env: %#v", publish.Env)
	}
	if strings.Contains(discover.If, "github.event_name == 'push'") || strings.Contains(publish.If, "github.event_name == 'push'") || publish.Needs != "discover" {
		t.Fatalf("manual-only discovery/publication dependency is unsafe: discover.if=%q publish.if=%q needs=%q", discover.If, publish.If, publish.Needs)
	}
	for jobName, job := range map[string]struct {
		Steps []altOfficeCLIWorkflowStep
	}{"discover": {discover.Steps}, "publish": {publish.Steps}} {
		checkoutCount := 0
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/checkout@") {
				checkoutCount++
				if value, ok := step.With["persist-credentials"]; !ok || value != false {
					t.Errorf("%s checkout persists credentials: %#v", jobName, step.With)
				}
			}
		}
		if checkoutCount != 1 {
			t.Errorf("%s checkout count = %d, want 1", jobName, checkoutCount)
		}
	}
	candidateIndex, candidate := altWorkflowStepIndex(t, publish.Steps, "Build unpushed qualification candidate")
	localIndex, local := altWorkflowStepIndex(t, publish.Steps, "Qualify actual OfficeCLI in candidate image")
	authIndex, auth := altWorkflowStepIndex(t, publish.Steps, "Authenticate the private registry")
	pushIndex, push := altWorkflowStepIndex(t, publish.Steps, "Push verified qualification image")
	remoteIndex, remote := altWorkflowStepIndex(t, publish.Steps, "Qualify pushed immutable digest")
	logoutIndex, logout := altWorkflowStepIndex(t, publish.Steps, "Remove private registry credentials")
	reportIndex, _ := altWorkflowStepIndex(t, publish.Steps, "Report immutable image digest")
	for stepName, step := range map[string]altOfficeCLIWorkflowStep{
		"Authenticate the private registry":   auth,
		"Push verified qualification image":   push,
		"Qualify pushed immutable digest":     remote,
		"Remove private registry credentials": logout,
	} {
		if step.Env["DOCKER_CONFIG"] != dockerConfig {
			t.Errorf("%s Docker credential configuration = %#v, want %q", stepName, step.Env, dockerConfig)
		}
	}
	if !(candidateIndex < localIndex && localIndex < authIndex && authIndex < pushIndex && pushIndex < remoteIndex && remoteIndex < logoutIndex && logoutIndex < reportIndex) {
		t.Fatalf("publication order is unsafe: candidate=%d local=%d auth=%d push=%d remote=%d logout=%d report=%d", candidateIndex, localIndex, authIndex, pushIndex, remoteIndex, logoutIndex, reportIndex)
	}
	candidateRun := executableShell(candidate.Run)
	for _, required := range []string{"docker build", "--platform linux/amd64", "--tag \"$qualification_image\"", "ALT_LIBRT_PACKAGE", "ALT_LIBRT_APT_SPEC", `--build-arg ALT_LIBRT_APT_SPEC="$ALT_LIBRT_APT_SPEC"`} {
		if !strings.Contains(candidateRun, required) {
			t.Errorf("single local image build is missing %q:\n%s", required, candidateRun)
		}
	}
	for _, forbidden := range []string{"docker buildx", "type=oci", "qualification.oci", "docker load", "--push"} {
		if strings.Contains(candidateRun, forbidden) {
			t.Errorf("single local image build contains forbidden %q:\n%s", forbidden, candidateRun)
		}
	}
	localRun := executableShell(local.Run)
	if !strings.Contains(localRun, "scripts/qualify-alt-officecli-image.sh") || !strings.Contains(localRun, "$qualification_image") {
		t.Errorf("local qualification does not exercise the reusable proof:\n%s", localRun)
	}
	pushRun := executableShell(push.Run)
	for _, required := range []string{"docker push \"$qualification_image\"", "tee push-metadata.txt", "scripts/docker-push-digest.sh < push-metadata.txt", "printf 'image_digest=%s\\n' \"$IMAGE_DIGEST\""} {
		if !strings.Contains(pushRun, required) {
			t.Errorf("push receipt contract is missing %q:\n%s", required, pushRun)
		}
	}
	if strings.Contains(pushRun, "docker build") {
		t.Errorf("push step rebuilds the qualified image:\n%s", pushRun)
	}
	remoteRun := executableShell(remote.Run)
	for _, required := range []string{"docker pull \"$IMAGE_DIGEST\"", "docker image inspect --format '{{.Id}}'", "test \"$remote_image_id\" = \"$local_image_id\"", "scripts/qualify-alt-officecli-image.sh", "$IMAGE_DIGEST"} {
		if !strings.Contains(remoteRun, required) {
			t.Errorf("remote digest qualification is missing %q:\n%s", required, remoteRun)
		}
	}
	authRun := executableShell(auth.Run)
	for _, required := range []string{
		`expected_docker_config="${RUNNER_TEMP:?}/alt-p11-officecli-docker-config-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"`,
		`test "$DOCKER_CONFIG" = "$expected_docker_config"`,
		`test ! -e "$DOCKER_CONFIG"`,
		`install -d -m 0700 "$DOCKER_CONFIG"`,
		"docker login ghcr.io",
	} {
		if !strings.Contains(authRun, required) {
			t.Errorf("registry authentication does not create/use isolated credentials %q:\n%s", required, authRun)
		}
	}
	cleanupRun := executableShell(logout.Run)
	for _, required := range []string{
		`expected_docker_config="${RUNNER_TEMP:?}/alt-p11-officecli-docker-config-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"`,
		`test -n "$DOCKER_CONFIG"`,
		`test "$DOCKER_CONFIG" = "$expected_docker_config"`,
		`rm -rf -- "$DOCKER_CONFIG"`,
		`test ! -e "$DOCKER_CONFIG"`,
	} {
		if !strings.Contains(cleanupRun, required) {
			t.Errorf("registry credential cleanup is missing %q:\n%s", required, cleanupRun)
		}
	}
	if logout.If != "always()" || !strings.Contains(cleanupRun, "docker logout ghcr.io") || strings.Contains(cleanupRun, `rm -rf -- "$DOCKER_CONFIG" ||`) {
		t.Errorf("registry credentials are not removed fail-closed on every path: if=%q run=%q", logout.If, cleanupRun)
	}
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type altOfficeCLIQualificationWorkflow struct {
	On struct {
		WorkflowDispatch struct {
			Inputs map[string]struct {
				Required bool   `yaml:"required"`
				Type     string `yaml:"type"`
			} `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
		Push struct {
			Branches []string `yaml:"branches"`
			Paths    []string `yaml:"paths"`
		} `yaml:"push"`
	} `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Env         map[string]string `yaml:"env"`
	Jobs        map[string]struct {
		Outputs     map[string]string          `yaml:"outputs"`
		Needs       string                     `yaml:"needs"`
		If          string                     `yaml:"if"`
		Env         map[string]string          `yaml:"env"`
		Permissions map[string]string          `yaml:"permissions"`
		Steps       []altOfficeCLIWorkflowStep `yaml:"steps"`
	} `yaml:"jobs"`
}

type altOfficeCLIWorkflowStep struct {
	Uses string            `yaml:"uses"`
	If   string            `yaml:"if"`
	With map[string]any    `yaml:"with"`
	Name string            `yaml:"name"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
}

func altWorkflowStep(t *testing.T, steps []altOfficeCLIWorkflowStep, name string) altOfficeCLIWorkflowStep {
	t.Helper()
	_, step := altWorkflowStepIndex(t, steps, name)
	return step
}

func altWorkflowStepIndex(t *testing.T, steps []altOfficeCLIWorkflowStep, name string) (int, altOfficeCLIWorkflowStep) {
	t.Helper()
	for index, step := range steps {
		if step.Name == name {
			return index, step
		}
	}
	t.Fatalf("ALT OfficeCLI publication workflow has no step %q", name)
	return -1, altOfficeCLIWorkflowStep{}
}

func executableShell(source string) string {
	var executable []string
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(shellExecutableLine(line))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		executable = append(executable, trimmed)
	}
	return strings.Join(executable, "\n")
}

func executableShellFromWorkflow(workflow altOfficeCLIQualificationWorkflow) string {
	var commands []string
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			commands = append(commands, executableShell(step.Run))
		}
	}
	return strings.Join(commands, "\n")
}

type officeCLIWorkflowContract struct {
	On struct {
		WorkflowDispatch struct {
			Inputs map[string]struct {
				Required bool   `yaml:"required"`
				Type     string `yaml:"type"`
			} `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
	Jobs map[string]officeCLIWorkflowJob `yaml:"jobs"`
}

type officeCLIWorkflowJob struct {
	Needs       string            `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Env         map[string]string `yaml:"env"`
	Strategy    struct {
		Matrix struct {
			Include []map[string]string `yaml:"include"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []officeCLIWorkflowStep `yaml:"steps"`
}

type officeCLIWorkflowStep struct {
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
	With map[string]any    `yaml:"with"`
}

func TestOfficeCLILiveWorkflowGraph_BindsExistingJobsAndMatrix(t *testing.T) {
	workflowText := readContractFile(t, repositoryRoot(t), ".github/workflows/ci.yml")
	var workflow officeCLIWorkflowContract
	if err := yaml.Unmarshal([]byte(workflowText), &workflow); err != nil {
		t.Fatalf("parse OfficeCLI CI workflow: %v", err)
	}
	input, ok := workflow.On.WorkflowDispatch.Inputs["expected_sha"]
	if !ok || !input.Required || input.Type != "string" || len(workflow.On.WorkflowDispatch.Inputs) != 1 {
		t.Fatalf("workflow_dispatch expected_sha input = %#v, present=%t; want sole required string", input, ok)
	}
	if len(workflow.Jobs) != 3 {
		t.Fatalf("Task 6 must reuse exactly three existing CI jobs, got %d", len(workflow.Jobs))
	}
	build := requireOfficeCLIWorkflowJob(t, workflow, "build-candidate")
	native := requireOfficeCLIWorkflowJob(t, workflow, "native")
	alt := requireOfficeCLIWorkflowJob(t, workflow, "alt-p11-userspace")
	for name, job := range map[string]officeCLIWorkflowJob{"native": native, "alt-p11-userspace": alt} {
		if job.Needs != "build-candidate" {
			t.Errorf("OfficeCLI live consumer %s needs %q, want build-candidate", name, job.Needs)
		}
	}

	preflightIndex, preflight := requireOfficeCLIWorkflowStep(t, build, "Bind trusted dispatch to exact commit")
	checkoutIndex := officeCLIWorkflowUsesIndex(build, "actions/checkout@")
	if preflightIndex < 0 || checkoutIndex <= preflightIndex || preflight.If != "github.event_name == 'workflow_dispatch'" {
		t.Fatalf("build-candidate preflight is not dispatch-only before checkout: preflight=%d checkout=%d if=%q", preflightIndex, checkoutIndex, preflight.If)
	}
	if preflight.Env["EXPECTED_SHA"] != "${{ inputs.expected_sha }}" ||
		!strings.Contains(preflight.Run, `test -n "$EXPECTED_SHA"`) ||
		!strings.Contains(preflight.Run, `test "$GITHUB_SHA" = "$EXPECTED_SHA"`) {
		t.Fatalf("build-candidate preflight does not fail closed on exact SHA: %#v", preflight)
	}

	wantMatrix := []map[string]string{
		{"runner": "windows-2025", "binary": "teamkit-v0.1.5-windows-amd64.exe", "officecli_asset": "officecli-win-x64.exe"},
		{"runner": "ubuntu-24.04", "binary": "teamkit-v0.1.5-linux-amd64", "officecli_asset": "officecli-linux-x64"},
		{"runner": "macos-15-intel", "binary": "teamkit-v0.1.5-darwin-amd64", "officecli_asset": "officecli-mac-x64"},
		{"runner": "macos-15", "binary": "teamkit-v0.1.5-darwin-arm64", "officecli_asset": "officecli-mac-arm64"},
	}
	if !reflect.DeepEqual(native.Strategy.Matrix.Include, wantMatrix) {
		t.Fatalf("native matrix = %#v, want exact four-runner matrix %#v", native.Strategy.Matrix.Include, wantMatrix)
	}
	nativeHermeticIndex, _ := requireOfficeCLIWorkflowStep(t, native, "Verify source and exact platform binary")
	nativeLiveIndex, nativeLive := requireOfficeCLIWorkflowStep(t, native, "Verify qualified OfficeCLI runtime and MCP handshake")
	if nativeLiveIndex <= nativeHermeticIndex || nativeLive.If != "github.event_name == 'workflow_dispatch'" {
		t.Fatalf("native live step placement/condition unsafe: hermetic=%d live=%d if=%q", nativeHermeticIndex, nativeLiveIndex, nativeLive.If)
	}
	if nativeLive.Env["TEAMKIT_OFFICECLI_RUNNER_ENVIRONMENT"] != "${{ runner.environment }}" {
		t.Fatalf("native Windows disposable provenance is not bound to runner.environment: %#v", nativeLive.Env)
	}
	_, trust := requireOfficeCLIWorkflowStep(t, native, "Verify OfficeCLI Developer ID signature and notarization policy")
	if trust.If != "github.event_name == 'workflow_dispatch' && startsWith(matrix.runner, 'macos-')" ||
		!strings.Contains(trust.Run, "codesign --verify --strict --verbose=2") ||
		!strings.Contains(trust.Run, "spctl --assess --type execute --verbose=4") {
		t.Fatalf("macOS trust step is not bound to both and only mac matrix lanes: %#v", trust)
	}
	for _, required := range []string{
		"LC_ALL=C",
		"spctl_output_file=$(mktemp)",
		"spctl_expected_file=$(mktemp)",
		`head -c 4097 >"$spctl_output_file"`,
		`spctl_status=${PIPESTATUS[0]}`,
		`spctl_output_size=$(wc -c <"$spctl_output_file" | tr -d '[:space:]')`,
		`test "$spctl_output_size" -le 4096`,
		`test "$spctl_status" -eq 3`,
		`for spctl_prefix in "" "$OFFICECLI_EVIDENCE: "; do`,
		`cmp -s -- "$spctl_output_file" "$spctl_expected_file"`,
		"rejected (the code is valid but does not seem to be an app)",
		"OFFICECLI_SPCTL_BARE_MACHO_NOT_APP",
	} {
		if !strings.Contains(trust.Run, required) {
			t.Errorf("macOS trust step does not contain exact bounded spctl exception %q:\n%s", required, trust.Run)
		}
	}
	if strings.Contains(trust.Run, "spctl_capture=") {
		t.Fatalf("macOS trust step must not pass raw spctl bytes through command substitution:\n%s", trust.Run)
	}
	if strings.Count(trust.Run, "spctl --assess --type execute --verbose=4") != 1 ||
		strings.Index(trust.Run, "codesign --verify --strict --verbose=2") >= strings.Index(trust.Run, "spctl --assess --type execute --verbose=4") {
		t.Fatalf("macOS trust step must assess the same exact asset once and only after strict codesign verification:\n%s", trust.Run)
	}

	_, altLive := requireOfficeCLIWorkflowStep(t, alt, "Verify qualified OfficeCLI in pinned ALT p11 userspace")
	if altLive.If != "github.event_name == 'workflow_dispatch'" ||
		!strings.Contains(altLive.Run, "CGO_ENABLED=0 go test -c -tags officecli_live") ||
		!strings.Contains(altLive.Run, "scripts/alt-container-smoke.sh") {
		t.Fatalf("ALT OfficeCLI live step is not the dispatch-only compiled common test: %#v", altLive)
	}
	if alt.Permissions["contents"] != "read" || alt.Permissions["packages"] != "read" {
		t.Fatalf("ALT private-image permissions = %#v, want contents/packages read", alt.Permissions)
	}
	altCheckoutIndex := officeCLIWorkflowUsesIndex(alt, "actions/checkout@")
	if altCheckoutIndex < 0 {
		t.Fatal("ALT job is missing actions/checkout")
	}
	if got := alt.Steps[altCheckoutIndex].With["persist-credentials"]; got != false {
		t.Fatalf("ALT checkout persist-credentials = %#v, want false so github.token is not placed in Git configuration before public smoke", got)
	}
	const (
		altImage        = "ghcr.io/i437918/kit-all-team/alt-p11-officecli@sha256:5ee493c6c7edbdb8d68fb0ab9af2847bae855c9042bc5f13f5fd6b3d0965a825"
		baseImage       = "registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea"
		altDockerConfig = "${{ runner.temp }}/alt-p11-officecli-read-docker-config-${{ github.run_id }}-${{ github.run_attempt }}"
	)
	for name, value := range alt.Env {
		upperName := strings.ToUpper(name)
		upperValue := strings.ToUpper(value)
		if name == "DOCKER_CONFIG" || name == "ALT_IMAGE" || strings.HasPrefix(name, "TEAMKIT_OFFICECLI_ALT_") ||
			(strings.Contains(upperName, "GHCR") && strings.Contains(upperName, "TOKEN")) ||
			(strings.Contains(upperValue, "GHCR") && strings.Contains(upperValue, "TOKEN")) ||
			strings.Contains(strings.ToLower(value), "github.token") {
			t.Fatalf("ALT job-level environment exposes private-image material %q=%q", name, value)
		}
	}
	authIndex, auth := requireOfficeCLIWorkflowStep(t, alt, "Authenticate private ALT qualification registry")
	smokeIndex, smoke := requireOfficeCLIWorkflowStep(t, alt, "Run exact candidate in pinned ALT p11 userspace")
	liveIndex, live := requireOfficeCLIWorkflowStep(t, alt, "Verify qualified OfficeCLI in pinned ALT p11 userspace")
	cleanupIndex, cleanup := requireOfficeCLIWorkflowStep(t, alt, "Remove private ALT registry credentials")
	if !(smokeIndex < authIndex && authIndex < liveIndex && liveIndex < cleanupIndex) {
		t.Fatalf("ALT private-image step order is unsafe: smoke=%d auth=%d live=%d cleanup=%d", smokeIndex, authIndex, liveIndex, cleanupIndex)
	}
	if auth.If != "github.event_name == 'workflow_dispatch'" || live.If != auth.If {
		t.Fatalf("ALT private-image steps are not dispatch-only: auth=%q live=%q", auth.If, live.If)
	}
	if cleanup.If != "always() && github.event_name == 'workflow_dispatch'" {
		t.Fatalf("ALT registry cleanup condition = %q", cleanup.If)
	}
	if smoke.Env["ALT_IMAGE"] != baseImage {
		t.Fatalf("ALT base smoke image = %q, want official public digest %q", smoke.Env["ALT_IMAGE"], baseImage)
	}
	if _, ok := smoke.Env["DOCKER_CONFIG"]; ok {
		t.Fatalf("ALT base smoke must not use isolated Docker config: %#v", smoke.Env)
	}
	for name, step := range map[string]officeCLIWorkflowStep{"auth": auth, "live": live, "cleanup": cleanup} {
		if step.Env["DOCKER_CONFIG"] != altDockerConfig {
			t.Errorf("%s isolated Docker config = %q, want %q", name, step.Env["DOCKER_CONFIG"], altDockerConfig)
		}
	}
	for _, step := range alt.Steps {
		if step.Name != auth.Name && step.Name != live.Name && step.Name != cleanup.Name {
			if _, ok := step.Env["DOCKER_CONFIG"]; ok {
				t.Fatalf("ALT isolated Docker config is exposed outside private-image steps in %q: %#v", step.Name, step.Env)
			}
		}
		if step.Name != smoke.Name && step.Name != live.Name && step.Env["ALT_IMAGE"] != "" {
			t.Fatalf("ALT image is exposed outside public smoke/private live steps in %q: %#v", step.Name, step.Env)
		}
		for _, name := range []string{
			"TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE",
			"TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE",
			"TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE",
		} {
			if step.Name != live.Name && step.Env[name] != "" {
				t.Fatalf("ALT provider evidence %q is exposed outside the live private-image step in %q: %#v", name, step.Name, step.Env)
			}
		}
	}
	if live.Env["ALT_IMAGE"] != altImage {
		t.Fatalf("ALT live image = %q, want immutable digest %q", live.Env["ALT_IMAGE"], altImage)
	}
	if live.Env["TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE"] != "glibc-pthread-6:2.38.0.223.f053ff-alt1.p11.1.x86_64" ||
		live.Env["TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE"] != "glibc-utils-6:2.38.0.223.f053ff-alt1.p11.1.x86_64" ||
		live.Env["TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE"] != "libicu74-1:7.4.2-alt1.x86_64" {
		t.Fatalf("ALT live provider evidence = %#v", live.Env)
	}
	if auth.Env["GHCR_TOKEN"] != "${{ github.token }}" || auth.Env["GHCR_USER"] != "${{ github.actor }}" ||
		!strings.Contains(auth.Run, `printf '%s' "$GHCR_TOKEN" | docker login ghcr.io --username "$GHCR_USER" --password-stdin`) {
		t.Fatalf("ALT private registry auth is not password-stdin bounded: %#v", auth)
	}
	for _, required := range []string{
		`expected_docker_config="${RUNNER_TEMP:?}/alt-p11-officecli-read-docker-config-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"`,
		`test "$DOCKER_CONFIG" = "$expected_docker_config"`,
		`test ! -e "$DOCKER_CONFIG"`,
		`install -d -m 0700 "$DOCKER_CONFIG"`,
	} {
		if !strings.Contains(auth.Run, required) {
			t.Errorf("ALT registry auth is missing %q:\n%s", required, auth.Run)
		}
	}
	for _, required := range []string{
		`expected_docker_config="${RUNNER_TEMP:?}/alt-p11-officecli-read-docker-config-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"`,
		`test "$DOCKER_CONFIG" = "$expected_docker_config"`,
		`case "$DOCKER_CONFIG" in`,
		"docker logout ghcr.io >/dev/null 2>&1 || true",
		`rm -rf -- "$DOCKER_CONFIG"`,
		`test ! -e "$DOCKER_CONFIG"`,
	} {
		if !strings.Contains(cleanup.Run, required) {
			t.Errorf("ALT registry cleanup is missing %q:\n%s", required, cleanup.Run)
		}
	}
	for _, step := range alt.Steps {
		if step.Name != auth.Name && (strings.Contains(step.Env["GHCR_TOKEN"], "github.token") || strings.Contains(step.Run, "github.token")) {
			t.Fatalf("github.token is exposed outside ALT registry authentication in %q", step.Name)
		}
	}

	const liveCommand = "go test -tags officecli_live ./internal/service -run TestOfficeCLILive_QualifiedAssetAndMCPHandshake -count=1 -timeout=3m"
	liveJobs := map[string]int{}
	trustJobs := map[string]int{}
	for jobName, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Run, liveCommand) {
				liveJobs[jobName]++
			}
			if strings.Contains(step.Run, "codesign --verify") || strings.Contains(step.Run, "spctl --assess") {
				trustJobs[jobName]++
			}
		}
	}
	if !reflect.DeepEqual(liveJobs, map[string]int{"native": 1, "alt-p11-userspace": 1}) {
		t.Fatalf("OfficeCLI live command placement = %#v, want native and ALT once each", liveJobs)
	}
	if !reflect.DeepEqual(trustJobs, map[string]int{"native": 1}) {
		t.Fatalf("macOS trust command placement = %#v, want native matrix only", trustJobs)
	}
}

func TestOfficeCLILiveWorkflow_SPCTLExceptionComparesBoundedRawBytes(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise the macOS trust policy step")
	}

	workflowText := readContractFile(t, repositoryRoot(t), ".github/workflows/ci.yml")
	var workflow officeCLIWorkflowContract
	if err := yaml.Unmarshal([]byte(workflowText), &workflow); err != nil {
		t.Fatalf("parse OfficeCLI CI workflow: %v", err)
	}
	_, trust := requireOfficeCLIWorkflowStep(t, requireOfficeCLIWorkflowJob(t, workflow, "native"), "Verify OfficeCLI Developer ID signature and notarization policy")

	fixture := t.TempDir()
	fakeBin := filepath.Join(fixture, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "codesign"), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	const fakeSPCTL = `#!/usr/bin/env bash
message='rejected (the code is valid but does not seem to be an app)'
case "$SPCTL_FIXTURE" in
  bare) printf '%s' "$message" ;;
  bare-lf) printf '%s\n' "$message" ;;
  prefixed) printf '%s: %s' "$OFFICECLI_EVIDENCE" "$message" ;;
  prefixed-lf) printf '%s: %s\n' "$OFFICECLI_EVIDENCE" "$message" ;;
  nul) printf 'rejected (the code is valid but does not seem to be an \000app)' ;;
  oversize) printf '%*s' 4097 '' ;;
  other) printf '%sx' "$message" ;;
  *) exit 97 ;;
esac
exit 3
`
	if err := os.WriteFile(filepath.Join(fakeBin, "spctl"), []byte(fakeSPCTL), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name    string
		fixture string
		wantOK  bool
	}{
		{name: "bare exact bytes", fixture: "bare", wantOK: true},
		{name: "bare exact bytes with terminal LF", fixture: "bare-lf", wantOK: true},
		{name: "prefixed exact bytes", fixture: "prefixed", wantOK: true},
		{name: "prefixed exact bytes with terminal LF", fixture: "prefixed-lf", wantOK: true},
		{name: "embedded NUL", fixture: "nul", wantOK: false},
		{name: "4097 byte output", fixture: "oversize", wantOK: false},
		{name: "other trailing byte", fixture: "other", wantOK: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command(bash, "-c", trust.Run)
			cmd.Env = append(os.Environ(),
				"PATH="+releaseTestBashPath(t, bash, fakeBin)+":/usr/bin:/bin",
				"OFFICECLI_EVIDENCE=/tmp/teamkit-officecli-fixture",
				"SPCTL_FIXTURE="+test.fixture,
			)
			output, err := cmd.CombinedOutput()
			if test.wantOK && err != nil {
				t.Fatalf("exact raw spctl classification failed: %v\n%s", err, output)
			}
			if !test.wantOK && err == nil {
				t.Fatalf("malformed raw spctl output unexpectedly passed:\n%s", output)
			}
		})
	}
}

func requireOfficeCLIWorkflowJob(t *testing.T, workflow officeCLIWorkflowContract, name string) officeCLIWorkflowJob {
	t.Helper()
	job, ok := workflow.Jobs[name]
	if !ok {
		t.Fatalf("workflow does not contain existing %s job", name)
	}
	return job
}

func requireOfficeCLIWorkflowStep(t *testing.T, job officeCLIWorkflowJob, name string) (int, officeCLIWorkflowStep) {
	t.Helper()
	for index, step := range job.Steps {
		if step.Name == name {
			return index, step
		}
	}
	t.Fatalf("workflow job does not contain step %q", name)
	return -1, officeCLIWorkflowStep{}
}

func officeCLIWorkflowUsesIndex(job officeCLIWorkflowJob, prefix string) int {
	for index, step := range job.Steps {
		if strings.HasPrefix(step.Uses, prefix) {
			return index
		}
	}
	return -1
}

func TestOfficeCLILiveRuntimeCommands_AreClosed(t *testing.T) {
	root := repositoryRoot(t)
	commands := map[string]int{}
	for _, name := range []string{"internal/service/officecli.go", "internal/service/officecli_live_test.go"} {
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, filepath.Join(root, filepath.FromSlash(name)), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				argument, matched := officeCLIRuntimeArgvExpression(function.Name.Name, call)
				if !matched {
					return true
				}
				argv, err := officeCLIStringSliceLiteral(argument)
				if err != nil {
					t.Fatalf("%s %s runtime argv: %v", name, function.Name.Name, err)
				}
				commands[strings.Join(argv, " ")]++
				return true
			})
		}
	}
	want := map[string]int{
		"config autoUpdate false": 1,
		"config autoUpdate":       1,
		"--version":               1,
		"mcp":                     1,
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("OfficeCLI runtime command set = %#v, want closed config/version/bare-mcp set %#v", commands, want)
	}
}

func officeCLIRuntimeArgvExpression(functionName string, call *ast.CallExpr) (ast.Expr, bool) {
	switch target := call.Fun.(type) {
	case *ast.Ident:
		if (target.Name == "capture" || target.Name == "officeCLICapture") && len(call.Args) == 3 {
			return call.Args[2], true
		}
	case *ast.SelectorExpr:
		if target.Sel.Name == "CommandContext" {
			if packageName, ok := target.X.(*ast.Ident); ok && packageName.Name == "exec" {
				if functionName == "officeCLICapture" {
					return nil, false
				}
				if len(call.Args) == 3 {
					return call.Args[2], true
				}
			}
		}
		if target.Sel.Name == "Run" && len(call.Args) == 3 {
			if runner, ok := target.X.(*ast.SelectorExpr); ok && runner.Sel.Name == "run" {
				return call.Args[2], true
			}
		}
	}
	return nil, false
}

func officeCLIStringSliceLiteral(expression ast.Expr) ([]string, error) {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return nil, &officeCLIWorkflowPolicyError{"runtime argv is not a literal []string"}
	}
	array, ok := literal.Type.(*ast.ArrayType)
	if !ok || array.Len != nil {
		return nil, &officeCLIWorkflowPolicyError{"runtime argv is not a literal []string"}
	}
	element, stringElements := array.Elt.(*ast.Ident)
	if !stringElements || element.Name != "string" {
		return nil, &officeCLIWorkflowPolicyError{"runtime argv is not a literal []string"}
	}
	values := make([]string, 0, len(literal.Elts))
	for _, element := range literal.Elts {
		basic, ok := element.(*ast.BasicLit)
		if !ok || basic.Kind != token.STRING {
			return nil, &officeCLIWorkflowPolicyError{"runtime argv contains a non-string literal"}
		}
		value, err := strconv.Unquote(basic.Value)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

type officeCLIWorkflowPolicyError struct{ message string }

func (e *officeCLIWorkflowPolicyError) Error() string { return e.message }

func TestFinalReleaseValidation_DoesNotRequireInformationalNetworkOrALTWorkflows(t *testing.T) {
	root := repositoryRoot(t)
	release := readContractFile(t, root, ".github/workflows/release.yml")
	for _, path := range []string{
		".github/workflows/nightly.yml",
		".github/workflows/alt-native.yml",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Errorf("informational workflow %s must remain available: %v", path, err)
		}
	}
	for _, forbidden := range []string{
		"qemu_run_id:", "QEMU_RUN_ID", ".github/workflows/nightly.yml", "alt-qemu-evidence", "ALT_VM_VERIFIED",
		"alt-native.yml", "ALT_NATIVE_RUNNER_VERIFIED",
	} {
		if strings.Contains(release, forbidden) {
			t.Errorf("final validation requires informational evidence via %q", forbidden)
		}
	}
}

func TestGitLabCI_SkipsTagPipelinesBeforeJobs(t *testing.T) {
	contract := readContractFile(t, repositoryRoot(t), ".gitlab-ci.yml")
	for _, required := range []string{
		"workflow:",
		"rules:",
		`if: '$CI_COMMIT_TAG'`,
		"when: never",
		"when: always",
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("GitLab CI tag-skip contract does not contain %q", required)
		}
	}
	rules := strings.Index(contract, "workflow:")
	stages := strings.Index(contract, "stages:")
	if rules < 0 || stages <= rules {
		t.Fatalf("GitLab workflow rules must be evaluated before stages/jobs: workflow=%d stages=%d", rules, stages)
	}
}

func TestCandidateWorkflows_PromoteOneDigestAddressedArtifactWithoutRebuild(t *testing.T) {
	root := repositoryRoot(t)
	ci := readContractFile(t, root, ".github/workflows/ci.yml")
	nightly := readContractFile(t, root, ".github/workflows/nightly.yml")
	release := readContractFile(t, root, ".github/workflows/release.yml")

	for _, required := range []string{
		"id: upload-candidate",
		`candidate-digest: ${{ format('sha256:{0}', steps.upload-candidate.outputs.artifact-digest) }}`,
		"TEAMKIT_TEST_BINARY",
		"shasum -a 256 --check SHA256SUMS",
		"native-evidence-${{ matrix.runner }}",
		"alt-userspace-evidence",
	} {
		if !strings.Contains(ci, required) {
			t.Errorf("CI does not contain %q", required)
		}
	}

	for _, required := range []string{
		"ci_run_id:",
		"candidate_digest:",
		"run-id: ${{ needs.resolve-candidate.outputs.ci-run-id }}",
		"CANDIDATE_ARTIFACT_DIGEST",
		"ALT_VM_VERIFIED",
	} {
		if !strings.Contains(nightly, required) {
			t.Errorf("nightly workflow does not contain %q", required)
		}
	}
	if strings.Contains(nightly, "scripts/build.sh") || strings.Contains(nightly, "go build") {
		t.Error("nightly workflow rebuilds instead of testing the CI candidate")
	}

	for _, required := range []string{
		"ci_run_id:",
		"candidate_digest:",
		"actions/runs/$CI_RUN_ID",
		".github/workflows/ci.yml",
		"run-id: ${{ inputs.ci_run_id }}",
		"CANDIDATE_ARTIFACT_DIGEST",
		"refs/heads/main",
		"FINAL_RELEASE_VALIDATION_OK",
	} {
		if !strings.Contains(release, required) {
			t.Errorf("release workflow does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"actions/setup-go@", "scripts/build.sh", "go build", "go test", "go vet"} {
		if strings.Contains(release, forbidden) {
			t.Errorf("release workflow rebuilds or retests source via %q", forbidden)
		}
	}
}

func TestSecurityAuditGate_CoversRepositoryCandidatesEvidenceAndFinalArchive(t *testing.T) {
	root := repositoryRoot(t)
	ci := readContractFile(t, root, ".github/workflows/ci.yml")
	nightly := readContractFile(t, root, ".github/workflows/nightly.yml")
	release := readContractFile(t, root, ".github/workflows/release.yml")

	for _, required := range []string{
		"teamkit-security-audit",
		"--repository . --path dist",
		"security-tool/security-auditor-linux-amd64 \\",
		"security-auditor-linux-amd64",
		"security-auditor-tool",
		"SECURITY-AUDIT.json",
		"security-audit-${{ matrix.runner }}.json",
		"security-audit-alt-userspace.json",
	} {
		if !strings.Contains(ci, required) {
			t.Errorf("CI security gate does not contain %q", required)
		}
	}
	buildAuditor := strings.Index(ci, "- name: Build release security auditor")
	auditCandidate := strings.Index(ci, "- name: Audit repository, history, and exact candidate set")
	uploadCandidate := strings.Index(ci, "- id: upload-candidate")
	if buildAuditor < 0 || auditCandidate <= buildAuditor || uploadCandidate <= auditCandidate {
		t.Fatalf("auditor build/candidate audit/upload ordering is unsafe: build=%d audit=%d upload=%d", buildAuditor, auditCandidate, uploadCandidate)
	}
	for _, required := range []string{
		"teamkit-security-audit",
		"security-audit-macos-current.json",
		"security-audit-alt-qemu.json",
	} {
		if !strings.Contains(nightly, required) {
			t.Errorf("nightly security gate does not contain %q", required)
		}
	}
	for _, required := range []string{
		"security-auditor-tool",
		"--path dist --path evidence",
		"dist/SECURITY-AUDIT.json",
		"dist/RELEASE-SECURITY-AUDIT.json",
	} {
		if !strings.Contains(release, required) {
			t.Errorf("release security gate does not contain %q", required)
		}
	}
	archive := strings.Index(release, "tar -czf dist/RELEASE-EVIDENCE.tar.gz")
	audit := strings.LastIndex(release, "--path dist --path evidence")
	upload := strings.LastIndex(release, "actions/upload-artifact@")
	if archive < 0 || audit <= archive || upload <= audit {
		t.Fatalf("release archive/security/upload ordering is unsafe: archive=%d audit=%d upload=%d", archive, audit, upload)
	}
}

func TestCandidateSecurityAudits_ScopeHistoryToExactCommit(t *testing.T) {
	root := repositoryRoot(t)
	github := readContractFile(t, root, ".github/workflows/ci.yml")
	gitlab := readContractFile(t, root, ".gitlab-ci.yml")

	if got := strings.Count(github, `--history-ref "$GITHUB_SHA"`); got != 3 {
		t.Fatalf("GitHub candidate audits must scope all three repository scans to exact GITHUB_SHA, got %d", got)
	}
	if got := strings.Count(gitlab, `--history-ref "$CI_COMMIT_SHA"`); got != 1 {
		t.Fatalf("GitLab candidate audit must scope repository history to exact CI_COMMIT_SHA, got %d", got)
	}
	for name, contract := range map[string]string{"GitHub": github, "GitLab": gitlab} {
		if strings.Contains(contract, "--no-history") || strings.Contains(contract, "--skip-history") {
			t.Errorf("%s candidate audit disables history instead of scoping it", name)
		}
	}
}

func TestBoundedReleasePublisher_StaticContract(t *testing.T) {
	root := repositoryRoot(t)
	files := []string{
		"scripts/publish-v0.1.3.ps1",
		"scripts/release/BoundedRelease.psm1",
		"scripts/release/test-bounded-release.ps1",
	}
	var publisher strings.Builder
	for _, path := range files {
		contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read publisher contract %s: %v", path, err)
		}
		publisher.Write(contents)
	}
	text := publisher.String()
	for _, required := range []string{
		"v0.1.3", "180", "12087", "main", "master", "GIT_TERMINAL_PROMPT", "124", "1200",
		"artifacts/keep", "protected_tags", "repository/tags", "/releases",
		"ConvertTo-Json -Depth 8 -Compress",
		"teamkit-v0.1.3-windows-amd64.exe", "teamkit-v0.1.3-linux-amd64",
		"teamkit-v0.1.3-darwin-amd64", "teamkit-v0.1.3-darwin-arm64",
		"SHA256SUMS", "SECURITY-AUDIT.json", "Hermes-Setup.exe", "certs.zip",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("publisher contract is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Read-Host", "--force", "reset --hard", "Remove-Item", "DELETE ", "$_ | ConvertTo-Json",
		"$GH_TOKEN@", "$GITLAB_TOKEN@", "?private_token=", "?access_token=",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("publisher contract contains forbidden %q", forbidden)
		}
	}
}

func TestV015PackageFirstPublisher_StaticContract(t *testing.T) {
	root := repositoryRoot(t)
	var publisher strings.Builder
	for _, path := range []string{
		"scripts/publish-v0.1.5.ps1",
		"scripts/release/BoundedRelease.psm1",
		"scripts/release/test-bounded-release.ps1",
	} {
		publisher.WriteString(readContractFile(t, root, path))
	}
	text := publisher.String()
	for _, required := range []string{
		"v0.1.5",
		"teamkit-v0.1.5-windows-amd64.exe",
		"teamkit-v0.1.5-linux-amd64",
		"teamkit-v0.1.5-darwin-amd64",
		"teamkit-v0.1.5-darwin-arm64",
		"SHA256SUMS",
		"SECURITY-AUDIT.json",
		"packages/generic",
		"expected_sha",
		"GitHubArtifactId",
		"GitLabVerifyJobId",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("v0.1.5 package-first publisher contract is missing %q", required)
		}
	}
	entry := readContractFile(t, root, "scripts/publish-v0.1.5.ps1")
	if strings.Contains(entry, "v0.1.4") {
		t.Error("v0.1.5 entry point reads legacy v0.1.4 metadata")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readContractFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}
