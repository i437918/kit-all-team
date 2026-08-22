# Hermes OfficeCLI MCP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** добавить OfficeCLI MCP в Team Kit из GitLab `1c/aisuz/ai`, чтобы при разворачивании Hermes закреплённый binary безопасно материализовался и регистрировался как локальный stdio MCP.

**Architecture:** разработка ведётся в isolated worktree от актуального GitLab `master`; один exact feature SHA обычным push отправляется в одноимённые GitLab/GitHub CI-ветки, а MR создаётся только в GitLab. Закрытый каталог выбирает immutable OfficeCLI asset; существующие bounded HTTP download, SHA-256 verifier, workspace.WriteFileAtomic, configure_application и renderer переиспользуются без нового installer/action/retry framework.

**Tech Stack:** Go 1.26.6, yaml.v3, стандартные crypto/sha256, net/http и os/exec, существующие Team Kit catalog/bootstrap/service/workspace packages, GitLab CI/Merge Requests и GitHub Actions.

**Spec:** docs/superpowers/specs/2026-08-18-hermes-officecli-mcp-design.md

---

## Актуализация репозиторной базы — 2026-08-20

Эта секция имеет приоритет над историческими SHA и обозначениями версий ниже.
План был подготовлен от состояния `v0.1.3`, но свежая сверка GitLab refs
показала, что опубликованный и текущий production baseline уже `v0.1.4`:

| Роль | Актуальное значение |
| --- | --- |
| GitLab `master` / `gitlab/HEAD` | `03bd00dec7f318aa87b82151243a4b6c632e43e2` (`chore(release): prepare v0.1.4`, 2026-08-20) |
| Published GitLab tag | annotated `v0.1.4` object `90781d471bb1aa3513e07050877f11169fbe1bf5` |
| Peeled `v0.1.4` commit | `03bd00dec7f318aa87b82151243a4b6c632e43e2` |
| Следующий выпуск с OfficeCLI | `v0.1.5` только в GitLab |

Для выполнения применяются следующие точные замены во всех задачах R0–10:

| Старое обозначение | Нормативная замена |
| --- | --- |
| Immutable baseline `v0.1.3` | Immutable baseline `v0.1.4` |
| Existing `scripts/publish-v0.1.3.ps1` and its tests | Reused bounded-publisher mechanism; historical `v0.1.3` entry point remains unchanged |
| Планируемый release/version contracts `v0.1.4` | Планируемый release/version contracts `v0.1.5` |
| `scripts/publish-v0.1.4.ps1`, `docs/CONFLUENCE-INSTALL-v0.1.4.md` | `scripts/publish-v0.1.5.ps1`, `docs/CONFLUENCE-INSTALL-v0.1.5.md` |

`v0.1.4`, его tag, release record и metadata являются неизменяемым legacy
baseline. Шесть исходных asset bytes не восстанавливаются: Release direct links
возвращают `404`, точный GitHub evidence run недоступен, а GitLab Generic Package
Registry пуст. Нельзя заявлять или проверять byte/hash equality для assets
`v0.1.4`, а также пересоздавать, заменять или зеркалировать их. R0 повторно
проверяет metadata `v0.1.4` read-only и fail-closed подтверждает отсутствие
GitLab/GitHub tag и Release `v0.1.5`.

Проверка GitLab Release API с полным перечнем assets требует отдельного
GitLab API credential; использовать OAuth-token, обнаруженный в URL remote,
для API запрещено. Поэтому execution gate обязан выполнять API-снимок только
через явно предоставленный GitLab token с read-only scope, не выводя его.

---

## Global Constraints

- Production-код не меняется, пока immutable OfficeCLI `v1.0.144`, tag commit
  `1ced45e900782c5083ed550ddf328ee974e425e7`, не пройдёт полную квалификацию
  Task 0.
- Production source of truth — `https://gitlab.example.invalid/1c/aisuz/ai.git`, ветка `master`.
- Upstream dependency source — `https://github.com/iOfficeAI/OfficeCLI`; его
  code не переносится в Team Kit, используются только qualified release assets.
- GitHub `dmitry-m1man/kit-all-team` используется только для CI; GitHub `main` не является baseline и GitHub release не создаётся.
- Local implementation branch создаётся только от freshly fetched GitLab `master`; текущая локальная ветка не переносится целиком.
- В GitHub и GitLab feature branches должен находиться один exact commit SHA; force-push и direct push в GitLab `master` запрещены.
- OfficeCLI `v1.0.144` допускается только в pinned mode: Team Kit после exact
  asset verification выполняет `officecli config autoUpdate false` и проверяет
  read-back exact `false`.
- Поддерживаемая матрица: Windows amd64, Linux amd64, macOS amd64, macOS arm64. ALT p11 использует Linux amd64.
- Windows ARM64 и Linux ARM64 не добавляются, потому что для них нет Team Kit release candidates.
- Источник runtime pin — compiled catalog. assets/payloads.json является проверяемым зеркалом supply-chain policy, а не вторым runtime loader.
- Runtime не использует latest, install.sh, install.ps1, officecli install или officecli mcp с target.
- Managed command всегда абсолютный. Shell wrappers, PATH lookup и пользовательский command запрещены.
- OfficeCLI MCP не получает `env`; неработающий для раннего MCP dispatch
  `OFFICECLI_SKIP_UPDATE` не используется и не попадает в managed contract.
- Config-команда меняет `${UserProfile}/.officecli/config.json` для всех
  OfficeCLI процессов текущего OS user. Это документируемая managed mutation, а
  не profile-local настройка Hermes.
- Team Kit не устанавливает OfficeCLI skills. Допускается upstream refresh уже
  существующих OfficeCLI skills; отсутствующие agent/skill directories не
  создаются, целевой Hermes profile использует встроенную команду `load_skill`
  единственного MCP-инструмента `officecli`.
- Старые версии OfficeCLI не удаляются автоматически.
- Широкий инструмент OfficeCLI умеет изменять Office-файлы; это явно документируемая trust boundary, а не скрытая read-only интеграция.
- Опубликованный GitLab release `v0.1.4`, его tag и assets неизменяемы. Активные
  version contracts обновляются до `v0.1.5` отдельным commit того же OfficeCLI
  MR в Task 7, а публикация выполняется только в GitLab через существующий
  bounded publisher после exact-final-SHA acceptance.
- GitHub Actions должен запускать реальные jobs. До снятия текущего billing
  blocker работа останавливается с `GITHUB_ACTIONS_BILLING_BLOCKED`.
- Любые GitHub/GitLab tokens загружаются только в environment текущего process,
  не выводятся и очищаются в `finally`.
- Go module path `github.com/mi1man-cmd/kit-all-team` не меняется: это import
  identity, а не признак authority репозитория.

## Критический путь

Task R0 → Task 0 → Task 1 → Task 2 → Task 3 → Task 4 → Task 5 → Task 6 → Task 7 → Task 8 → Task 9 → Task 10.

Tasks 2 и 3 можно разрабатывать параллельно после Task 1. Tasks 4 и 5 требуют их объединения. Task 6 использует готовый provisioner и renderer, поэтому не начинается раньше Task 5.

---

### Task R0: Создать feature worktree от актуального GitLab master

**Files:**

- Local-only: .git/config and .git/worktrees
- Create in target worktree: docs/OFFICECLI-QUALIFICATION.md
- Create in target worktree: docs/superpowers/specs/2026-08-18-hermes-officecli-mcp-design.md
- Create in target worktree: docs/superpowers/plans/2026-08-18-hermes-officecli-mcp.md

**Interfaces:**

- Consumes: GitLab production URL, GitHub CI mirror URL, existing v0.1.4 repository contracts.
- Produces: clean worktree .worktrees/officecli-mcp, local branch codex/hermes-officecli-mcp, recorded gitlabBase SHA.
- Invariant: no implementation commit may have a parent outside fetched GitLab master history.

- [ ] **Step R0.1: Проверить исходное расхождение и сохранить пользовательские файлы**

В текущем workspace выполнить только read-only checks:

~~~powershell
git status --short
git branch --show-current
git rev-parse HEAD
git remote -v
~~~

Не stage, не переносить и не удалять ранее существовавшие .tmp-review-8f06652,
DOCX и generator script. Текущий local HEAD и GitHub main не являются
production baseline.

Audit snapshot от 2026-08-20:

- GitLab master / published v0.1.4: 03bd00dec7f318aa87b82151243a4b6c632e43e2;
- GitLab annotated v0.1.4 tag object: 90781d471bb1aa3513e07050877f11169fbe1bf5;
- GitHub main и исходный local HEAD не являются production baseline и перед
  execution проверяются заново.

Эти значения являются только объяснением расхождения; execution pin получается
заново в следующем step.

- [ ] **Step R0.2: Настроить отдельный GitLab remote и получить exact baseline**

~~~powershell
$gitlabURL = 'https://gitlab.example.invalid/1c/aisuz/ai.git'
$existingGitLabURL = git remote get-url gitlab 2>$null
if ($LASTEXITCODE -eq 0) {
  if ($existingGitLabURL -cne $gitlabURL) { throw 'GITLAB_REMOTE_URL_MISMATCH' }
} else {
  git remote add gitlab $gitlabURL
}
$remoteLine = git ls-remote --heads $gitlabURL refs/heads/master
if ($LASTEXITCODE -ne 0 -or $remoteLine.Count -ne 1) { throw 'GITLAB_MASTER_UNAVAILABLE' }
$gitlabBase = [string](($remoteLine -split '\s+')[0])
if ($gitlabBase -notmatch '^[0-9a-f]{40}$') { throw 'GITLAB_MASTER_SHA_INVALID' }
git fetch --no-tags gitlab master
if ($LASTEXITCODE -ne 0) { throw 'GITLAB_FETCH_FAILED' }
$fetchedBase = [string](git rev-parse refs/remotes/gitlab/master)
if ($fetchedBase -cne $gitlabBase) { throw 'GITLAB_MASTER_MOVED_DURING_FETCH' }
~~~

При GITLAB_MASTER_MOVED_DURING_FETCH повторить Step R0.2 один раз с новым
remote SHA. Не использовать cached GitHub `main`.

- [ ] **Step R0.3: Проверить, что GitLab baseline содержит Team Kit и ещё не содержит OfficeCLI runtime**

~~~powershell
$requiredPaths = @(
  'go.mod',
  'cmd/teamkit/main.go',
  'internal/catalog/catalog.go',
  'internal/hermes/profile.go',
  'internal/bootstrap/effects.go',
  'internal/service/service.go',
  'internal/service/operation_contract.go',
  '.github/workflows/ci.yml',
  '.github/workflows/release.yml',
  '.github/workflows/nightly.yml',
  '.github/workflows/alt-native.yml',
  '.github/workflows/hermes-windows-e2e.yml',
  '.gitlab-ci.yml',
  'scripts/build.ps1',
  'scripts/build.sh',
  'scripts/publish-v0.1.3.ps1',
  'scripts/release/BoundedRelease.psm1',
  'scripts/release/test-bounded-release.ps1',
  'test/release/docs_test.go',
  'test/release/ci_test.go',
  'test/release/workflow_lifecycle_test.go'
)
$gitlabBase = [string](git rev-parse refs/remotes/gitlab/master)
$treePaths = @(git ls-tree -r --name-only $gitlabBase)
$missingPaths = @($requiredPaths | Where-Object { $_ -notin $treePaths })
if ($missingPaths.Count -ne 0) { throw "GITLAB_BASELINE_INCOMPATIBLE: $($missingPaths -join ',')" }
git grep -n -i officecli $gitlabBase -- internal assets .github .gitlab-ci.yml scripts docs
~~~

Expected: OfficeCLI встречается только в historical documentation как
исключённый scope. Любой production reference требует остановки и повторного
аудита, а не наложения старого плана поверх нового кода.

Тем же read-only gate подтвердить GitLab annotated tag и Release `v0.1.4`, их
tag object/peeled commit и release metadata. Зафиксировать только metadata
baseline; asset bytes и hashes не читать, не строить для них snapshot и не
сравнивать. Недоступность шести legacy assets является исторически установленным
фактом, а не поводом для mutation `v0.1.4`.

Read-only inspect обязан подтвердить, что существующий `BoundedRelease.psm1`
можно параметризовать version, candidate SHA, Generic Package provenance и
postverify без нового publisher. Активные build/workflow/test contracts должны
указывать `v0.1.5`; GitHub release workflow остаётся validation/evidence-only.
Отсутствие interfaces возвращает `GITLAB_RELEASE_BASELINE_INCOMPATIBLE`.

Через GitLab API fail-closed подтвердить отсутствие tag, Release и Generic Package
`teamkit` версии `v0.1.5`, а также protected-tag rule для `v0.1.5`. Любой
existing или conflicting package/tag/release state возвращает
`GITLAB_V0_1_5_PREFLIGHT_FAILED`; Task 10 повторяет preflight race-safe перед
mutation.

- [ ] **Step R0.4: Проверить GitHub CI credentials без вывода token**

~~~powershell
$tokenLine = Get-Content -LiteralPath 'G:\.hermes\.env' -Encoding UTF8 |
  Where-Object { $_ -match '^GITHUB_TOKEN=' } |
  Select-Object -First 1
if (-not $tokenLine) { throw 'GITHUB_TOKEN_MISSING' }
$previousGHToken = $env:GH_TOKEN
try {
  $env:GH_TOKEN = $tokenLine.Substring('GITHUB_TOKEN='.Length).Trim().Trim('"').Trim("'")
  gh auth status --hostname github.com
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_CI_AUTH_REQUIRED' }
  $canPush = gh api repos/dmitry-m1man/kit-all-team --jq '.permissions.push'
  if ($canPush -cne 'true') { throw 'GITHUB_CONTENTS_WRITE_REQUIRED' }
  gh api repos/dmitry-m1man/kit-all-team/actions/workflows/ci.yml --silent
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_ACTIONS_READ_REQUIRED' }
  $githubMain = gh api repos/dmitry-m1man/kit-all-team/branches/main --jq '.commit.sha'
  if ($githubMain -notmatch '^[0-9a-f]{40}$') { throw 'GITHUB_MAIN_SHA_INVALID' }
  $githubMainProtected = gh api repos/dmitry-m1man/kit-all-team/branches/main --jq '.protected'
  if ($githubMainProtected -cne 'false') { throw 'GITHUB_MAIN_FAST_FORWARD_PERMISSION_UNVERIFIED' }
  $v014TagRefs = @(gh api repos/dmitry-m1man/kit-all-team/git/matching-refs/tags/v0.1.4 --jq '.[].ref')
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_TAG_PREFLIGHT_FAILED' }
  $v014ReleaseIDs = @(gh api repos/dmitry-m1man/kit-all-team/releases --paginate --jq '.[] | select(.tag_name == "v0.1.4") | .id')
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_RELEASE_PREFLIGHT_FAILED' }
  if ($v014TagRefs.Count -ne 0 -or $v014ReleaseIDs.Count -ne 0) { throw 'GITHUB_V0_1_4_COLLISION' }
} finally {
  if ($null -eq $previousGHToken) { Remove-Item Env:GH_TOKEN -ErrorAction SilentlyContinue } else { $env:GH_TOKEN = $previousGHToken }
}
~~~

