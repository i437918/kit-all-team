# OfficeCLI qualification record

## Decision

`QUALIFIED_PINNED_AUTOUPDATE_DISABLED_SOURCE`

This source-only decision permits Tasks 1–5. It does **not** claim that a
Windows binary was started, that its persisted configuration was read back, or
that an MCP handshake occurred. Task 9 must execute the disposable Windows
technical smoke before recording
`QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME`; a missing or failed smoke
blocks Release. Task 10/Release separately requires accepted corporate Windows
policy/equivalence evidence or a formal waiver.

No `latest` release was requested or accepted. A future upstream release is
not accepted automatically.

## Runtime qualification gate

Source-record status: **outcome-neutral; runtime acceptance is conditional on external exact-SHA evidence**.

This source commit records no future PASS or FAIL. Runtime acceptance for its
exact SHA is determined only by one matching GitHub `workflow_dispatch` run and
one matching GitLab push pipeline; their later result remains external evidence
and requires no source-only status commit. Исходная запись нейтральна к результату. Итог runtime определяется только на основании external exact-SHA evidence, привязанного к SHA кандидата; успешный инженерный результат сам по себе не публикует `v0.1.5` и не закрывает отдельные corporate Windows/release gates.

An exact candidate is accepted only if its matching external GitLab push
pipeline and all four native GitHub jobs (`windows-2025`, `ubuntu-24.04`,
`macos-15-intel`, `macos-15`) plus the GitHub ALT p11 job pass. A manual
dispatch must provide `expected_sha` equal to `GITHUB_SHA`; the workflow fails
before checkout or any download when that value is missing or different. Pull
requests and ordinary pushes use the exact public ALT base only; they neither
authenticate to GHCR nor run OfficeCLI and do not constitute runtime
qualification evidence.

The native smoke uses the production catalog, bounded downloader, digest
verifier, atomic `0700` writer and OfficeCLI provisioner. Its first OfficeCLI
invocations are fixed `config autoUpdate false` and exact read-back `false`.
Only after persisted JSON is independently checked does it verify version
`1.0.144` and run bounded line-delimited JSON-RPC `initialize`,
`notifications/initialized`, and `tools/list` twice. Responses are bound to
request IDs; each stage is limited to 10 seconds and 1 MiB, each process to 30
seconds. The catalog SHA-256 is required before configuration/MCP and after the
second start, and updater siblings remain absent.

Unix lanes isolate HOME, XDG and temporary roots and allow only the OfficeCLI
config delta. Windows refuses to run outside an ephemeral GitHub-hosted OS
account, uses the effective Known Folder profile rather than environment-only
home substitution, and permits refresh writes solely inside one preseeded
existing OfficeCLI skill identity. A second sequential start must leave the
profile manifest unchanged. The fixture is disposable CI evidence; Team Kit
does not install it. Both macOS lanes additionally require `codesign --verify
--strict --verbose=2` and `spctl --assess --type execute --verbose=4` on the
exact downloaded asset. ALT receives the already verified Linux bytes and the
same compiled Go protocol test; it has no installer, download, or shell MCP
parser.

Strict `codesign` and exact SHA-256 remain mandatory. The exact-asset `spctl`
command also remains mandatory and runs exactly once. Its usual zero exit is
accepted; a nonzero result is accepted only for exit 3 with bounded C-locale
output exactly equal to
`rejected (the code is valid but does not seem to be an app)` after removing one
terminal LF and then only the exact evidence-path prefix. Overflow, any other
exit/output, or assessment of a generated wrapper/archive fails closed.

The ALT primary run still uses the production provisioner and common MCP smoke
without diagnostic adapters. Only after that run has returned nonzero does the
same test binary rerun once in the same exact pinned image with a test-only
adapter that records `config-set` and `config-read-back` status, bounded
stdout/stderr, and the child error. Records are ASCII-only and no more than 4096
bytes. The outer script always exits with the primary status, so a successful
diagnostic rerun cannot produce compatibility evidence or bypass config/MCP.

## ALT qualification-image evidence (not runtime qualification)

Publication run: https://github.com/dmitry-m1man/kit-all-team/actions/runs/32541524174

Event: `push`

- Publication source SHA: `9964ba4dd9f30cf115da223fa7554345e8a8bdfe`.
- Exact base image: `registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea`.
- Exact qualification image: `ghcr.io/dmitry-m1man/kit-all-team/alt-p11-officecli@sha256:fe1aef6ae65d887389aa11bd9f9bdf99a924b4f6f587edaeb37307b3e2e99a48`.
- `/lib64/librt.so.1` provider: `glibc-pthread-6:2.38.0.223.f053ff-alt1.p11.1.x86_64`.
- `/usr/bin/ldd` provider: `glibc-utils-6:2.38.0.223.f053ff-alt1.p11.1.x86_64`.
- `/usr/lib64/libicuuc.so.74` and `/usr/lib64/libicudata.so.74` provider:
  `libicu74-1:7.4.2-alt1.x86_64`.
