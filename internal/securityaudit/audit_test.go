package securityaudit

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	teamkitbuildinfo "github.com/mi1man-cmd/kit-all-team/internal/buildinfo"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAuditRepository_FindsForbiddenTrackedPathAndDeletedHistorySecret(t *testing.T) {
	root := newGitRepository(t)
	writeFile(t, filepath.Join(root, ".env"), "PUBLIC_SELECTOR=wms\n")
	git(t, root, "add", ".env")
	git(t, root, "commit", "-m", "track forbidden path")

	secret := strings.Join([]string{"gl", "pat-", strings.Repeat("A", 24)}, "")
	writeFile(t, filepath.Join(root, "temporary.txt"), "token="+secret+"\n")
	git(t, root, "add", "temporary.txt")
	git(t, root, "commit", "-m", "temporary credential")
	if err := os.Remove(filepath.Join(root, "temporary.txt")); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-u")
	git(t, root, "commit", "-m", "remove temporary credential")

	report, err := Audit(context.Background(), Options{Repository: root})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit passed with a forbidden path and historical secret")
	}
	assertFinding(t, report, "tracked_paths", "forbidden_path")
	assertFinding(t, report, "git_history", "source_access_token")
	assertReportDoesNotReveal(t, report, secret)
}

func TestAuditRepository_RejectsTrackedCertificateDirectory(t *testing.T) {
	root := newGitRepository(t)
	writeFile(t, filepath.Join(root, "certs", "company.crt"), "certificate fixture\n")
	git(t, root, "add", "certs/company.crt")
	git(t, root, "commit", "-m", "track certificate")

	report, err := Audit(context.Background(), Options{Repository: root})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit passed with a tracked certificate directory")
	}
	assertFinding(t, report, "tracked_paths", "forbidden_path")
}

func TestAuditRepository_ScansReachableCommitAndAnnotatedTagMessages(t *testing.T) {
	root := newGitRepository(t)
	commitSecret := strings.Join([]string{"gl", "pat-", strings.Repeat("C", 24)}, "")
	tagSecret := strings.Join([]string{"github", "_pat_", "tag_", strings.Repeat("D", 32)}, "")
	git(t, root, "commit", "--allow-empty", "-m", "release note "+commitSecret)
	git(t, root, "tag", "-a", "security-audit-fixture", "-m", "tag note "+tagSecret)

	report, err := Audit(context.Background(), Options{Repository: root})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit skipped secrets in reachable commit and annotated tag messages")
	}
	assertFinding(t, report, "git_history", "source_access_token")
	assertFinding(t, report, "git_history", "github_token")
	assertReportDoesNotReveal(t, report, commitSecret)
	assertReportDoesNotReveal(t, report, tagSecret)
}

func TestAuditRepository_FindsRemovedEmptyForbiddenPathWithNewlineAncestor(t *testing.T) {
	root := newGitRepository(t)
	emptyBlob := gitInput(t, root, nil, "hash-object", "-w", "--stdin")
	subtree := gitInput(t, root, []byte(fmt.Sprintf("100644 blob %s\t.env\x00", emptyBlob)), "mktree", "-z")
	rootTree := gitInput(t, root, []byte(fmt.Sprintf("040000 tree %s\todd\nname\x00", subtree)), "mktree", "-z")
	parent := gitOutput(t, root, "rev-parse", "HEAD")
	commit := gitInput(t, root, []byte("historical forbidden path\n"), "commit-tree", rootTree, "-p", parent)
	git(t, root, "update-ref", "refs/heads/historical-path-fixture", commit)

	report, err := Audit(context.Background(), Options{Repository: root})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit skipped a removed empty .env below a newline-containing directory")
	}
	assertFinding(t, report, "git_history", "forbidden_path")
}