Никогда не печатать token, не добавлять G:\.hermes\.env в Git и не помещать
credential в remote URL. Existing GitHub tag/Release `v0.1.4`, protected `main`
без заранее доказанного bypass либо blocking ruleset являются ранними blockers.

После API-проверки настроить явный remote `github-ci`, не переименовывая и не
используя двусмысленный `origin`:

~~~powershell
$githubCIURL = 'https://github.com/dmitry-m1man/kit-all-team.git'
$existingGitHubCIURL = git remote get-url github-ci 2>$null
if ($LASTEXITCODE -eq 0) {
  if ($existingGitHubCIURL -cne $githubCIURL) { throw 'GITHUB_CI_REMOTE_URL_MISMATCH' }
} else {
  git remote add github-ci $githubCIURL
}
$tokenLine = Get-Content -LiteralPath 'G:\.hermes\.env' -Encoding UTF8 |
  Where-Object { $_ -match '^GITHUB_TOKEN=' } | Select-Object -First 1
if (-not $tokenLine) { throw 'GITHUB_TOKEN_MISSING' }
$previousGHToken = $env:GH_TOKEN
try {
  $env:GH_TOKEN = $tokenLine.Substring('GITHUB_TOKEN='.Length).Trim().Trim('"').Trim("'")
  git -c credential.helper= -c 'credential.helper=!gh auth git-credential' fetch --no-tags github-ci main
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_CI_FETCH_FAILED' }
  $gitlabBase = [string](git rev-parse refs/remotes/gitlab/master)
  git merge-base --is-ancestor refs/remotes/github-ci/main $gitlabBase
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_MIRROR_DIVERGED' }
  git -c credential.helper= -c 'credential.helper=!gh auth git-credential' push --dry-run github-ci refs/remotes/github-ci/main:refs/heads/main
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_MAIN_FAST_FORWARD_PERMISSION_UNVERIFIED' }
  git -c credential.helper= -c 'credential.helper=!gh auth git-credential' push --dry-run github-ci HEAD:refs/heads/codex/hermes-officecli-permission-probe
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_CONTENTS_WRITE_REQUIRED' }
} finally {
  if ($null -eq $previousGHToken) { Remove-Item Env:GH_TOKEN -ErrorAction SilentlyContinue } else { $env:GH_TOKEN = $previousGHToken }
}
~~~

`GITHUB_MIRROR_DIVERGED` и отсутствие доказанного обычного fast-forward права на
`main` останавливают работу здесь, а не после GitLab merge в Task 9. Force-push
и временное отключение protection/rulesets запрещены.

Через authenticated GitLab API fail-closed проверить project access: создание
feature branch/MR/pipeline доступно, а release credential имеет роль для
Release API и тега под существующим protected-tag rule. `git push --dry-run` в
новый `codex/hermes-officecli-permission-probe` проверяет branch write без
создания ref. Отсутствие любой capability останавливает работу до разработки.

- [ ] **Step R0.5: Проверить отсутствие конфликтующих remote feature branches**

~~~powershell
$gitlabURL = 'https://gitlab.example.invalid/1c/aisuz/ai.git'
$gitlabFeature = git ls-remote --heads $gitlabURL refs/heads/codex/hermes-officecli-mcp
if ($LASTEXITCODE -ne 0) { throw 'GITLAB_FEATURE_LOOKUP_FAILED' }
$tokenLine = Get-Content -LiteralPath 'G:\.hermes\.env' -Encoding UTF8 |
  Where-Object { $_ -match '^GITHUB_TOKEN=' } |
  Select-Object -First 1
if (-not $tokenLine) { throw 'GITHUB_TOKEN_MISSING' }
$previousGHToken = $env:GH_TOKEN
try {
  $env:GH_TOKEN = $tokenLine.Substring('GITHUB_TOKEN='.Length).Trim().Trim('"').Trim("'")
  $githubFeature = @(gh api repos/dmitry-m1man/kit-all-team/git/matching-refs/heads/codex/hermes-officecli-mcp --jq '.[].object.sha')
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_FEATURE_LOOKUP_FAILED' }
  $githubFeatureFound = $githubFeature.Count -ne 0
} finally {
  if ($null -eq $previousGHToken) { Remove-Item Env:GH_TOKEN -ErrorAction SilentlyContinue } else { $env:GH_TOKEN = $previousGHToken }
}
if ($githubFeatureFound -or $gitlabFeature) { throw 'OFFICECLI_FEATURE_BRANCH_COLLISION' }
~~~

Expected: обе remote feature branches отсутствуют. Не удалять и не
force-update существующую ветку; при collision требуется отдельное решение
пользователя.

- [ ] **Step R0.6: Доказать доступность GitHub Actions до начала разработки**

Текущий внешний blocker: GitHub run 32114768851 завершился до запуска первого
step из-за account payments/spending limit. До исправления billing не начинать
production-код: иначе cross-platform acceptance невозможно завершить.

После исправления billing выполнить baseline dispatch на текущем GitHub main.
Token загружается только в process memory и удаляется в finally:

~~~powershell
$tokenLine = Get-Content -LiteralPath 'G:\.hermes\.env' -Encoding UTF8 |
  Where-Object { $_ -match '^GITHUB_TOKEN=' } |
  Select-Object -First 1
if (-not $tokenLine) { throw 'GITHUB_TOKEN_MISSING' }
$previousGHToken = $env:GH_TOKEN
try {
  $env:GH_TOKEN = $tokenLine.Substring('GITHUB_TOKEN='.Length).Trim().Trim('"').Trim("'")
  $githubMain = gh api repos/dmitry-m1man/kit-all-team/branches/main --jq '.commit.sha'
  if ($githubMain -notmatch '^[0-9a-f]{40}$') { throw 'GITHUB_MAIN_SHA_INVALID' }
  $dispatchAt = (Get-Date).ToUniversalTime()
  gh workflow run ci.yml --repo dmitry-m1man/kit-all-team --ref main
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_ACTIONS_DISPATCH_FAILED' }
  $deadline = (Get-Date).AddSeconds(60)
  $run = $null
  do {
    Start-Sleep -Seconds 3
    $candidateRuns = @(gh run list --repo dmitry-m1man/kit-all-team --workflow ci.yml --branch main --event workflow_dispatch --limit 3 --json databaseId,createdAt,headSha,url | ConvertFrom-Json)
    $run = $candidateRuns | Where-Object { [datetime]$_.createdAt -ge $dispatchAt -and $_.headSha -ceq $githubMain } | Select-Object -First 1
  } until ($run -or (Get-Date) -ge $deadline)
  if (-not $run) { throw 'GITHUB_ACTIONS_RUN_IDENTITY_INVALID' }
  gh run watch $run.databaseId --repo dmitry-m1man/kit-all-team --exit-status
  if ($LASTEXITCODE -ne 0) {
    $jobs = gh api "repos/dmitry-m1man/kit-all-team/actions/runs/$($run.databaseId)/jobs" | ConvertFrom-Json
    if ($jobs.total_count -eq 0) { throw 'GITHUB_ACTIONS_BILLING_BLOCKED' }
    throw 'GITHUB_BASELINE_CI_FAILED'
  }
} finally {
  if ($null -eq $previousGHToken) { Remove-Item Env:GH_TOKEN -ErrorAction SilentlyContinue } else { $env:GH_TOKEN = $previousGHToken }
}
~~~

Expected: baseline workflow PASS. Это passive CI wait; оно не входит в active
development estimate.

- [ ] **Step R0.7: Создать isolated worktree через required skill**

Использовать superpowers:using-git-worktrees. Target:

~~~powershell
git worktree add '.worktrees/officecli-mcp' -b 'codex/hermes-officecli-mcp' refs/remotes/gitlab/master
~~~

После создания:

~~~powershell
$gitlabBase = git rev-parse refs/remotes/gitlab/master
git -C '.worktrees/officecli-mcp' status --short
$worktreeHead = git -C '.worktrees/officecli-mcp' rev-parse HEAD
if ($worktreeHead -cne $gitlabBase) { throw 'OFFICECLI_WORKTREE_BASE_MISMATCH' }
git -C '.worktrees/officecli-mcp' merge-base --is-ancestor $gitlabBase HEAD
if ($LASTEXITCODE -ne 0) { throw 'OFFICECLI_WORKTREE_ANCESTRY_INVALID' }
~~~

Expected: empty status и HEAD, равный fetched GitLab master. Все последующие
Task 0–Task 9 выполняются с workdir `.worktrees/officecli-mcp`; Task 10 создаёт
отдельный clean detached publish worktree exact merge SHA.

- [ ] **Step R0.8: Перенести только утверждённые design/plan и создать qualification record**

GitLab baseline ещё не содержит эти два документа. Через `apply_patch` создать
в target worktree exact approved copies:

- docs/superpowers/specs/2026-08-18-hermes-officecli-mcp-design.md;
- docs/superpowers/plans/2026-08-18-hermes-officecli-mcp.md.

Не cherry-pick и не копировать commits текущей локальной ветки. Затем через
`apply_patch` создать только docs/OFFICECLI-QUALIFICATION.md и записать:

- production repository URL и ref master;
- exact gitlabBase SHA и timestamp проверки;
- GitHub CI mirror URL и planned ref codex/hermes-officecli-mcp;
- local branch codex/hermes-officecli-mcp;
- immutable legacy metadata `v0.1.4` и факт недоступности его original assets;
- исходные audit snapshot SHA из Step R0.1;
- status REPOSITORY_BASELINE_VERIFIED;
- правило same feature SHA in GitHub and GitLab.

Qualification record не содержит asset bytes/hashes `v0.1.4`, tokens, usernames
или credential-helper state.

- [ ] **Step R0.9: Проверить и commit baseline evidence**

~~~powershell
git -C '.worktrees/officecli-mcp' diff --check
git -C '.worktrees/officecli-mcp' status --short
git -C '.worktrees/officecli-mcp' add docs/OFFICECLI-QUALIFICATION.md docs/superpowers/specs/2026-08-18-hermes-officecli-mcp-design.md docs/superpowers/plans/2026-08-18-hermes-officecli-mcp.md
git -C '.worktrees/officecli-mcp' commit -m "docs(officecli): record GitLab production baseline"
~~~

Expected: commit основан на gitlabBase и добавляет только утверждённые plan,
design и qualification record. Creation
commit этих files становится историческим anchor; последующие Tasks не имеют
права их менять.

---

### Task 0: Квалифицировать exact OfficeCLI v1.0.144 и принятую update policy

**Files:**

- Modify: docs/OFFICECLI-QUALIFICATION.md
- Inspect upstream: src/officecli/Program.cs
- Inspect upstream: src/officecli/McpServer.cs
- Inspect upstream: src/officecli/Core/UpdateChecker.cs
- Inspect upstream: src/officecli/Core/SkillInstaller.cs
- Inspect upstream: src/officecli/Core/Installer.cs
- Inspect upstream: src/officecli/officecli.csproj
- Inspect upstream: .github/workflows/build.yml
- Inspect upstream: LICENSE
- Verify locally: no Team Kit production files

**Purpose:** зафиксировать утверждённые tag/commit `v1.0.144` и доказать две
раздельные гарантии: Team Kit сохраняет pinned binary, отключая upstream
self-update через persisted config, а известный existing-only skill refresh
принимается и документируется. `latest` не запрашивается и новый upstream
release автоматически не принимается.

Все `gh api` calls Task 0 выполнять внутри того же in-memory `GH_TOKEN`
`try/finally` wrapper, что в Step R0.4; token и API headers не выводить.

- [ ] **Step 0.1: Подтвердить exact approved release identity**

В PowerShell получить release по утверждённому tag без исполнения upstream
scripts:

~~~powershell
$officeCLIRepository = 'iOfficeAI/OfficeCLI'
$approvedTag = 'v1.0.144'
$approvedCommit = '1ced45e900782c5083ed550ddf328ee974e425e7'
$release = gh api "repos/$officeCLIRepository/releases/tags/$approvedTag" | ConvertFrom-Json
$tag = [string]$release.tag_name
if ($LASTEXITCODE -ne 0 -or $release.draft -or $release.prerelease -or $tag -cne $approvedTag) {
  throw 'OFFICECLI_PINNED_RELEASE_INVALID'
}
~~~

Получить commit, на который указывает tag:

~~~powershell
$officeCLIRepository = 'iOfficeAI/OfficeCLI'
$tag = 'v1.0.144'
$approvedCommit = '1ced45e900782c5083ed550ddf328ee974e425e7'
$tagRef = gh api "repos/$officeCLIRepository/git/ref/tags/$tag" | ConvertFrom-Json
$tagRef.object.type
$tagRef.object.sha
~~~

Если tag является annotated, раскрыть tag object до commit:

~~~powershell
$officeCLIRepository = 'iOfficeAI/OfficeCLI'
$tag = 'v1.0.144'
$approvedCommit = '1ced45e900782c5083ed550ddf328ee974e425e7'
$tagRef = gh api "repos/$officeCLIRepository/git/ref/tags/$tag" | ConvertFrom-Json
if ($tagRef.object.type -eq 'tag') {
  $annotatedTag = gh api "repos/$officeCLIRepository/git/tags/$($tagRef.object.sha)" | ConvertFrom-Json
  if ($annotatedTag.object.type -ne 'commit') { throw 'OFFICECLI_TAG_TARGET_INVALID' }
  $commit = [string]$annotatedTag.object.sha
} elseif ($tagRef.object.type -eq 'commit') {
  $commit = [string]$tagRef.object.sha
} else {
  throw 'OFFICECLI_TAG_TARGET_INVALID'
}
$commit
if ($commit -cne $approvedCommit) { throw 'OFFICECLI_PIN_POLICY_MISMATCH' }
~~~

Каждый shell block повторно задаёт exact public constants и не полагается на
переменные предыдущего process. Не переходить на другой tag или commit без
нового решения пользователя и изменения spec.

- [ ] **Step 0.2: Доказать persisted auto-update policy и границы skill refresh**

Получить исходники именно по qualified tag:

~~~powershell
$officeCLIRepository = 'iOfficeAI/OfficeCLI'
$commit = '1ced45e900782c5083ed550ddf328ee974e425e7'
$paths = @(
  'src/officecli/Program.cs',
  'src/officecli/McpServer.cs',
  'src/officecli/Core/UpdateChecker.cs',
  'src/officecli/Core/SkillInstaller.cs',
  'src/officecli/Core/Installer.cs'
)
foreach ($path in $paths) {
  $endpoint = 'repos/{0}/contents/{1}?ref={2}' -f $officeCLIRepository,$path,$commit
  $encoded = gh api $endpoint --jq '.content'
  [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String(($encoded -replace '\s','')))
}
~~~

Code-review record обязан точно зафиксировать:

1. `OFFICECLI_SKIP_UPDATE` не является MCP control: ранний `officecli mcp`
   branch запускается до общего environment guard, поэтому env удаляется из
   Team Kit profile и contract;
2. `officecli config autoUpdate false` обрабатывается ранним config branch и
   сохраняет user-global `${UserProfile}/.officecli/config.json`;
3. exit code config-команды недостаточен: `SaveConfig(false)` upstream может
   быть проигнорирован, поэтому Team Kit обязан выполнить read-back и принять
   только exact `false`;
4. при persisted `autoUpdate=false` `CheckInBackground` не применяет pending
   binary, после bounded skill refresh возвращает управление до
   `SpawnRefreshProcess`, HTTP и создания `.update/.partial/.old`;
5. refresh выбирает только уже установленные OfficeCLI skill identities с
   существующим `SKILL.md`, не создаёт новый agent или новый sub-skill, но может
   перезаписать пользовательские файлы и добавить bundled files внутри уже
   существующего skill directory;
6. `lastSkillRefreshVersion` делает последовательный второй запуск той же
   версии content-idempotent при успешном сохранении config; строгую гарантию
   «ровно один раз» при concurrent start или ошибке persistence не заявлять;
7. Team Kit не устанавливает on-disk OfficeCLI skills и использует встроенную
   команду `load_skill` единственного MCP-инструмента `officecli` для целевого
   профиля Hermes;
8. config и MCP dispatch обходят `Installer.MaybeAutoInstall`, а `--version`
   имеет непустой argv и поэтому не запускает bare-invocation auto-install;
   Team Kit никогда не вызывает OfficeCLI без аргументов.

Зафиксировать license file на exact commit и подтвердить, что модель Team Kit
download/use не нарушает его условия. Отсутствующая или несогласованная license
останавливает работу с OFFICECLI_LICENSE_UNRESOLVED.

Если хотя бы одно утверждение не доказано исходниками exact tagged commit,
завершить задачу с `OFFICECLI_UPDATE_POLICY_UNVERIFIED`.

- [ ] **Step 0.3: Подтвердить Hermes stdio contract**

В pinned Hermes commit f80f453ae0679347e38abc917c7f94f717bf96c5 проверить config schema и fixture для local MCP. Зафиксировать, что он принимает:

~~~yaml
mcp_servers:
  officecli:
    command: C:\absolute\path\officecli.exe
    args:
      - mcp
    enabled: true
~~~

На POSIX command должен быть абсолютным POSIX path. OfficeCLI entry не содержит
`env`. Если pinned Hermes не поддерживает mixed HTTP plus stdio MCP, остановить
выполнение с `HERMES_STDIO_MCP_UNSUPPORTED`.

- [ ] **Step 0.4: Собрать четыре exact assets**

Внутри нового authenticated block повторно запросить exact
`releases/tags/v1.0.144`, проверить tag/draft/prerelease и только затем из
`release.assets` выбрать ровно:

- officecli-win-x64.exe;
- officecli-linux-x64;
- officecli-mac-x64;
- officecli-mac-arm64.

Для каждого записать exact asset ID, file name, browser_download_url и size.
Отклонить отсутствующий, дублированный, пустой или превышающий 48 MiB asset.
Поле release.assets[].digest обязательно и должно точно соответствовать форме
sha256: плюс 64 lowercase hex symbols; отсутствующий или malformed digest
означает `OFFICECLI_PINNED_RELEASE_ASSET_INVALID`.

Получить SHA256SUMS из того же release. Для каждого asset сравнить SHA-256 из manifest, GitHub API digest и локального файла. Все три значения должны совпасть; prefix sha256: из API удалить только перед сравнением.

- [ ] **Step 0.5: Отложить disposable Windows smoke до Task 6**

Task 0 не запускает Windows binary и не утверждает runtime/MCP evidence. После
точной source/asset qualification Tasks 1–5 могут начаться только при record
`QUALIFIED_PINNED_AUTOUPDATE_DISABLED_SOURCE`. Обязательный disposable Windows
profile-delta, config/read-back, MCP handshake и existing-skill fixture
выполняются Task 6; отсутствие либо ошибка этого smoke блокирует Release.
Corporate Windows policy/equivalence evidence либо формальное waiver остаётся
отдельным Task 10/Release gate: Task 6 поставляет только technical smoke.

- [ ] **Step 0.6: Дополнить qualification record результатом exact source/asset gate**

Обновить docs/OFFICECLI-QUALIFICATION.md, созданный в Task R0, и добавить:

- release tag, commit, publication timestamp;
- результат source review и policy
  `auto_update_disabled_user_config/existing_skills_refresh_accepted`;
- exact license identity и результат проверки условий использования;
- четырьмя asset IDs, именами, URL, exact sizes и полными SHA-256;
- Hermes schema evidence;
- явным решением `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_SOURCE` или `REJECTED`;
- именем проверявшего workflow/run при наличии.

`REJECTED` record не разрешает Task 1.
`QUALIFIED_PINNED_AUTOUPDATE_DISABLED_SOURCE` является единственным источником
конкретных pin-значений и update policy для Tasks 1–5; он не утверждает Windows
runtime smoke.

- [ ] **Step 0.7: Проверить gate diff**

~~~powershell
git diff --check
git status --short
~~~

Expected: изменён только qualification record; isolated worktree не содержит
пользовательские `.tmp-review`, DOCX или generator files исходного workspace.

- [ ] **Step 0.8: Commit gate evidence**

~~~powershell
git add docs/OFFICECLI-QUALIFICATION.md
git commit -m "docs(officecli): qualify immutable upstream release"
~~~

**Stop condition:** без `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_SOURCE` record
Tasks 1–5 запрещены. Release дополнительно запрещён без успешного Task 6 Windows
smoke, а Task 10/Release — без принятого corporate Windows evidence или waiver.

---

### Task 1: Добавить closed catalog и точный platform selector

**Files:**

- Modify: internal/catalog/catalog.go
- Modify: internal/catalog/catalog_test.go
- Modify: assets/payloads.json
- Modify: assets/README.md
- Create: test/release/payload_manifest_test.go
- Reference: docs/OFFICECLI-QUALIFICATION.md

- [ ] **Step 1.1: Написать падающие catalog tests**

Добавить таблицу TestLookupOfficeCLIAsset_PlatformMatrix с пятью успешными строками:

- Windows + amd64 → Windows asset;
- Linux + amd64 → Linux asset;
- ALT Linux + amd64 → тот же Linux asset;
- macOS + amd64 → macOS Intel asset;
- macOS + arm64 → macOS ARM64 asset.

Для каждой строки сравнить exact version, commit, upstream file name, URL, size
и полный SHA-256 из `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_SOURCE` record.

Добавить TestLookupOfficeCLIAsset_RejectsUnsupportedPlatform для Windows arm64, Linux arm64, ALT arm64, macOS 386, неизвестной ОС и пустой architecture. Требовать errors.Is(err, ErrOfficeCLIPlatformUnsupported).

- [ ] **Step 1.2: Запустить RED**

~~~powershell
go test ./internal/catalog -run OfficeCLI -count=1
~~~

Expected: compile failure из-за отсутствующих OfficeCLIAsset и LookupOfficeCLIAsset.

- [ ] **Step 1.3: Реализовать минимальный catalog API**

Добавить:

~~~go
var ErrOfficeCLIPlatformUnsupported = errors.New("OFFICECLI_PLATFORM_UNSUPPORTED")

type OfficeCLIAsset struct {
    Version      string
    Commit       string
    OS           domain.OSFamily
    Architecture string
    FileName     string
    URL          string
    Size         int64
    SHA256       string
}

func LookupOfficeCLIAsset(family domain.OSFamily, architecture string) (OfficeCLIAsset, error)
~~~

Правила реализации:

- pins являются package-private immutable array;
- ALT нормализуется в Linux только при lookup;
- URL должен быть exact HTTPS release URL, без latest;
- SHA-256 хранится полными 64 hex symbols;
- returned struct не содержит shared mutable map/slice;
- никаких network calls и чтения JSON в runtime.

- [ ] **Step 1.4: Синхронизировать supply-chain manifest**

В assets/payloads.json добавить объект officeCLI с version, commit и четырьмя
assets. В assets/README.md описать этот объект и правило exact pin. Не менять
существующие Hermes, certificate, toolchain и ALT pins.

В test/release/payload_manifest_test.go распарсить JSON, вызвать LookupOfficeCLIAsset для четырёх native targets и сравнить каждое поле. Отдельно доказать, что ALT lookup равен Linux amd64 asset.

- [ ] **Step 1.5: Запустить GREEN**

~~~powershell
go test ./internal/catalog -run OfficeCLI -count=1
go test ./test/release -run PayloadManifest -count=1
~~~

Expected: PASS.

- [ ] **Step 1.6: Commit catalog pins**

~~~powershell
git add internal/catalog/catalog.go internal/catalog/catalog_test.go assets/payloads.json assets/README.md test/release/payload_manifest_test.go
git commit -m "feat(catalog): pin qualified OfficeCLI assets"
~~~

---

### Task 2: Параметризовать bounded downloader и создать provisioner

**Files:**

- Modify: internal/service/service.go
- Modify: internal/service/service_test.go
- Create: internal/service/officecli.go
- Create: internal/service/officecli_test.go
- Create: internal/service/officecli_userhome_windows.go
- Create: internal/service/officecli_userhome_other.go
- Reuse: internal/workspace/workspace.go
- Reuse: internal/pathsafe/pathsafe.go

- [ ] **Step 2.1: Расширить существующий downloader contract test**

Расширить существующий TestService_DefaultDownloaderHasFiniteTimeout:

- default Hermes installer downloader имеет maxBytes == 4 MiB;
- его oversize error остаётся HERMES_INSTALLER_TOO_LARGE;
- timeout остаётся 30 seconds.

Это предотвращает случайное расширение installer trust boundary до 48 MiB.

- [ ] **Step 2.2: Написать падающие provisioner tests**

Создать табличные тесты:

- TestOfficeCLIProvisioner_CacheHitDisablesAutoUpdateWithoutDownload;
- TestOfficeCLIProvisioner_DownloadsVerifiesAndPublishes0700;
- TestOfficeCLIProvisioner_RejectsChecksumBeforeWrite;
- TestOfficeCLIProvisioner_RejectsEmptyWrongSizeAndOversized;
- TestOfficeCLIProvisioner_ReadyDetectsTamperAndNonExecutable;
- TestOfficeCLIProvisioner_RejectsRedirectedManagedPath;
- TestOfficeCLIProvisioner_EnsureRepairsTamperedRegularFile;
- TestOfficeCLIProvisioner_NeverExecutesUnverifiedBinary;
- TestOfficeCLIProvisioner_RejectsRedirectedConfigOrLogPathBeforeProcess;
- TestOfficeCLIProvisioner_ReadyRejectsRedirectedConfigOrLogPath;
- TestOfficeCLIUserHome_MatchesOfficeCLIEffectiveProfile;
- TestOfficeCLIProvisioner_RejectsConfigSetFailureAndNonFalseReadback;
- TestOfficeCLIProvisioner_RejectsConfigTimeoutAndOversizedOutput;
- TestOfficeCLIProvisioner_ReadyRequiresPersistedAutoUpdateFalse;
- TestOfficeCLIConfigState_RejectsMissingMalformedDuplicateNonBoolAndTrue;
- TestOfficeCLIConfigState_RejectsKnownFieldTypeMismatchAndWrongCase;
- TestOfficeCLIProvisioner_ConfigDoesNotChangeBinarySHA;
- TestOfficeCLIProvisioner_ReadyDetectsUpdateSiblings;
- TestOfficeCLIProvisioner_EnsureRemovesRegularUpdateSiblingsAfterPolicySet;
- TestOfficeCLIProvisioner_RejectsUnsafeUpdateSibling;
- TestOfficeCLIProvisioner_LeavesExistingValidFileBytesUntouched.

Проверять полный SHA-256, exact expected size, regular file, отсутствие symlink/junction ancestors и executable bits на POSIX. На Windows executable-bit assertion пропустить, но regular-file и checksum оставить обязательными.

- [ ] **Step 2.3: Запустить RED**

~~~powershell
go test ./internal/service -run "TestService_DefaultDownloaderHasFiniteTimeout|TestOfficeCLIProvisioner_|TestOfficeCLIConfigState_|TestOfficeCLIUserHome_" -count=1
~~~

Expected: compile failure из-за отсутствующего provisioner и параметров downloader.

- [ ] **Step 2.4: Обобщить только лимит существующего downloader**

Изменить private httpDownloader на:

~~~go
type httpDownloader struct {
    client        *http.Client
    maxBytes      int64
    tooLargeError string
}
~~~

