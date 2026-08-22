# Hermes Auto-Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically discover and verify Hermes without asking interactive users whether it is installed, where `HERMES_HOME` is, or which version they have.

**Architecture:** A pure, injectable resolver in `internal/hermes` selects a safe home and executable, parses a bounded version contract, and runs read-only capability probes for later `0.20.x` versions. The CLI uses it to hydrate desired state and present concise results, while `internal/service` independently re-verifies the runtime before planning or mutation. Domain/config/receipt types retain observed home and version without breaking legacy documents.

**Tech Stack:** Go 1.26.6 standard library, existing `pathsafe`, `domain`, CLI/service/reconcile packages, table-driven tests, PowerShell build commands, GitHub native runners and pinned ALT p11 userspace.

## Global Constraints

- Interactive Hermes flow must not ask installed yes/no, `HERMES_HOME`, or version; non-Hermes keeps the installed question.
- `--hermes-home` and `--app-installed` remain explicit automation overrides.
- Discovery precedence is explicit home, safe `HERMES_HOME`, platform default, then a provable standard parent only when no stronger source exists.
- Defaults are `%LOCALAPPDATA%\hermes` on Windows and `$HOME/.hermes` on macOS, Linux, and ALT Linux.
- Check exact home candidates before the first `PATH` candidate; never scan disks or search past unsafe/incompatible PATH shadowing.
- Accept stable Hermes versions `>= 0.20.1` and `< 0.21.0`; later `0.20.x` requires read-only capability probes.
- Existing installed homes must exist and every accepted path must be absolute, non-symlink/non-reparse, and non-overlapping with `KIT_ALL_TEAM_HOME`.
- An install directory is not automatically `HERMES_HOME`; Linux root/FHS layouts do not authorize using `/opt`, `/usr`, or `/usr/local` as home.
- Preserve Windows manual-install fail-closed behavior, POSIX managed installation, service defense-in-depth, secret redaction, and legacy `.env`/receipt readability.
- Persist actual verified home and, when Hermes is already installed, actual version in desired state and operation receipt.

---

### Task 1: Stable version range and read-only capability contract

**Files:**
- Create: `internal/hermes/version.go`
- Modify: `internal/hermes/executable.go`
- Modify: `internal/hermes/executable_test.go`

**Interfaces:**
- Produces: `RuntimeInfo`, `ParseRuntimeInfo(executable string, output []byte) (RuntimeInfo, error)`, `VerifyExecutable(ctx context.Context, path string, capture executableCapture) (RuntimeInfo, error)`.
- Produces constants: `HermesMinimumVersion = "0.20.1"`, `HermesMaximumExclusiveVersion = "0.21.0"`.
- Consumes: the existing bounded capture and `executableReady` filesystem check.

- [x] **Step 1: Add failing range/parser tests**

Add table tests that build the existing five-line version output with `0.20.0`, `0.20.1`, `0.20.2`, `0.20.99`, and `0.21.0`. Assert `0.20.1`, `0.20.2`, and `0.20.99` parse, while recognized out-of-range versions satisfy a new sentinel:

```go
var ErrVersionUnsupported = errors.New("HERMES_VERSION_UNSUPPORTED")

if !errors.Is(err, ErrVersionUnsupported) {
    t.Fatalf("error=%v, want HERMES_VERSION_UNSUPPORTED", err)
}
```

Also reject prerelease tokens, integer overflow, invalid UTF-8, carriage returns outside CRLF, oversized output, a foreign install directory, and version lines duplicated elsewhere in the output.

- [x] **Step 2: Add failing capability-probe tests**

Inject a capture fake that records argv. Assert `0.20.1` invokes only `--version`. For `0.20.2`, assert this exact read-only sequence:

```go
[][]string{
    {"--version"},
    {"profile", "--help"},
    {"profile", "create", "--help"},
    {"doctor", "--help"},
}
```

