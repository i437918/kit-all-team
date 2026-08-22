# GitLab Master Replacement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Заменить ветку `master` GitLab AISUZ AI текущим Go-проектом без объединения со старой историей, сохранить 11 `content-*` и удалить доступные ссылки на старый проект.

**Architecture:** Локальный проверенный `HEAD` становится новой удалённой `master` одним `force-with-lease`, привязанным к старому SHA. Защита ветки ослабляется только на время push и восстанавливается до очистки тегов, Releases, packages и MR.

**Tech Stack:** Git 2.x, GitLab protected branches, GitLab CI, Go 1.26.6, Chrome-authenticated GitLab UI.

## Global Constraints

- Target repository: `https://gitlab.example.invalid/1c/aisuz/ai.git`.
- Expected old `master`: `a9ccc8bebc87df4fc415934173ed46cd82895ed7`.
- Не объединять старую и новую историю.
- Не изменять ни одну ветку `content-*`.
- Не передавать GitLab token в URL, argv, plan, receipt или вывод команд.
- При любом неожиданном SHA остановиться без исправляющего force push.
- Восстановить запрет force push до удаления любых старых refs.

---

### Task 1: Актуализировать публичную документацию источника

**Files:**
- Modify: `README.md`
- Modify: `docs/EXTERNAL-BLOCKERS.md`
- Test: `test/release/docs_test.go`

**Interfaces:**
- Consumes: подтверждённый HTTPS-доступ к `content-aisuz` и `develop`.
- Produces: документацию без ложного блокера `LIVE_GITLAB_CREDENTIAL_MISSING` и с GitLab `master` как source authority.

- [ ] **Step 1: Обновить документацию**

Добавить в `README.md` ссылку на `https://gitlab.example.invalid/1c/aisuz/ai/-/tree/master` как авторитетный источник и удалить весь раздел `LIVE_GITLAB_CREDENTIAL_MISSING` из `docs/EXTERNAL-BLOCKERS.md`.

- [ ] **Step 2: Выполнить документальные проверки**

Run: `$env:GOCACHE=(Resolve-Path '.tools\go-cache').Path; & '.tools\go\go\bin\go.exe' test -count=1 ./test/release`
Expected: PASS.

Run: `rg -n "LIVE_GITLAB_CREDENTIAL_MISSING" README.md docs/EXTERNAL-BLOCKERS.md`
Expected: no matches.

- [ ] **Step 3: Зафиксировать изменение**

```bash
git add README.md docs/EXTERNAL-BLOCKERS.md
git commit -m "docs: mark GitLab source access verified"
```

### Task 2: Локальный gate перед публикацией

**Files:**
- Read: `go.mod`
- Read: `.gitlab-ci.yml`
- Produce ignored evidence: `.cache/master-replacement-security.json`

**Interfaces:**
- Consumes: финальный чистый локальный `HEAD` после Task 1.
- Produces: проверенный SHA, который разрешено отправить в `master`.

- [ ] **Step 1: Проверить дерево и запрещённые tracked paths**

Run: `git status --short`
Expected: empty.

Run: `git ls-files -- .env db .teamkit dist certs '*.pem' '*.key' teamkit.exe`
Expected: empty.

- [ ] **Step 2: Выполнить полный Go gate**

Run: `$env:GOCACHE=(Resolve-Path '.tools\go-cache').Path; & '.tools\go\go\bin\go.exe' vet ./...`
Expected: exit 0.

Run: `& '.tools\go\go\bin\go.exe' test -count=1 ./...`
Expected: PASS for every package.

- [ ] **Step 3: Проверить доступную историю и исходники аудитором**

Run: `$publishedCommit = git rev-parse HEAD; & '.tools\go\go\bin\go.exe' run ./cmd/teamkit-security-audit --repository . --commit $publishedCommit --output .cache/master-replacement-security.json`
Expected: `security-audit: passed` and `passed=true` in JSON.

### Task 3: Зафиксировать удалённое состояние

**Files:** none. Exact remote refs are compared in memory with the immutable table below; credentials and remote output are not written to repository files.

**Interfaces:**
- Consumes: read-only GitLab HTTPS credentials.
- Produces: lease SHA and полный снимок content refs.

- [ ] **Step 1: Сверить master**

Run: `git ls-remote --heads <target> refs/heads/master`
Expected: exactly `a9ccc8bebc87df4fc415934173ed46cd82895ed7 refs/heads/master`.

- [ ] **Step 2: Сохранить и проверить content refs**