Оставить общий DownloadPort без изменения. Добавить две константы:

~~~go
const maxInstallerBytes = 4 << 20
const maxOfficeCLIBytes = 48 << 20
~~~

Зафиксировать новую private signature
`downloader(maxBytes int64, tooLargeError string) DownloadPort`. Existing
posixInstaller вызывает её с `maxInstallerBytes` и
`HERMES_INSTALLER_TOO_LARGE`; OfficeCLI — с `maxOfficeCLIBytes` и
`OFFICECLI_ASSET_TOO_LARGE`. Comment `installer payload` обобщить до `external
payload`. Даже при injected DownloadPort provisioner повторно проверяет empty,
exact size и 48 MiB ceiling.

Не добавлять unbounded mode и не менять HTTP timeout.

- [ ] **Step 2.5: Реализовать узкий provisioner**

В internal/service/officecli.go добавить private officeCLIProvisioner:

~~~go
type officeCLIProvisioner struct {
    asset      catalog.OfficeCLIAsset
    path       string
    configPath string
    download   DownloadPort
    verify     func([]byte, string) bool
    write      func(string, []byte) error
    run        platform.ProcessRunner
    capture    func(context.Context, string, []string) (stdout, stderr []byte, err error)
    readConfig func(string) ([]byte, error)
}

func (p *officeCLIProvisioner) Path() string
func (p *officeCLIProvisioner) Ensure(context.Context) error
func (p *officeCLIProvisioner) Ready(context.Context) (bool, error)
~~~

Поведение Ensure:

1. проверить managed path, regular file, exact size/SHA и mode без запуска
   OfficeCLI;
2. вычислить canonical OS user home и проверить, что `.officecli/config.json`
   является exact contained path; существующие `.officecli`, `config.json` и,
   если parsed config имеет `log=true`, `officecli.log` не могут быть
   symlink/junction/non-regular объектами;
3. при любом managed/config security/path error вернуть `ErrForeignProfile` до
   process/network/write;
4. только если asset отсутствует или tampered regular file, скачать
   `p.asset.URL`, отклонить empty, wrong size, >48 MiB и SHA mismatch;
5. вызвать injected executable-writer, являющийся узким adapter над
   существующим workspace.WriteFileAtomic с фиксированным mode 0700;
6. повторно проверить exact binary bytes и config/log targets, только после
   этого вызвать verified
   executable через existing `platform.ProcessRunner` с literal argv
   `[]string{"config", "autoUpdate", "false"}` и fixed child timeout 10 seconds;
7. выполнить bounded capture того же exact executable с literal argv
   `[]string{"config", "autoUpdate"}` и потребовать exit 0, пустой stderr и
   stdout после удаления только CR/LF exact `false`, без включения output в
   error/log; fixed child timeout — 10 seconds, до запуска настроить ограниченные
   writers с existing `maxHermesCommandOutput` 64 KiB как суммарным пределом;
8. только после подтверждённого read-back удалить exact known regular siblings
   `<binary>.update`, `<binary>.update.partial`, `<binary>.old` из уже
   проверенного managed parent; symlink, junction, directory, path escape, stat
   вернуть `ErrForeignProfile`, а remove error — стабильный
   `OFFICECLI_UPDATE_ARTIFACT_REMOVE_FAILED`; не следовать по ссылке и не
   выполнять рекурсивное удаление;
9. повторно вызвать `Ready(ctx)` и вернуть
   `OFFICECLI_AUTOUPDATE_CONFIG_FAILED`, если persisted postcondition ложна.

Даже при cache hit Steps 6–9 выполняются, потому что пользователь мог включить
auto-update вручную. Cache hit запрещает только download и binary write. Setter
exit 0 не является доказательством: upstream `v1.0.144` может проигнорировать
`SaveConfig(false)` и напечатать success, поэтому read-back обязателен.

`Ready(ctx)` остаётся read-only: сначала проверяет exact asset и отсутствие трёх
exact updater siblings, затем повторно вычисляет canonical effective OS user
home, доказывает containment и через `lstat`/существующие pathsafe primitives
проверяет `.officecli` и `config.json` до чтения. После безопасного чтения config
при `log=true` он так же проверяет `officecli.log`; redirected ancestor,
symlink/junction, non-regular leaf или path/stat error возвращается как стабильная
security error. Только после этих проверок `Ready` консервативно валидирует всю
exact upstream `v1.0.144` AppConfig schema: top-level object, case-sensitive
camelCase keys `lastUpdateCheck`, `latestVersion`, `autoUpdate`, `log`,
`installedBinaryVersion`, `lastSkillRefreshVersion`, отсутствие duplicates и
правильные типы (`DateTime?`, nullable strings, booleans), после чего требует
`autoUpdate=false`. Unknown либо неверно-регистровый key также fail-closed:
immutable pin не нуждается в forward-compatible schema. Это важно, потому что
upstream при ошибке десериализации любого known field отбрасывает весь config и
использует default `autoUpdate=true`; частичный Go-parser запрещён. Обычный
updater sibling означает `false,nil`; missing, malformed, schema/type mismatch
или `autoUpdate=true` возвращают `false,nil`.
`Ready` не запускает OfficeCLI, чтобы Observe/Plan не дописывал
`officecli.log`, если пользователь ранее включил upstream logging.

Cleanup — это узкий repair owned drift с тремя literal именами, а не updater:
glob, recursive delete и cleanup вне exact managed parent запрещены. После
cleanup `Ensure` повторно проверяет binary SHA/size/mode, отсутствие siblings и
persisted policy. На Windows locked file даёт стабильную ошибку и не обходится.
Unit tests проверяют exact error identity через `errors.Is`/stable code и что ни
один другой файл managed parent не удаляется.

Managed path:

- Windows: HERMES_HOME/.teamkit/officecli/<catalog Version>/officecli.exe;
- Linux, ALT, macOS: HERMES_HOME/.teamkit/officecli/<catalog Version>/officecli.

Upstream file name остаётся в catalog и operation contract, но не используется как произвольная часть конечного имени.

Не менять существующий `Options.WritePrivate`/privateWriter с mode 0600 и не
расширять все secret-writer fixtures. Для OfficeCLI создать отдельную private
service closure с фиксированным 0700, переиспользующую тот же atomic writer.
`configPath` получать один раз через platform-specific `officeCLIUserHome`, не
из `HERMES_HOME`: Windows implementation использует уже подключённый
`golang.org/x/sys/windows` и
`windows.KnownFolderPath(windows.FOLDERID_Profile, 0)`, а `!windows` —
`os.UserHomeDir`. Это намеренно совпадает с upstream
`Environment.SpecialFolder.UserProfile` и остаётся user-global OfficeCLI policy.
`officecli_userhome_other.go` имеет explicit `//go:build !windows`; общий файл не
содержит runtime OS branch.
Team Kit не пишет JSON самостоятельно; direct reader нужен только для read-only
readiness. Existing
config parser возвращает также effective `log` flag для preflight exact log path;
его содержимое никогда не выводится. Missing `.officecli` допустим и создаётся
только verified upstream config-командой, но существующий redirected/non-directory
объект отклоняется до запуска.
`platform.ProcessRunner` и существующая `systemProcessRunner` invocation
mechanics переиспользуются для setter. Для query добавить только узкий bounded
capture seam на том же `exec.CommandContext`, с раздельными ограниченными
stdout/stderr buffers; существующий общий process framework не расширять.

- [ ] **Step 2.6: Запустить GREEN**

~~~powershell
go test ./internal/service -run "TestService_DefaultDownloaderHasFiniteTimeout|TestOfficeCLIProvisioner_|TestOfficeCLIConfigState_|TestOfficeCLIUserHome_" -count=1
~~~

Expected: PASS.

- [ ] **Step 2.7: Commit provisioner**

~~~powershell
git add internal/service/service.go internal/service/service_test.go internal/service/officecli.go internal/service/officecli_test.go internal/service/officecli_userhome_windows.go internal/service/officecli_userhome_other.go
git commit -m "feat(service): materialize pinned OfficeCLI asset"
~~~

---

### Task 3: Расширить Hermes renderer для mixed HTTP и stdio MCP

**Files:**

- Modify: internal/hermes/profile.go
- Modify: internal/hermes/hermes_test.go
- Modify: internal/hermes/managed_state.go
- Modify: internal/hermes/managed_state_test.go
- Modify: internal/hermes/profile_schema_test.go

- [ ] **Step 3.1: Написать semantic YAML RED test**

Расширить существующий
`TestProfile_Render_ContainsPinnedToolchainAndMCPs` так, чтобы parser требовал
ровно четыре MCP:

- v8std: exact url, enabled true, без command/args/env;
- customllm-jira: сохранить exact URL, headers, `sampling=false`,
  `supports_parallel_tool_calls=false`, `connect_timeout=60`, `timeout=120`;
- customllm-confluence: сохранить те же действующие remote-MCP поля и их exact
  значения;
- officecli: exact absolute command, args из одного элемента `mcp`, без `env`,
  enabled true и без `url`.

Command в test строить через filepath.Join(t.TempDir(), platform managed name),
а не hardcoded POSIX path. Дополнительно запретить command, начинающийся с cmd,
powershell, pwsh, sh или bash; запретить secret canary в полном YAML.

Добавить TestProfile_RenderRejectsRelativeOfficeCLICommand. Расширить
`TestProfile_RenderForSchema_RendersCorporateAtlassianMCPs` для схем 34 и 37 и
доказать, что OfficeCLI не меняет semantic representation трёх существующих
MCP.

- [ ] **Step 3.2: Запустить RED**

~~~powershell
go test ./internal/hermes -run "Profile.*OfficeCLI|Profile_Render" -count=1
~~~

Expected: failure, потому что renderer пока создаёт три remote MCP и не умеет
локальный stdio OfficeCLI.

- [ ] **Step 3.3: Реализовать минимальное расширение модели**

Добавить в Profile поле OfficeCLICommand string и метод, который возвращает
копию профиля с проверенным command:

~~~go
func (p Profile) WithOfficeCLI(command string) (Profile, error)
~~~

ProfileFromDesired сохраняет текущую signature. Это позволяет renderer-коммиту
оставаться компилируемым до service wiring. WithOfficeCLI отклоняет неabsolute
path. Расширить существующий mcpYAML union: `url` сделать `omitempty`, добавить
`command`, `args` и `env` с `omitempty`, не меняя действующие headers, sampling,
parallel и timeout fields. Если OfficeCLICommand задан, Render сохраняет
`v8std`, Jira и Confluence без изменений и добавляет `officecli` четвёртым;
production bootstrap в Task 4 делает OfficeCLI обязательным.

Не читать PATH, env пользователя или catalog OfficeCLI внутри renderer: renderer получает уже выбранный absolute path.

- [ ] **Step 3.4: Расширить существующий strict managed-state validator**

Сначала добавить RED tests в managed_state_test.go:

- профиль v0.1.3 с тремя MCP больше не считается готовым;
- exact профиль с четырьмя MCP проходит;
- missing/extra MCP, относительный command, изменённые args, добавленные env или URL у
  OfficeCLI отклоняются;
- transport exclusivity: v8std/Jira/Confluence отклоняются при любых
  `command`/`args`/`env`, а OfficeCLI — при `url`, `headers`, `sampling`,
  `supports_parallel_tool_calls`, `connect_timeout` или `timeout`;
- любое изменение URL/headers/timeouts/sampling/parallel contract Jira или
  Confluence по-прежнему отклоняется;
- v8std validation остаётся без изменений.

Затем расширить существующий `validateManagedConfig`: ожидаемое количество —
`2 + len(catalog.AtlassianMCPs())`, то есть четыре; OfficeCLI проверяется как
локальный stdio MCP, а уже существующие три MCP — прежним кодом. Новый
параллельный validator не создавать.

- [ ] **Step 3.5: Запустить GREEN**

~~~powershell
go test ./internal/hermes -run "Profile.*OfficeCLI|Profile_Render|Managed.*OfficeCLI|VerifyManaged" -count=1
~~~

Expected: PASS.

- [ ] **Step 3.6: Commit renderer и managed-state contract**

~~~powershell
git add internal/hermes/profile.go internal/hermes/hermes_test.go internal/hermes/managed_state.go internal/hermes/managed_state_test.go internal/hermes/profile_schema_test.go
git commit -m "feat(hermes): render OfficeCLI stdio MCP"
~~~

---

### Task 4: Подключить provisioner к существующему configure_application и readiness

**Files:**

- Modify: internal/bootstrap/effects.go
- Modify: internal/bootstrap/effects_test.go
- Modify: internal/bootstrap/install_readiness_test.go
- Modify: internal/bootstrap/effects_windows_test.go
- Modify: internal/service/service.go
- Modify: internal/service/service_test.go
- Modify: internal/service/officecli.go

- [ ] **Step 4.1: Написать bootstrap lifecycle RED tests**

Добавить в bootstrap:

~~~go
type OfficeCLIPort interface {
    Path() string
    Ensure(context.Context) error
    Ready(context.Context) (bool, error)
}
~~~

Сначала тестами определить порядок внутри существующего Hermes `configure`:

1. ensureHermes;
2. OfficeCLI.Ensure: asset verification/materialization, fixed config set и
   verified read-back `autoUpdate=false`;
3. profile secret save;
4. существующая certificate environment/configuration stage;
5. profile render/write;
6. doctor остаётся только в verify action.

Тест должен доказать, что ошибка OfficeCLI.Ensure происходит до profile secret,
certificate configuration и profile YAML writes внутри `configure`. Внешний
`mutationWithStoreExecutable` уже может materialize certificate CA до вызова
bootstrap.configure; ради OfficeCLI этот проверенный порядок не перестраивать.

- [ ] **Step 4.2: Зафиксировать readiness semantics**

Расширить тесты Observe:

- валидный config и отсутствующий OfficeCLI → ApplicationReady false;
- tampered OfficeCLI → ApplicationReady false;
- valid OfficeCLI и missing/malformed/`autoUpdate=true` config →
  ApplicationReady false;
- valid OfficeCLI и persisted `autoUpdate=false` → OfficeCLI ready;
- security/path error из Ready → тот же error;
- валидный OfficeCLI плюс существующие условия → ApplicationReady true;
- non-Hermes state не требует OfficeCLI port и не меняет handoff.

