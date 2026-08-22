# Russian Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Перевести три пользовательских руководства на русский язык и привести их к подтверждённому состоянию локального `v0.1.0-rc.1`.

**Architecture:** Поясняющий Markdown-текст заменяется русским, а машинно значимые команды, пути, переменные, коды ошибок и CI-маркеры сохраняются. Устаревшие release-утверждения заменяются фактами из текущего `dist/SHA256SUMS` и локальных evidence; тесты меняются только для новых русских формулировок.

**Tech Stack:** Markdown, Go release-contract tests, PowerShell, Git.

## Global Constraints

- Не переводить команды, пути, URL, версии, переменные окружения, стабильные коды ошибок и CI-маркеры.
- Не заявлять о публикации GitHub-релиза или нативной проверке macOS/ALT без evidence.
- Сохранить `AGENTS.md` в пределах 200–400 слов.
- Не ослаблять машинные release-контракты.

---

### Task 1: Руководство участника

**Files:**
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: текущая структура `cmd/`, `internal/`, `test/`, `scripts/`, `docs/`.
- Produces: краткое русскоязычное руководство для разработчиков.

- [ ] **Step 1: Перевести и актуализировать структуру и команды**

Сохранить команды `go test ./...`, `go test -race ./...`, `go vet ./...`, `go fmt ./...`, `go build ./cmd/teamkit`, `make verify` и добавить актуальные пакеты безопасности без расширения объёма.

- [ ] **Step 2: Проверить объём**

Run: PowerShell-подсчёт слов после удаления Markdown-кода.
Expected: от 200 до 400 слов.

### Task 2: Руководство установки и внешние блокеры

**Files:**
- Modify: `docs/INSTALL.md`
- Modify: `docs/EXTERNAL-BLOCKERS.md`

**Interfaces:**
- Consumes: `dist/SHA256SUMS`, `dist/evidence/alt-qemu-local/LOCAL-RESULT.txt`, commit `e59abc66955447cfc42438cf2629a6a0f00fb1aa`.
- Produces: точные русскоязычные инструкции и перечень неподтверждённых внешних проверок.

- [ ] **Step 1: Исправить статус RC**

Указать, что четыре локальных неподписанных артефакта собраны, но GitHub-релиз не опубликован. Сохранить команды проверки только выбранного Unix-бинарника.

- [ ] **Step 2: Перевести эксплуатационные ограничения**

Сохранить `HERMES_WINDOWS_INSTALL_DIR_UNVERIFIED`, `HERMES_EXECUTABLE_UNVERIFIED`, `AI_APP_REQUIRED`, `GIT_ASKPASS`, `HERMES_HOME` и `KIT_ALL_TEAM_HOME` без перевода.

- [ ] **Step 3: Актуализировать внешние блокеры**

Добавить подтверждённые ограничения нативной macOS, ALT p11, локального ALT QEMU и Hermes `0.20.0` против требуемого `0.20.1`; не превращать локальные/cross-build проверки в native claims.

### Task 3: Release-контракты и проверка

**Files:**
- Modify if required: `test/release/docs_test.go`
- Modify if required: `test/release/workflow_lifecycle_test.go`

**Interfaces:**
- Consumes: переведённые `docs/INSTALL.md` и `docs/EXTERNAL-BLOCKERS.md`.
- Produces: тесты, проверяющие русские пользовательские формулировки при сохранённых машинных маркерах.

- [ ] **Step 1: Запустить release-тесты и зафиксировать ожидаемый RED**

Run: `go test -count=1 ./test/release`
Expected: FAIL только на переведённых английских фрагментах, если они проверялись буквально.

- [ ] **Step 2: Обновить только текстовые ожидания**

Заменить английские фразы в массивах ожиданий русскими эквивалентами; команды и маркеры оставить прежними.

- [ ] **Step 3: Выполнить итоговые проверки**

Run: `go test -count=1 ./test/release`
Expected: PASS.

Run: `git diff --check`
Expected: exit code 0.

Run: `rg -n "private release|Install the Internal Release Candidate|External Blockers" AGENTS.md docs/INSTALL.md docs/EXTERNAL-BLOCKERS.md`
Expected: совпадений нет.

- [ ] **Step 4: Зафиксировать результат**

```bash
git add AGENTS.md docs/INSTALL.md docs/EXTERNAL-BLOCKERS.md test/release/docs_test.go test/release/workflow_lifecycle_test.go
git commit -m "docs: translate and update Russian guides"
```
