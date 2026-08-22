# ALT p11 OfficeCLI bounded finish

**Status:** approved on 2026-08-22; bounded finalization is governed by external exact-SHA evidence and is not self-certified by this document.

**Supersedes for the remaining finalization only:**
`docs/superpowers/specs/2026-08-21-alt-p11-officecli-qualification-image-design.md`
where that document describes a single runtime package or leaves the final
CI/image binding unspecified. The broader OfficeCLI product design in
`docs/superpowers/specs/2026-08-18-hermes-officecli-mcp-design.md` remains in
force.

## Goal

Produce one clean, single-commit OfficeCLI feature candidate whose exact SHA passes GitLab
CI and the full GitHub Windows, Linux, macOS Intel, macOS ARM and ALT p11
matrix. Reuse the already published ALT qualification image; do not rebuild or
republish it.

This stage ends at an engineering candidate. GitLab MR merge, corporate
Windows evidence or waiver, merge-SHA CI, tag, package and Release publication
remain separate authorized release work.

## Fixed evidence and immutable inputs

- Qualification-image publication source SHA and bounded-finish starting
  baseline: `9964ba4dd9f30cf115da223fa7554345e8a8bdfe`.
- Successful image publication and post-push qualification:
  `https://github.com/dmitry-m1man/kit-all-team/actions/runs/32541524174`.
- Qualification image:
  `ghcr.io/dmitry-m1man/kit-all-team/alt-p11-officecli@sha256:fe1aef6ae65d887389aa11bd9f9bdf99a924b4f6f587edaeb37307b3e2e99a48`.
- Base image:
  `registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea`.
- OfficeCLI Linux asset: `35316133` bytes, SHA-256
  `32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8`.
- `/lib64/librt.so.1` provider:
  `glibc-pthread-6:2.38.0.223.f053ff-alt1.p11.1.x86_64`.
- `/usr/bin/ldd` provider:
  `glibc-utils-6:2.38.0.223.f053ff-alt1.p11.1.x86_64`.
- `/usr/lib64/libicuuc.so.74` and `/usr/lib64/libicudata.so.74` provider:
  `libicu74-1:7.4.2-alt1.x86_64`.

Changing any digest, OfficeCLI byte, package NEVRA, base image or package path
is outside this bounded finish and requires a new design and qualification.

## Required architecture

### SPEC-01: preserve ordinary ALT CI

The existing one-argument Team Kit ALT smoke remains required on pull requests,
ordinary pushes and manual dispatch. It uses only the public pinned base image.
It must not log in to GHCR, depend on the private qualification image or execute
OfficeCLI.

The `alt-p11-userspace` job remains one of the three existing CI jobs. No new
job or workflow is introduced.

### SPEC-02: scope the private image to OfficeCLI dispatch

Only the existing dispatch-only OfficeCLI step may use the private
qualification image. In `alt-p11-userspace` the required order is:

1. public-base Team Kit smoke;
2. dispatch-only isolated GHCR authentication;
3. dispatch-only OfficeCLI live qualification using the exact digest;
4. unconditional-on-failure, dispatch-only credential cleanup;
5. evidence upload.

The job permissions are exactly `contents: read` and `packages: read`.
`${{ github.token }}` is exposed only to the authentication step, supplied to
`docker login` through stdin, and is never placed in a URL, artifact, build
argument or Git configuration.

`DOCKER_CONFIG` is an isolated runner-temp directory containing both
`github.run_id` and `github.run_attempt`. Only authentication, OfficeCLI live
qualification and cleanup receive it. Cleanup validates the exact path, treats
registry logout as best-effort, removes the validated directory, and requires
its absence afterward.

### SPEC-03: fail closed before Docker in OfficeCLI mode

`scripts/alt-container-smoke.sh` keeps the public base digest as its default.
Its one-argument mode keeps the current Team Kit behavior.

The three-argument OfficeCLI mode accepts only the exact qualification digest.
Any other `ALT_IMAGE` returns exit `64`, emits stable marker
`ALT_IMAGE_PIN_MISMATCH`, and must fail before invoking Docker.

