package release_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDockerPushDigest_AcceptsExactlyOneDocumentedReceipt(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise the Docker push receipt parser")
	}
	root := repositoryRoot(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	receipt := "The push refers to repository [ghcr.io/dmitry-m1man/kit-all-team/alt-p11-officecli]\n" +
		"qualification-deadbeef: digest: " + digest + " size: 1987\n"
	cmd := exec.Command(bash, releaseTestBashPath(t, bash, filepath.Join(root, "scripts", "docker-push-digest.sh")))
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(receipt)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("parse documented docker push receipt: %v\n%s", err, output)
	}
	if got := string(bytes.TrimSpace(output)); got != digest {
		t.Fatalf("parsed digest = %q, want %q", got, digest)
	}
}

func TestDockerPushDigest_AcceptsReceiptWithoutMapfileBuiltin(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise the Docker push receipt parser")
	}
	root := repositoryRoot(t)
	digest := "sha256:" + strings.Repeat("c", 64)
	receipt := "qualification-deadbeef: digest: " + digest + " size: 1987\n"
	bashEnv := filepath.Join(t.TempDir(), "bash-env")
	if err := os.WriteFile(bashEnv, []byte("enable -n mapfile 2>/dev/null || true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, releaseTestBashPath(t, bash, filepath.Join(root, "scripts", "docker-push-digest.sh")))
	cmd.Dir = root
	cmd.Env = append(releaseTestEnvWithout("BASH_ENV"), "BASH_ENV="+releaseTestBashPath(t, bash, bashEnv))
	cmd.Stdin = strings.NewReader(receipt)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("parse documented docker push receipt without mapfile builtin: %v\n%s", err, output)
	}
	if got := string(bytes.TrimSpace(output)); got != digest {
		t.Fatalf("parsed digest = %q, want %q", got, digest)
	}
}

func TestDockerPushDigest_RejectsMissingMalformedOrAmbiguousReceipt(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise the Docker push receipt parser")
	}
	root := repositoryRoot(t)
	script := releaseTestBashPath(t, bash, filepath.Join(root, "scripts", "docker-push-digest.sh"))
	valid := "qualification-deadbeef: digest: sha256:" + strings.Repeat("b", 64) + " size: 1987\n"
	for name, receipt := range map[string]string{
		"missing":   "The push refers to repository [ghcr.io/example/image]\n",
		"malformed": "Digest: sha256:" + strings.Repeat("b", 64) + "\n",
		"ambiguous": valid + valid,
	} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(bash, script)
			cmd.Dir = root
			cmd.Stdin = strings.NewReader(receipt)
			if output, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("invalid receipt accepted with output %q", output)
			}
		})
	}
}