func TestAuditRepository_RecursivelyScansRemovedArchiveBlobByMagic(t *testing.T) {
	root := newGitRepository(t)
	secret := strings.Join([]string{"github", "_pat_", "archive_", strings.Repeat("E", 32)}, "")
	archivePath := filepath.Join(root, "temporary-payload.bin")
	if err := os.WriteFile(archivePath, zipBytes(t, "nested/result.log", []byte(secret)), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "temporary-payload.bin")
	git(t, root, "commit", "-m", "temporary archived evidence")
	if err := os.Remove(archivePath); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-u")
	git(t, root, "commit", "-m", "remove archived evidence")

	report, err := Audit(context.Background(), Options{Repository: root})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit skipped a secret inside a removed archive blob")
	}
	assertFinding(t, report, "git_history", "github_token")
	assertReportDoesNotReveal(t, report, secret)
}

func TestAuditRepository_FailsClosedWhenReachableMetadataExceedsLimit(t *testing.T) {
	root := newGitRepository(t)
	tree := gitOutput(t, root, "rev-parse", "HEAD^{tree}")
	parent := gitOutput(t, root, "rev-parse", "HEAD")
	message := bytes.Repeat([]byte{'x'}, int(maxInputBytes)+1)
	commit := gitInput(t, root, message, "commit-tree", tree, "-p", parent)
	git(t, root, "update-ref", "refs/heads/oversized-metadata-fixture", commit)

	report, err := Audit(context.Background(), Options{Repository: root})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit passed reachable metadata beyond its inspection limit")
	}
	assertFinding(t, report, "git_history", "oversized_input")
}

func TestAuditArtifacts_InspectsZIPAndTarGzContents(t *testing.T) {
	root := testutil.TempDir(t)
	githubToken := strings.Join([]string{"github", "_pat_", "11_", strings.Repeat("B", 40)}, "")
	privateKey := strings.Join([]string{"-----BEGIN ", "PRIVATE KEY-----", "\nopaque\n-----END PRIVATE KEY-----\n"}, "")
	writeZIP(t, filepath.Join(root, "logs.zip"), "nested/execution.log", githubToken)
	writeTarGz(t, filepath.Join(root, "RELEASE-EVIDENCE.tar.gz"), "native/result.txt", privateKey)

	report, err := Audit(context.Background(), Options{Paths: []string{root}, Commit: strings.Repeat("c", 40)})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit passed with secrets inside archives")
	}
	assertFinding(t, report, "artifact_archive", "github_token")
	assertFinding(t, report, "artifact_archive", "private_key")
	assertReportDoesNotReveal(t, report, githubToken)
	assertReportDoesNotReveal(t, report, privateKey)
}

func TestAuditArtifacts_RejectsForbiddenArtifactPathsWithoutSecretLikeContents(t *testing.T) {
	root := testutil.TempDir(t)
	writeFile(t, filepath.Join(root, ".env"), "PROJECT=wms\n")
	writeFile(t, filepath.Join(root, "certs", "company.pem"), "public certificate fixture\n")

	report, err := Audit(context.Background(), Options{Paths: []string{root}})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit passed forbidden artifact paths whose contents did not resemble secrets")
	}
	assertFinding(t, report, "artifact_files", "forbidden_path")
	assertFindingLocationsAreOpaque(t, report, root, ".env", "company.pem")
}

func TestAuditArtifacts_RejectsForbiddenZIPAndTarEntries(t *testing.T) {
	root := testutil.TempDir(t)
	writeZIP(t, filepath.Join(root, "payload.zip"), "nested/.env", "PROJECT=wms\n")
	writeTarGz(t, filepath.Join(root, "evidence.tar.gz"), "certs/company.pem", "public certificate fixture\n")

	report, err := Audit(context.Background(), Options{Paths: []string{root}})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit passed forbidden paths inside ZIP and TAR archives")
	}
	assertFinding(t, report, "artifact_archive", "forbidden_path")
	assertFindingLocationsAreOpaque(t, report, "nested/.env", "certs/company.pem")
}

func TestAuditArtifacts_RejectsForbiddenEntryInRenamedArchiveDetectedByMagic(t *testing.T) {
	root := testutil.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "payload.bin"), zipBytes(t, "nested/.env", []byte("PROJECT=wms\n")), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Audit(context.Background(), Options{Paths: []string{root}})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit passed a forbidden entry in an archive detected only by magic")
	}
	assertFinding(t, report, "artifact_archive", "forbidden_path")
	assertFindingLocationsAreOpaque(t, report, "nested/.env")
}