Partial OfficeCLI evidence remains rejected: the asset and compiled live test
must both be present or both absent.

### SPEC-04: prove all runtime providers

Immediately before the unchanged Go live test, the restricted container must:

1. require `/lib64/librt.so.1` and compare its canonical RPM NEVRA with the
   fixed `glibc-pthread` value;
2. require executable `/usr/bin/ldd` and compare its canonical RPM NEVRA with
   the fixed `glibc-utils` value;
3. require both ICU libraries and compare both canonical owners with the fixed
   `libicu74` value;
4. execute absolute `/usr/bin/ldd /opt/officecli`;
5. fail if the output contains literal `not found`;
6. run the production config/read-back/version/MCP live test.

The smoke remains UID/GID `1000:1000`, read-only, capability-free,
`no-new-privileges`, network-disabled and bounded by its existing tmpfs and
timeouts. Runtime package installation, symlinks, copied libraries and host
library mounts remain forbidden.

### SPEC-05: keep diagnostics narrow

The existing one-time fail-only ALT diagnostic rerun is permitted only for the
exact qualification digest and mode `stderr-stage-v1`. The public base digest,
mutable tags and every other image are rejected. Diagnostics never turn a
failed primary run into success and never emit qualification evidence.

### SPEC-06: retire the publication bootstrap trigger

`.github/workflows/publish-alt-p11-officecli.yml` becomes manual-only. Its
temporary feature-branch/path `push` trigger and every push-specific condition
or ref expression are removed before the final feature push. The existing
`workflow_dispatch` `expected_sha` fail-closed binding remains.

The already published image is not rebuilt, pushed, made public, retagged or
deleted during this work.

### SPEC-07: record evidence without claiming final success early

Before the candidate commit:

- the design and qualification record list the base, output digest, three exact
  providers, OfficeCLI size/SHA, source SHA and publication run;
- the publication run is described as image-construction/userspace evidence,
  not final feature runtime qualification;
- `docs/OFFICECLI-QUALIFICATION.md` uses this outcome-neutral source status:

  > Source-record status: **outcome-neutral; runtime acceptance is conditional
  > on external exact-SHA evidence**.
- `docs/TEST-MATRIX.md` keeps OfficeCLI ALT as a required manual exact-SHA gate.

The source record neither predicts nor self-asserts the later CI result. The
matching GitHub `workflow_dispatch` run and GitLab push pipeline provide the
PASS or FAIL verdict for the frozen candidate SHA. The future URLs cannot be
embedded in the commit they verify, so the implementation returns the evidence
bundle in the final task completion report. A future MR or release process may
copy that bundle under separate authorization. This bounded finish does not claim that an MR has already been created or updated. No evidence-only source commit is created after CI; such a commit would create a new SHA and require
another complete cycle.

On any hard stop, the final task completion report still returns a partial evidence bundle: candidate SHA, any already selected run/pipeline IDs and URLs,
a stable failure code and the last confirmed delivery stage. Partial evidence
does not convert failure into qualification and never requires a source change.

### SPEC-08: one frozen candidate and one remote cycle

After local verification and review, create exactly one final-pin commit whose
parent is the fixed starting baseline, then freeze its SHA. Fetch current
GitLab `master`; it must already be an ancestor of that baseline and therefore
of the candidate. A moved master is a bounded-stop condition: do not merge or
rebase it in this work. Publish the same SHA
normally to `gitlab/codex/hermes-officecli-mcp` and
`github-ci/codex/hermes-officecli-mcp`; force-push, rebase and amend of
published history are forbidden.

Preflight both credential paths before any mutation. Push and verify the GitHub
feature ref first; then snapshot remote run/pipeline identities, push GitLab and
immediately dispatch GitHub `ci.yml`. The dispatch supplies `expected_sha`
equal to the frozen candidate SHA. Only one new matching GitHub run and one new
matching GitLab pipeline are accepted; both are monitored concurrently.
GitHub `run_attempt` must remain `1`, and the GitLab `verify` job must not be
retried.

## Files in scope

Production/CI:

- `.github/workflows/ci.yml`
- `.github/workflows/publish-alt-p11-officecli.yml`
- `scripts/alt-container-smoke.sh`
- `internal/service/officecli_live_test.go`

Contract tests:

- `test/release/ci_test.go`
- `test/release/scripts_test.go`
- `test/release/docs_test.go`

Documentation:

- `docs/superpowers/specs/2026-08-22-alt-p11-officecli-bounded-finish-design.md`
- `docs/superpowers/plans/2026-08-22-alt-p11-officecli-bounded-finish.md`
- `docs/OFFICECLI-QUALIFICATION.md`
- `docs/TEST-MATRIX.md`
- `README.md`
- `CHANGELOG.md`
- `docs/EXTERNAL-BLOCKERS.md`
- `docs/CONFLUENCE-INSTALL-v0.1.5.md`
- `docs/RELEASE-CHECKLIST.md`

## Explicit non-goals

- no Dockerfile or qualification-script change;
- no image discovery, build, publication or registry mutation;
- no OfficeCLI catalog, binary, downloader, provisioner, MCP or profile change;
- no raw-byte `spctl` behavior change;
- no new CI job, workflow, dependency or credential;
- no release package, tag, MR merge or GitLab Release;
- no change to corporate Windows evidence requirements.

## Acceptance criteria

Local:

- the interrupted six-file patch and all three untracked planning/spec files
  have byte-validated recoverable copies outside the repository;
- changed Go files are `gofmt` clean;
- changed shell files pass `bash -n`;
- focused ALT/OfficeCLI/CI/docs tests pass;
- no source document predicts a future runtime result or remains stale after an
  external exact-SHA PASS/FAIL verdict;
- `go test ./...`, `go vet ./...` and both command builds pass;
- `git diff --check` passes;
- final verification is non-mutating and leaves the reviewed pre-staged tree
  unchanged;
- independent functional and CI/security reviews have no Critical or Important
  findings and no unresolved Minor finding after the sole permitted recheck;
- the candidate is exactly one commit whose parent is
  `9964ba4dd9f30cf115da223fa7554345e8a8bdfe`;
- the sole commit pathset equals the declared bounded allowlist exactly;
- the worktree is clean after the candidate commit.

Remote, all for the same exact SHA:

- GitLab push pipeline succeeds;
- GitHub `build-candidate` succeeds;
- Windows, Ubuntu, macOS Intel and macOS ARM OfficeCLI lanes succeed;
- macOS strict codesign and bounded raw-byte `spctl` gates succeed;
- public-base Team Kit ALT smoke succeeds without GHCR;
- private-digest ALT OfficeCLI ownership, `ldd`, ICU, config, version and MCP
  checks succeed;
- credential cleanup and evidence upload succeed;
- GitLab contains exactly one successful `verify` job and its artifact exists;
- all remote refs and run metadata equal the candidate SHA.

## Hard stop

Do not enter another fix/publish loop. Stop and report if any of these occurs:

- a digest, NEVRA, OfficeCLI byte or base-image change is required;
- GHCR returns `403`/`denied` for the Actions token;
- a new runtime/platform dependency or defect appears;
- current `gitlab/master` is not an ancestor of the fixed starting baseline;
  merge and rebase are forbidden in this bounded cycle;
- remote refs diverge or a run is not uniquely bound to the candidate SHA;
- any required job is missing, skipped or failed;
- source must change after the remote cycle starts;
- four active hours are consumed before a clean local candidate exists.

## Time budget

- WIP repair and RED tests: 20–30 minutes.
- Minimal implementation and focused GREEN: 35–55 minutes.
- Outcome-neutral evidence documentation and docs contract: 30–50 minutes.
- Full local verification and parallel review: 35–60 minutes.
- One commit, push and dispatch: 20–35 minutes.
- Parallel remote CI wait: 45–180 minutes.

Expected active work is 2.5–3.75 hours, capped at 4 hours. Expected wall-clock
is 3.25–6.75 hours. The remote wait does not authorize additional fixes; a new
failure class triggers the hard stop.