Service-level test должен доказать, что Plan при missing и valid asset не
вызывает downloader/writer/process setter/capture, а только read-only config
reader после successful SHA/path validation. Redirected path возвращает
ErrForeignProfile до config read, Prepare, сохранения desired state и открытия
secret store.

profileConfigReady должен получать exact OfficeCLI path и считать существующий
v0.1.3 YAML с тремя MCP неготовым. Сохранить его текущую узкую модель:
`bytes.Equal` с exact `RenderForSchema(ProfileFromDesired(...).WithOfficeCLI())`.
Не протаскивать туда context/runtime/owner/toolchain и не вызывать
`VerifyManagedProfile`; строгий validator остаётся в existing verify action.

- [ ] **Step 4.3: Запустить bootstrap RED**

~~~powershell
go test ./internal/bootstrap -run "OfficeCLI|HermesLifecycle|Profile.*Ready" -count=1
~~~

Expected: compile/test failure.

- [ ] **Step 4.4: Реализовать bootstrap wiring без нового action**

Добавить OfficeCLI OfficeCLIPort в Effects.

В configure после ensureHermes и до ProfileEnvironment:

~~~go
if e.OfficeCLI == nil {
    return fmt.Errorf("OFFICECLI_REQUIRED")
}
if err := e.OfficeCLI.Ensure(ctx); err != nil {
    return err
}
~~~

`Ensure` обязан завершить set/read-back policy до profile secret, certificate
configuration и YAML writes. `OFFICECLI_AUTOUPDATE_CONFIG_FAILED` не маскируется
и не оставляет частично записанный profile.

Во всех трёх существующих Hermes call sites `ProfileFromDesired` — configure,
`verifyHermesManagedState` и `profileConfigReady` — применить
`profile.WithOfficeCLI(e.OfficeCLI.Path())`. Это гарантирует, что write,
verification и readiness используют одну exact модель профиля.

В Observe для Hermes вызвать `Ready(ctx)` и включить результат в
ApplicationReady:

~~~text
installReady AND owned AND configReady AND certificateReady AND officeCLIReady
~~~

ToolchainReady остаётся отдельным existing observed field; не дублировать его внутри ApplicationReady.

Existing verify action также вызывает `Ready(ctx)` до успешного результата:
строгая проверка YAML сама по себе не видит user-global OfficeCLI config и не
заменяет этот gate.

- [ ] **Step 4.5: Реализовать один platform resolver**

В service добавить pure helper:

~~~go
func resolveOfficeCLIAsset(family domain.OSFamily, macArchitecture string) (catalog.OfficeCLIAsset, error)
~~~

Правила:

- Windows, Linux и ALT всегда выбирают amd64: это уже существующие target
  families Team Kit и для них не выпускаются ARM64 candidates;
- macOS использует runtime.GOARCH и принимает amd64 или arm64;
- остальные комбинации возвращают OFFICECLI_PLATFORM_UNSUPPORTED.

Pure helper тестируется на всей матрице независимо от текущего runner. Проверка
соответствия выбранной пользователем desired.OS реальной host OS является
общесистемной задачей, потому что тот же selector уже управляет Hermes
installer; её не добавлять скрыто в OfficeCLI feature.

Создать один provisioner для Plan и один с теми же immutable inputs для mutation
assembly. В Service.Plan заменить bare bootstrap.Effects с одним
HermesExecutable на bootstrap.Effects с HermesExecutable и OfficeCLI. Plan
вызывает provisioner только через read-only Ready: DownloadPort, writer и
process runner не вызываются.
Mutation передаёт provisioner через EffectInputs в EffectsFactory.

Обновить прямые Hermes fixtures в effects_test.go, install_readiness_test.go и
effects_windows_test.go, чтобы каждый fixture либо передавал fake OfficeCLI,
либо явно проверял OFFICECLI_REQUIRED. В частности, существующие profile tests
должны использовать `WithOfficeCLI`, а service fixture, вручную собирающая
`bootstrap.Effects` из EffectInputs, обязана прокинуть `input.OfficeCLI`.
Существующий test про три personal secret keys по-прежнему ожидает ровно три
секрета, но дополнительно проверяет non-nil OfficeCLI port. После commit весь
repository должен компилироваться.

- [ ] **Step 4.6: Расширить managed-path validation**

В validateHermesServicePaths и officeCLIProvisioner path checks разрешить и
валидировать только:

- HERMES_HOME/.teamkit/officecli;
- exact version directory;
- canonical officecli или officecli.exe file.

Symlink, junction, non-directory ancestor и non-regular final file должны завершаться ErrForeignProfile до network, secrets или write.

Не разрешать glob, пользовательский version string или произвольный filename.

- [ ] **Step 4.7: Запустить targeted GREEN**

~~~powershell
go test ./internal/bootstrap -run "OfficeCLI|HermesLifecycle|Profile.*Ready" -count=1
go test ./internal/service -run "OfficeCLI|Mutation|Plan" -count=1
~~~

Expected: PASS.

- [ ] **Step 4.8: Оставить wiring и contract одним atomic slice**

Не commit и не push состояние, в котором OfficeCLI уже materializes/renders,
но ещё не входит в operation contract. Сразу выполнить Task 5 и сделать один
общий GREEN commit в Step 5.5.

---

### Task 5: Привязать OfficeCLI к operation contract и retry

**Files:**

- Modify: internal/service/operation_contract.go
- Modify: internal/service/operation_contract_test.go
- Modify: internal/service/service_test.go

- [ ] **Step 5.1: Написать canonical contract RED test**

Расширить существующий
`TestDefaultOperationContract_HermesBindsOrderedMCPServers`: canonical JSON
должен содержать ordered `MCPServers` из четырёх записей — неизменённые v8std,
Jira, Confluence и новый OfficeCLI. Новый параллельный MCP contract или pointer
в `operationHermesContract` не создавать.

Расширить существующий `operationMCPServerContract` optional stdio-полями и
добавить вложенную immutable asset identity:

~~~go
type operationMCPServerContract struct {
    // existing ID/Endpoint/Headers/Sampling/parallel/timeouts remain
    Command     string                            `json:"command,omitempty"`
    Args        []string                          `json:"args,omitempty"`
    Asset       *operationOfficeCLIAssetContract  `json:"asset,omitempty"`
}

type operationOfficeCLIAssetContract struct {
    Version            string
    Commit             string
    OS                 domain.OSFamily
    Architecture       string
    FileName           string
    URL                string
    Size               int64
    SHA256             string
    UpdatePolicy       string
    SkillRefreshPolicy string
}
~~~

JSON field names asset задать явно в snake_case. Абсолютный managed path хранится
только как `Command`; дублирующее поле `Path` не добавлять. Args равны `["mcp"]`,
environment отсутствует. `UpdatePolicy` имеет exact значение
`auto_update_disabled_user_config`, `SkillRefreshPolicy` —
`existing_installed_only_best_effort`. SchemaVersion остаётся 1; новый canonical
JSON меняет operation hash fail-closed.

Test requirements:

- изменение любого поля выбранного для текущего host asset, path, args,
  update policy или skill-refresh policy меняет hash;
- изменение невыбранного asset другой платформы не обязано менять текущий host contract;
- первые три элемента `MCPServers` byte-for-byte совпадают с текущим canonical
  contract;
- non-Hermes canonical contract не содержит officecli;
- порядок args детерминирован;
- секретные значения отсутствуют.
- legacy RC2 canonical JSON и известный hash не меняются как historical fixture,
  но retry такого receipt теперь fail-closed возвращает
  `OPERATION_CONTRACT_MISMATCH` до adapters, потому что RC2 identity не связывает
  обязательный OfficeCLI asset.

- [ ] **Step 5.2: Запустить RED**

~~~powershell
go test ./internal/service -run "OperationContract.*OfficeCLI|DefaultOperationContract" -count=1
~~~

Expected: failure, потому что OfficeCLI ещё не входит в contract.

- [ ] **Step 5.3: Реализовать contract из общего resolver**

defaultOperationContract обязан использовать тот же resolveOfficeCLIAsset и тот
же managed path helper, что provisioner. Нельзя повторно собирать URL, filename
или path строковой конкатенацией. OfficeCLI append-ится четвёртым только для
Hermes; legacy `MCP` и legacy RC2 structs не изменяются.

Минимально изменить `validateLegacyRC2Operation`: exact historical RC2 shape
можно распознать для стабильной диагностики, но resume больше не разрешается.
Это закрывает единственное исключение, в котором операция не содержала
OfficeCLI identity; migration/rewrite старого receipt не выполнять.

Существующий
TestService_RetryRejectsChangedOperationContractBeforePrivateAdapters расширить
assertion-ом, что receipt, созданный до OfficeCLI contract, и любой изменённый
pin отклоняются до downloader, secret store и writer. Новый retry framework и
отдельный retry flow не создавать.

- [ ] **Step 5.4: Запустить GREEN**

~~~powershell
go test ./internal/service -run "OperationContract|RetryRejectsChanged" -count=1
~~~

Expected: PASS.

- [ ] **Step 5.5: Запустить full slice tests и commit wiring plus identity**

~~~powershell
go test ./internal/hermes ./internal/bootstrap ./internal/service -count=1
git add internal/bootstrap/effects.go internal/bootstrap/effects_test.go internal/bootstrap/install_readiness_test.go internal/bootstrap/effects_windows_test.go internal/service/service.go internal/service/service_test.go internal/service/officecli.go internal/service/operation_contract.go internal/service/operation_contract_test.go
git commit -m "feat(bootstrap): configure pinned OfficeCLI in Hermes"
~~~

Command является единственным canonical managed path. Args и вложенная
selected-asset identity с обеими policy полностью задают оставшийся stdio
contract; runtime environment отсутствует.

---

### Task 6: Добавить real-platform smoke в существующий GitHub CI

**Files:**

- Create: internal/service/officecli_live_test.go
- Modify: .github/workflows/ci.yml
- Modify: scripts/alt-container-smoke.sh
- Modify: test/release/scripts_test.go
- Modify: test/release/ci_test.go
- Modify: docs/TEST-MATRIX.md
- Modify: docs/OFFICECLI-QUALIFICATION.md

- [ ] **Step 6.1: Написать build-tagged live smoke**

Создать test с build tag officecli_live:

~~~go
func TestOfficeCLILive_QualifiedAssetAndMCPHandshake(t *testing.T)
~~~

Test использует production catalog, downloader, digest verifier и provisioner. Он не реализует второй download path.

Добавить один reusable Go protocol helper в этом же _test.go. Он использует
framing, установленный source review Task 0, ограничивает каждое чтение 1 MiB и каждый этап
10 seconds, связывает responses с request IDs, отправляет initialize,
notifications/initialized и tools/list. Bash grep или второй protocol parser в
ALT script запрещены.

На каждом native runner проверить:

1. selected asset соответствует runtime.GOOS/runtime.GOARCH;
2. exact SHA и size проходят;
3. production provisioner первой OfficeCLI-командой выполняет fixed config
   set/read-back и persisted
   config содержит `autoUpdate=false`;
4. только после этого `--version` соответствует `1.0.144`;
5. `initialize` возвращает JSON-RPC 2.0, `protocolVersion=2024-11-05` и
   `serverInfo.name=officecli`;
6. следующий `tools/list` содержит ровно один tool `officecli`;
7. process завершён за 30 seconds;
8. exact SHA binary до config/MCP и после второго MCP start совпадает с catalog;
9. на Unix/macOS HOME/XDG/TMP roots изолированы; Windows lane использует
   effective `Environment.SpecialFolder.UserProfile` disposable GitHub-hosted
   account и не считает подмену HOME/USERPROFILE достаточной изоляцией. На
   Unix/macOS clean-home manifest разрешает только `.officecli/config.json`; на
   Windows pre/post manifest всего effective profile допускает только exact
   config delta и не требует отсутствия pre-existing runner files. Ни одна lane
   не создаёт новую agent/skill identity, а рядом с binary отсутствуют `.update`,
   `.update.partial`, `.old`;
10. второй последовательный MCP start не меняет разрешённый filesystem delta;
11. test не выполняет document mutation command.

Windows disposable fixture отдельно preseed-ит копию одной embedded OfficeCLI
skill identity с `SKILL.md`, фиксирует manifest этой identity и после config
set/read-back выполняет exact MCP дважды без промежуточного `--version`. Любые
bounded refresh writes разрешены только внутри этой существующей skill identity;
создание нового agent/sub-skill запрещено. Второй последовательный start после
успешно сохранённого marker не меняет manifest. Ошибка самого best-effort refresh
не является readiness-gate, но любая запись за пределами preseed identity
отклоняет technical smoke. Fixture не копируется в реальный user home и не
означает, что Team Kit устанавливает skill.

Если fixture заранее включает OfficeCLI `log=true`, append в
`.officecli/officecli.log` разрешён только для fixed несекретных config argv и
не включается в обычный clean-home matrix. Upstream existing-skill refresh уже
проверяется deferred Windows fixture Task 6 и не дублируется на четырёх
non-Windows runners.

На обеих macOS lanes дополнительно выполнить для exact downloaded binary:

~~~text
codesign --verify --strict --verbose=2 <binary-path>
spctl --assess --type execute --verbose=4 <binary-path>
~~~

Обе команды обязательны: hash и process start не заменяют проверку Developer ID
signature и notarization policy.

На любом native runner при TEAMKIT_OFFICECLI_KEEP_PATH, заданном как absolute
directory, разрешить сохранить уже проверенный binary только как CI evidence;
macOS job использует его для codesign/spctl, Linux ALT job — для container. При
TEAMKIT_OFFICECLI_EXISTING_PATH test повторно проверяет local asset по catalog
size/SHA и выполняет тот же config-first protocol helper без Team Kit download.
В обычном запуске обе
переменные отсутствуют, используется t.TempDir и всё удаляется автоматически.

- [ ] **Step 6.2: Запустить Windows live smoke только в disposable профиле**

~~~powershell
go test -tags officecli_live ./internal/service -run TestOfficeCLILive_QualifiedAssetAndMCPHandshake -count=1 -timeout=3m
~~~

