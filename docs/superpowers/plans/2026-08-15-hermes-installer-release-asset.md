# Hermes Installer GitLab Asset Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the pinned `Hermes-Setup.exe` to GitLab Release `v0.1.0-rc.1` and document the official-site-first installation and CustomLLM prerequisites for ordinary users.

**Architecture:** Keep installer bytes outside Git history. Validate and upload the exact signed EXE through GitLab APIs with an in-memory credential, verify the release download byte-for-byte, then update README under a release-contract test.

**Tech Stack:** PowerShell 7, Windows Authenticode, GitLab Project Uploads and Releases APIs, Go release-contract tests, Russian Markdown.

## Global Constraints

- Target only existing GitLab Release `v0.1.0-rc.1`; do not create or resume `v0.1.0-rc.2`.
- Upload only `C:\Users\Dmitriy\Downloads\Hermes-Setup.exe`, size `7597376`, SHA-256 `505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5`, with valid signer `Nous Research Inc.`.
- Never print, persist, or place the GitLab token in a URL or command argument.
- Do not add the EXE, `certs.zip`, PEM files, or downloaded verification copies to Git history.
- Preserve the existing `certs.zip` release asset and all four Team Kit binaries.
- Publish README only after the remote EXE bytes have been downloaded and verified.
- Do not add a source-text test for human README prose; verify the real remote asset and retain the existing release suite.

---

### Task 1: Upload and verify the exact installer

**Files:**
- External source: `C:\Users\Dmitriy\Downloads\Hermes-Setup.exe`
- External target: GitLab Project Upload and Release `v0.1.0-rc.1`

**Interfaces:**
- Consumes: Windows Credential Manager entry for `gitlab.tools.enterprise.ru`, GitLab APIs, pinned EXE identity.
- Produces: exactly one release asset named `Hermes-Setup.exe` with direct asset path `/Hermes-Setup.exe`.

- [x] **Step 1: Revalidate local identity**

Run PowerShell checks and stop unless every assertion succeeds:

```powershell
$exe = 'C:\Users\Dmitriy\Downloads\Hermes-Setup.exe'
$item = Get-Item -LiteralPath $exe
$hash = (Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.ToLowerInvariant()
$signature = Get-AuthenticodeSignature -LiteralPath $exe
if ($item.Length -ne 7597376) { throw 'HERMES_INSTALLER_SIZE_MISMATCH' }
if ($hash -ne '505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5') { throw 'HERMES_INSTALLER_HASH_MISMATCH' }
if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notlike 'CN=Nous Research Inc.*') { throw 'HERMES_INSTALLER_SIGNATURE_INVALID' }
```

- [x] **Step 2: Resolve the credential only in process memory**

Pipe `protocol=https`, `host=gitlab.tools.enterprise.ru`, and
`path=1c/aisuz/ai.git` to `git credential fill`. Parse the returned key/value
lines into a PowerShell hashtable, require a nonblank password, and create
`@{'PRIVATE-TOKEN'=$credential.password}`. Never print either collection.

- [x] **Step 3: Upload or verify idempotently**

Read `/api/v4/projects/1c%2Faisuz%2Fai/releases/v0.1.0-rc.1`. If one
`Hermes-Setup.exe` link exists, skip creation and verify it. If none exists,
upload the exact file to `/api/v4/projects/1c%2Faisuz%2Fai/uploads`, then POST
one release asset link with:

```powershell
@{
    name              = 'Hermes-Setup.exe'
    url               = $uploadedFullURL
    link_type         = 'other'
    direct_asset_path = '/Hermes-Setup.exe'
}
```

Reject more than one matching link and do not replace `certs.zip`.

- [x] **Step 4: Verify the release redirect and download exact bytes through the API**

Require
`https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.0-rc.1/downloads/Hermes-Setup.exe`
to redirect to the exact project-scoped upload URL recorded by the release. The
private browser route requires an authenticated GitLab session and does not accept
`PRIVATE-TOKEN`, so download the bytes through
`/api/v4/projects/1c%2Faisuz%2Fai/uploads/<secret>/Hermes-Setup.exe` with the
in-memory header to a `New-TemporaryFile` path. Require size `7597376` and the
pinned SHA-256, then remove only that exact temporary file in `finally`. Re-read
the release and require exactly one `Hermes-Setup.exe` and one `certs.zip` asset.

### Task 2: Update README

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: verified direct release asset and approved design.
- Produces: official-site-first Windows instructions and complete Hermes prerequisites.

- [x] **Step 1: Update «Что потребуется до начала»**

State that Team Kit configures CustomLLM model `generic-development`; the
user must obtain its token and complete the corporate «Начало работы» procedure
at Confluence page `1017637995`. Explain that system certificates are installed
per that guide, while local `certs.zip` is used when postal models remain
unreachable and is required beside the current Team Kit binary for Hermes setup.

- [x] **Step 2: Update release downloads and checksums**

Add the direct `Hermes-Setup.exe` release link beside `certs.zip` and add this row:

```markdown
| `Hermes-Setup.exe` | `505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5` |
```

- [x] **Step 3: Update «Hermes в Windows»**

Tell the user to install from `https://hermes-agent.nousresearch.com/` first. If
the site or download is unavailable, use `Hermes-Setup.exe` from the same GitLab
Release, verify SHA-256 and signer `Nous Research Inc.`, complete the graphical
installation, and then start Team Kit. Preserve the fail-closed
`HERMES_WINDOWS_INSTALL_DIR_UNVERIFIED` explanation.

- [x] **Step 4: Verify existing release contracts**

Run:

```powershell
$env:GOCACHE=(Resolve-Path '.tools\go-cache').Path
$env:GOMODCACHE=(Resolve-Path '.tools\gomodcache').Path
& '.tools\go\go\bin\go.exe' test ./test/release -count=1
```

Expected: PASS without adding a test that asserts literal README prose.

### Task 3: Repository verification and publication

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/plans/2026-08-15-hermes-installer-release-asset.md`

**Interfaces:**
- Consumes: verified remote asset and green release contract.
- Produces: documentation commit published by fast-forward to GitLab `master`.

- [x] **Step 1: Run complete verification**

```powershell
$env:GOCACHE=(Resolve-Path '.tools\go-cache').Path
$env:GOMODCACHE=(Resolve-Path '.tools\gomodcache').Path
& '.tools\go\go\bin\go.exe' vet ./...
& '.tools\go\go\bin\go.exe' test ./... -count=1
git diff --check
git ls-files | rg -i 'Hermes-Setup\.exe|certs\.zip|\.pem$|\.key$'
```

Expected: vet/tests/diff checks pass and the tracked-file scan prints nothing.

- [x] **Step 2: Commit only text and tests**

```powershell
git add README.md docs/superpowers/plans/2026-08-15-hermes-installer-release-asset.md
git commit -m "docs: publish Hermes installer fallback"
```

- [x] **Step 3: Publish without force**

Fetch GitLab `master`, require `git rev-list --left-right --count FETCH_HEAD...HEAD`
to report zero remote-only commits, then push `HEAD:master` without force. Verify
`git ls-remote ... refs/heads/master` equals local `HEAD` and re-read the release
API to confirm both `Hermes-Setup.exe` and `certs.zip` remain present.