The fake outputs must prove `profile` exposes `create`, `profile create` exposes both `--no-alias` and `--no-skills`, and `doctor --help` succeeds. Missing any marker, non-zero exit, invalid UTF-8, or output above 64 KiB must satisfy `ErrExecutableUnverified` and retain the stable public code `HERMES_EXECUTABLE_UNVERIFIED`.

- [x] **Step 3: Run the focused tests and confirm RED**

Run:

```powershell
go test -mod=vendor ./internal/hermes -run 'Version|Executable|Capabilities' -count=1
```

Expected: compile/test failure because range parsing and capability probes do not exist and the old code accepts only exact `0.20.1` output.

- [x] **Step 4: Implement the strict parser and verifier**

Use numeric `major`, `minor`, and `patch` fields rather than lexical comparison. `RuntimeInfo` has no secrets:

```go
type RuntimeInfo struct {
    Executable  string
    InstallDir  string
    Version     string
}
```

`ParseRuntimeInfo` validates the complete existing multiline shape, canonical absolute paths, containment of executable below `InstallDir`, and a stable `MAJOR.MINOR.PATCH`. `VerifyExecutable` rechecks filesystem safety, captures `--version`, rejects versions outside `[0.20.1,0.21.0)`, and performs the three help probes only when patch is greater than `1` in the `0.20` line. Keep `ResolveCompatibleExecutable` as a compatibility wrapper until Task 4 migrates service callers.

- [x] **Step 5: Run GREEN and commit**

```powershell
go fmt ./internal/hermes
go test -mod=vendor ./internal/hermes -run 'Version|Executable|Capabilities' -count=1
go vet -mod=vendor ./internal/hermes
git add internal/hermes/version.go internal/hermes/executable.go internal/hermes/executable_test.go
git commit -m "feat(hermes): accept verified 0.20 version range"
```

Expected: all commands exit zero.

### Task 2: Safe home and executable discovery

**Files:**
- Create: `internal/hermes/discovery.go`
- Create: `internal/hermes/discovery_test.go`
- Modify: `internal/platform/hermes.go`
- Modify: `internal/platform/platform_test.go`

**Interfaces:**
- Consumes: `VerifyExecutable`, `domain.OSFamily`, `pathsafe`, injected environment/filesystem/process functions.
- Produces: `DiscoveryRequest`, `DiscoveryDependencies`, `DiscoveryResult`, and `Discover(ctx, request, dependencies) (DiscoveryResult, error)`.
- Produces: corrected `platform.DefaultHermesHome(family, home, localAppData)`.

- [x] **Step 1: Write failing default-home tests**

Change the platform matrix to require:

```go
{domain.OSWindows, `C:\Users\dev`, `C:\Users\dev\AppData\Local`, `C:\Users\dev\AppData\Local\hermes`},
{domain.OSMacOS, `/Users/dev`, ``, `/Users/dev/.hermes`},
{domain.OSLinux, `/home/dev`, ``, `/home/dev/.hermes`},
{domain.OSALTLinux, `/home/dev`, ``, `/home/dev/.hermes`},
```

Assert empty required environment values return `ErrHermesHomeUnavailable`.

- [x] **Step 2: Write failing discovery precedence and safety tests**

Define the public shapes exactly:

```go
type DiscoveryRequest struct {
    OS                   domain.OSFamily
    ExplicitHome         string
    InstalledOverride    *bool
    KitHome              string
}

type DiscoveryResult struct {
    Installed  bool
    Home       string
    Executable string
    Version    string
}
```

`DiscoveryDependencies` supplies `Getenv`, `UserHomeDir`, `LookPath`, `Lstat`, `Abs`, and bounded `Capture`. Table tests must cover:

