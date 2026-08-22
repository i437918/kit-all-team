# Cross-Platform Go Team Kit Design

## Status and Goal

This design is approved for implementation and supersedes the earlier Windows-only
scaffold. The product is a small Go CLI named `teamkit` that prepares one isolated
1C AI workspace for one project. It targets Windows, macOS, general Linux, and ALT
Linux while keeping operating-system effects behind narrow adapters.

The fast-complete release is an unsigned internal release candidate. OfficeCLI and
all office-document handling are excluded. Team Kit installs Hermes when selected,
but never installs other AI applications.

## Product Contract

The interactive questionnaire collects the OS family, AI application and installation
status, `KIT_ALL_TEAM_HOME`, conditional `HERMES_HOME`, project, role, exactly one
toolchain, and required secrets through masked input. One `KIT_ALL_TEAM_HOME` owns
exactly one project/environment. Its `.env` contains only non-secret selections;
application credentials live in the selected application's private `.env`.

The closed catalog contains 11 projects, three roles, two toolchains, and four OS
families. Project content always comes from the central AISUZ AI repository at branch
`content-<project>`. The nested `db/` checkout uses the catalog-bound repository and
`develop`. Team Kit may clone, fetch, verify cleanliness, and fast-forward DB, but
does not create business commits or push. Managed hooks reject commit or push while
the active branch is `develop`.

For a non-empty workspace, the user chooses content update, DB update, both, or no
change. Local changes produce `LOCAL_CHANGES_DETECTED`; the program never resets,
stashes, overwrites, or deletes them.

## Architecture

The functional core implements:

```text
DesiredState + ObservedState -> OperationPlan -> Apply -> Verify -> Receipt
```

The same reconciler backs `plan`, `apply`, `status`, `retry`, and `update`. Planning
is deterministic and contains no secret values. Effects are represented as typed,
ordered operations with stable IDs. Receipts record non-secret inputs, effect results,
source identities, hashes, and state version. Status is read-only. Retry consumes the
receipt and re-executes only incomplete idempotent effects.

Imperative work is isolated behind filesystem, Git, process, secret, network, and
clock ports. The production shell uses `os/exec` and system Git; tests use deterministic
in-process fixtures. Platform packages implement path discovery, private file modes,
process invocation, and reparse/symlink checks without leaking platform concerns into
catalog or planning code.

## Hermes and Other Applications

Hermes profiles use identity `1c-<project>-<role>-<toolchain>`. A profile contains
exactly one pinned toolchain and the independent `v8std` MCP declaration. CustomLLM
is the default provider at `https://llm.example.invalid/v1`, model
`generic-development`, with API key reference
`HERMES_CUSTOM_LLM_API_KEY`. Configuration generation is versioned and
validated before publication.

Windows uses the verified local `Hermes-Setup.exe`. POSIX systems use a downloaded,
pinned NousResearch installer script, never an unverified `curl | bash` pipeline.
Certificates are copied only into `HERMES_HOME/certs`; application-local environment
variables point Git, curl, Python, Node, and Hermes at that bundle. The system trust
store is never modified.

For another installed AI application, Team Kit emits one paste-ready handoff for the
selected toolchain plus `v8std`. A missing non-Hermes application returns
`AI_APP_REQUIRED`.

## Security and Failure Handling

Selectors and configuration documents reject unknown, missing, duplicate, or extra
fields. Canonical path containment is component-aware. Workspace publication uses
staging plus atomic rename where supported, refuses symlink/reparse traversal, and
never assumes ownership of foreign residue.

Secrets are accepted only through masked input, written with user-only permissions,
and excluded from plans, receipts, logs, command arguments, URLs, archives, and Git.
Git authentication uses a temporary `GIT_ASKPASS` helper. Tests place unique canaries
in every secret channel and scan all observable output.

Errors have stable codes. Network and subprocess failures retain bounded diagnostic
state. A failed effect cannot advance the receipt. Verification failure leaves the
workspace recoverable and reports the first proven cause without destructive cleanup.

## Verification and Release

Contract tests cover all `11 x 3 x 2 x 4` combinations. Native GitHub-hosted jobs run
Windows amd64, Linux amd64, macOS amd64, and macOS arm64 binaries. ALT has two levels:
a digest-pinned p11 container for pull requests and an official p11 QCOW2 guest under
QEMU for nightly/release. A future cloud ALT runner plugs into the same black-box suite.

Release artifacts are built once, tested as exact bytes, and accompanied by SHA-256
and a machine-readable evidence bundle. A private `v0.1.0-rc.1` prerelease is allowed
after required local and hosted gates pass. `ALT_USERSPACE_COMPATIBLE`,
`ALT_VM_VERIFIED`, and `ALT_NATIVE_RUNNER_VERIFIED` remain distinct claims. Signing,
notarization, public publication, and a stable release are later ceremonies.
