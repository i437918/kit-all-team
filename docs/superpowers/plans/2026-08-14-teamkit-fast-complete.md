# Team Kit Fast Complete Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a private, unsigned `v0.1.0-rc.1` of `teamkit` that reproducibly reconciles one selected 1C project environment on Windows, macOS, generic Linux, and ALT Linux.

**Architecture:** A small Go CLI parses an immutable desired state, validates it against a closed catalog, produces a deterministic plan, and applies idempotent adapters for workspace, Git, Hermes, and non-Hermes applications. Secrets remain in the selected application's private environment file; receipts and logs contain only redacted metadata. Platform-specific behavior lives behind narrow interfaces and is verified by native CI plus pinned ALT Linux container and QEMU evidence.

**Tech Stack:** Go 1.26.6, standard library first, `golang.org/x/term` only for safe secret input, system Git, GitHub Actions, Docker/Podman for ALT container checks, QEMU for the ALT image smoke test.

## Global Constraints

- One `KIT_ALL_TEAM_HOME` represents exactly one project and one deployed environment.
- OfficeCLI and all office-document handling are out of scope.
- The catalog is closed: 11 projects, 3 roles, 2 toolchains, and 4 OS families; the contract matrix contains 264 combinations.
- Team Kit treats the database checkout as read-only and never commits or pushes it. Direct work on `develop` is blocked by local hooks.
- Exactly one toolchain is installed per profile; the v8std MCP endpoint is configured separately.
- `.env`, `/db/`, and `/.teamkit/` are ignored in each deployed workspace. Logs, plans, status, and receipts never contain tokens or LLM keys.
- No reset, stash, recursive cleanup, system trust-store changes, public repository, paid runner, signing, or notarization.
- Every behavior change follows red/green/refactor. Parallel workers own disjoint paths and do not commit; the coordinator commits verified integration slices.

---

## Task 1: Bootstrap the Go Repository and Contributor Contract

**Files:**
- Create: `go.mod`
- Create: `cmd/teamkit/main.go`
- Create: `internal/buildinfo/buildinfo.go`
- Create: `internal/buildinfo/buildinfo_test.go`
- Create: `AGENTS.md`
- Create: `README.md`
- Create: `Makefile`

- [x] Write a test proving default build metadata is stable and nonempty.
- [x] Run `go test ./internal/buildinfo` and capture the expected failure.
- [x] Add the minimal module, build metadata, and a CLI entry point that delegates to a future runner.
- [x] Run `go test ./internal/buildinfo` and capture the passing result.
- [x] Document exact local commands, directory ownership, Go naming rules, testing expectations, secret handling, and commit/PR conventions in a 200–400 word `AGENTS.md` titled “Repository Guidelines”.
- [x] Add a concise README and cross-platform Make targets for format, vet, test, race, build, and verify.

## Task 2: Closed Catalog and Desired-State Validation

**Owner paths:** `internal/domain/**`, `internal/catalog/**`

**Files:**
- Create: `internal/domain/state.go`
- Create: `internal/domain/errors.go`
- Create: `internal/domain/state_test.go`
- Create: `internal/catalog/catalog.go`
- Create: `internal/catalog/catalog_test.go`

- [ ] Write table tests for every project URL/branch, role, toolchain pin, OS family, AI application, provider, and MCP value.
- [ ] Write a contract test enumerating exactly `11 × 3 × 2 × 4 = 264` valid core combinations and rejecting unknown identifiers with stable error codes.
- [ ] Run `go test ./internal/domain ./internal/catalog` and capture the expected failures.
- [ ] Implement immutable value types, closed enumerations, catalog lookup, and validation without network calls.
- [ ] Run the tests, then refactor duplicated fixtures while preserving the 264-case assertion.

## Task 3: Deterministic Planner and Reconciliation Receipt

**Owner path:** `internal/reconcile/**`

**Files:**
- Create: `internal/reconcile/action.go`
- Create: `internal/reconcile/planner.go`
- Create: `internal/reconcile/receipt.go`
- Create: `internal/reconcile/planner_test.go`
- Create: `internal/reconcile/receipt_test.go`