- explicit home wins over environment/default;
- safe environment wins over default;
- each corrected platform default;
- exact official and managed executable layouts win over PATH;
- absent exact candidates fall back to only the first PATH candidate;
- explicit/environment relative, symlink/reparse, unsafe or ambiguous home returns `HERMES_HOME_AUTO_DETECT_FAILED` without fallback;
- an incompatible or unsafe exact/PATH executable fails closed and does not become `Installed=false`;
- no executable produces `Installed=false` and a safe target home;
- `InstalledOverride=true` plus no executable fails;
- `InstalledOverride=false` skips PATH/runtime execution and returns the safe install target;
- installed home must exist; absent install target may be created only under a validated existing parent;
- `KitHome` equal to, inside, or containing home returns `HOME_PATH_OVERLAP`;
- `/opt/hermes-agent`, `/usr/local/bin/hermes`, and arbitrary `Install directory` values never derive `/opt`, `/usr`, or `/usr/local` as home;
- a parent is derived only for the enumerated `<home>/hermes-agent/venv/...` or `<home>/.teamkit/hermes-agent-source/venv/...` shape, with no explicit/env/default source and every component validated.

- [x] **Step 3: Run RED**

```powershell
go test -mod=vendor ./internal/platform ./internal/hermes -run 'DefaultHermesHome|Discover' -count=1
```

Expected: failures on old Windows/macOS defaults and missing discovery API.

- [x] **Step 4: Implement deterministic discovery**

Add sentinel errors with stable prefixes:

```go
var ErrHomeAutoDetect = errors.New("HERMES_HOME_AUTO_DETECT_FAILED")
```

Resolve one home source, validate it, check exact candidates in documented order, then inspect exactly one `LookPath("hermes")` result. Never walk directories. Use `filepath.Rel` plus `pathsafe`/`Lstat` checks to reject nesting, links, reparse points, and standard-parent ambiguity. Preserve the concrete underlying cause only after the stable prefix and append `use --hermes-home with an absolute verified path` without leaking environment contents beyond the accepted public path.

- [x] **Step 5: Run GREEN and commit**

```powershell
go fmt ./internal/hermes ./internal/platform
go test -mod=vendor ./internal/platform ./internal/hermes -run 'DefaultHermesHome|Discover|Version|Executable|Capabilities' -count=1
go vet -mod=vendor ./internal/platform ./internal/hermes
git add internal/hermes/discovery.go internal/hermes/discovery_test.go internal/platform/hermes.go internal/platform/platform_test.go
git commit -m "feat(hermes): discover safe runtime home"
```

### Task 3: Persist observed version without breaking legacy state

**Files:**
- Modify: `internal/domain/state.go`
- Modify: `internal/domain/state_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/reconcile/receipt.go`
- Modify: `internal/reconcile/receipt_test.go`
- Modify: `internal/service/operation_contract.go`
- Modify: `internal/service/operation_contract_test.go`

**Interfaces:**
- Produces: `DesiredStateInput.HermesVersion string` and `DesiredState.HermesVersion() string`.
- Produces optional public key `HERMES_VERSION` for installed Hermes.
- Consumes: `HermesMinimumVersion`, `HermesMaximumExclusiveVersion`, actual discovered version.

- [x] **Step 1: Add failing domain/config round-trip tests**

Construct installed Hermes desired state with `HermesVersion: "0.20.2"`; assert accessor equality and exact encoded keys including `HERMES_VERSION=0.20.2`. Assert non-Hermes states reject/omit the field. Assert legacy Hermes `.env` without `HERMES_VERSION` still decodes, while new documents round-trip exactly. `HERMES_VERSION` remains non-secret and must not contain whitespace/newlines.

- [x] **Step 2: Add failing receipt/contract tests**

Assert new receipt JSON contains:

```json
"hermes_home":"/home/dev/.hermes","hermes_version":"0.20.2"
```

and `DesiredState()` reconstructs both. Parse an existing schema-1 fixture with no `hermes_version` successfully. Assert the external Hermes operation contract changes when observed version changes and contains minimum `0.20.1`, maximum-exclusive `0.21.0`, and observed `0.20.2`, without an executable path.