- OfficeCLI Linux asset: `35316133` bytes; SHA-256
  `32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8`.

Both the unpushed image candidate and the subsequently pulled exact digest
passed provider ownership checks, absolute `/usr/bin/ldd /opt/officecli`, the
no-`not found` check, persisted configuration read-back, exact version, and MCP
live checks. This is image-construction/userspace evidence.

It does not establish `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME`, does not qualify the final feature candidate, and does not replace the required exact-SHA GitHub native/ALT run or the matching GitLab pipeline.

## Supported product contract

OfficeCLI is added only to the Hermes profile. Team Kit does not add it to the
handoff for other applications. The accepted executable set is closed:

| Platform | Qualified OfficeCLI asset |
| --- | --- |
| Windows x64 | `officecli-win-x64.exe` |
| Linux x64 | `officecli-linux-x64` |
| macOS Intel | `officecli-mac-x64` |
| macOS Apple Silicon | `officecli-mac-arm64` |
| ALT Linux p11 x64 | `officecli-linux-x64` |

ALT reuses the qualified Linux amd64 bytes and has a separate p11 userspace
smoke. It is not a fifth build and does not claim native ALT qualification.
The managed path is
`${HERMES_HOME}/.teamkit/officecli/v1.0.144/officecli`, with `officecli.exe` as
the Windows filename. No PATH change, system installation, new installer, or
updater is part of this contract, and old pinned versions are not removed.

The exact `v1.0.144` release is accepted because its source, release identity,
four assets, sizes, and independent SHA-256 pins are bound below. A future
`latest` is not substituted. `OFFICECLI_SKIP_UPDATE` is not used as MCP control
because upstream dispatches `mcp` before that guard.

The fixed command sequence is `officecli config autoUpdate false`, exact
`officecli config autoUpdate` read-back, and independent verification of the
user-global `${UserProfile}/.officecli/config.json`. Only exact persisted
`false` allows cleanup. Detection is fail-closed; cleanup is restricted to
`.update`, `.update.partial`, `.old` inside the owned managed parent.

The accepted residual behavior is best-effort refresh of only previously
installed OfficeCLI skills in every discovered agent home. Existing local
edits can be overwritten. Skills such as `officecli-pptx`, `officecli-docx`,
`officecli-xlsx`, and others are file instruction/reference packs, not more
MCP servers. Team Kit installs no on-disk OfficeCLI skills, does not rely on a
default Hermes skill directory, and uses the built-in `load_skill` command of
the one `officecli` MCP tool.

The `officecli` tool can read and modify Office documents. Retry reuses the
existing `configure_application` action. After configuration the Hermes
profile contains four MCP entries: v8std, Jira, Confluence, and OfficeCLI.
These product capabilities do not determine the external exact-SHA verdict.

## Immutable upstream evidence

The controller-provided, non-executed source/asset snapshot at
`task0-upstream/evidence.json` records:

- repository: `iOfficeAI/OfficeCLI`
- release ID: `369836880`
- tag: `v1.0.144`
- peeled commit: `1ced45e900782c5083ed550ddf328ee974e425e7`
- publication timestamp: `2026-08-13T10:53:09Z`
- SHA256SUMS file SHA-256:
  `1a97c51cacdaed13df326233553a57adfe54f8b8264bd0a7458b87e6a8041d36`

All listed source files were re-hashed locally and their byte lengths and
SHA-256 values match the evidence:

| File | Bytes | SHA-256 |
| --- | ---: | --- |
| `.github/workflows/build.yml` | 9488 | `e4746afc0d4642d16861ec70a208f38932a0f78ef88328973edf857a4d1fee22` |
| `LICENSE` | 11375 | `7e282402a5a6db33995fe638bb3fe79013f9884d8f7d15a42e481c1e86aadda1` |
| `src/officecli/Core/Installer.cs` | 18907 | `f9ee5ef91ab4b8568719a4040cdd6d33df6a2685e989248331e729a560e5842e` |
| `src/officecli/Core/SkillInstaller.cs` | 35714 | `73e4d9ce52e72685741375adc6d18cc3f25c47d90abf5e99a7127118aa32bfe5` |
| `src/officecli/Core/UpdateChecker.cs` | 39353 | `81052f005f1e7090bd400ded1b06126bfc8eda8f17de886093f8398377848dbf` |
| `src/officecli/McpServer.cs` | 32341 | `bd4c734d6b4e6f53977ff6097ac72ad55a651e472ece7b04abaef106a31e0aa9` |
| `src/officecli/officecli.csproj` | 2697 | `598147d3430259c7cf1de3106bebd7dc0f55f6597695f14699f56f547b2ba279` |
| `src/officecli/Program.cs` | 12431 | `67e692e5307456b4e4de9cf3b5b575e52aa181442abd355120e6f64e8013d50f` |

`officecli.csproj` declares version `1.0.144`.

## Qualified assets

The snapshot contains exactly the four required distinct, non-empty assets.
Each is at most 48 MiB; its lower-case `sha256:` API digest is well-formed; the
API digest (without its prefix), `SHA256SUMS`, and locally re-hashed file are
identical.