- [ ] Write tests proving equal desired/current states produce a no-op plan, partial states produce ordered minimal actions, and nonempty workspaces expose the four approved update choices.
- [ ] Write tests proving receipts are deterministic, atomic in shape, and redact canary secrets.
- [ ] Run `go test ./internal/reconcile` and capture the expected failure.
- [ ] Implement typed actions, stable ordering, resumable checkpoints, status calculation, retry selection, and redacted JSON receipts.
- [ ] Re-run the package tests and ensure no timestamps enter deterministic plan equality.

## Task 4: Workspace, Configuration, and Secret Boundaries

**Owner paths:** `internal/workspace/**`, `internal/secrets/**`

**Files:**
- Create: `internal/workspace/workspace.go`
- Create: `internal/workspace/env.go`
- Create: `internal/workspace/gitignore.go`
- Create: `internal/workspace/workspace_test.go`
- Create: `internal/secrets/store.go`
- Create: `internal/secrets/store_test.go`

- [ ] Write tests for empty/nonempty classification, one-project ownership, atomic writes, required ignore entries, public workspace variables, and rejection of secret keys in workspace `.env`.
- [ ] Write tests for application-local secret files, restrictive permissions where supported, path-only status output, and canary redaction.
- [ ] Run both package test suites and capture the expected failures.
- [ ] Implement filesystem interfaces, crash-safe temporary-file replacement, normalized paths, minimal dotenv parsing, and redaction.
- [ ] Re-run tests on Windows and keep platform conditionals isolated to small files with build tags only when required.

## Task 5: Read-Only Git Acquisition and Safety Hooks

**Owner path:** `internal/gitx/**`

**Files:**
- Create: `internal/gitx/runner.go`
- Create: `internal/gitx/repository.go`
- Create: `internal/gitx/hooks.go`
- Create: `internal/gitx/repository_test.go`
- Create: `internal/gitx/hooks_test.go`

- [ ] Write fake-runner tests for clone/fetch/fast-forward behavior, content branch selection, DB `develop` checkout into `db/`, credential injection through process environment, and sanitized errors.
- [ ] Write tests proving generated hooks reject commits and pushes from `develop` while permitting feature branches.
- [ ] Write negative tests proving no command contains reset, stash, token text, or an unintended remote write.
- [ ] Run `go test ./internal/gitx` and capture the expected failure.
- [ ] Implement an argument-vector system-Git wrapper, URL-safe authentication environment, non-destructive updates, and portable hooks.
- [ ] Re-run tests and inspect captured command arguments for secret canaries.

## Task 6: Hermes and Alternative Application Adapters

**Owner paths:** `internal/hermes/**`, `internal/apps/**`

**Files:**
- Create: `internal/hermes/provider.go`
- Create: `internal/hermes/profile.go`
- Create: `internal/hermes/certs.go`
- Create: `internal/hermes/installer.go`
- Create: `internal/hermes/hermes_test.go`
- Create: `internal/apps/apps.go`
- Create: `internal/apps/handoff.go`
- Create: `internal/apps/apps_test.go`

- [ ] Write tests for the exact CustomLLM endpoint/model/env-key mapping, one role profile, exactly one pinned toolchain, separate v8std MCP configuration, certificate extraction beneath `HERMES_HOME/certs`, and four application-local CA variables.
- [ ] Write tests that Windows installation accepts only the pinned EXE SHA-256 and valid signer metadata, and that other platforms return an actionable platform-specific installation status without pretending parity.
- [ ] Write tests that an absent non-Hermes application returns `AI_APP_REQUIRED`, while an installed application emits one paste-ready, secret-free handoff command.
- [ ] Run package tests and capture the expected failures.
- [ ] Implement config rendering, idempotent profile materialization, safe ZIP extraction, checksum verification, installer abstraction, application detection, and handoff generation.
- [ ] Re-run tests with secret canaries and path traversal fixtures.

## Task 7: CLI Questionnaire and End-to-End Commands

**Files:**
- Create: `internal/cli/run.go`
- Create: `internal/cli/flags.go`
- Create: `internal/cli/prompt.go`
- Create: `internal/cli/run_test.go`
- Modify: `cmd/teamkit/main.go`

