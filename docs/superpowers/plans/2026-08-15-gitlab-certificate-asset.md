# GitLab Certificate Release Asset Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the pinned corporate CA archive as a `certs.zip` asset of GitLab release `v0.1.0-rc.1` and point ordinary users to it from the Russian README.

**Architecture:** Keep certificate bytes outside Git history. Upload the validated local archive to the private GitLab project, attach the returned URL to the existing release, verify the remote bytes, and document the asset and SHA-256.

**Tech Stack:** GitLab Releases API, GitLab Project Uploads API, PowerShell, Markdown, Go release tests.

## Global Constraints

- Never print, persist, or pass the GitLab token in a command argument.
- Publish only `G:\Почтатех\AI\certs.zip` with SHA-256 `88d85e7e7d64c061c195f93c517500bdc91fccfb9b5a8115da9f6a5a17e689f8`.
- Reject any archive containing private-key markers or private-key filename extensions.
- Do not add ZIP or PEM files to Git history.
- Do not replace or rebuild the four existing RC binaries.

---

### Task 1: Publish and document certs.zip

**Files:**
- Modify: `README.md`
- Create: `docs/superpowers/plans/2026-08-15-gitlab-certificate-asset.md`
- External: GitLab release `v0.1.0-rc.1`
- Test: `test/release`

**Interfaces:**
- Consumes: Windows Credential Manager GitLab credential, Project Uploads API, Releases API.
- Produces: GitLab release asset named `certs.zip` and a direct README download link.

- [x] **Step 1: Validate the local archive**

Calculate SHA-256, list ZIP entries, and scan filenames and bounded entry contents for private-key markers. Require the exact three CA PEM entries and the pinned digest.

- [x] **Step 2: Upload without exposing credentials**

Resolve the GitLab credential into process memory, upload with an HTTP header object rather than command-line arguments, and create an asset link named `certs.zip` with type `other`. If the link already exists, verify it instead of creating a duplicate.

- [x] **Step 3: Verify the remote release and bytes**

Read the release via API, require exactly one `certs.zip` asset, download it to a temporary file using authenticated HTTP, and require the pinned SHA-256 before deleting the temporary file.

- [x] **Step 4: Update README**

Replace every instruction to obtain `certs.zip` from an administrator with instructions to download it from the same GitLab release. Add the direct asset link and pinned SHA-256; retain the requirement to place the archive beside Team Kit for Hermes.

- [x] **Step 5: Verify documentation and repository safety**

Run:

```powershell
git diff --check
git ls-files | rg -i "certs\.zip|\.pem$|\.key$"
go test ./test/release -count=1
```

Expected: no tracked certificate/archive files and all release tests pass.

- [x] **Step 6: Commit documentation**

```powershell
git add README.md docs/superpowers/plans/2026-08-15-gitlab-certificate-asset.md
git commit -m "docs: link certificate release asset"
```
