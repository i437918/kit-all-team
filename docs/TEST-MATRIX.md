# Test Matrix

Эта матрица разделяет уже опубликованный `teamkit v0.1.0 (unsigned internal release)` и подготовленный, но не опубликованный контракт `v0.1.5`. Строки `OfficeCLI ...` и `OFFICECLI_ALT_USERSPACE_COMPATIBLE` относятся только к кандидату `v0.1.5`; остальные исторические утверждения о `v0.1.0` от этого не изменяются. `Required` означает обязательную проверку exact candidate, а `Informational` не является release gate и не разрешает заявлять неподтверждённую платформенную или сетевую совместимость.

| Claim | Environment | Frequency | Gate |
| --- | --- | --- | --- |
| Windows amd64 | `windows-2025` exact candidate | PR/push | Required |
| Linux amd64 | `ubuntu-24.04` exact candidate | PR/push | Required |
| macOS amd64 | `macos-15-intel` exact candidate | PR/push | Required |
| macOS arm64 | `macos-15` exact candidate | PR/push | Required |
| OfficeCLI Windows amd64 runtime | ephemeral `windows-2025` effective OS profile, exact `v1.0.144` asset | Manual dispatch bound to exact SHA | Required acceptance gate; missing/failed smoke rejects release |
| OfficeCLI Linux amd64 runtime | `ubuntu-24.04`, exact `v1.0.144` asset | Manual dispatch bound to exact SHA | Required acceptance gate |
| OfficeCLI macOS amd64 runtime and trust policy | `macos-15-intel`, exact `v1.0.144` asset, `codesign` + `spctl` | Manual dispatch bound to exact SHA | Required acceptance gate |
| OfficeCLI macOS arm64 runtime and trust policy | `macos-15`, exact `v1.0.144` asset, `codesign` + `spctl` | Manual dispatch bound to exact SHA | Required acceptance gate |
| `OFFICECLI_ALT_USERSPACE_COMPATIBLE` | userspace-only ALT p11; exact image `ghcr.io/i437918/kit-all-team/alt-p11-officecli@sha256:5ee493c6c7edbdb8d68fb0ab9af2847bae855c9042bc5f13f5fd6b3d0965a825`; providers `glibc-pthread-6:2.38.0.223.f053ff-alt1.p11.1.x86_64`, `glibc-utils-6:2.38.0.223.f053ff-alt1.p11.1.x86_64`, `libicu74-1:7.4.2-alt1.x86_64` | Manual dispatch bound to exact SHA | Required acceptance gate |
| Current macOS arm64 | `macos-26` | Nightly | Required nightly |
| Hermes Windows static contract | disposable `windows-2025` | Manual, opt-in visible launch | Informational; never a release gate |
| `ALT_USERSPACE_COMPATIBLE` | public base `registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea`; no GHCR or OfficeCLI | PR/push | Required |
| `ALT_VM_VERIFIED` | official ALT p11 QCOW2 under QEMU TCG | Nightly/manual | Not verified for v0.1.0; informational, not a release gate |
| `ALT_NATIVE_RUNNER_VERIFIED` | `self-hosted,linux,x64,alt-p11` | Manual | Not verified; no eligible runner, not a release gate |
| Trusted corporate network probe | eligible self-hosted corporate/VPN runner | Manual | Not verified; hosted runner cannot resolve internal DNS, not a release gate |
| Binary signing | Windows/Linux/macOS artifacts | Release | Team Kit binaries are unsigned; macOS is not Apple-signed or notarized |
| Environment mode menu | Windows, Linux, macOS Intel/ARM; ALT contract | Every PR/push | Exact add/update dispatch, invalid/EOF/cancel paths |
| Operation-first receipt | Windows and POSIX quoting | Every PR/push | Receipt inspected before owner/`.env`; `RETRY_REQUIRED` has exact command |
| Registry states | Windows/macOS/Linux/ALT paths | Every PR/push | Missing/Valid/Corrupt/Unavailable, strict JSON, 64 entries/65536 bytes |
| No-op barrier | Hermetic process and service spies | Every PR/push | `Ничего` performs no post-choice reads, credentials, network or writes |
| Registry promotion failure | Hermetic service/CLI spies | Every PR/push | Warning and exit 0 after product success; no rollback/retry |
| Non-Hermes handoff | 10 applications × 2 toolchains | Every PR/push | One secret-free selected pinned toolchain plus v8std; missing app fails early |
| Hermes toolchain | New and existing profile × 3 roles × 2 toolchains | Every PR/push | Exactly one selected toolchain; unselected toolchain absent |