func TestALTOfficeCLIImageQualification_ExecutesIsolationAndRuntimeProof(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise ALT OfficeCLI image qualification")
	}
	root := repositoryRoot(t)
	fixtureRoot := t.TempDir()
	fakeBin := filepath.Join(fixtureRoot, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(fixtureRoot, "docker.args")
	fakeDocker := "#!/usr/bin/env bash\nprintf '%s\\0' \"$@\" > \"$DOCKER_ARGS_LOG\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "docker"), []byte(fakeDocker), 0o755); err != nil {
		t.Fatal(err)
	}
	officeCLI := filepath.Join(fixtureRoot, "officecli-linux-x64")
	liveTest := filepath.Join(fixtureRoot, "officecli-live.test")
	for _, path := range []string{officeCLI, liveTest} {
		if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	image := "ghcr.io/i437918/kit-all-team/alt-p11-officecli@sha256:" + strings.Repeat("c", 64)
	librtNEVRA := "glibc-pthread-6:2.38.0.76.e9f05-44.p11.2.x86_64"
	lddNEVRA := "glibc-utils-6:2.38.0.76.e9f05-44.p11.2.x86_64"
	icuNEVRA := "libicu74-1:7.4.2-alt1.x86_64"
	cmd := exec.Command(bash,
		releaseTestBashPath(t, bash, filepath.Join(root, "scripts", "qualify-alt-officecli-image.sh")),
		image, librtNEVRA, lddNEVRA, icuNEVRA,
		releaseTestBashPath(t, bash, officeCLI),
		releaseTestBashPath(t, bash, liveTest),
	)
	cmd.Dir = root
	cmd.Env = append(releaseTestEnvWithout("PATH", "DOCKER_ARGS_LOG"),
		"PATH="+releaseTestBashPath(t, bash, fakeBin)+":/usr/bin:/bin",
		"DOCKER_ARGS_LOG="+releaseTestBashPath(t, bash, logPath),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("qualify ALT OfficeCLI image: %v\n%s", err, output)
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	contract := strings.ReplaceAll(string(logged), "\x00", "\n")
	for _, required := range []string{
		"run",
		"--rm",
		"--user",
		"1000:1000",
		"--read-only",
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--network=none",
		"ALT_LDD_PACKAGE=" + lddNEVRA,
		"ALT_ICU_PACKAGE=" + icuNEVRA,
		"owner_nevra=",
		"test -x /usr/bin/ldd",
		`ldd_owner="$(rpm -qf /usr/bin/ldd)"`,
		"ldd_owner_nevra=",
		`test "$ldd_owner_nevra" = "$ALT_LDD_PACKAGE"`,
		`test -r /usr/lib64/libicuuc.so.74`, `test -r /usr/lib64/libicudata.so.74`, `icu_owner="$(rpm -qf /usr/lib64/libicuuc.so.74)"`, `icu_data_owner="$(rpm -qf /usr/lib64/libicudata.so.74)"`, "icu_owner_nevra=", "icu_data_owner_nevra=", `test "$icu_owner_nevra" = "$ALT_ICU_PACKAGE"`, `test "$icu_data_owner_nevra" = "$ALT_ICU_PACKAGE"`,
		"/usr/bin/ldd /opt/officecli",
		"not found",
		"TEAMKIT_OFFICECLI_EXISTING_PATH=/opt/officecli",
		"TestOfficeCLILive_QualifiedAssetAndMCPHandshake",
		image,
		librtNEVRA,
		lddNEVRA,
		icuNEVRA,
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("qualification Docker boundary is missing %q:\n%s", required, contract)
		}
	}
	for _, forbidden := range []string{"command -v ldd", `test "$(command -v ldd)" = /usr/bin/ldd`} {
		if strings.Contains(contract, forbidden) {
			t.Errorf("qualification Docker boundary contains brittle PATH-dependent %q:\n%s", forbidden, contract)
		}
	}
}

func TestGitLabVerifyPinsExactDockerRunner(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise the GitLab shell-runner script")
	}
	root := repositoryRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".gitlab-ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Variables map[string]string `yaml:"variables"`
		Default   struct {
			Image string `yaml:"image"`
		} `yaml:"default"`
		Verify struct {
			Script []string `yaml:"script"`
			Tags   []string `yaml:"tags"`
		} `yaml:"verify"`
	}
	if err := yaml.Unmarshal(workflow, &config); err != nil {
		t.Fatalf("parse .gitlab-ci.yml: %v", err)
	}
	if config.Default.Image != "golang:1.26.6" {
		t.Fatalf("Docker runner must use the exact Go image, got %q", config.Default.Image)
	}
	if config.Variables["GIT_STRATEGY"] != "clone" {
		t.Fatalf("security audit requires a fresh clone without stale runner refs, got GIT_STRATEGY=%q", config.Variables["GIT_STRATEGY"])
	}
	wantRunnerTags := []string{"docker-only", "proxy", "reregister", "ukd"}
	if strings.Join(config.Verify.Tags, ",") != strings.Join(wantRunnerTags, ",") {
		t.Fatalf("verify job must be pinned to the active Linux Docker runner, got %q", config.Verify.Tags)
	}
	goVersionIndex := -1
	for i, line := range config.Verify.Script {
		if strings.TrimSpace(line) == "go version" {
			goVersionIndex = i
			break
		}
	}
	if goVersionIndex < 0 {
		t.Fatal("verify job does not run go version")
	}

	cache := t.TempDir()
	fakeBin := filepath.Join(cache, "go1.26.6-linux-amd64", "go", "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "go"), []byte("#!/usr/bin/env bash\nprintf 'go version go1.26.6 linux/amd64\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	script := "set -euo pipefail\nexport PATH=/usr/bin:/bin\n" + strings.Join(config.Verify.Script[:goVersionIndex+1], "\n")
	cmd := exec.Command(bash, "-c", script)
	cmd.Dir = root
	cacheForBash := releaseTestBashPath(t, bash, cache)
	cmd.Env = append(os.Environ(), "TEAMKIT_GO_CACHE_DIR="+cacheForBash)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("GitLab verify prefix cannot bootstrap Go without a runner-installed toolchain: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "go version go1.26.6 linux/amd64") {
		t.Fatalf("verify prefix used the wrong Go toolchain: %s", output)
	}
}