На обычной developer Windows эту команду не запускать: подмена
`HOME`/`USERPROFILE` не перенаправляет `.NET SpecialFolder.UserProfile`. Локальный
run допустим только под доказанно disposable OS account/VM; иначе выполнить
обычные unit tests, а обязательный live PASS получить на ephemeral
`windows-2025` в Step 6.3. Expected: никаких writes вне disposable effective OS
profile и managed test binary directory.
Это обязательный release gate: missing или failed Windows smoke отклоняет
release независимо от успешной source/asset qualification Task 0. После PASS
Task 6 дополняет qualification record distinct runtime decision
`QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME`, связывая его с exact source
record и technical smoke evidence; такой decision не заменяет Task 10 corporate
Windows policy/equivalence evidence либо формальное waiver.

- [ ] **Step 6.3: Добавить trusted live matrix в существующий workflow**

Не добавлять внешний download в обязательные PR/feature-branch lanes. Сохранить
их hermetic unit/contract/integration/race coverage без изменений.

В существующий `workflow_dispatch` добавить обязательный input `expected_sha` и
fail-closed preflight `GITHUB_SHA == expected_sha`. В уже существующей native
matrix (`windows-2025`, `ubuntu-24.04`, `macos-15-intel`, `macos-15`) добавить
условный OfficeCLI live step только для `workflow_dispatch`. В step выполнить:

~~~text
go test -tags officecli_live ./internal/service -run TestOfficeCLILive_QualifiedAssetAndMCPHandshake -count=1 -timeout=3m
~~~

Новый workflow и новые jobs не создавать. Pull request/обычный push не получает
network-flaky step; cross-platform OfficeCLI evidence запускается ручным
dispatch на exact ref и остаётся обязательным acceptance gate. Это также
обходит отсутствие `codex/**` в текущем push trigger, не расширяя постоянный
trigger surface.

- [ ] **Step 6.4: Проверить ALT p11 тем же Linux asset**

В уже существующий `alt-p11-userspace` job добавить условные dispatch-only steps
для того же officecli_live test с
TEAMKIT_OFFICECLI_KEEP_PATH, направленным в dist/evidence/officecli. Это повторно
использует production provisioner и не предполагает общую файловую систему
между GitHub runners. Сохранить первый обязательный positional input скрипта
для Team Kit candidate. Добавить второй и третий inputs как all-or-none пару:
exact OfficeCLI asset и compiled officecli-live.test. Поэтому существующий
hermetic ALT PR job с одним аргументом не меняет поведение, а trusted live job
передаёт все три; частичная пара завершается стабильной ошибкой.

Собрать reusable test binary без CGO и смонтировать его вместе с asset в pinned
ALT p11 container:

~~~text
CGO_ENABLED=0 go test -c -tags officecli_live -o dist/evidence/officecli/officecli-live.test ./internal/service
~~~

В контейнере задать TEAMKIT_OFFICECLI_EXISTING_PATH и запустить только
TestOfficeCLILive_QualifiedAssetAndMCPHandshake. Так ALT использует тот же Go
protocol helper, а не отдельную shell-реализацию.

Production provisioner сохраняет mode 0700. Поскольку ALT container работает
как UID 1000, перед bind mount создать только CI evidence copy с mode 0755,
повторно проверить её size/SHA-256 против catalog и убедиться, что bytes
совпадают с 0700 original. Production path/mode не менять; непроверенный chmod
исходного managed binary запрещён.

В pinned ALT p11 container выполнить только:

- test -x;
- `officecli config autoUpdate false` и verified read-back exact `false` как
  первые OfficeCLI invocations;
- officecli --version только после policy read-back;
- bounded officecli mcp initialize и tools/list.

Не скачивать asset внутри контейнера и не выполнять installer scripts. Добавить evidence OFFICECLI_ALT_USERSPACE_COMPATIBLE.

- [ ] **Step 6.5: Зафиксировать workflow policy тестами**

Tests должны запрещать в OfficeCLI flow:

- releases/latest;
- install.sh и install.ps1;
- officecli install;
- officecli skills/skill install;
- officecli mcp с target;
- bare `officecli` invocation;
- OFFICECLI_SKIP_UPDATE в runtime YAML/operation contract;
- curl-pipe-shell;
- initial download URL, не равный exact catalog URL.

Policy tests также требуют literal config argv, read-back before MCP, immutable
post-MCP SHA, отсутствие Team Kit skill-copy/install code и отсутствие новых
agent/skill identities в clean-home evidence.

GitHub release asset может штатно перенаправить exact initial HTTPS URL на
временный GitHub object URL. Downloader сохраняет стандартную redirect-модель,
но всегда проверяет final bytes по exact size и SHA-256; latest и произвольный
initial URL запрещены.

Также проверить наличие всех четырёх native lanes и ALT p11 evidence.
Отдельными assertions доказать, что OfficeCLI live steps отсутствуют на
pull_request/обычном push, `workflow_dispatch` включает их в существующих jobs,
а missing/mismatched `expected_sha` останавливает workflow до download.

- [ ] **Step 6.6: Запустить targeted CI policy tests**

~~~powershell
go test ./test/release -run "Scripts|CI|Workflow|OfficeCLI|ALT" -count=1
~~~

Expected: PASS.

- [ ] **Step 6.7: Commit CI smoke**

~~~powershell
git add internal/service/officecli_live_test.go .github/workflows/ci.yml scripts/alt-container-smoke.sh test/release/scripts_test.go test/release/ci_test.go docs/TEST-MATRIX.md
git commit -m "test(ci): verify OfficeCLI on supported platforms"
~~~

---

### Task 7: Подготовить v0.1.5 contracts и документацию в том же MR

**Files:**

- Modify: README.md
- Modify: docs/INSTALL.md
- Modify: docs/SECURITY.md
- Modify: CHANGELOG.md
- Modify: docs/RELEASE-CHECKLIST.md
- Modify: docs/EXTERNAL-BLOCKERS.md
- Modify: docs/OFFICECLI-QUALIFICATION.md
- Modify: test/release/docs_test.go
- Modify: .gitlab-ci.yml
- Modify: .github/workflows/ci.yml
- Modify: .github/workflows/release.yml
- Modify: .github/workflows/nightly.yml
- Modify: .github/workflows/alt-native.yml
- Modify: .github/workflows/hermes-windows-e2e.yml
- Modify: scripts/build.ps1
- Modify: scripts/build.sh
- Modify: scripts/release/BoundedRelease.psm1
- Modify: scripts/release/test-bounded-release.ps1
- Create: scripts/publish-v0.1.5.ps1
- Modify: test/release/ci_test.go
- Modify: test/release/workflow_lifecycle_test.go
- Create: docs/CONFLUENCE-INSTALL-v0.1.5.md
- Preserve unchanged: scripts/publish-v0.1.3.ps1,
  docs/CONFLUENCE-INSTALL-v0.1.3.md

- [ ] **Step 7.1: Написать падающие documentation и version-contract tests**

В test/release/docs_test.go добавить assertions для supported matrix, managed
path, exact `v1.0.144`, persisted user-global `autoUpdate=false`, accepted
existing-skill refresh, отсутствие Team Kit skill install, broad Office-file
mutation boundary и Unreleased boundary с неизменяемостью уже опубликованных releases. В
`test/release/ci_test.go` заменить active-contract expectation на exact
`v0.1.5` для build version и четырёх artifact names. В lifecycle tests добавить
запрет Team Kit GitHub tag/Release publication.

~~~powershell
go test ./test/release -run "Documentation.*OfficeCLI|Version|Release|Workflow|Bounded" -count=1
~~~

Expected: FAIL, пока docs и active version contracts не обновлены.

- [ ] **Step 7.2: Минимально параметризовать существующий release mechanism**

Обновить version-bearing CI/build files до exact `v0.1.5`. Создать
`scripts/publish-v0.1.5.ps1` как тонкую version entry point над существующим
`scripts/release/BoundedRelease.psm1`, а не новый publisher. Module получает
version и expected SHA параметрами, сохраняя bounds, hash comparisons, tag
prechecks, resume и post-verification. Прежний шаг direct push в `master`
заменить verify-only проверкой, поскольку release candidate доставляется через
MR. `.github/workflows/release.yml` остаётся validation/evidence workflow и не
создаёт GitHub tag, Release или assets.

При dispatch `ci.yml` module обязан передавать
`inputs.expected_sha=CandidateSha`; regression test проверяет этот exact input.
Для оптимизированного v0.1.5 flow module принимает уже проверенные exact GitLab
pipeline ID, verify job ID и GitHub run/artifact IDs, повторно валидирует их
SHA/digests и не dispatch-ит дублирующую CI. Publisher принимает package name
`teamkit`, package version `v0.1.5` и exact набор из шести filenames:
`teamkit-v0.1.5-windows-amd64.exe`, `teamkit-v0.1.5-linux-amd64`,
`teamkit-v0.1.5-darwin-amd64`, `teamkit-v0.1.5-darwin-arm64`, `SHA256SUMS`,
`SECURITY-AUDIT.json`. До GitLab Release он загружает их в Generic Package
Registry, authenticated API повторно скачивает каждый файл и fail-closed
сравнивает SHA-256. Release links строятся только на package URLs.
Existing/conflicting package, tag или Release state останавливает publisher без
overwrite/delete; legacy `v0.1.4` assets не читаются и не используются как
bytes/hash evidence.

Parameterization обязана поддерживать historical v0.1.3 и active v0.1.5 file
sets. Все существующие v0.1.3 simulations остаются GREEN, поэтому неизменённый
`scripts/publish-v0.1.3.ps1` не ломается. Historical v0.1.3 docs/tag/release
contracts не переписывать.

- [ ] **Step 7.3: Описать поведение без маркетинговых обещаний**

Документация должна точно сообщать:

- OfficeCLI добавляется только в Hermes profile;
- supported platform table;
- ALT использует qualified Linux amd64 asset и имеет отдельный p11 smoke;
- exact qualified version и SHA pins;
- managed path и отсутствие PATH changes;
- почему принят exact `v1.0.144` и почему `OFFICECLI_SKIP_UPDATE` не используется
  как MCP control;
- fixed `officecli config autoUpdate false`, обязательный read-back и
  user-global `${UserProfile}/.officecli/config.json`;
- возможность best-effort refresh только ранее установленных OfficeCLI skills
  во всех обнаруженных agent homes, включая перезапись local edits;
- что skills — это файловые instruction/reference packs (`officecli-pptx`,
  `officecli-docx`, `officecli-xlsx` и другие), а не дополнительные MCP-серверы;
- Team Kit не устанавливает on-disk skills, не полагается на default Hermes
  skill directory и использует встроенную команду `load_skill` единственного
  MCP-инструмента `officecli`;
- отсутствие нового installer/updater;
- fail-closed обнаружение и узкий cleanup только `.update`, `.update.partial`,
  `.old` внутри owned managed parent после подтверждения `autoUpdate=false`;
- что tool officecli может читать и изменять Office documents;
- retry повторно использует существующий configure_application;
- удаление старых pinned versions не выполняется;
- профиль после настройки содержит четыре MCP: v8std, Jira, Confluence и
  OfficeCLI.

- [ ] **Step 7.4: Обновить release boundary**

Убрать прежнее исключение OfficeCLI только из актуального раздела Unreleased.
Явно указать:

- уже опубликованные releases, tags и assets остаются неизменяемыми;
- первый полностью проверяемый release с OfficeCLI — `v0.1.5` только в GitLab;
- active version contracts `v0.1.5` входят отдельным small commit в этот же MR,
  чтобы не повторять второй MR и вторую CI-матрицу;
- release запрещён без зелёных четырёх native lanes и ALT p11 smoke;
- `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME` record и CI run URL являются
  release evidence.

Не переписывать исторический `v0.1.3` и legacy metadata `v0.1.4` sections CHANGELOG, опубликованные
инструкции `docs/CONFLUENCE-INSTALL-v0.1.3.md` или ссылки на его assets.
Создать отдельный `docs/CONFLUENCE-INSTALL-v0.1.5.md`.

- [ ] **Step 7.5: Проверить release contracts и документацию**

~~~powershell
go test ./test/release -run "Documentation|Version|Release|Workflow|Bounded|OfficeCLI" -count=1
pwsh -NoProfile -File scripts/release/test-bounded-release.ps1
git diff --check
~~~

Expected: PASS.

- [ ] **Step 7.6: Commit release preparation отдельно от feature code**

~~~powershell
git add README.md docs/INSTALL.md docs/SECURITY.md CHANGELOG.md docs/RELEASE-CHECKLIST.md docs/EXTERNAL-BLOCKERS.md docs/OFFICECLI-QUALIFICATION.md docs/CONFLUENCE-INSTALL-v0.1.5.md .gitlab-ci.yml .github/workflows/ci.yml .github/workflows/release.yml .github/workflows/nightly.yml .github/workflows/alt-native.yml .github/workflows/hermes-windows-e2e.yml scripts/build.ps1 scripts/build.sh scripts/release/BoundedRelease.psm1 scripts/release/test-bounded-release.ps1 scripts/publish-v0.1.5.ps1 test/release/docs_test.go test/release/ci_test.go test/release/workflow_lifecycle_test.go
git commit -m "chore(release): prepare GitLab v0.1.5"
~~~

---

### Task 8: Выполнить локальную проверку feature candidate

**Files:**

- Verify all changed files
- Verify active version contracts are v0.1.5
- Preserve unchanged: legacy `v0.1.4` tag, Release record and metadata; do not
  recover, recreate, mirror or byte/hash-verify its assets

- [ ] **Step 8.1: Форматировать только изменённый Go code**

Получить список Go files feature-ветки относительно сохранённого GitLab
baseline,
добавить новые untracked Go files и передать только их gofmt. Не запускать
механическую перезапись unrelated files.

Проверка:

~~~powershell
$gitlabBase = git rev-parse refs/remotes/gitlab/master
git merge-base --is-ancestor $gitlabBase HEAD
if ($LASTEXITCODE -ne 0) { throw 'GITLAB_BASELINE_NOT_ANCESTOR' }
$mergeBase = git merge-base HEAD $gitlabBase
$changedGo = @(
  git diff --name-only --diff-filter=ACMR $mergeBase HEAD -- '*.go'
  git ls-files --others --exclude-standard -- '*.go'
) | Sort-Object -Unique
if ($changedGo.Count -gt 0) { gofmt -w $changedGo }
$unformatted = if ($changedGo.Count -gt 0) { gofmt -l $changedGo } else { @() }
if ($unformatted) { throw "GOFMT_FAILED: $unformatted" }
~~~