- [x] **Step 3: Run RED**

```powershell
go test -mod=vendor ./internal/domain ./internal/config ./internal/reconcile ./internal/service -run 'HermesVersion|Legacy|OperationContract' -count=1
```

Expected: compile failures because desired state and serialized shapes lack the field.

- [x] **Step 4: Implement optional backward-compatible metadata**

Add `hermesVersion` to immutable desired state and `json:"hermes_version,omitempty"` to `desiredSnapshot`. Keep receipt schema version `1`: the new known field is optional and old hashes remain reproducible because the empty field is omitted. `config.Decode` accepts the key only for Hermes and tolerates its absence; `config.Encode` emits it only when non-empty. New interactive/automatic installed states always set it; managed not-yet-installed states honestly leave it empty.

Replace `operationHermesContract.CompatibleVersion` with:

```go
MinimumVersion          string `json:"minimum_version,omitempty"`
MaximumExclusiveVersion string `json:"maximum_exclusive_version,omitempty"`
ObservedVersion         string `json:"observed_version,omitempty"`
```

- [x] **Step 5: Run GREEN and commit**

```powershell
go fmt ./internal/domain ./internal/config ./internal/reconcile ./internal/service
go test -mod=vendor ./internal/domain ./internal/config ./internal/reconcile ./internal/service -run 'HermesVersion|Legacy|OperationContract' -count=1
git add internal/domain internal/config internal/reconcile/receipt.go internal/reconcile/receipt_test.go internal/service/operation_contract.go internal/service/operation_contract_test.go
git commit -m "feat(state): record observed Hermes version"
```

### Task 4: Wizard auto-detection and automation overrides

**Files:**
- Modify: `internal/cli/prompt.go`
- Modify: `internal/cli/prompt_test.go`
- Modify: `internal/cli/flags.go`
- Modify: `internal/cli/run.go`
- Modify: `internal/cli/run_test.go`
- Modify: `cmd/teamkit/main.go`

**Interfaces:**
- Consumes: `hermes.Discover`, `DiscoveryRequest`, explicit string flags in `options`.
- Produces: injectable `Runner.HermesDiscovery func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error)`.
- Produces JSON metadata: `"hermes":{"installed":true,"home":"...","version":"0.20.2"}` for plan/apply results.

- [x] **Step 1: Add failing questionnaire tests**

For Hermes input, provide only OS, application, `KIT_ALL_TEAM_HOME`, project, role, toolchain, and update. Assert output does not contain:

```go
for _, forbidden := range []string{"AI-приложение уже установлено", "Введите HERMES_HOME", "Введите версию Hermes"} {
    if strings.Contains(output, forbidden) { t.Fatalf("unexpected prompt %q", forbidden) }
}
```

Inject a discovery result `{Installed:true, Home: hermesHome, Version:"0.20.2"}` and assert desired state receives all three values. In a separate Codex test, assert the numbered installed question remains.

- [x] **Step 2: Add failing override/output tests**

Assert `--hermes-home` reaches `DiscoveryRequest.ExplicitHome`. Assert absent `--app-installed` yields `nil`, `true` yields a true pointer, and `false` yields a false pointer. Test interactive text exactly as three concise lines. In `--json`, assert there is one valid JSON document and no preceding prose. Assert discovery failure prevents any service call.

- [x] **Step 3: Run RED**

```powershell
go test -mod=vendor ./internal/cli ./cmd/teamkit -run 'Hermes|Questionnaire|Override|JSON' -count=1
```

Expected: current wizard blocks waiting for installed/home input and no discovery dependency exists.

- [x] **Step 4: Split questionnaire stages and hydrate options**

Refactor the questionnaire into `completeApplication`, `completeKitHome`, and `completeProject` stages. The Runner sequence for `plan`/`apply` becomes:

```go
select OS and application
if non-Hermes: ask/require app-installed
ask/require KIT_ALL_TEAM_HOME
if Hermes: discover using overrides, assign appInstalled/hermesHome/hermesVersion
ask/require project, role, toolchain, update
build DesiredState
```

Keep `options.appInstalled` as a string so absence remains distinguishable from explicit false. Add `hermesVersion` only as an internal hydrated value; do not add a user-facing version flag. Production `cmd/teamkit/main.go` wires the real resolver. Normal output prints the concise observation; JSON encodes it in result metadata.

- [x] **Step 5: Run GREEN and commit**

```powershell
go fmt ./internal/cli ./cmd/teamkit
go test -mod=vendor ./internal/cli ./cmd/teamkit -run 'Hermes|Questionnaire|Override|JSON' -count=1
go vet -mod=vendor ./internal/cli ./cmd/teamkit
git add internal/cli cmd/teamkit/main.go
git commit -m "feat(cli): auto-detect Hermes runtime"
```

### Task 5: Service defense-in-depth and race-safe re-verification

**Files:**
- Modify: `internal/service/service.go`
- Modify: `internal/service/service_test.go`
- Modify: `internal/service/operation_contract.go`

**Interfaces:**
- Consumes: fully hydrated `domain.DesiredState`, `hermes.VerifyExecutable`, and safe exact-home/PATH resolution.
- Changes injection to: `Options.ResolveHermesRuntime func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error)`.
- Produces: a verified executable only when re-observed home/version match desired state.

- [x] **Step 1: Add failing service tests**

Cover `Plan`, `Apply`, and `Retry` for installed Hermes. The injected resolver returns the same home/version and succeeds. Then independently change executable, home, version, capability output, and first PATH candidate between CLI discovery and service use; each must fail before opening mutation adapters. Assert actual `0.20.2` reaches profile execution only after re-verification.

Add regression tests that:

- a legacy desired state with empty version accepts any otherwise compatible `0.20.x` and is still verified;
- a desired state with `0.20.2` rejects a re-observed `0.20.3` as runtime drift;
- `AppInstalled=false` does not invoke external runtime discovery and preserves Windows manual/POSIX managed branches;
- home overlap is checked after `KIT_ALL_TEAM_HOME` is known, then independently rechecked by the service before filesystem/network mutation.

- [x] **Step 2: Run RED**

```powershell
go test -mod=vendor ./internal/service -run 'HermesRuntime|RuntimeDrift|HomeOverlap|ManagedInstall' -count=1
```

Expected: compile failure on the new resolver signature or behavioral failure because the current service verifies PATH only and exact `0.20.1`.

- [x] **Step 3: Replace the old executable-only resolver**

Change `hermesExecutable` to obtain a complete `DiscoveryResult`, require `Installed=true`, compare canonical `Home` with `desired.HermesHome()`, and compare `Version` when desired has a non-empty observed version. Return only the verified executable to existing effects. Do not trust the CLI result or cached PATH. Keep `validateServicePaths`, `rejectHomeOverlap`, `ExecutableReady`, secret-store safety, and operation locking unchanged.

- [x] **Step 4: Update all service fixtures and run GREEN**

```powershell
go fmt ./internal/service
go test -mod=vendor ./internal/service -run 'HermesRuntime|RuntimeDrift|HomeOverlap|ManagedInstall|Plan|Apply|Retry' -count=1
go test -mod=vendor ./internal/bootstrap ./internal/hermes ./internal/engine -count=1
go vet -mod=vendor ./internal/service
git add internal/service
git commit -m "feat(service): reverify discovered Hermes runtime"
```

### Task 6: User documentation, release contracts, and cross-platform verification

**Files:**
- Modify: `README.md`
- Modify: `docs/INSTALL.md`
- Modify: `test/release/docs_test.go`
- Modify: `test/integration/blackbox_test.go`
- Modify: `docs/superpowers/plans/2026-08-16-hermes-auto-detection.md` (check completed boxes only after evidence)