func TestGitLabVerifyUsesVendoredDependenciesOffline(t *testing.T) {
	root := repositoryRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".gitlab-ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Variables map[string]string `yaml:"variables"`
	}
	if err := yaml.Unmarshal(workflow, &config); err != nil {
		t.Fatalf("parse .gitlab-ci.yml: %v", err)
	}

	goExecutable := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goExecutable += ".exe"
	}
	cmd := exec.Command(goExecutable, "list", "./...")
	cmd.Dir = root
	cmd.Env = append(releaseTestEnvWithout("GOFLAGS", "GOMODCACHE", "GOPROXY", "GOSUMDB", "GOWORK"),
		"GOFLAGS="+config.Variables["GOFLAGS"],
		"GOMODCACHE="+t.TempDir(),
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOWORK=off",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("GitLab verify cannot resolve dependencies with external module access disabled: %v\n%s", err, output)
	}
}

func TestEnsureGoForcesIPv4ForOfficialDownload(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise the Go bootstrap script")
	}
	root := repositoryRoot(t)
	fixtureRoot := t.TempDir()
	fakeBin := filepath.Join(fixtureRoot, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCurl := `#!/usr/bin/env bash
for argument in "$@"; do
  if [[ $argument == --ipv4 ]]; then
    printf 'CURL_IPV4_SELECTED\n'
    exit 91
  fi
done
printf 'CURL_IPV4_REQUIRED\n'
exit 90
`
	if err := os.WriteFile(filepath.Join(fakeBin, "curl"), []byte(fakeCurl), 0o755); err != nil {
		t.Fatal(err)
	}

	cache := filepath.Join(fixtureRoot, "cache")
	script := `export PATH="` + releaseTestBashPath(t, bash, fakeBin) + `:/usr/bin:/bin"` + "\n. scripts/ensure-go.sh"
	cmd := exec.Command(bash, "-c", script)
	cmd.Dir = root
	cmd.Env = append(releaseTestEnvWithout("TEAMKIT_GO_CACHE_DIR"),
		"TEAMKIT_GO_CACHE_DIR="+releaseTestBashPath(t, bash, cache),
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("controlled curl boundary unexpectedly completed")
	}
	if !strings.Contains(string(output), "CURL_IPV4_SELECTED") || strings.Contains(string(output), "CURL_IPV4_REQUIRED") {
		t.Fatalf("Go bootstrap did not force the official download over IPv4:\n%s", output)
	}
}

func releaseTestEnvWithout(names ...string) []string {
	result := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		drop := false
		for _, name := range names {
			if strings.EqualFold(key, name) {
				drop = true
				break
			}
		}
		if !drop {
			result = append(result, entry)
		}
	}
	return result
}

func releaseTestBashPath(t *testing.T, bash, value string) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		return filepath.ToSlash(value)
	}
	convert := exec.Command(bash, "-c", `cygpath -u -- "$1"`, "teamkit-test", value)
	converted, err := convert.Output()
	if err != nil {
		t.Fatalf("convert test path for Git Bash: %v", err)
	}
	return strings.TrimSpace(string(converted))
}