- [ ] **Step 8.2: Запустить полный локальный quality gate один раз**

~~~powershell
go vet ./...
go test ./...
go build ./cmd/teamkit
go build ./cmd/teamkit-security-audit
git diff --check
~~~

Expected: все команды code 0. Локальный full race не дублировать: go test -race ./... остаётся в четырёх native GitHub lanes.

- [ ] **Step 8.3: Выполнить security-focused review**

Проверить diff поиском:

~~~powershell
$gitlabBase = [string](git rev-parse refs/remotes/gitlab/master)
$historicalV013 = @(
  'scripts/publish-v0.1.3.ps1',
  'docs/CONFLUENCE-INSTALL-v0.1.3.md'
)
git diff --exit-code $gitlabBase HEAD -- $historicalV013
if ($LASTEXITCODE -ne 0) { throw 'V0_1_3_PUBLISHED_ENTRY_OR_DOC_CHANGED' }
git grep -n 'OFFICECLI_SAFE_RELEASE_UNAVAILABLE' HEAD -- . ':(exclude)docs/superpowers/plans/2026-08-18-hermes-officecli-mcp.md' ':(exclude)docs/superpowers/specs/2026-08-18-hermes-officecli-mcp-design.md'
$obsoleteExit = $LASTEXITCODE
if ($obsoleteExit -eq 0) { throw 'OBSOLETE_OFFICECLI_BLOCKER_PRESENT' }
if ($obsoleteExit -ne 1) { throw 'OBSOLETE_OFFICECLI_BLOCKER_SCAN_FAILED' }
rg -n "latest|OFFICECLI_SAFE_RELEASE_UNAVAILABLE|OFFICECLI_SKIP_UPDATE|officecli install|officecli skills? install|install\.sh|install\.ps1|cmd /c|powershell|pwsh|sh -c|bash -c" internal assets .github scripts docs --glob '!docs/superpowers/plans/2026-08-18-hermes-officecli-mcp.md' --glob '!docs/superpowers/specs/2026-08-18-hermes-officecli-mcp-design.md'
~~~

Каждое `latest`/installer/shell совпадение должно быть либо
запретом/документацией, либо существующим Hermes installer pin. В OfficeCLI
runtime не должно быть ни одного такого пути. Для
Отдельный `git grep` покрывает все tracked files и требует zero
`OFFICECLI_SAFE_RELEASE_UNAVAILABLE`; сам negative assertion этого plan/spec
исключён;
`OFFICECLI_SKIP_UPDATE` разрешён только в объяснении upstream поведения и
запрете policy tests, но отсутствует в runtime YAML, Go contract и execution.

Проверить, что historical published entry point
`scripts/publish-v0.1.3.ps1` и инструкция
`docs/CONFLUENCE-INSTALL-v0.1.3.md` не изменены относительно GitLab baseline.
Никакие `docs/release-evidence/v0.1.3-baseline.*` не создаются и не требуются:
их не существовало в feature base, а legacy `v0.1.4` сохраняется только как
metadata без неподтверждённых asset-byte/hash claims.

Также проверить, что:

- SHA сравнивается до WriteFileAtomic;
- verified binary запускается только с literal config argv, а set всегда
  сопровождается independent read-back exact `false`;
- Plan/Observe читает persisted user config напрямую и не запускает config
  command;
- post-config/post-MCP SHA равен catalog SHA, update sibling files отсутствуют;
- regular updater siblings очищаются только после policy read-back, а unsafe
  sibling type/path и ошибка удаления завершаются fail-closed;
- Team Kit не копирует и не устанавливает OfficeCLI skills; accepted refresh
  ограничен уже существующими upstream skill identities и описан в docs;
- secret values не входят в profile, contract, logs или errors;
- Plan не выполняет network/write;
- non-Hermes behavior не изменён;
- symlink/junction rejection происходит до download;
- v8std, Jira и Confluence config не изменены;
- strict managed state ожидает ровно четыре MCP;
- existing ordered MCPServers расширен четвёртой записью без параллельного
  contract framework;
- error codes стабильны и проверяются тестами.

- [ ] **Step 8.4: Запросить code review**

Использовать superpowers:requesting-code-review для всего диапазона OfficeCLI commits. Все P0/P1 замечания исправить через TDD; P2 оценить явно. После исправлений повторить только affected targeted tests и затем Step 8.2 один раз.

- [ ] **Step 8.5: Commit review fixes и выполнить финальный status audit**

Если gofmt или review изменили файлы, повторить affected tests и создать один
малый Conventional Commit. Затем:

~~~powershell
git status --short
git log --oneline -10
~~~

Expected: isolated feature worktree clean; plan/spec, qualification record и
OfficeCLI commits находятся в истории. Никакие tokens, `.env`, `db`, `.teamkit`
или generated operation receipts не tracked/staged. Пользовательские untracked
files исходного workspace в этот worktree не копировались.

---

### Task 9: Доставить единый v0.1.5 candidate через GitLab MR

**External state:**

- GitLab branch/MR/pipeline: `1c/aisuz/ai`
- GitHub CI branch/workflow: `dmitry-m1man/kit-all-team`
- Source changes after Step 9.2: forbidden; новый commit требует полного
  повторения exact-SHA gates

Steps 9.2–9.6 используют один `$deliveryBranch =
'codex/hermes-officecli-mcp'` и один `$candidateSHA`. Значения повторно
вычисляются в каждом shell block; stale variables из R0 не используются.

- [ ] **Step 9.1: Обновить GitLab master без переписывания истории**

~~~powershell
git fetch --no-tags gitlab master
if ($LASTEXITCODE -ne 0) { throw 'GITLAB_FETCH_FAILED' }
$currentGitLabMaster = git rev-parse refs/remotes/gitlab/master
git merge-base --is-ancestor $currentGitLabMaster HEAD
if ($LASTEXITCODE -ne 0) {
  git merge --no-edit refs/remotes/gitlab/master
  if ($LASTEXITCODE -ne 0) { throw 'GITLAB_MASTER_INTEGRATION_REQUIRED' }
  go vet ./...
  go test ./...
  go build ./cmd/teamkit
  go build ./cmd/teamkit-security-audit
  git diff --check
}
if (git status --porcelain) { throw 'FEATURE_WORKTREE_NOT_CLEAN' }
$deliveryBranch = 'codex/hermes-officecli-mcp'
$candidateSHA = [string](git rev-parse HEAD)
if ($candidateSHA -notmatch '^[0-9a-f]{40}$') { throw 'CANDIDATE_SHA_INVALID' }
~~~

Если `master` продвинулся, разрешён только обычный merge commit с повторной
локальной проверкой и code review изменившегося диапазона. Rebase, amend и
force-push запрещены.

- [ ] **Step 9.2: Обычным push опубликовать один candidate SHA в оба CI refs**

~~~powershell
$deliveryBranch = 'codex/hermes-officecli-mcp'
$candidateSHA = [string](git rev-parse HEAD)
git push gitlab "HEAD:refs/heads/$deliveryBranch"
if ($LASTEXITCODE -ne 0) { throw 'GITLAB_FEATURE_PUSH_FAILED' }
$tokenLine = Get-Content -LiteralPath 'G:\.hermes\.env' -Encoding UTF8 |
  Where-Object { $_ -match '^GITHUB_TOKEN=' } | Select-Object -First 1
if (-not $tokenLine) { throw 'GITHUB_TOKEN_MISSING' }
$previousGHToken = $env:GH_TOKEN
try {
  $env:GH_TOKEN = $tokenLine.Substring('GITHUB_TOKEN='.Length).Trim().Trim('"').Trim("'")
  git -c credential.helper= -c 'credential.helper=!gh auth git-credential' push github-ci "HEAD:refs/heads/$deliveryBranch"
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_CI_FEATURE_PUSH_FAILED' }
  $githubFeatureSHA = [string]((git -c credential.helper= -c 'credential.helper=!gh auth git-credential' ls-remote --heads github-ci "refs/heads/$deliveryBranch") -split '\s+')[0]
} finally {
  if ($null -eq $previousGHToken) { Remove-Item Env:GH_TOKEN -ErrorAction SilentlyContinue } else { $env:GH_TOKEN = $previousGHToken }
}
$gitlabFeatureSHA = [string]((git ls-remote --heads gitlab "refs/heads/$deliveryBranch") -split '\s+')[0]
if ($gitlabFeatureSHA -cne $candidateSHA -or $githubFeatureSHA -cne $candidateSHA) {
  throw 'FEATURE_REMOTE_SHA_MISMATCH'
}
~~~

Scoped credential helper действует только для одной команды и получает token из
`GH_TOKEN`; постоянный Git config не меняется. Не помещать token в remote URL,
command arguments или logs.

- [ ] **Step 9.3: Потребовать две зелёные проверки exact candidate SHA**

GitLab branch push должен создать pipeline с `sha == candidateSHA`. Через
authenticated GitLab API выбрать ровно один latest pipeline с
`ref == deliveryBranch`, `source == push` и exact SHA, дождаться `success` и
сохранить pipeline ID/URL/ref/source/SHA/status в external delivery evidence.
Pipeline другого SHA или неоднозначная выборка не засчитываются.

GitHub workflow запускается вручную, потому что текущий push trigger не включает
`codex/**`:

~~~powershell
$deliveryBranch = 'codex/hermes-officecli-mcp'
$candidateSHA = [string](git rev-parse HEAD)
$tokenLine = Get-Content -LiteralPath 'G:\.hermes\.env' -Encoding UTF8 |
  Where-Object { $_ -match '^GITHUB_TOKEN=' } | Select-Object -First 1
if (-not $tokenLine) { throw 'GITHUB_TOKEN_MISSING' }
$previousGHToken = $env:GH_TOKEN
try {
  $env:GH_TOKEN = $tokenLine.Substring('GITHUB_TOKEN='.Length).Trim().Trim('"').Trim("'")
  $dispatchAt = (Get-Date).ToUniversalTime()
  gh workflow run ci.yml --repo dmitry-m1man/kit-all-team --ref $deliveryBranch -f expected_sha=$candidateSHA
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_ACTIONS_DISPATCH_FAILED' }
  $deadline = (Get-Date).AddSeconds(60)
  $run = $null
  do {
    Start-Sleep -Seconds 3
    $candidateRuns = @(gh run list --repo dmitry-m1man/kit-all-team --workflow ci.yml --branch $deliveryBranch --event workflow_dispatch --limit 3 --json databaseId,createdAt,headSha,url | ConvertFrom-Json)
    $run = $candidateRuns | Where-Object { [datetime]$_.createdAt -ge $dispatchAt -and $_.headSha -ceq $candidateSHA } | Select-Object -First 1
  } until ($run -or (Get-Date) -ge $deadline)
  if (-not $run) { throw 'GITHUB_ACTIONS_RUN_IDENTITY_INVALID' }
  gh run watch $run.databaseId --repo dmitry-m1man/kit-all-team --exit-status
  if ($LASTEXITCODE -ne 0) {
    $jobs = gh api "repos/dmitry-m1man/kit-all-team/actions/runs/$($run.databaseId)/jobs" | ConvertFrom-Json
    if ($jobs.total_count -eq 0) { throw 'GITHUB_ACTIONS_BILLING_BLOCKED' }
    throw 'GITHUB_FEATURE_CI_FAILED'
  }
} finally {
  if ($null -eq $previousGHToken) { Remove-Item Env:GH_TOKEN -ErrorAction SilentlyContinue } else { $env:GH_TOKEN = $previousGHToken }
}
~~~

- [ ] **Step 9.4: Проверить platform/security evidence**

Для `candidateSHA` обязательны:

- GitLab Linux verify pipeline PASS;
- Windows amd64, Linux amd64, macOS amd64 и macOS arm64 native jobs PASS;
- ALT p11 userspace PASS с тем же Linux amd64 asset;
- exact SHA/size, `--version`, MCP initialize и `tools/list` PASS;
- macOS `codesign --verify --strict` и `spctl --assess` PASS;
- security audit artifacts и одинаковый Team Kit candidate digest во всех jobs.

До merge единого release candidate требуется smoke на Windows с политикой, эквивалентной
целевой рабочей станции: AppLocker/Defender/SmartScreen evidence либо формально
согласованное освобождение. Иначе merge/release блокируется кодом
`WINDOWS_POLICY_EVIDENCE_UNAVAILABLE`.

- [ ] **Step 9.5: Создать и безопасно merge GitLab MR**

Создать MR `$deliveryBranch -> master` только в GitLab. В description указать
candidate SHA, qualification record, GitLab pipeline URL, GitHub run URL,
platform evidence и результаты локальных команд. GitHub PR, tag и Release не
создавать.

GitLab сейчас не включает обязательное `pipelines must succeed`. Поэтому
непосредственно перед merge вручную проверить, что latest MR pipeline имеет
`sha == candidateSHA` и status `success`. Если GitLab `master` изменился, влить его
в feature branch обычным merge commit, повторить Tasks 8 и 9.2–9.4 и обновить
MR evidence.

После approval повторно fetch/read GitLab `master`, сохранить его exact
`expectedTargetSHA`, сверить с branch API и потребовать, чтобы `candidateSHA`
являлся его descendant. Повторно прочитать MR через GitLab API и потребовать
`state == opened`, exact source/target branches, `sha == candidateSHA` и
`diff_refs.head_sha == candidateSHA`. В MR/delivery evidence сохранить
`expectedTargetSHA`.

Выполнить merge API request с параметрами `sha=candidateSHA`, `squash=false` и
штатным merge method. Это expected-source-SHA guard: HTTP 409/422 означает, что
candidate изменился, и требует полного rerun, а не повторной кнопки Merge. Сразу
перечитать MR и сохранить `state=merged`, `merge_commit_sha` и merge timestamp.
GitLab API не имеет атомарного target-SHA guard, поэтому Step 9.6 обязательно
проверяет exact parents/tree. Если target успел измениться в race, release
блокируется; автоматический revert уже выполненного merge запрещён.