**Interfaces:**
- Consumes: final wizard wording, stable error codes, supported range and override behavior.
- Produces: one novice-friendly scenario split and regression-tested release documentation.

- [x] **Step 1: Add failing documentation and black-box assertions**

Require both user documents to say that Hermes is detected automatically, `Hermes-Setup.exe` is downloaded only when installation is planned, and installed users do not need it. Reject instructions that tell interactive users to answer installed/home/version questions. Require explanations for `HERMES_HOME_AUTO_DETECT_FAILED`, `HERMES_VERSION_UNSUPPORTED`, and the `>=0.20.1,<0.21.0` range.

Add a black-box fake Hermes fixture for `0.20.2` whose help probes satisfy the contract; run `teamkit plan` with no Hermes installed/home answers and assert the emitted state/result contains the fixture home/version.

- [x] **Step 2: Run RED**

```powershell
go test -mod=vendor ./test/release ./test/integration -run 'Documentation|Hermes|Wizard' -count=1
```

Expected: old README/INSTALL wording and the current black-box interface fail.

- [x] **Step 3: Rewrite the user path without duplicate starts**

Document common Team Kit download/hash steps once, followed by a visible branch:

1. «Hermes уже установлен» — run Team Kit; it detects home/version automatically; no `Hermes-Setup.exe` is needed.
2. «Планируется установка Hermes» — download the installer in advance on Windows (official site, release fallback), then run Team Kit; POSIX managed install remains automatic.
3. «Используется другое AI-приложение» — retain the installed question and no Hermes assets.

Explain overrides only in an administrator/automation subsection. Update frequent errors with actionable absolute-path examples but do not ask ordinary users to guess `HERMES_HOME`.

- [ ] **Step 4: Run the full local verification**

```powershell
go fmt ./...
git diff --check
go vet -mod=vendor ./...
go test -mod=vendor -count=1 ./...
go test -mod=vendor -race -count=1 ./...
```

Expected: all commands exit zero and no generated binary is staged.

Local status on 2026-08-16: formatting, `diff --check`, `vet` and the full non-race suite pass. The Windows race run remains delegated to CI because the local host has no C compiler required by CGO (`gcc` is not present).

- [x] **Step 5: Cross-build exact supported targets**

```powershell
$env:CGO_ENABLED = '0'
$env:GOFLAGS = '-mod=vendor'
New-Item -ItemType Directory -Force -Path '.tools\cross' | Out-Null
$env:GOOS='windows'; $env:GOARCH='amd64'; go build -o '.tools\cross\teamkit-windows-amd64.exe' ./cmd/teamkit
$env:GOOS='linux';   $env:GOARCH='amd64'; go build -o '.tools\cross\teamkit-linux-amd64' ./cmd/teamkit
$env:GOOS='darwin';  $env:GOARCH='amd64'; go build -o '.tools\cross\teamkit-darwin-amd64' ./cmd/teamkit
$env:GOOS='darwin';  $env:GOARCH='arm64'; go build -o '.tools\cross\teamkit-darwin-arm64' ./cmd/teamkit
Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED,Env:GOFLAGS -ErrorAction SilentlyContinue
```

Expected: four binaries are produced. Native Windows/Linux/macOS Intel/macOS ARM and pinned ALT p11 userspace jobs must subsequently pass in existing CI; container ALT evidence is not represented as native ALT VM evidence.

- [x] **Step 6: Commit documentation and test contract**

```powershell
git add README.md docs/INSTALL.md test/release/docs_test.go test/integration/blackbox_test.go docs/superpowers/plans/2026-08-16-hermes-auto-detection.md
git commit -m "docs: explain automatic Hermes detection"
git status --short
```

Expected: clean worktree. Do not tag or publish a new RC in this task; release is a separate explicitly authorized step after CI evidence.