func releaseTestBash() (string, error) {
	if runtime.GOOS == "windows" {
		git, err := exec.LookPath("git")
		if err == nil {
			candidate := filepath.Clean(filepath.Join(filepath.Dir(git), "..", "bin", "bash.exe"))
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return exec.LookPath("bash")
}

func TestBuildScriptsDeclareExactReleaseContract(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))

	want := []string{
		"teamkit-${VERSION}-windows-amd64.exe",
		"teamkit-${VERSION}-linux-amd64",
		"teamkit-${VERSION}-darwin-amd64",
		"teamkit-${VERSION}-darwin-arm64",
		"CGO_ENABLED",
		"-trimpath",
		"-ldflags",
		"SHA256SUMS",
	}

	for _, name := range []string{"build.ps1", "build.sh"} {
		data, err := os.ReadFile(filepath.Join(root, "scripts", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := normalizeContract(string(data))
		for _, fragment := range want {
			if !strings.Contains(text, fragment) {
				t.Errorf("%s does not contain %q", name, fragment)
			}
		}
	}
}

func TestBuildScriptsRunFromRepositoryRootAndHashOnlyFourDeclaredArtifacts(t *testing.T) {
	root := repositoryRoot(t)
	posix := readContractFile(t, root, "scripts/build.sh")
	windows := readContractFile(t, root, "scripts/build.ps1")

	for _, required := range []string{
		`cd "$REPOSITORY_ROOT"`,
		"manifest_artifacts=(",
		`sha256sum "${manifest_artifacts[@]}"`,
		`shasum -a 256 "${manifest_artifacts[@]}"`,
	} {
		if !strings.Contains(posix, required) {
			t.Errorf("build.sh does not contain %q", required)
		}
	}
	for _, required := range []string{"Push-Location -LiteralPath $RepositoryRoot", "try {", "finally {", "Pop-Location"} {
		if !strings.Contains(windows, required) {
			t.Errorf("build.ps1 does not contain %q", required)
		}
	}
	for _, artifact := range []string{
		"teamkit-${VERSION}-windows-amd64.exe",
		"teamkit-${VERSION}-linux-amd64",
		"teamkit-${VERSION}-darwin-amd64",
		"teamkit-${VERSION}-darwin-arm64",
	} {
		if strings.Count(normalizeContract(posix), artifact) < 2 {
			t.Errorf("build.sh does not explicitly declare %s for both build and manifest", artifact)
		}
		if strings.Count(normalizeContract(windows), artifact) < 1 {
			t.Errorf("build.ps1 does not explicitly declare manifest artifact %s", artifact)
		}
	}
	for _, script := range []string{posix, windows} {
		if strings.Contains(script, "sha256sum teamkit-*") || strings.Contains(script, "shasum -a 256 teamkit-*") {
			t.Error("build script hashes an open-ended wildcard instead of the four declared outputs")
		}
	}
}

func TestBuildScriptsRejectDirtySourceTrees(t *testing.T) {
	root := repositoryRoot(t)
	posix := readContractFile(t, root, "scripts/build.sh")
	windows := readContractFile(t, root, "scripts/build.ps1")

	for name, script := range map[string]string{"build.sh": posix, "build.ps1": windows} {
		for _, required := range []string{"git status", "--porcelain", "--untracked-files=all", "SOURCE_TREE_DIRTY"} {
			if !strings.Contains(script, required) {
				t.Errorf("%s does not fail closed on a dirty source tree: missing %q", name, required)
			}
		}
	}
}

func TestALTQEMUScriptPinsSignedManifestAndAbsoluteBackingImage(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "scripts", "alt-qemu-smoke.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, fragment := range []string{
		"SHA256SUM.asc",
		"17F112840DE94827C9C109FD3E2B30EA57EF33CE",
		"VALIDSIG",
		"IMAGE=$(realpath \"$IMAGE\")",
		"format=qcow2,if=ide",
		"format=raw,if=ide,media=cdrom,readonly=on",
		"artifact-lifecycle-smoke.sh",
		"os-release.txt",
		"filesystem.txt",
		`image-verification/SHA256SUM`,
		`image-verification/SHA256SUM.asc`,
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("ALT QEMU script does not contain %q", fragment)
		}
	}
}

