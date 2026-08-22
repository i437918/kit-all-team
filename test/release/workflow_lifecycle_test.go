package release_test

import (
	"strings"
	"testing"
)

func TestALTNativeWorkflow_ConsumesExactCICandidateAndRunsLifecycle(t *testing.T) {
	workflow := readContractFile(t, repositoryRoot(t), ".github/workflows/alt-native.yml")
	for _, required := range []string{
		"VERSION: v0.1.5",
		"ci_run_id:",
		"candidate_digest:",
		"required: true",
		"actions: read",
		".github/workflows/ci.yml",
		`run-id: ${{ inputs.ci_run_id }}`,
		`CANDIDATE_ARTIFACT_DIGEST: ${{ inputs.candidate_digest }}`,
		"candidate-binaries",
		"sha256sum --check --strict SHA256SUMS",
		"scripts/artifact-lifecycle-smoke.sh",
		"ARTIFACT_LIFECYCLE_VERIFIED",
		"candidate_digest=%s",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("ALT native workflow does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"actions/setup-go@", "go build", "go test", "go run"} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("ALT native workflow rebuilds or retests source via %q", forbidden)
		}
	}
	if strings.Contains(workflow, "v0.1.0-rc.2") {
		t.Error("ALT native workflow still targets the RC2 candidate")
	}
	lifecycle := strings.Index(workflow, "scripts/artifact-lifecycle-smoke.sh")
	verified := strings.LastIndex(workflow, "ALT_NATIVE_RUNNER_VERIFIED")
	if lifecycle < 0 || verified <= lifecycle {
		t.Fatalf("ALT native verification marker precedes lifecycle evidence: lifecycle=%d verified=%d", lifecycle, verified)
	}
}

func TestHermesWindowsContract_IsManualExperimentalAndNeverClaimsInstall(t *testing.T) {
	workflow := readContractFile(t, repositoryRoot(t), ".github/workflows/hermes-windows-e2e.yml")
	for _, required := range []string{
		"hermes-windows-installer-contract-experimental",
		"VERSION: v0.1.5",
		"workflow_dispatch:",
		"ci_run_id:",
		"candidate_digest:",
		"payload_release:",
		"launch_visible_installer:",
		"type: boolean",
		"default: false",
		"actions: read",
		".github/workflows/ci.yml",
		`run-id: ${{ inputs.ci_run_id }}`,
		"candidate-binaries",
		"SECURITY-AUDIT.json",
		"505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5",
		"Get-AuthenticodeSignature",
		"Nous Research Inc.",
		`if: ${{ inputs.launch_visible_installer }}`,
		"UseShellExecute = $false",
		"HERMES_WINDOWS_INSTALLER_VISIBLE_LAUNCH_OBSERVED",
		"HERMES_WINDOWS_INSTALLER_STATIC_CONTRACT_PASSED",
		`ui_completion = "not_observed"`,
		`install_root_claim = "not_made"`,
		"result.json",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("Hermes Windows contract workflow does not contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"v0.1.0-rc.2",
		"-ArgumentList '/S'",
		`-ArgumentList "/S"`,
		"/silent",
		"/quiet",
		"HERMES_WINDOWS_INSTALLER_E2E_VERIFIED",
		"verified = $true",
		"HERMES_HOME_PERSISTENCE_MISMATCH",
		"HERMES_INSTALL_ROOT_MISMATCH",
		"TEAMKIT_PLAN_NOT_VERIFIED",
		`"plan", "--non-interactive"`,
		"Get-Command hermes",
		"go build",
		"go test",
		"actions/setup-go@",
	} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("Hermes Windows contract workflow contains unsupported proof %q", forbidden)
		}
	}
	staticProof := strings.Index(workflow, "HERMES_WINDOWS_INSTALLER_STATIC_CONTRACT_PASSED")
	visibleLaunch := strings.Index(workflow, "HERMES_WINDOWS_INSTALLER_VISIBLE_LAUNCH_OBSERVED")
	if staticProof < 0 || visibleLaunch <= staticProof {
		t.Fatalf("optional visible launch must follow static contract evidence: static=%d launch=%d", staticProof, visibleLaunch)
	}

	for _, releaseWorkflow := range []string{".github/workflows/release.yml", ".github/workflows/nightly.yml"} {
		contents := readContractFile(t, repositoryRoot(t), releaseWorkflow)
		if strings.Contains(contents, "hermes-windows-installer-contract-experimental") ||
			strings.Contains(contents, "HERMES_WINDOWS_INSTALLER_STATIC_CONTRACT_PASSED") {
			t.Errorf("%s incorrectly treats the experimental Hermes contract as a release gate", releaseWorkflow)
		}
	}
}

func TestGitHubWorkflows_NeverPublishTeamKitTagsOrReleases(t *testing.T) {
	root := repositoryRoot(t)
	for _, path := range []string{
		".github/workflows/ci.yml",
		".github/workflows/release.yml",
		".github/workflows/nightly.yml",
		".github/workflows/alt-native.yml",
		".github/workflows/hermes-windows-e2e.yml",
	} {
		t.Run(path, func(t *testing.T) {
			workflow := strings.ToLower(readContractFile(t, root, path))
			for _, forbidden := range []string{
				"contents: write",
				"gh release create",
				"gh release upload",
				"actions/create-release",
				"softprops/action-gh-release",
				"refs/tags/",
				"git tag ",
				"git push --tags",
			} {
				if strings.Contains(workflow, forbidden) {
					t.Errorf("GitHub workflow contains forbidden Team Kit tag/Release publication %q", forbidden)
				}
			}
		})
	}
}

func TestReleaseWorkflow_RequiresWorkflowDispatchCIEvidence(t *testing.T) {
	workflow := readContractFile(t, repositoryRoot(t), ".github/workflows/release.yml")
	required := `test "$(gh api "repos/$GITHUB_REPOSITORY/actions/runs/$CI_RUN_ID" --jq '.event')" = "workflow_dispatch"`
	if !strings.Contains(workflow, required) {
		t.Fatalf("release validation does not reject CI evidence from non-workflow_dispatch runs")
	}
}

func TestHermesWindowsContract_DocsKeepExperimentalEvidenceOutOfReleaseClaims(t *testing.T) {
	root := repositoryRoot(t)
	required := map[string][]string{
		"docs/INSTALL.md": {
			"Экспериментальный ручной процесс",
			"не доказывает завершение установки",
		},
		"docs/TEST-MATRIX.md": {
			"Hermes Windows static contract",
			"Informational; never a release gate",
		},
		"docs/RELEASE-CHECKLIST.md": {
			"`HERMES_WINDOWS_INSTALLER_STATIC_CONTRACT_PASSED` is informational only",
			"must not be cited as unattended-installation or install-root evidence",
		},
		"docs/EXTERNAL-BLOCKERS.md": {
			"`hermes-windows-installer-contract-experimental`",
			"не подтверждает автоматическую установку",
		},
	}
	for path, fragments := range required {
		contents := readContractFile(t, root, path)
		for _, fragment := range fragments {
			if !strings.Contains(contents, fragment) {
				t.Errorf("%s does not contain experimental Hermes boundary %q", path, fragment)
			}
		}
	}

	for path, fragment := range map[string]string{
		"docs/INSTALL.md":           "доказывает unattended installation и выбранный install root",
		"docs/TEST-MATRIX.md":       "Required before installer claim",
		"docs/EXTERNAL-BLOCKERS.md": "GitHub `windows-2025` E2E",
	} {
		if strings.Contains(readContractFile(t, root, path), fragment) {
			t.Errorf("%s retains unsupported Hermes claim %q", path, fragment)
		}
	}
}