| Asset ID | Name | URL | Bytes | SHA-256 |
| ---: | --- | --- | ---: | --- |
| 512852026 | `officecli-win-x64.exe` | `https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-win-x64.exe` | 33382312 | `e780cc6a5385f84b4d54d71b0c179904ed534125ec33fe39b1a8711fa80e387e` |
| 512852030 | `officecli-linux-x64` | `https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-linux-x64` | 35316133 | `32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8` |
| 512852027 | `officecli-mac-x64` | `https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-mac-x64` | 34705536 | `366100643d757b0da24829422897ca74768a894b5ecd1a471a1336f8e2a0787d` |
| 512852025 | `officecli-mac-arm64` | `https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-mac-arm64` | 33760816 | `04757163428c5bde8d91e8f838517818e74722157722ca5f3877b6716b77bd45` |

## Source-reviewed policy

Accepted policy:
`auto_update_disabled_user_config/existing_skills_refresh_accepted`.

The reviewed exact source establishes all of the following.

1. `Program.cs` dispatches `officecli mcp` before the general
   `OFFICECLI_SKIP_UPDATE` guard. That environment variable is not an MCP
   control and is excluded from the Team Kit profile and contract.
2. `officecli config autoUpdate false` is an early config branch. The config
   location is `${UserProfile}/.officecli/config.json`. Its setter returns
   success after calling `SaveConfig(config)` without testing that boolean, so
   Team Kit must independently read back and require exact `false`.
3. With `autoUpdate=false`, `CheckInBackground` skips `ApplyPendingUpdate`,
   performs the bounded existing-skill refresh, then returns before
   `SpawnRefreshProcess`; it therefore neither applies a pending binary nor
   starts the HTTP/download path that creates `.update`, `.update.partial`, or
   `.old`.
4. `RefreshInstalled` requires the agent directory, skill directory, and that
   skill's existing `SKILL.md`; it creates no new agent or sub-skill identity.
   It can overwrite existing user files and add bundled files inside the
   existing skill directory. Its `lastSkillRefreshVersion` marker is saved
   best-effort, so normal sequential same-version content idempotence is
   accepted but exactly-once behavior under concurrent starts or failed
   persistence is not claimed.
5. Team Kit does not install on-disk OfficeCLI skills. The single MCP tool is
   `officecli`; its built-in `load_skill` command reads embedded guidance for
   the target Hermes profile.
6. Config and MCP early dispatch bypass `Installer.MaybeAutoInstall`.
   `MaybeAutoInstall` itself returns for any non-empty argv, so `--version`
   does not invoke bare-command auto-install. Team Kit never invokes OfficeCLI
   with an empty argv.

Source-only MCP framing is line-delimited JSON-RPC over stdio: `McpServer`
uses `ReadLineAsync`/`WriteLineAsync`, advertises JSON-RPC 2.0 `initialize`,
`protocolVersion` `2024-11-05`, `serverInfo.name` `officecli`, and exactly one
tool named `officecli`. This is not runtime-handshake evidence.

## License

The exact `LICENSE` is Apache License 2.0, with
`SPDX-License-Identifier: Apache-2.0` and copyright
`Copyright 2026 OfficeCLI (https://OfficeCLI.AI)`. Team Kit's model of direct
download and use of the selected upstream asset does not redistribute upstream
bytes in the Team Kit repository or release; it is compatible with this license
for the qualified use. Any future redistribution must preserve the applicable
Apache 2.0 license and notice obligations.

## Hermes stdio schema evidence

The provided exact Hermes source evidence for pinned commit
`f80f453ae0679347e38abc917c7f94f717bf96c5` was inspected in
`hermes_cli__mcp_config.py`, `hermes_cli__mcp_startup.py`, and
`tests__hermes_cli__test_mcp_config.py`. It stores a map at
`mcp_servers`, accepts stdio `command` with optional `args`, supports HTTP
`url` entries in the same map, and tests an HTTP plus stdio configuration.

Team Kit's required declaration is:

```yaml
mcp_servers:
  officecli:
    command: C:\absolute\path\officecli.exe
    args:
      - mcp
    enabled: true
```

The POSIX variant uses an absolute POSIX command path. The OfficeCLI entry has
no `env`; Hermes accepts that omission. Absolute-path enforcement is a Team Kit
managed-contract requirement, not a claim that the reviewed Hermes parser
enforces it itself. The source evidence supports mixed HTTP plus stdio MCP;
`HERMES_STDIO_MCP_UNSUPPORTED` does not apply.

## Remaining gates

Runtime acceptance remains conditional: the external GitLab push pipeline and
all four native plus ALT GitHub jobs must pass for the same exact candidate SHA.
The resulting evidence bundle belongs in the final task completion report and
later MR/release record, not in a post-CI source-only status commit.

Corporate Windows policy/equivalence evidence or a formal waiver remains a
separate Task 10 / Release gate. No verifier workflow/run is recorded for this
source-only gate.