Every native lane runs unit, contract, integration, black-box, vet, and race
checks with `TEAMKIT_TEST_BINARY` bound to the exact binary downloaded from
`build-candidate`. The black-box lifecycle covers version, help, plan, status,
secret-free alternative-app configure/verify, retry/update, stable JSON, and
rejected secret flags without network or secret access. It also uses a real
temporary launcher on `PATH` for the positive case and proves that a claimed
but absent launcher fails before workspace creation. The catalog contract
enumerates 264 combinations. Process-level add/update tests isolate all user and
configuration homes, use only local Git fixtures, prove registry confidentiality,
MRU selection, automatic single selection, the no-op barrier, operation-first
retry, and rejection of every unknown network remote. Live internal
endpoint probes run only on trusted `main`, schedules, or manual dispatch and
never replace hermetic fixtures.

OfficeCLI network-backed runtime checks are deliberately absent from
`pull_request` and ordinary `push`. A trusted `workflow_dispatch` requires an
exact `expected_sha`; the workflow rejects an empty or mismatched value before
checkout or download. The existing four-lane native matrix then downloads only
the catalog URL for the selected platform, verifies exact size/SHA-256, uses the
production provisioner to persist and read back `autoUpdate=false`, verifies
`v1.0.144`, and performs two bounded line-delimited JSON-RPC handshakes. The
macOS lanes additionally require Developer ID signature and notarization-policy
assessment. Windows runs only in the disposable GitHub-hosted account and
checks the effective OS profile, including a pre-existing-skill refresh fixture;
changing `HOME` or `USERPROFILE` is not treated as isolation.

Both macOS lanes still run strict `codesign` and exactly one
`spctl --assess --type execute` against the exact catalog-verified bare Mach-O.
Normal `spctl` success remains accepted. The only accepted nonzero result is
exit 3 whose C-locale output, after removing one terminal LF and then only the
exact known evidence-path prefix, is exactly
`rejected (the code is valid but does not seem to be an app)`. Capture is
limited to 4096 bytes; overflow, another exit, or any other output fails the
lane. This narrow bare-Mach-O classification does not relax SHA-256 or strict
signature verification and does not assess a wrapper, archive, or substituted
asset.

The dispatch-only ALT step reuses the verified Linux evidence bytes and the
same compiled Go live test inside the pinned p11 container. It does not download
OfficeCLI or implement MCP framing in shell. The normal one-argument ALT script
path is the public-base PR/push check: it pulls only the exact public base, uses
no GHCR credentials, and does not run OfficeCLI.

If the unchanged production live smoke fails, the same test binary is rerun
once in the same exact pinned image with a test-only stage/stderr diagnostic
adapter. Each emitted record is ASCII-only and at most 4096 bytes. The script
always returns the primary failure status; the diagnostic rerun cannot emit
`OFFICECLI_ALT_USERSPACE_COMPATIBLE` or qualify a release.

Container success is not native ALT verification. For `v0.1.0`, only pinned p11
userspace is confirmed. The available QEMU workflow can download an exact CI
artifact and record the console, `/etc/os-release`, kernel identity and build
metadata, but no successful final QEMU/VM or native ALT evidence is claimed.
BaseALT's `SHA256SUM` and `SHA256SUM.asc` are verified against the pinned
repository key and reviewed fingerprint before the pinned image SHA-256 is
accepted.

The manual Hermes workflow can verify the exact Team Kit candidate, installer
SHA-256, and Authenticode signer. Its optional GUI process launch records only an
observation: it does not prove automatic or unattended installation, UI
completion, or the selected `HERMES_HOME`.