func TestALTUserspaceScript_RejectsPartialOfficeCLILiveEvidencePair(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise the ALT userspace script")
	}
	root := repositoryRoot(t)
	fixture := t.TempDir()
	candidate := filepath.Join(fixture, "teamkit")
	officeCLI := filepath.Join(fixture, "officecli")
	for _, path := range []string{candidate, officeCLI} {
		if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fakeBin := filepath.Join(fixture, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "docker"), []byte("#!/usr/bin/env bash\nprintf 'UNEXPECTED_DOCKER_REACHED\\n' >&2\nexit 89\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	for name, arguments := range map[string][]string{
		"asset only": {candidate, officeCLI},
		"test only":  {candidate, "", officeCLI},
	} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(bash, append([]string{"scripts/alt-container-smoke.sh"}, arguments...)...)
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "PATH="+releaseTestBashPath(t, bash, fakeBin)+":/usr/bin:/bin")
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatal("partial OfficeCLI evidence pair unexpectedly succeeded")
			}
			if !strings.Contains(string(output), "OFFICECLI_LIVE_EVIDENCE_PAIR_REQUIRED") {
				t.Fatalf("partial pair returned an unstable error:\n%s", output)
			}
		})
	}
}

func TestALTUserspaceScript_ProvidesExecutableManagedRootForOfficeCLILiveTest(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise the ALT userspace script")
	}
	root := repositoryRoot(t)
	fixture := t.TempDir()
	fakeBin := filepath.Join(fixture, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	dockerLog := filepath.Join(fixture, "docker.log")
	fakeDocker := `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$TEAMKIT_DOCKER_LOG"
`
	if err := os.WriteFile(filepath.Join(fakeBin, "docker"), []byte(fakeDocker), 0o755); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(fixture, "teamkit"),
		filepath.Join(fixture, "officecli"),
		filepath.Join(fixture, "officecli-live.test"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command(bash, "scripts/alt-container-smoke.sh", paths[0], paths[1], paths[2])
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PATH="+releaseTestBashPath(t, bash, fakeBin)+":/usr/bin:/bin",
		"TEAMKIT_DOCKER_LOG="+releaseTestBashPath(t, bash, dockerLog),
		"ALT_IMAGE=ghcr.io/i437918/kit-all-team/alt-p11-officecli@sha256:5ee493c6c7edbdb8d68fb0ab9af2847bae855c9042bc5f13f5fd6b3d0965a825",
		"TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE=glibc-pthread-6:2.38.0.223.f053ff-alt1.p11.1.x86_64",
		"TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE=glibc-utils-6:2.38.0.223.f053ff-alt1.p11.1.x86_64",
		"TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE=libicu74-1:7.4.2-alt1.x86_64",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("controlled ALT script failed: %v\n%s", err, output)
	}
	log, err := os.ReadFile(dockerLog)
	if err != nil {
		t.Fatal(err)
	}
	contract := ""
	for _, line := range strings.Split(string(log), "\n") {
		if strings.Contains(line, "TEAMKIT_OFFICECLI_EXISTING_PATH=/opt/officecli") {
			contract = line
			break
		}
	}
	if contract == "" {
		t.Fatalf("controlled Docker log has no OfficeCLI live invocation:\n%s", log)
	}
	for _, required := range []string{
		"--tmpfs /tmp:rw,nosuid,nodev,noexec,mode=1777",
		"--tmpfs /run/teamkit-officecli:rw,nosuid,nodev,exec,mode=0700,uid=1000,gid=1000",
		"TMPDIR=/run/teamkit-officecli",
		"TEAMKIT_OFFICECLI_EXEC_ROOT=/run/teamkit-officecli",
		"--env TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE=glibc-pthread-6:2.38.0.223.f053ff-alt1.p11.1.x86_64",
		"--env TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE=glibc-utils-6:2.38.0.223.f053ff-alt1.p11.1.x86_64",
		"--env TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE=libicu74-1:7.4.2-alt1.x86_64",
		"test -r /lib64/librt.so.1",
		"owner_nevra=\"$(rpm -q --qf",
		`test "$owner_nevra" = "$TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE"`,
		"test -x /usr/bin/ldd",
		`ldd_owner="$(rpm -qf /usr/bin/ldd)"`,
		`ldd_owner_nevra="$(rpm -q --qf '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' "$ldd_owner")"`,
		`test "$ldd_owner_nevra" = "$TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE"`,
		"test -r /usr/lib64/libicuuc.so.74",
		"test -r /usr/lib64/libicudata.so.74",
		`icu_owner="$(rpm -qf /usr/lib64/libicuuc.so.74)"`,
		`icu_owner_nevra="$(rpm -q --qf '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' "$icu_owner")"`,
		`test "$icu_owner_nevra" = "$TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE"`,
		`icu_data_owner="$(rpm -qf /usr/lib64/libicudata.so.74)"`,
		`icu_data_owner_nevra="$(rpm -q --qf '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n' "$icu_data_owner")"`,
		`test "$icu_data_owner_nevra" = "$TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE"`,
		`! /usr/bin/ldd /opt/officecli | grep -F "not found"`,
		"--env TEAMKIT_OFFICECLI_ALT_IMAGE=ghcr.io/i437918/kit-all-team/alt-p11-officecli@sha256:5ee493c6c7edbdb8d68fb0ab9af2847bae855c9042bc5f13f5fd6b3d0965a825",
		"TEAMKIT_OFFICECLI_ALT_DIAGNOSTICS=stderr-stage-v1",
		"OFFICECLI_ALT_DIAGNOSTIC_COMPLETE",
		`exit "$officecli_status"`,
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("ALT live container does not provide executable managed root %q:\n%s", required, contract)
		}
	}
	if strings.Contains(contract, `test "$(rpm -qf /lib64/librt.so.1)" =`) {
		t.Fatalf("ALT live container compares raw rpm -qf output instead of canonical NEVRA:\n%s", contract)
	}
	if strings.Count(contract, "TEAMKIT_OFFICECLI_ALT_DIAGNOSTICS=stderr-stage-v1") != 1 {
		t.Fatalf("ALT diagnostic rerun is not a single bounded fail-only path:\n%s", contract)
	}
	if !strings.Contains(string(output), "OFFICECLI_ALT_USERSPACE_COMPATIBLE") {
		t.Fatalf("ALT live evidence marker missing:\n%s", output)
	}
}

