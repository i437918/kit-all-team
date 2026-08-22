# Russian User README Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the developer-oriented English `README.md` with a self-contained Russian guide that a non-technical employee can use on Windows, macOS, Linux, or ALT Linux.

**Architecture:** Keep one progressive-disclosure document: purpose and platform truth first, interactive installation next, recovery and security after that, and non-interactive CLI flags last. Treat the GitLab `v0.1.0-rc.1` release as the distribution source while preserving explicit limitations for Windows Hermes automation, native macOS verification, and native ALT Linux verification.

**Tech Stack:** Russian Markdown, Go CLI contract, GitLab Release links, PowerShell, POSIX shell.

## Global Constraints

- The primary path is interactive `teamkit apply`; long flag combinations belong only in “Для опытных пользователей”.
- One `KIT_ALL_TEAM_HOME` contains exactly one selected project and one deployed environment.
- Cover Windows amd64, Linux amd64, macOS Intel, macOS Apple Silicon, and ALT Linux without claiming unverified native support.
- Never instruct users to place secrets in CLI flags, Git URLs, the workspace `.env`, or shell history.
- Preserve the read-only database checkout and the prohibition on direct work in `develop`.
- State that Office document processing is excluded and may be added only as a future optional adapter.

---

### Task 1: Replace README with the end-user guide

**Files:**
- Modify: `README.md`
- Reference: `docs/superpowers/specs/2026-08-15-user-readme-design.md`
- Reference: `internal/cli/prompt.go`
- Reference: `internal/cli/run.go`
- Test: `test/release/docs_test.go`

**Interfaces:**
- Consumes: CLI commands `plan`, `apply`, `status`, `retry`, `update`, `version`; prompt values from `internal/cli/prompt.go`; GitLab release `v0.1.0-rc.1`.
- Produces: one Russian `README.md` serving as the default end-user entry point.

- [x] **Step 1: Record the current documentation checks**

Run:

```powershell
go test ./test/release -count=1
```

Expected: PASS before the documentation-only change.

- [x] **Step 2: Replace the README content**

Use these exact top-level sections in order:

```markdown
# 1C Team Kit
## Что делает программа
## Поддерживаемые системы
## Что потребуется до начала
## Загрузка программы
## Запуск в Windows
## Запуск в macOS
## Запуск в Linux и ALT Linux
## Ответы на вопросы мастера
## Если выбран Hermes
## Если выбрано другое AI-приложение
## Что появится в рабочей папке
## Проверка, повтор и обновление
## Безопасность
## Частые ошибки
## Известные ограничения
## Для опытных пользователей
## Дополнительная документация
```

Include the release page and the four exact artifact names:

```text
https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.0-rc.1
teamkit-v0.1.0-rc.1-windows-amd64.exe
teamkit-v0.1.0-rc.1-linux-amd64
teamkit-v0.1.0-rc.1-darwin-amd64
teamkit-v0.1.0-rc.1-darwin-arm64
```

Use `teamkit apply` as the primary interactive command. Explain all prompt values, all 11 project IDs (`aisuz`, `apa`, `asbnu`, `asku`, `easr`, `eisko`, `esed`, `uat`, `unip`, `zup`, `wms`), three roles, and the mutually exclusive toolchains. Give platform-specific SHA-256 and launch commands. Label macOS as cross-built but not natively verified and ALT Linux as intended/partially verified but not natively verified.

- [x] **Step 3: Verify factual markers and Markdown hygiene**

Run:

```powershell
rg -n "v0\.1\.0-rc\.1|teamkit apply|Windows|macOS|ALT Linux|не подтвержд|HERMES_WINDOWS_INSTALL_DIR_UNVERIFIED|AI_APP_REQUIRED|KIT_ALL_TEAM_HOME|develop" README.md
git diff --check
```

Expected: all required product markers are present; no whitespace errors.

- [x] **Step 4: Run release documentation tests**

Run:

```powershell
go test ./test/release -count=1
```

Expected: PASS.

- [x] **Step 5: Review for secret-safety and misleading claims**

Run:

```powershell
rg -n "--.*token|--.*key|полностью поддерж|нативно провер" README.md
```

Expected: no secret-bearing flags; every native-verification phrase is an explicit limitation rather than an unsupported success claim.

- [x] **Step 6: Commit the user guide**

```powershell
git add README.md docs/superpowers/plans/2026-08-15-user-readme.md
git commit -m "docs: add Russian user guide"
```