- [ ] Write black-box-style runner tests for `plan`, `apply`, `status`, `retry`, and `update`, including JSON output, stable exit codes, noninteractive flags, and the seven-question interactive flow.
- [ ] Write tests for Ctrl-C, invalid project/home reuse, nonempty workspace choices, installed/missing Hermes, installed/missing alternative application, and secret prompts that never echo or persist in history.
- [ ] Run `go test ./internal/cli` and capture the expected failure.
- [ ] Implement dependency-injected orchestration over the catalog, planner, adapters, and receipt store.
- [ ] Wire `cmd/teamkit/main.go`, run CLI tests, then run `go run ./cmd/teamkit --help` as the first executable smoke test.

## Task 8: Integration, Fault-Injection, and Security Tests

**Files:**
- Create: `test/integration/reconcile_test.go`
- Create: `test/integration/git_fixture_test.go`
- Create: `test/integration/fault_test.go`
- Create: `test/security/secrets_test.go`
- Create: `test/blackbox/cli_test.go`
- Create: `test/testutil/fakes.go`

- [ ] Create local bare Git fixtures only; no live credentials are required for the deterministic suite.
- [ ] Prove first apply, repeated no-op apply, interrupted apply plus retry, selective update, and source/database refresh behavior.
- [ ] Inject filesystem, Git, certificate, installer, and network-command failures and assert stable recovery guidance and non-corrupt receipts.
- [ ] Scan stdout, stderr, receipts, plans, environment dumps, and test artifacts for token/LLM canaries.
- [ ] Build the exact binary, exercise it as a subprocess, and verify exit codes and JSON schemas.
- [ ] Run `go test ./...`, `go test -race ./...`, and `go vet ./...` until all pass.

## Task 9: Reproducible Builds, CI, and ALT Linux Evidence

**Files:**
- Create: `scripts/build.ps1`
- Create: `scripts/build.sh`
- Create: `scripts/verify.ps1`
- Create: `scripts/verify.sh`
- Create: `scripts/alt-container-smoke.sh`
- Create: `scripts/alt-qemu-smoke.sh`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/nightly.yml`
- Create: `docs/TEST-MATRIX.md`

- [ ] Write script contract tests or dry-run assertions for artifact names, `CGO_ENABLED=0`, fixed version metadata, and SHA-256 manifests.
- [ ] Build `windows/amd64`, `linux/amd64`, `darwin/amd64`, and `darwin/arm64` from one source revision and test the exact Windows/Linux artifacts locally where executable.
- [ ] Configure native jobs on `windows-2025`, `ubuntu-24.04`, `macos-15-intel`, and `macos-15`, with `macos-26` nightly.
- [ ] Add a pinned ALT p11 container smoke job and a separate QEMU workflow/script using the official qcow2 image; label them as distinct compatibility claims.
- [ ] Upload test logs, matrix results, hashes, SBOM-equivalent Go module/build info, and release evidence without secrets.
- [ ] Run YAML/static validation and local build verification.

## Task 10: Release Documentation, Live Smoke, and Private RC

**Files:**
- Create: `docs/INSTALL.md`
- Create: `docs/SECURITY.md`
- Create: `docs/RELEASE-CHECKLIST.md`
- Create: `docs/EXTERNAL-BLOCKERS.md`
- Create: `CHANGELOG.md`
- Modify: `README.md`

- [ ] Document the complete Windows/macOS/Linux/ALT flow, read-only DB policy, application-local secrets/certificates, recovery commands, and known unsigned-binary warnings.
- [ ] Record the verified portable Go fallback and the blocked all-users MSI install with its exact privilege cause.
- [ ] Run reachable live GitLab, CustomLLM, certificate, and Hermes checks only with credentials passed directly from their private store; record missing external access as evidence, not as a product-test failure.
- [ ] Create or reuse the private GitHub repository `mi1man-cmd/kit-all-team`, configure secrets without displaying values, push the feature branch, open a pull request, and wait for CI when authentication permits.
- [ ] Compare local and CI SHA-256 values for the exact candidate binaries.
- [ ] Tag and publish private prerelease `v0.1.0-rc.1` only after all deterministic gates pass; if GitHub authentication or external runners remain unavailable, leave a complete local release bundle and record the sole external blocker.
- [ ] Run independent specification and code-quality reviews, address findings with focused red/green cycles, and execute the full verification command set immediately before declaring completion.