Expected exact refs:

```text
e2be87876bd4ab30925381efc3d52d595271501b refs/heads/content-aisuz
50d9feedc74ab9a9f9786e960e2bed78ecf59517 refs/heads/content-apa
4a6c1e8c0bfd933cabf810f3a0fbe398d4df22f0 refs/heads/content-asbnu
0b88ff225a8d53ceb2eac8ee9f333de506543a17 refs/heads/content-asku
f699d62325a03d6908c1356c2e24d06b8c4344a5 refs/heads/content-easr
ff8be89e58242680730cdb098d7198ae54c98d00 refs/heads/content-eisko
fbd2ae5a1e57ab375ab2dbeb3f5728ec9c81f7c4 refs/heads/content-esed
cdee0393b1e0c97c986730c7e5c2cbc137d6f022 refs/heads/content-uat
41d6783f4e7fd02fbca6363e38d667527631bc59 refs/heads/content-unip
50d9feedc74ab9a9f9786e960e2bed78ecf59517 refs/heads/content-wms
58444b2e2aa666a0c38b35f53a297746dfe01bbd refs/heads/content-zup
```

### Task 4: Контролируемая замена master

**Files:** none.

**Interfaces:**
- Consumes: финальный local `HEAD`, exact lease и GitLab Owner browser session.
- Produces: удалённую `master`, равную local `HEAD`, с восстановленной защитой.

- [ ] **Step 1: Получить action-time подтверждение изменения branch protection**

Подтверждение должно прямо разрешать временно включить force push для `master`, выполнить один push и сразу выключить force push.

- [ ] **Step 2: Временно разрешить force push в GitLab UI**

На `Settings → Repository → Protected branches` оставить `master` protected и изменить только `Allowed to force push` с false на true.

- [ ] **Step 3: Выполнить единственный controlled push**

```bash
git push --force-with-lease=refs/heads/master:a9ccc8bebc87df4fc415934173ed46cd82895ed7 \
  https://gitlab.example.invalid/1c/aisuz/ai.git \
  HEAD:refs/heads/master
```

Expected: forced update to the exact local `HEAD`; no other ref updated.

- [ ] **Step 4: Немедленно запретить force push**

В GitLab UI вернуть `Allowed to force push` в false до любых других действий.

- [ ] **Step 5: Проверить master и content refs**

Run: `git ls-remote --heads <target> refs/heads/master refs/heads/content-*`
Expected: `master` equals the captured local SHA; every `content-*` byte-for-byte equals Task 3.

### Task 5: Проверить pipeline новой master

**Files:** none.

**Interfaces:**
- Consumes: новый SHA `master` и восстановленную защиту.
- Produces: GitLab pipeline с успешным verify job и четырьмя artifacts.

- [ ] **Step 1: Открыть pipeline точного master SHA**

Проверить, что pipeline source SHA равен опубликованному local `HEAD`.

- [ ] **Step 2: Дождаться verify**

Expected: `go vet`, `go test`, `scripts/build.sh v0.1.0-rc.1` и security audit завершены успешно; `dist/` artifact доступен.

### Task 6: Удалить старые публикационные ссылки

**Files:** none.

**Interfaces:**
- Consumes: зелёный pipeline новой `master` и сохранённую запрещённую force protection.
- Produces: отсутствие доступных старых tags, Releases, packages и открытого MR !5.

- [ ] **Step 1: Получить отдельное action-time подтверждение удаления**

Перечислить четыре тега, все существовавшие до замены Releases/packages и MR `!5`; не объединять это подтверждение с будущей публикацией нового Release.

- [ ] **Step 2: Атомарно удалить четыре старых тега**

```bash
git push --atomic --delete https://gitlab.example.invalid/1c/aisuz/ai.git \
  team-kit-control-plane-windows-20260730 v1.0.0 v1.0.1 v1.1.0
```

- [ ] **Step 3: Удалить pre-replacement Releases и packages**

Через GitLab UI удалить только записи, которые существовали до Task 4. Не создавать новый Release в этой операции.

- [ ] **Step 4: Закрыть MR !5**

Закрыть `https://gitlab.example.invalid/1c/aisuz/ai/-/merge_requests/5` как относящийся к удалённому проекту; комментарий не добавлять.

- [ ] **Step 5: Финальная проверка**

Expected: `master` равна published SHA; четыре старых тега отсутствуют; старые Releases/packages отсутствуют; MR !5 closed; 11 `content-*` неизменны; force push запрещён.