func TestALTUserspaceScript_RejectsUnpinnedQualificationImageOverride(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise the ALT userspace script")
	}
	root := repositoryRoot(t)
	fixture := t.TempDir()
	candidate := filepath.Join(fixture, "teamkit")
	officeCLI := filepath.Join(fixture, "officecli")
	liveTest := filepath.Join(fixture, "officecli-live.test")
	for _, path := range []string{candidate, officeCLI, liveTest} {
		if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fakeBin := filepath.Join(fixture, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "docker"), []byte("#!/usr/bin/env bash\nprintf 'UNEXPECTED_DOCKER_REACHED\\n' >&2\nexit 89\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, "scripts/alt-container-smoke.sh", candidate, officeCLI, liveTest)
	cmd.Dir = root
	cmd.Env = append(releaseTestEnvWithout("PATH", "ALT_IMAGE"),
		"PATH="+releaseTestBashPath(t, bash, fakeBin)+":/usr/bin:/bin",
		"ALT_IMAGE=registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea",
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("unpinned ALT image override unexpectedly succeeded")
	}
	if !strings.Contains(string(output), "ALT_IMAGE_PIN_MISMATCH") {
		t.Fatalf("unpinned ALT image override returned unstable error:\n%s", output)
	}
	if strings.Contains(string(output), "UNEXPECTED_DOCKER_REACHED") {
		t.Fatalf("unpinned ALT image override reached Docker:\n%s", output)
	}
}

func TestALTUserspaceScript_DefaultModeKeepsPublicBaseImage(t *testing.T) {
	bash, err := releaseTestBash()
	if err != nil {
		t.Skip("bash is required to exercise the ALT userspace script")
	}
	root := repositoryRoot(t)
	fixture := t.TempDir()
	candidate := filepath.Join(fixture, "teamkit")
	if err := os.WriteFile(candidate, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(fixture, "bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	dockerLog := filepath.Join(fixture, "docker.log")
	fakeDocker := `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$TEAMKIT_DOCKER_LOG"
`
	if err := os.WriteFile(filepath.Join(fakeBin, "docker"), []byte(fakeDocker), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, "scripts/alt-container-smoke.sh", candidate)
	cmd.Dir = root
	cmd.Env = append(releaseTestEnvWithout(
		"PATH",
		"ALT_IMAGE",
		"TEAMKIT_OFFICECLI_ALT_LIBRT_PACKAGE",
		"TEAMKIT_OFFICECLI_ALT_LDD_PACKAGE",
		"TEAMKIT_OFFICECLI_ALT_ICU_PACKAGE",
	),
		"PATH="+releaseTestBashPath(t, bash, fakeBin)+":/usr/bin:/bin",
		"TEAMKIT_DOCKER_LOG="+releaseTestBashPath(t, bash, dockerLog),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("controlled default ALT script failed: %v\n%s", err, output)
	}
	log, err := os.ReadFile(dockerLog)
	if err != nil {
		t.Fatal(err)
	}
	const publicBaseImage = "registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea"
	if !strings.Contains(string(log), publicBaseImage) {
		t.Fatalf("default ALT smoke image = %q, want official public base %q", log, publicBaseImage)
	}
	if strings.Contains(string(log), "ghcr.io") {
		t.Fatalf("default ALT smoke must not reach GHCR:\n%s", log)
	}
	if strings.Contains(string(log), "TEAMKIT_OFFICECLI_ALT_") {
		t.Fatalf("default ALT smoke must not carry OfficeCLI qualification evidence:\n%s", log)
	}
}

func TestALTOfficeCLIQualificationImage(t *testing.T) {
	image := readContractFile(t, repositoryRoot(t), "docker/alt-p11-officecli/Dockerfile")
	from := dockerInstructionCommands(image, "FROM")
	if len(from) != 1 || from[0] != "registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea" {
		t.Fatalf("ALT OfficeCLI image base instruction = %#v", from)
	}
	if args := dockerInstructionCommands(image, "ARG"); len(args) != 6 || args[0] != "ALT_LIBRT_PACKAGE" || args[1] != "ALT_LIBRT_APT_SPEC" || args[2] != "ALT_LDD_PACKAGE" || args[3] != "ALT_LDD_APT_SPEC" || args[4] != "ALT_ICU_PACKAGE" || args[5] != "ALT_ICU_APT_SPEC" {
		t.Fatalf("ALT OfficeCLI image package arguments = %#v", args)
	}
	run := strings.Join(dockerInstructionCommands(image, "RUN"), "\n")
	for _, required := range []string{
		`test -n "$ALT_LIBRT_PACKAGE"`,
		`test -n "$ALT_LIBRT_APT_SPEC"`,
		`[[ "$ALT_LIBRT_APT_SPEC" =~ ^[A-Za-z0-9][A-Za-z0-9+._-]*=[^[:space:]=]+$ ]]`,
		`test -n "$ALT_LDD_PACKAGE"`,
		`test -n "$ALT_LDD_APT_SPEC"`,
		`[[ "$ALT_LDD_APT_SPEC" =~ ^[A-Za-z0-9][A-Za-z0-9+._-]*=[^[:space:]=]+$ ]]`,
		`test -n "$ALT_ICU_PACKAGE"`, `test -n "$ALT_ICU_APT_SPEC"`, `[[ "$ALT_ICU_APT_SPEC" =~ ^[A-Za-z0-9][A-Za-z0-9+._-]*=[^[:space:]=]+$ ]]`,
		`apt-get install -y "$ALT_LIBRT_APT_SPEC" "$ALT_LDD_APT_SPEC" "$ALT_ICU_APT_SPEC"`,
		"owner=\"$(rpm -qf /lib64/librt.so.1)\"",
		"owner_nevra=\"$(rpm -q --qf",
		`test "$owner_nevra" = "$ALT_LIBRT_PACKAGE"`,
		"ldd_owner=\"$(rpm -qf /usr/bin/ldd)\"",
		"ldd_owner_nevra=\"$(rpm -q --qf",
		`test "$ldd_owner_nevra" = "$ALT_LDD_PACKAGE"`,
	} {
		if !strings.Contains(run, required) {
			t.Errorf("ALT OfficeCLI executable Docker RUN contract is missing %q:\n%s", required, run)
		}
	}
	for _, forbidden := range []string{"glibc-pthread", "glibc-utils", "libicu74", "ln -s", "curl ", "| sh", "ldd /opt/officecli", `apt-get install -y "$ALT_LIBRT_PACKAGE"`, `apt-get install -y "$ALT_LDD_APT_SPEC"`, `apt-get install -y "$ALT_ICU_APT_SPEC"`, `test "$(rpm -qf /lib64/librt.so.1)" =`, `test "$(rpm -qf /usr/bin/ldd)" =`} {
		if strings.Contains(run, forbidden) {
			t.Errorf("ALT OfficeCLI executable Docker RUN contract contains forbidden %q:\n%s", forbidden, run)
		}
	}
	if copyInstructions := dockerInstructionCommands(image, "COPY"); len(copyInstructions) != 0 {
		t.Errorf("ALT OfficeCLI image must not copy shared libraries or OfficeCLI bytes: %#v", copyInstructions)
	}
}

func shellExecutableLine(line string) string {
	singleQuoted := false
	doubleQuoted := false
	escaped := false
	for index, r := range line {
		if escaped {
			escaped = false
			continue
		}
		switch r {
		case '\\':
			if !singleQuoted {
				escaped = true
			}
		case '\'':
			if !doubleQuoted {
				singleQuoted = !singleQuoted
			}
		case '"':
			if !singleQuoted {
				doubleQuoted = !doubleQuoted
			}
		case '#':
			if !singleQuoted && !doubleQuoted && (index == 0 || line[index-1] == ' ' || line[index-1] == '\t') {
				return line[:index]
			}
		}
	}
	return line
}
func dockerInstructionCommands(source, instruction string) []string {
	var commands []string
	var command strings.Builder
	collecting := false
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(shellExecutableLine(line))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !collecting {
			prefix := instruction + " "
			if !strings.HasPrefix(trimmed, prefix) {
				continue
			}
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		} else {
			command.WriteByte(' ')
		}
		collecting = strings.HasSuffix(trimmed, "\\")
		command.WriteString(strings.TrimSpace(strings.TrimSuffix(trimmed, "\\")))
		if !collecting {
			commands = append(commands, command.String())
			command.Reset()
		}
	}
	return commands
}
func TestArtifactLifecycleScriptExercisesReleaseCommands(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "scripts", "artifact-lifecycle-smoke.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, fragment := range []string{" plan ", " apply ", " status ", " retry ", " update ", "ARTIFACT_LIFECYCLE_VERIFIED", "needs_apply", "ready", "ls-files)"} {
		if !strings.Contains(text, fragment) {
			t.Errorf("artifact lifecycle script does not contain %q", fragment)
		}
	}
}

func TestVerifyScriptsFormatTrackedAndUntrackedGoFilesPortably(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	posix, err := os.ReadFile(filepath.Join(root, "scripts", "verify.sh"))
	if err != nil {
		t.Fatal(err)
	}
	windows, err := os.ReadFile(filepath.Join(root, "scripts", "verify.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(posix), "mapfile") || !strings.Contains(string(posix), "--cached --others --exclude-standard") {
		t.Fatalf("verify.sh is not macOS Bash 3 compatible or omits untracked files")
	}
	if strings.Contains(string(windows), "$GoFiles | & gofmt") || !strings.Contains(string(windows), "& gofmt -l @GoFiles") {
		t.Fatalf("verify.ps1 does not pass Go files as argv")
	}
}

func normalizeContract(value string) string {
	value = strings.ReplaceAll(value, "$($Version)", "${VERSION}")
	value = strings.ReplaceAll(value, "${Version}", "${VERSION}")
	return value
}