- [ ] **Step 9.6: Повторить acceptance на итоговом GitLab merge SHA**

Перед shell block задать non-secret `TEAMKIT_OFFICECLI_MR_IID` exact IID и
`TEAMKIT_OFFICECLI_EXPECTED_TARGET_SHA` из Step 9.5. Внутри in-memory GitLab
credential `try/finally` повторно получить MR по этому IID; feature branch после
merge может быть удалена и больше не является источником identity.

~~~powershell
$deliveryBranch = 'codex/hermes-officecli-mcp'
$mrIID = [int]$env:TEAMKIT_OFFICECLI_MR_IID
if ($mrIID -le 0) { throw 'GITLAB_MR_IID_REQUIRED' }
$expectedTargetSHA = [string]$env:TEAMKIT_OFFICECLI_EXPECTED_TARGET_SHA
if ($expectedTargetSHA -notmatch '^[0-9a-f]{40}$') { throw 'GITLAB_EXPECTED_TARGET_SHA_REQUIRED' }
$gitlabToken = [string]$env:GITLAB_TOKEN
if ([string]::IsNullOrWhiteSpace($gitlabToken)) { throw 'GITLAB_TOKEN_MISSING' }
$gitlabHeaders = @{ 'PRIVATE-TOKEN' = $gitlabToken }
try {
  $mergedMR = Invoke-RestMethod -Method Get -Headers $gitlabHeaders -Uri "https://gitlab.example.invalid/api/v4/projects/1c%2Faisuz%2Fai/merge_requests/$mrIID"
} finally {
  $gitlabHeaders.Clear()
  $gitlabToken = $null
}
if ($mergedMR.state -cne 'merged' -or $mergedMR.source_branch -cne $deliveryBranch -or $mergedMR.target_branch -cne 'master') {
  throw 'GITLAB_MERGED_MR_IDENTITY_INVALID'
}
$candidateSHA = [string]$mergedMR.sha
$expectedMergeSHA = [string]$mergedMR.merge_commit_sha
if ($candidateSHA -notmatch '^[0-9a-f]{40}$' -or $expectedMergeSHA -notmatch '^[0-9a-f]{40}$') {
  throw 'GITLAB_MERGED_MR_SHA_INVALID'
}
git fetch --no-tags gitlab master
$productionSHA = [string](git rev-parse refs/remotes/gitlab/master)
if ($productionSHA -cne $expectedMergeSHA) { throw 'GITLAB_MASTER_MOVED_AFTER_MERGE' }
git merge-base --is-ancestor $candidateSHA $productionSHA
if ($LASTEXITCODE -ne 0) { throw 'GITLAB_MERGE_OMITS_CANDIDATE' }
$parentParts = @((git rev-list --parents -n 1 $productionSHA) -split '\s+')
if ($parentParts.Count -ne 3 -or $parentParts[1] -cne $expectedTargetSHA -or $parentParts[2] -cne $candidateSHA) {
  throw 'GITLAB_MERGE_PARENT_IDENTITY_INVALID'
}
$productionTree = [string](git rev-parse "$productionSHA`^{tree}")
$candidateTree = [string](git rev-parse "$candidateSHA`^{tree}")
if ($productionTree -cne $candidateTree) { throw 'GITLAB_MERGE_TREE_IDENTITY_INVALID' }
$tokenLine = Get-Content -LiteralPath 'G:\.hermes\.env' -Encoding UTF8 |
  Where-Object { $_ -match '^GITHUB_TOKEN=' } | Select-Object -First 1
if (-not $tokenLine) { throw 'GITHUB_TOKEN_MISSING' }
$previousGHToken = $env:GH_TOKEN
try {
  $env:GH_TOKEN = $tokenLine.Substring('GITHUB_TOKEN='.Length).Trim().Trim('"').Trim("'")
  git -c credential.helper= -c 'credential.helper=!gh auth git-credential' fetch --no-tags github-ci main
  git merge-base --is-ancestor refs/remotes/github-ci/main $productionSHA
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_MIRROR_DIVERGED' }
  git -c credential.helper= -c 'credential.helper=!gh auth git-credential' push github-ci "${productionSHA}:refs/heads/main"
  if ($LASTEXITCODE -ne 0) { throw 'GITHUB_MAIN_FAST_FORWARD_FAILED' }
  $githubMainSHA = [string]((git -c credential.helper= -c 'credential.helper=!gh auth git-credential' ls-remote --heads github-ci refs/heads/main) -split '\s+')[0]
  if ($githubMainSHA -cne $productionSHA) { throw 'GITHUB_MAIN_SHA_MISMATCH' }
} finally {
  if ($null -eq $previousGHToken) { Remove-Item Env:GH_TOKEN -ErrorAction SilentlyContinue } else { $env:GH_TOKEN = $previousGHToken }
}
~~~

Push обязан быть обычным fast-forward; force запрещён. Запустить GitHub
`ci.yml` на `main` с `expected_sha=productionSHA` тем же безопасным token flow,
дождаться всех native/ALT checks и проверить `headSha == productionSHA`.
Дополнительно потребовать успешный GitLab `master` pipeline с тем же SHA.
Feature-SHA evidence не заменяет этот post-merge gate. Сохранить exact GitLab
pipeline/job/artifact IDs и GitHub run/artifact IDs, names, sizes и API digests;
если API digest отсутствует, скачать artifact и вычислить SHA-256 fail-closed.

- [ ] **Step 9.7: Зафиксировать delivery evidence без нового commit**

Сохранить MR URL/IID, expected target SHA, candidate/merge SHA, exact GitLab
pipeline/job/artifact IDs/URL и
exact GitHub run/artifact IDs/URL/digests в MR/release evidence. Не дописывать
их в tracked file после acceptance: любой новый commit сделал бы проверенный
SHA устаревшим. На этом единый v0.1.5 release candidate находится в production
GitLab `master`; Task 10 только публикует его.

---

### Task 10: Опубликовать GitLab Release v0.1.5 существующим bounded publisher

**External action:** создание package, upload, MR, CI dispatch, merge и Release
требуют отдельного свежего подтверждения. До него выполняются только read-only
preflight и local verification.

**Tracked files:** none. Любое изменение source после Task 9 создаёт новый SHA
и возвращает работу в Task 8.

- [ ] **Step 10.1: Fail-closed preflight Generic Package provenance**

После exact-final-SHA acceptance existing bounded publisher повторно проверяет
metadata legacy `v0.1.4` read-only, не читая assets и не сравнивая их bytes/hashes.
Через authenticated GitLab API он требует отсутствия GitLab tag, Release и Generic
Package Registry package `teamkit` версии `v0.1.5`: inventory проходит все
статусы package API и все страницы, а не только default/first page. Проверяются
existing protected-tag contract, exact final SHA, GitLab pipeline/verify job и
pinned successful GitHub `workflow_dispatch` evidence exact того же SHA.
Любой existing package record/file, tag или Release, включая partial package,
возвращает `GITLAB_V0_1_5_PREFLIGHT_FAILED` или
`GITLAB_V0_1_5_RESUME_CONFLICT`; это manual blocker для этой версии. Publisher
не удаляет, не перезаписывает и не пытается автоматически resume такой state.

- [ ] **Step 10.2: Выполнить bounded GitLab-only package-first publication**

Параметризованный existing publisher строит exact набор шести файлов:

1. `teamkit-v0.1.5-windows-amd64.exe`
2. `teamkit-v0.1.5-linux-amd64`
3. `teamkit-v0.1.5-darwin-amd64`
4. `teamkit-v0.1.5-darwin-arm64`
5. `SHA256SUMS`
6. `SECURITY-AUDIT.json`

Он сверяет candidate hashes/manifest с exact GitLab и GitHub CI evidence, затем
выполняет ровно шесть one-shot `PUT` в GitLab Generic Package Registry package
`teamkit`, version `v0.1.5`, в перечисленном порядке. Каждый `PUT` принимается
только при exact HTTP `201` и никогда не retry-ится. После каждого файла
publisher заново инвентаризирует все package statuses/pages и требует ровно
загруженный prefix: без extra/duplicate files и с совпадающим API digest, если
он предоставлен. После шестого файла authenticated API повторно скачивает все
шесть package files и сравнивает full SHA-256 с candidate hashes fail-closed.

Непосредственно перед tag publisher повторно проверяет production refs, exact
GitLab verify job и all-status/all-page inventory exact-six. Только после этого
он создаёт GitLab tag и GitLab Release `v0.1.5`, чьи шесть links указывают на
Generic Package URLs. Любой timeout, transport ambiguity, non-201, race,
duplicate/extra file, wrong digest либо ref/job movement после первого `PUT`
оставляет unlinked partial package для ручного расследования и завершает работу
без tag, Release, delete или retry. Direct push в `master`, GitHub tag и GitHub
Release API запрещены.

- [ ] **Step 10.3: Postverify GitLab-only release**

Через GitLab API и clean temp directory подтвердить target tag/Release == final
SHA, exact package name/version, exact six filenames, package URL каждого Release
link и SHA-256 всех authenticated повторно скачанных bytes. Выполнить `teamkit
version` для четырёх binaries там, где runner доступен, и подтвердить отсутствие
GitHub tag/Release `v0.1.5`. Release завершён только после
`GITLAB_V0_1_5_POSTVERIFY_OK`; conflicting или incomplete state остаётся
fail-closed без delete/overwrite.

#### Historical superseded Task 10 text (non-normative)

Прежний recipe публикации `v0.1.4` отменён и не является исполняемой
инструкцией. Он зависел от недоступных legacy asset bytes и несуществующего
tracked `v0.1.3` baseline evidence. Для `v0.1.4` разрешена только read-only
проверка неизменных tag/Release metadata; recovery, mirroring, replacement и
byte/hash claims запрещены. Единственный активный publication contract выше —
package-first `v0.1.5`.

---

## Definition of Done

Функциональность завершена только когда одновременно истинны все условия:

1. Exact OfficeCLI `v1.0.144`/`1ced45e900782c5083ed550ddf328ee974e425e7`
   имеет source record `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_SOURCE` и
   Task 6 runtime record `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME`.
2. Catalog и assets/payloads.json содержат одинаковые exact pins.
3. Team Kit скачивает только selected pinned asset, проверяет exact size и SHA-256 и атомарно публикует mode 0700.
4. Повторный запуск использует валидный cache; tamper вызывает штатное восстановление через configure_application.
5. Hermes config содержит ровно четыре MCP: неизменённые v8std, Jira,
   Confluence и exact local OfficeCLI stdio declaration.
6. После asset verification Team Kit выполняет fixed
   `config autoUpdate false`, проверяет persisted exact `false` и не полагается
   на `OFFICECLI_SKIP_UPDATE`; effective OS user home, config/log paths и их типы
   проверены до process/readiness, post-MCP binary SHA не меняется и update
   sibling files отсутствуют.
7. Team Kit не устанавливает OfficeCLI skills. Best-effort refresh ранее
   установленных OfficeCLI skills во всех обнаруженных agent homes, возможная
   перезапись local edits и user-global config scope явно документированы; clean
   home не получает новые agent/skill identities.
8. Strict managed state и existing ordered operation `MCPServers` знают о
   четвёртом MCP; retry fail-closed реагирует на любое изменение OfficeCLI
   identity до приватных adapters, включая retirement RC2 resume при сохранении
   immutable historical fixture.
9. Non-Hermes applications не видят OfficeCLI.
10. Локальные vet/test/build/diff checks зелёные.
11. Feature merged в GitLab `master` через MR; exact merge SHA имеет успешный
    GitLab pipeline и GitHub Windows amd64, Linux amd64, macOS amd64, macOS
    arm64 и ALT p11 evidence; его два parent SHA и tree доказуемо соответствуют
    проверенным target/candidate SHA и candidate tree.
12. Version/release-preparation commit входит в тот же reviewed MR; GitLab
    tag/Release `v0.1.5` указывает именно на повторно проверенный merge SHA.
13. GitLab Generic Package `teamkit` версии `v0.1.5` содержит ровно шесть
    expected filenames; все они authenticated повторно скачаны и hash-verified,
    а GitLab Release links указывают на package. GitHub не содержит tag/Release
    `v0.1.5`.
14. GitLab `v0.1.4`, его tag, Release record и metadata остались неизменными;
    для его недоступных legacy assets не заявляется byte/hash equality и не
    выполняется recovery, replacement или mirroring.
15. GitHub Actions billing исправлен, corporate Windows policy evidence принят,
    а secrets и пользовательские файлы не попали в commits/artifacts/logs.

## Оценка

| Работа | Активное время |
|---|---:|
| GitLab baseline, worktree и qualification exact v1.0.144 | 0.6–0.9 ч |
| Catalog, pins и manifest consistency | 0.4–0.6 ч |
| Downloader, provisioner и verified config policy | 1.2–1.6 ч |
| Renderer и strict managed state | 0.5–0.8 ч |
| Bootstrap/service wiring и ordered operation contract | 1.0–1.4 ч |
| Native/ALT live smoke и CI policy tests | 1.0–1.4 ч |
| v0.1.5 contracts, документация, local verification и review fixes | 1.0–1.5 ч |
| Exact-SHA feature delivery и GitLab MR | 0.5–0.9 ч |
| **До merge единого release candidate** | **6.2–9.1 ч** |
| Bounded GitLab publication и post-verify | 0.5–0.8 ч |
| **Полный GitLab Release v0.1.5** | **6.7–9.9 ч** |

Оценка предполагает exact qualified `v1.0.144`, использование уже
существующих Team Kit primitives и отсутствие platform-specific workaround по
результатам smoke. Ожидание нового upstream release удалено из критического
пути. Исправление GitHub billing, MR approval и получение corporate Windows
evidence в активное время не входят. Если macOS policy или ALT p11 выявит incompatibility, работа
останавливается внешним blocker и переоценивается отдельно.

GitLab и GitHub checks запускаются параллельно для feature SHA и затем для
итогового merge SHA. Пассивное ожидание составляет примерно 45–120 минут;
второй MR/CI-цикл удалён. Rerun из-за внешней инфраструктуры увеличивает
wall-clock, но не добавляется скрыто к активным 6.7–9.9 часа.