func TestAuditRepository_ReleaseHistoryScopeIgnoresUnrelatedRefs(t *testing.T) {
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(testutil.TempDir(t), "missing.gitconfig"))

	firstRoot := newGitRepository(t)
	commit := gitOutput(t, firstRoot, "rev-parse", "HEAD")
	secondRoot := testutil.TempDir(t)
	git(t, firstRoot, "clone", "--no-local", firstRoot, secondRoot)
	configureGitIdentity(t, secondRoot)

	secret := strings.Join([]string{"gl", "pat-", strings.Repeat("R", 24)}, "")
	secretBlob := gitInput(t, secondRoot, []byte(secret), "hash-object", "-w", "--stdin")
	secretTree := gitInput(t, secondRoot, []byte(fmt.Sprintf("100644 blob %s\tprivate.txt\n", secretBlob)), "mktree")
	secretCommit := gitInput(t, secondRoot, []byte("unrelated history\n"), "commit-tree", secretTree)
	git(t, secondRoot, "update-ref", "refs/heads/unrelated", secretCommit)
	defaultReport, err := Audit(context.Background(), Options{Repository: secondRoot, Commit: commit})
	if err != nil {
		t.Fatalf("Audit repository-wide scope: %v", err)
	}
	if defaultReport.Passed {
		t.Fatal("repository-wide audit skipped the unrelated ref")
	}
	if defaultReport.HistoryScope != "all_refs" {
		t.Fatalf("repository-wide audit reported history scope %q", defaultReport.HistoryScope)
	}
	assertFinding(t, defaultReport, "git_history", "source_access_token")

	first, err := Audit(context.Background(), Options{Repository: firstRoot, Commit: commit, HistoryRef: commit})
	if err != nil {
		t.Fatalf("Audit first release scope: %v", err)
	}
	second, err := Audit(context.Background(), Options{Repository: secondRoot, Commit: commit, HistoryRef: commit})
	if err != nil {
		t.Fatalf("Audit second release scope: %v", err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("release-scoped evidence differs across mirrors:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}
	if !first.Passed {
		t.Fatalf("unrelated ref affected release-scoped audit: %#v", first.Findings)
	}
	if first.HistoryScope != "exact_commit_ancestry" {
		t.Fatalf("release audit reported history scope %q", first.HistoryScope)
	}
}

func TestAuditCleanInputs_EmitsDeterministicMachineReadableEvidence(t *testing.T) {
	root := newGitRepository(t)
	artifacts := testutil.TempDir(t)
	writeFile(t, filepath.Join(artifacts, "result.txt"), "candidate_digest=sha256:"+strings.Repeat("a", 64)+"\n")

	first, err := Audit(context.Background(), Options{Repository: root, Paths: []string{artifacts}})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	second, err := Audit(context.Background(), Options{Repository: root, Paths: []string{artifacts}})
	if err != nil {
		t.Fatalf("second Audit: %v", err)
	}
	if !first.Passed || first.Commit == "" || first.SchemaVersion != 1 {
		t.Fatalf("unexpected clean report: %#v", first)
	}
	one, _ := json.Marshal(first)
	two, _ := json.Marshal(second)
	if !bytes.Equal(one, two) {
		t.Fatalf("reports are not deterministic:\n%s\n%s", one, two)
	}
	if !bytes.Contains(one, []byte(`"passed":true`)) || !bytes.Contains(one, []byte(`"scopes"`)) {
		t.Fatalf("machine evidence is incomplete: %s", one)
	}
}

func TestAuditArtifacts_DoesNotTreatEnvironmentVariableNamesAsSecretValues(t *testing.T) {
	root := testutil.TempDir(t)
	writeFile(t, filepath.Join(root, "source.go"), `const PublicProviderAPIKey = "TEAMKIT_PUBLIC_PROVIDER_API_KEY"`)

	report, err := Audit(context.Background(), Options{Paths: []string{root}})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !report.Passed {
		t.Fatalf("environment variable name triggered findings: %#v", report.Findings)
	}
}

func TestAuditArtifacts_DetectsUppercaseAssignedCredentialValue(t *testing.T) {
	root := testutil.TempDir(t)
	secret := strings.Repeat("Z", 32)
	writeFile(t, filepath.Join(root, "runtime.env"), "api_key="+secret+"\n")

	report, err := Audit(context.Background(), Options{Paths: []string{root}})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit skipped an uppercase credential value")
	}
	assertFinding(t, report, "artifact_files", "credential_assignment")
	assertReportDoesNotReveal(t, report, secret)
}

func TestAuditArtifacts_FailsClosedWhenNestedArchiveExceedsDepthLimit(t *testing.T) {
	secret := strings.Join([]string{"gl", "pat-", strings.Repeat("F", 24)}, "")
	contents := []byte(secret)
	name := "payload.txt"
	for level := 0; level < maxArchiveDepth+1; level++ {
		contents = zipBytes(t, name, contents)
		name = "nested.zip"
	}
	root := testutil.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "outer.zip"), contents, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Audit(context.Background(), Options{Paths: []string{root}})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit passed an archive deeper than its inspection limit")
	}
	assertFinding(t, report, "artifact_archive", "archive_depth_exceeded")
	assertReportDoesNotReveal(t, report, secret)
}

