# Russian Numbered Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the English free-text interactive questionnaire with Russian numbered menus while preserving every non-interactive selector and stored domain ID.

**Architecture:** Add one reusable choice-menu primitive in `internal/cli`, then compose catalog-backed menus in the existing questionnaire. Keep free-text paths separate, translate only user-facing secret labels in the console adapter, and leave service/domain behavior unchanged.

**Tech Stack:** Go 1.26.6 standard library, existing catalog/domain packages, table-driven tests, Russian Markdown.

## Global Constraints

- Keep all 11 existing AI applications as direct choices; do not add a Hermes/Other grouping.
- Persist stable IDs such as `windows`, `codex`, `developer`, and `cc_1c_skills`, never menu numbers.
- Fixed choices accept numbers only and retry invalid input; free-text paths and masked secrets are not numbered.
- Preserve non-interactive CLI flags and existing `.env` decoding.
- Do not replace the published `v0.1.0-rc.1` binaries.

---

### Task 1: Numbered Russian questionnaire

**Files:**
- Modify: `internal/cli/prompt.go`
- Create: `internal/cli/prompt_test.go`
- Modify: `internal/cli/run_test.go`

**Interfaces:**
- Consumes: `catalog.OperatingSystems()`, `catalog.AIApplications()`, `catalog.Projects()`, `catalog.Roles()`, `catalog.Toolchains()`.
- Produces: `questionnaire.askChoice(ctx, *string, question, []choice)` and `questionnaire.askText(ctx, *string, prompt)`.

- [x] **Step 1: Add failing menu tests**

Test exact Russian rendering, number-to-ID mapping, invalid-number retry, empty EOF, and context cancellation. Update interactive Runner fixtures to use numeric answers while retaining non-interactive tests unchanged.

- [x] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/cli -count=1
```

Expected: failures because the old questionnaire still accepts textual selectors and prints English prompts.

- [x] **Step 3: Implement the minimal menu primitive and questionnaire wiring**

Define:

```go
type choice struct {
    value string
    label string
}
```

`askChoice` prints the question, numbered labels, and `Введите номер ответа: `. It parses a trimmed integer in `[1,len(choices)]`, assigns the associated stable value, and repeats with `Некорректный номер. Введите число от 1 до N.`. `askText` prints one Russian prompt and rejects an empty EOF with `INPUT_REQUIRED`.

- [x] **Step 4: Verify GREEN**

Run:

```powershell
go test ./internal/cli -count=1
go vet ./internal/cli
```

Expected: PASS.

### Task 2: Russian masked credential prompts

**Files:**
- Modify: `internal/credentials/console.go`
- Create: `internal/credentials/console_test.go`

**Interfaces:**
- Consumes: internal keys `GITLAB_USERNAME`, `GITLAB_TOKEN`, `HERMES_CUSTOM_LLM_API_KEY`.
- Produces: Russian display labels without changing keys passed through `credentials.Resolver`.

- [x] **Step 1: Add a failing console-output test**

Use a pipe-backed `ConsoleReader` and assert the output contains `Логин GitLab (GITLAB_USERNAME)`, `Токен GitLab (GITLAB_TOKEN)` and `Ключ CustomLLM (HERMES_CUSTOM_LLM_API_KEY)` followed by `(ввод скрыт)`.

- [x] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/credentials -run Console -count=1
```

Expected: FAIL on the previous raw English/environment-key prompt.

- [x] **Step 3: Add display-label mapping in the console adapter**

Map only known secret keys at output time. Unknown labels remain unchanged for compatibility. Do not change Resolver keys, secret persistence, redaction, or terminal masking.

- [x] **Step 4: Verify GREEN**

Run:

```powershell
go test ./internal/credentials -count=1
go vet ./internal/credentials
```

Expected: PASS.

### Task 3: User documentation and full verification

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/plans/2026-08-15-russian-numbered-wizard.md`

**Interfaces:**
- Consumes: the exact questionnaire implemented by Tasks 1–2.
- Produces: Russian user instructions matching the executable output.

- [x] **Step 1: Update README**

Replace the statement that questions are English and the old value-entry table with the numbered Russian flow. Explicitly retain text entry for paths and masked credentials.

- [x] **Step 2: Run full verification**

Run:

```powershell
go fmt ./internal/cli ./internal/credentials
go vet ./...
go test ./... -count=1
git diff --check
```

Cross-build:

```powershell
$env:CGO_ENABLED='0'
$env:GOOS='windows'; $env:GOARCH='amd64'; go build -o .tools/cross/teamkit-windows-amd64.exe ./cmd/teamkit
$env:GOOS='linux';   $env:GOARCH='amd64'; go build -o .tools/cross/teamkit-linux-amd64 ./cmd/teamkit
$env:GOOS='darwin';  $env:GOARCH='amd64'; go build -o .tools/cross/teamkit-darwin-amd64 ./cmd/teamkit
$env:GOOS='darwin';  $env:GOARCH='arm64'; go build -o .tools/cross/teamkit-darwin-arm64 ./cmd/teamkit
```

Expected: every command exits zero.

- [x] **Step 3: Commit**

```powershell
git add internal/cli internal/credentials README.md docs/superpowers/plans/2026-08-15-russian-numbered-wizard.md
git commit -m "feat(cli): add Russian numbered wizard"
```
