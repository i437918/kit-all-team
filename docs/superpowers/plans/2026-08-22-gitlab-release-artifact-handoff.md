# GitLab Release Artifact Handoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a kept GitLab handoff artifact the only byte source and final gate after the initial exact-SHA GitHub CI.

**Architecture:** A GitLab `release-handoff` job imports the candidate once, writes a manifest, and retains it. The publisher compares that handoff and GitLab `verify` artifact locally; GitHub metadata remains provenance only.

**Tech Stack:** GitLab CI YAML, GitLab API, PowerShell 7, Go contract tests.

**Spec:** `docs/superpowers/specs/2026-08-22-gitlab-release-artifact-handoff-design.md`

## Global Constraints

- Handoff is exactly once per exact SHA and kept before any release mutation.
- No post-handoff GitHub byte download, `archive_download_url`, workflow dispatch, or CI wait.
- Validate exactly six files, SHA256, embedded version/commit, and one kept GitLab artifact set before mutation.
- Ambiguity, expiry, duplicates, and mismatches fail closed.

---

### Task 1: Retain the GitLab handoff candidate

**Files:**
- Modify: `.gitlab-ci.yml`
- Test: `test/release/ci_test.go`

**Interfaces:**
- Consumes: `GITHUB_RUN_ID`, `GITHUB_ARTIFACT_ID`, `GITHUB_ARTIFACT_DIGEST`, and exact SHA as protected CI inputs.
- Produces: one kept `release-handoff` artifact with `handoff/MANIFEST.json` and six release files.

- [ ] **Step 1: Write the failing contract test**

Add `TestGitLabReleaseHandoff_StoresOneKeptExactCandidateSet`. Assert that `.gitlab-ci.yml` declares `release-handoff`, `handoff/MANIFEST.json`, all six v0.1.5 filenames, and `expire_in: never`.

- [ ] **Step 2: Verify RED**

Run `go test ./test/release -run TestGitLabReleaseHandoff_StoresOneKeptExactCandidateSet -count=1`.

Expected: fail because `release-handoff` does not exist.

- [ ] **Step 3: Implement the job**

Add `release-handoff` after `verify`. Fetch the candidate once using the protected provenance inputs, verify its immutable digest and exact SHA, copy exactly six files into `handoff/`, generate `MANIFEST.json` with `commit`, `version`, GitHub provenance, and SHA256 map, and keep the artifact.

- [ ] **Step 4: Verify GREEN and commit**

Run `go test ./test/release -run TestGitLabReleaseHandoff_StoresOneKeptExactCandidateSet -count=1` and commit with `ci(release): retain GitLab candidate handoff`.

### Task 2: Replace publisher GitHub gates with GitLab validation

**Files:**
- Modify: `scripts/publish-v0.1.5.ps1`
- Modify: `scripts/release/BoundedRelease.psm1`
- Test: `scripts/release/test-bounded-release.ps1`

**Interfaces:**
- Consumes: `GitLabHandoffJobId`, `GitLabVerifyJobId`, and manifest provenance.
- Produces: a publication set `{ Files, Hashes, gitlab_handoff_job_id, gitlab_job_id }` validated solely from GitLab artifacts.

- [ ] **Step 1: Write failing PowerShell tests**

Add `Test-V015HandoffRejectsGitHubByteDownload` and `Test-V015HandoffRejectsGitHubDispatch`. With valid mock handoff and verify manifests, assert any GitHub artifact-byte request or workflow dispatch throws; matching GitLab manifests return six hashes.

- [ ] **Step 2: Verify RED**

Run `pwsh -NoProfile -File scripts/release/test-bounded-release.ps1 -Only Test-V015HandoffRejectsGitHubByteDownload`.

Expected: fail because v0.1.5 still calls GitHub artifact metadata/final validation workflow.

- [ ] **Step 3: Implement local GitLab-only validation**

Add `GitLabHandoffJobId` to the entrypoint and `New-ReleaseContext`. Make `Get-VerifiedExactCI` validate the kept handoff and verify job, their exact SHA, manifests, six filenames, hashes, and embedded identity. Change `final-validation` to compare the two GitLab manifests locally; remove GitHub API/download/dispatch from the v0.1.5 path.

- [ ] **Step 4: Verify GREEN and commit**

Run both focused PowerShell tests and commit with `fix(release): validate GitLab handoff locally`.

### Task 3: Remove the obsolete GitHub final release gate

**Files:**
- Modify: `.github/workflows/release.yml`
- Modify: `test/release/ci_test.go`
- Modify: `docs/RELEASE-CHECKLIST.md`

**Interfaces:**
- Consumes: GitLab handoff/verify IDs and manifests.
- Produces: a v0.1.5 release path without `ci_run_id`, `candidate_digest`, or GitHub candidate download.

- [ ] **Step 1: Write the failing contract test**

Add `TestFinalReleaseWorkflow_V015DoesNotRequireCandidateBytes`. Assert the v0.1.5 path contains neither `actions/download-artifact`, `ci_run_id`, nor `candidate_digest`, and the checklist requires GitLab handoff validation.

- [ ] **Step 2: Verify RED**

Run `go test ./test/release -run TestFinalReleaseWorkflow_V015DoesNotRequireCandidateBytes -count=1`.

Expected: fail because `release.yml` currently downloads the candidate and accepts GitHub CI inputs.

- [ ] **Step 3: Implement and verify GREEN**

Remove the v0.1.5 publisher dependency on `.github/workflows/release.yml`, document GitLab handoff ID/keep/API verification, then run the focused Go test.

- [ ] **Step 4: Run full validation and commit**

Run `pwsh -NoProfile -File scripts/release/test-bounded-release.ps1`, `go test ./...`, `go vet ./...`, and `git diff --check`; commit with `docs(release): require GitLab artifact handoff`.

### Task 4: Promote and publish once

**Files:**
- No source changes.

**Interfaces:**
- Consumes: merged exact SHA, one source GitHub CI, one kept GitLab handoff job, and one successful GitLab verify job.
- Produces: one Generic Package, protected annotated tag, GitLab Release, and API verification report.

- [ ] **Step 1: Merge exact SHA**

Push, create MR, require GitLab CI, fast-forward merge, and sync GitHub `main` to the identical SHA.

- [ ] **Step 2: Run source CI and one handoff**

Run source CI once; pass its immutable provenance to GitLab handoff; wait for successful handoff and verify jobs; keep the handoff artifact.

- [ ] **Step 3: Verify before publisher**

Use GitLab API to prove one kept handoff, one verify job, six filenames, hashes, embedded version/commit, and exact SHA.

- [ ] **Step 4: Publish once and post-verify**

Run `scripts/publish-v0.1.5.ps1` once with GitLab handoff/verify IDs. Verify through GitLab API the protected annotated tag, Release labels, exactly one `teamkit/v0.1.5` package, six matching hashes, and kept artifacts.