func TestAuditArtifacts_InspectsArchiveBySignatureWhenExtensionIsMissing(t *testing.T) {
	secret := strings.Join([]string{"github", "_pat_", "22_", strings.Repeat("G", 40)}, "")
	root := testutil.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "downloaded-artifact.bin"), zipBytes(t, "result.log", []byte(secret)), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Audit(context.Background(), Options{Paths: []string{root}})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit skipped an archive whose extension was missing")
	}
	assertFinding(t, report, "artifact_archive", "github_token")
	assertReportDoesNotReveal(t, report, secret)
}

func assertFinding(t *testing.T, report Report, scope, rule string) {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.Scope == scope && finding.Rule == rule {
			return
		}
	}
	t.Fatalf("finding %s/%s absent from %#v", scope, rule, report.Findings)
}

func assertReportDoesNotReveal(t *testing.T, report Report, secret string) {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(secret)) {
		t.Fatal("audit evidence exposed a secret value")
	}
}

func assertFindingLocationsAreOpaque(t *testing.T, report Report, values ...string) {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if bytes.Contains(data, []byte(value)) {
			t.Fatalf("audit evidence exposed a forbidden artifact location: %q", value)
		}
	}
}

func newGitRepository(t *testing.T) string {
	t.Helper()
	root := testutil.TempDir(t)
	git(t, root, "init")
	configureGitIdentity(t, root)
	writeFile(t, filepath.Join(root, "README.md"), "clean repository\n")
	git(t, root, "add", "README.md")
	git(t, root, "commit", "-m", "initial")
	return root
}

func configureGitIdentity(t *testing.T, root string) {
	t.Helper()
	git(t, root, "config", "user.name", "Security Test")
	git(t, root, "config", "user.email", "security@example.invalid")
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git command failed: %v: %s", err, output)
	}
}

func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	return gitInput(t, root, nil, args...)
}

func gitInput(t *testing.T, root string, input []byte, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git command failed: %v: %s", err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeZIP(t *testing.T, path, name, contents string) {
	t.Helper()
	writeFile(t, path, string(zipBytes(t, name, []byte(contents))))
}

func zipBytes(t *testing.T, name string, contents []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	entry, err := archive.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func writeTarGz(t *testing.T, path, name, contents string) {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	data := []byte(contents)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, buffer.String())
}

func TestAuditArtifacts_RecordsEmbeddedIdentityForAllCandidates(t *testing.T) {
	root := testutil.TempDir(t)
	commit := strings.Repeat("a", 40)
	fixture := buildIdentityFixture(t, root, "v0.1.5", commit)
	for _, name := range releaseCandidateBinaryNames {
		data, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Audit(context.Background(), Options{Paths: []string{root}, Commit: commit})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !report.Passed {
		t.Fatalf("audit rejected matching candidates: %#v", report.Findings)
	}
	if len(report.Binaries) != len(releaseCandidateBinaryNames) {
		t.Fatalf("candidate identity count = %d, want %d", len(report.Binaries), len(releaseCandidateBinaryNames))
	}
	for _, identity := range report.Binaries {
		if identity.SHA256 == "" || identity.Version != "v0.1.5" || identity.Commit != commit {
			t.Fatalf("unexpected candidate identity: %#v", identity)
		}
	}
}

func TestAuditArtifacts_AcceptsTrimpathReleaseScriptIdentity(t *testing.T) {
	script := "scripts/build.sh"
	var command []string
	if runtime.GOOS == "windows" {
		pwsh, err := exec.LookPath("pwsh")
		if err != nil {
			t.Skip("pwsh is required to run the Windows release build script")
		}
		script = "scripts/build.ps1"
		command = []string{pwsh, "-NoProfile", "-File", script, "-Version", "v0.1.5", "-OutputDir", "dist"}
	} else {
		bash, err := exec.LookPath("bash")
		if err != nil {
			t.Skip("bash is required to run the release build script")
		}
		command = []string{bash, script, "v0.1.5", "dist"}
	}

	root := testutil.TempDir(t)
	commit := strings.Repeat("a", 40)
	source := filepath.Join(root, "source")
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate security-audit test source")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	copyReleaseBuildFixture(t, repositoryRoot, source)
	git(t, source, "init", "-q")
	git(t, source, "config", "core.autocrlf", "false")
	git(t, source, "config", "user.email", "fixture@example.invalid")
	git(t, source, "config", "user.name", "release fixture")
	git(t, source, "add", ".")
	git(t, source, "commit", "-m", "fixture release build")
	status := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	status.Dir = source
	statusOutput, err := status.CombinedOutput()
	if err != nil {
		t.Fatalf("read fixture source status: %v\n%s", err, statusOutput)
	}
	if strings.TrimSpace(string(statusOutput)) != "" {
		t.Fatalf("fixture source must be clean before release build: %s", statusOutput)
	}

	build := exec.Command(command[0], command[1:]...)
	build.Dir = source
	build.Env = append(os.Environ(),
		"TEAMKIT_SOURCE_REVISION="+commit,
		"TEAMKIT_SOURCE_COMMIT_TIME=2026-08-23T00:00:00Z",
	)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build trimpath release candidates: %v\n%s", err, output)
	}

	report, err := Audit(context.Background(), Options{Paths: []string{filepath.Join(source, "dist")}, Commit: commit})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !report.Passed {
		t.Fatalf("audit rejected trimpath release candidates: %#v", report.Findings)
	}
	if len(report.Binaries) != len(releaseCandidateBinaryNames) {
		t.Fatalf("candidate identity count = %d, want %d", len(report.Binaries), len(releaseCandidateBinaryNames))
	}
	for _, identity := range report.Binaries {
		if identity.Version != "v0.1.5" || identity.Commit != commit {
			t.Fatalf("unexpected trimpath candidate identity: %#v", identity)
		}
	}
}
func copyReleaseBuildFixture(t *testing.T, sourceRoot, destinationRoot string) {
	t.Helper()
	err := filepath.WalkDir(sourceRoot, func(source string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceRoot, source)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		first, _, _ := strings.Cut(relative, string(filepath.Separator))
		if first == ".git" || first == ".teamkit" || first == "dist" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destination := filepath.Join(destinationRoot, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported fixture source entry %q", relative)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		return os.WriteFile(destination, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copy release build fixture: %v", err)
	}
}
func TestAuditArtifacts_RejectsMismatchedEmbeddedIdentity(t *testing.T) {
	root := testutil.TempDir(t)
	commit := strings.Repeat("a", 40)
	matchingFixture := buildIdentityFixture(t, root, "v0.1.5", commit)
	mismatchedFixture := buildIdentityFixture(t, root, "v0.1.5", strings.Repeat("b", 40))
	for index, name := range releaseCandidateBinaryNames {
		fixture := matchingFixture
		if index == 0 {
			fixture = mismatchedFixture
		}
		if err := os.WriteFile(filepath.Join(root, name), mustReadFile(t, fixture), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Audit(context.Background(), Options{Paths: []string{root}, Commit: commit})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if report.Passed {
		t.Fatal("audit accepted a candidate whose embedded commit differs from the canonical revision")
	}
	assertFinding(t, report, "candidate_identity", "embedded_commit")
}

func TestEmbeddedCandidateIdentity_RequiresOneTerminatedCandidateMarker(t *testing.T) {
	commit := strings.Repeat("a", 40)
	marker := teamkitbuildinfo.EmbeddedIdentityPrefix
	valid := marker + "v0.1.5:" + commit + "\x00"

	tests := []struct {
		name string
		data string
	}{
		{
			name: "trailing hexadecimal data",
			data: marker + "v0.1.5:" + commit + "a",
		},
		{
			name: "trailing non hexadecimal data",
			data: marker + "v0.1.5:" + commit + "suffix",
		},
		{
			name: "conflicting candidate markers",
			data: valid + marker + "v0.1.5:" + strings.Repeat("b", 40) + "\x00",
		},
		{
			name: "duplicate candidate markers",
			data: valid + valid,
		},
		{
			name: "malformed extra marker",
			data: valid + marker + "not-a-release-identity",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version, foundCommit := embeddedCandidateIdentity([]byte(test.data))
			if version != "" || foundCommit != "" {
				t.Fatalf("embeddedCandidateIdentity() = (%q, %q), want no identity", version, foundCommit)
			}
		})
	}
}

func TestEmbeddedCandidateIdentity_AcceptsTerminatedCandidateMarkerWithLinkerDefault(t *testing.T) {
	commit := strings.Repeat("a", 40)
	data := teamkitbuildinfo.EmbeddedIdentityPrefix + "dev:unknownadjacent-static-data" +
		teamkitbuildinfo.EmbeddedIdentityPrefix + "v0.1.5:" + commit + "\x00"

	version, foundCommit := embeddedCandidateIdentity([]byte(data))
	if version != "v0.1.5" || foundCommit != commit {
		t.Fatalf("embeddedCandidateIdentity() = (%q, %q), want (%q, %q)", version, foundCommit, "v0.1.5", commit)
	}
}
func buildIdentityFixture(t *testing.T, directory, version, commit string) string {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	fixture := filepath.Join(directory, "teamkit-fixture")
	flags := "-X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.version=" + version + " -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.commit=" + commit + " -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.identity=" + "teamkit-build-identity-v1:" + version + ":" + commit
	command := exec.Command("go", "build", "-buildvcs=false", "-trimpath", "-ldflags", flags, "-o", fixture, "./cmd/teamkit")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build identity fixture: %v\n%s", err, output)
	}
	return fixture
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAuditRepository_PublicHistoryRecordsDistinctCanonicalIdentity(t *testing.T) {
	root := newGitRepository(t)
	publicRevision := gitOutput(t, root, "rev-parse", "HEAD")
	canonicalRevision := strings.Repeat("c", 40)
	if canonicalRevision == publicRevision {
		t.Fatal("fixture must exercise two distinct revisions")
	}

	report, err := Audit(context.Background(), Options{
		Repository: root,
		Commit:     canonicalRevision,
		HistoryRef: publicRevision,
	})
	if err != nil {
		t.Fatalf("Audit two-SHA candidate: %v", err)
	}
	if !report.Passed {
		t.Fatalf("two-SHA candidate audit failed: %#v", report.Findings)
	}
	if report.Commit != canonicalRevision {
		t.Fatalf("canonical commit = %q, want %q", report.Commit, canonicalRevision)
	}
	if report.HistoryRevision != publicRevision {
		t.Fatalf("audited public history revision = %q, want %q", report.HistoryRevision, publicRevision)
	}
}
