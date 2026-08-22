# OfficeCLI MCP для профиля Hermes

> **Актуализация 2026-08-20 (имеет приоритет над историческими версиями
> ниже):** свежий GitLab baseline — `master` и published `v0.1.4` на commit
> `03bd00dec7f318aa87b82151243a4b6c632e43e2`; annotated tag object —
> `90781d471bb1aa3513e07050877f11169fbe1bf5`. Следующий выпуск с OfficeCLI
> должен быть только GitLab `v0.1.5`. Во всём документе `v0.1.3` как
> immutable baseline заменяется на `v0.1.4`, а прежний целевой `v0.1.4` — на
> `v0.1.5`; это включает release-evidence filenames, historical publisher
> entry point и release contracts. `v0.1.4` — immutable legacy metadata baseline:
> его шесть original asset bytes недоступны и не могут быть byte/hash-verified,
> recreated, replaced или mirrored.
> Перед началом R0 заново получает GitLab master/tag/release и fail-closed
> подтверждает отсутствие `v0.1.5` tag/Release в GitLab и GitHub.
>
> Release direct links `v0.1.4` возвращают `404`, exact GitHub evidence run
> недоступен, а GitLab Generic Package Registry пуст. Это объясняет, почему
> `v0.1.4` сохраняется только как metadata baseline.

## Статус и принятое решение

Для интеграции принят immutable release OfficeCLI `v1.0.144`, tag commit
`1ced45e900782c5083ed550ddf328ee974e425e7`. Production-код начинается после
проверки release metadata, license, четырёх platform assets, их размеров и
SHA-256, а также MCP handshake. Ожидание нового upstream release больше не
является предпосылкой.

Team Kit не полагается на `OFFICECLI_SKIP_UPDATE=1`: в `v1.0.144` ранний MCP
dispatch обходит общий environment guard. После проверки exact binary Team Kit
запускает только фиксированную upstream-команду
`officecli config autoUpdate false`, затем выполняет command read-back exact
`false`, а последующий readiness read-only разбирает persisted JSON напрямую.
Поэтому binary остаётся закреплённым за Team Kit и не вступает в цикл
self-update/repair. Exit code setter недостаточен: upstream `v1.0.144` может
вернуть success даже при неуспешном сохранении config.

Эта настройка хранится в `${UserProfile}/.officecli/config.json`, действует на
все процессы OfficeCLI текущего OS user и не является profile-local настройкой
Hermes. Побочный эффект явно документируется. Ручное повторное включение
auto-update не поддерживается: следующий Team Kit observe возвращает not-ready,
а `configure_application` снова устанавливает `false`.

Даже при `autoUpdate=false` OfficeCLI при переходе на новую версию пытается
обновить уже установленные OfficeCLI skills во всех обнаруженных agent homes и
записывает `lastSkillRefreshVersion`. В обычном последовательном сценарии marker
делает следующий запуск той же версии content-idempotent, но refresh best-effort
и может завершиться частично, повториться после ошибки persistence либо при
конкурентном старте. Team Kit не считает его readiness-gate и не обещает retry.
Это принято как ограниченный side effect: Team Kit не устанавливает эти skills,
не использует их для целевого Hermes profile и рекомендует встроенную команду
`load_skill` единственного MCP-инструмента `officecli`. Отсутствующие agents и
OfficeCLI skill folders upstream refresh не создаёт, но пользовательские правки
в уже установленных OfficeCLI skills могут быть перезаписаны.

Под OfficeCLI skills здесь понимаются файловые пакеты инструкций и references
для AI-агентов, например `officecli-pptx`, `officecli-docx`, `officecli-xlsx`,
`morph-ppt` и `officecli-pitch-deck`. Это не исполняемые модули и не
дополнительные MCP-серверы. Их автоматическая установка не нужна для исходной
цели: единственный MCP-инструмент `officecli` отдаёт embedded guidance по
команде `load_skill`, не записывая его в каталоги skills целевого профиля.

## Цель

При настройке приложения Hermes Team Kit загружает один закреплённый нативный
OfficeCLI asset из `https://github.com/iOfficeAI/OfficeCLI`, проверяет его
SHA-256, атомарно размещает в управляемом
версионном каталоге и добавляет OfficeCLI как локальный stdio MCP в
`config.yaml` выбранного профиля.

## Репозитории и маршрут изменения

Production source of truth — GitLab-репозиторий
`https://gitlab.example.invalid/1c/aisuz/ai.git`, ветка `master`.
GitHub-репозиторий `https://github.com/dmitry-m1man/kit-all-team.git`
используется только как cross-platform CI mirror и не является источником
публикации.

Аудит 2026-08-20 зафиксировал GitLab `master` и published `v0.1.4` на
`03bd00dec7f318aa87b82151243a4b6c632e43e2`; annotated tag object
`v0.1.4` — `90781d471bb1aa3513e07050877f11169fbe1bf5`. GitHub `main` и
текущая локальная ветка не используются как baseline. Перед разработкой SHA
GitLab `master` запрашивается заново, а isolated worktree создаётся именно от
этого fetched commit.

Local branch `codex/hermes-officecli-mcp` создаётся от GitLab `master`. Exact
feature HEAD без переписывания истории обычным push отправляется в одноимённые
GitLab- и GitHub-ветки. Равенство SHA проверяется через remote API, после чего
выполняются GitLab pipeline и обязательные hermetic/trusted-live GitHub checks,
а Merge Request создаётся только в GitLab `master`.

GitLab использует merge commit и сейчас не запрещает merge при неуспешном
pipeline. Поэтому перед merge вручную проверяется успешный exact-feature-SHA
pipeline. После merge фиксируется новый SHA GitLab `master`, обычным
fast-forward push переносится в GitHub `main`, и обе CI-системы повторно
проверяют уже итоговый SHA. При расхождении истории GitHub работа останавливается
с `GITHUB_MIRROR_DIVERGED`; force-push запрещён.

Текущий GitLab release `v0.1.4`, его tag и metadata остаются неизменяемыми.
Он не является byte/hash baseline: шесть original assets не восстанавливаются,
не заменяются и не зеркалируются. Prepublication и postpublication gates
проверяют metadata `v0.1.4` read-only и не заявляют equality его asset bytes.
Первый полностью проверяемый выпуск с OfficeCLI — `v0.1.5`, публикуемый только
в GitLab после exact-final-SHA acceptance. Активные version contracts и
существующий bounded GitLab publisher параметризуются для `v0.1.5`; новый
publisher/workflow, GitHub tag и GitHub Release не создаются. До создания
GitLab Release publisher загружает шесть assets в GitLab Generic Package Registry
как package `teamkit`, version `v0.1.5`, повторно скачивает их authenticated API
и сравнивает exact SHA-256; Release links ссылаются только на этот package.

Чтобы не выполнять второй MR и вторую полную CI-матрицу без повышения качества,
version contracts `v0.1.5` и тонкая entry point существующего publisher входят
в тот же OfficeCLI MR. Feature и release изменения остаются отдельными малыми
commits для ревью, но образуют один release candidate. Target тега — итоговый
merge commit этого MR, повторно проверенный обеими CI-системами.

До начала production-кода GitHub Actions должен реально запускать jobs. На
момент аудита run `32114768851` был остановлен до первого шага из-за
payments/spending limit; пока это не исправлено, действует внешний blocker
`GITHUB_ACTIONS_BILLING_BLOCKED`.

## Объём

- Изменение действует только для `application=hermes`.
- Production-изменение выполняется только в ветке, основанной на актуальном
  GitLab `master`.
- Поддерживаются Windows amd64, Linux amd64, macOS amd64 и macOS arm64.
- ALT Linux использует тот же Linux amd64 asset и проходит отдельный smoke.
- OfficeCLI `v1.0.144`, commit, имена assets, HTTPS URL, размеры и SHA-256
  фиксируются после qualification; `latest` в runtime запрещён.
- OfficeCLI хранится в
  `HERMES_HOME/.teamkit/officecli/<version>/officecli[.exe]` с режимом `0700`
  на POSIX.
- В Hermes создаётся MCP `officecli` с абсолютным `command`,
  `args: ["mcp"]`, `enabled: true` и без `env`; auto-update отключается
  отдельной фиксированной config-командой до записи profile YAML.
- Существующие MCP `v8std`, `customllm-jira` и `customllm-confluence`
  остаются byte/semantic-equivalent; OfficeCLI становится четвёртым MCP.
- OfficeCLI не добавляется в handoff других AI-приложений.
- Новые секреты, вопросы мастера и переменные пользовательского `.env` не
  добавляются.
- Доставка включает Merge Request в GitLab `master`, exact-final-SHA проверки и
  GitLab Release `v0.1.5`; GitHub используется только для CI evidence.

## Архитектура

Закрытый каталог хранит неизменяемую таблицу `OS/architecture -> asset`.
Существующий `DownloadPort`, SHA-verifier и `workspace.WriteFileAtomic`
используются повторно; новый installer framework не создаётся. Лимит
HTTP-загрузчика становится параметром конкретного потребителя: 4 MiB остаются
для POSIX installer Hermes, OfficeCLI получает отдельный предел 48 MiB.

Небольшой `officeCLIProvisioner` в пакете `service` связывает выбранный asset,
абсолютный путь, downloader, verifier и атомарный writer. Он реализует узкий
`bootstrap.OfficeCLIPort` с операциями `Path`, `Ensure` и context-aware `Ready`.
Provisioner после exact asset validation использует уже существующий process
runner с фиксированными argv `config autoUpdate false`; shell и пользовательские
аргументы отсутствуют. До первого process launch он проверяет canonical
user-home containment и отсутствие symlink/junction/non-regular объектов у
`.officecli`, `config.json` и, если существующий config включает logging,
`officecli.log`. В mutation-path фиксированная query-команда выполняется через
bounded capture с раздельными stdout/stderr и fixed timeout.
На Windows effective home разрешается через
`windows.KnownFolderPath(FOLDERID_Profile)`, потому что подмена
`HOME`/`USERPROFILE` не меняет `.NET SpecialFolder.UserProfile`; на Unix/macOS
используется `os.UserHomeDir`. `config autoUpdate` принимает только exact
`false`; затем `Ready` сначала повторяет проверку asset и отсутствие точных
соседних updater-файлов `.update`, `.update.partial`, `.old`. До чтения config он
повторно доказывает canonical effective-home containment и безопасный тип
`.officecli`/`config.json`; при parsed `log=true` так же проверяет
`officecli.log`. Лишь затем read-only разбирает exact user config всей pinned
AppConfig schema: case-sensitive camelCase keys, отсутствие duplicates,
совместимые типы nullable `lastUpdateCheck`, `latestVersion`,
`installedBinaryVersion`, `lastSkillRefreshVersion` и boolean `autoUpdate`/`log`.
Unknown/wrong-case key или type mismatch fail-closed; частично извлекать только
`autoUpdate` нельзя, поскольку upstream при любой ошибке десериализации всего
AppConfig возвращается к default `autoUpdate=true`. После полной проверки
требуется boolean `autoUpdate=false`. Поэтому redirected config/log path либо
несовместимый JSON не может дать ложный `ApplicationReady`. Это сохраняет
Observe/Plan без process и log writes; новый process framework не создаётся.
Существующий `configure_application` вызывает `Ensure` после существующего
Hermes ensure, но до profile secret, certificate configuration и профильного
YAML. Внешняя mutation stage уже может materialize корпоративный CA до входа в
configure; её порядок ради OfficeCLI не перестраивается. Новый reconcile action
и отдельный retry не создаются.

`ApplicationReady` включает сертификаты, проверенный OfficeCLI asset,
подтверждённое `autoUpdate=false` и точный profile из четырёх MCP. Строгий
`VerifyManagedProfile` проверяет именно managed YAML; существующие
Jira/Confluence headers, timeouts, disabled sampling и parallel-tool contract не
меняются. Удаление, подмена, неверный тип файла, redirected ancestor или потеря
исполняемого режима переводят состояние в not-ready либо возвращают стабильную
ошибку безопасности. Следующий штатный `retry` повторяет существующий
`configure_application`.

Existing verify action сочетает `VerifyManagedProfile` с отдельным вызовом
`OfficeCLIPort.Ready(ctx)`: строгая YAML-проверка не видит внешнюю user-global
config policy и не заменяет её.

## Operation contract

Существующий ordered `operationContract.MCPServers` расширяется четвёртым
элементом `officecli`; параллельный MCP contract не создаётся. OfficeCLI entry
включает версию, целевую платформу и архитектуру, имя asset, URL, размер,
SHA-256, абсолютный managed path (он же stdio command), `args` и immutable
`update_policy=auto_update_disabled_user_config` и
`skill_refresh_policy=existing_installed_only_best_effort`. Environment у stdio
MCP отсутствует. Изменение любого pin или policy выбранного asset делает
сохранённую операцию несовместимой до открытия приватных адаптеров. Legacy RC2
contract не переписывается, а Non-Hermes contract сохраняет существующее
поведение.

## Безопасность и ошибки

- `GITHUB_TOKEN` используется только для upstream/GitHub CI API; GitLab
  credential — только для GitLab MR/release API. Оба существуют только в памяти
  process, не выводятся и не попадают в Git config или remote URL.
- Разрешены только URL из закрытого каталога; redirects не отменяют SHA-256.
- Пустой, превышающий 48 MiB или не совпавший по SHA payload не публикуется.
- Существующий корректный файл повторно не скачивается.
- Повреждённый управляемый файл заменяется только полной атомарной записью.
- `Ready` считает любой exact updater sibling drift. `Ensure` только после
  подтверждённого `autoUpdate=false` удаляет обычные файлы с тремя фиксированными
  именами из проверенного managed parent; symlink, junction, directory, path
  escape или ошибка удаления завершаются fail-closed. После cleanup повторяются
  SHA, sibling-absence и config-policy checks. Это repair owned drift, а не
  реализация updater.
- Версионный путь исключает замену запущенного Windows `.exe`; удаление старых
  версий не входит в этот релиз.
- Team Kit запускает config-команду только после exact size/SHA/path validation
  binary и preflight user-config paths, всегда передавая argv напрямую без
  shell. Ошибка команды возвращает `OFFICECLI_AUTOUPDATE_CONFIG_FAILED`; вывод,
  отличный от exact `false`, делает приложение not-ready.
- `autoUpdate=false` является общей настройкой OS user. Team Kit не обещает
  изоляцию этой настройки одним Hermes profile и не скрывает изменение
  `${UserProfile}/.officecli/config.json`.
- Если пользователь заранее включил OfficeCLI logging, fixed config-команды
  могут дописать несекретные argv в `${UserProfile}/.officecli/officecli.log`;
  Team Kit не меняет настройку `log`, не выводит содержимое и документирует этот
  bounded side effect.
- Upstream skill refresh может менять только ранее установленные OfficeCLI skill
  trees, в том числе `${UserProfile}/.hermes/skills`, `.agents/skills` и другие
  обнаруженные agent homes. Team Kit не создаёт эти trees, не включает их в
  managed profile и не считает их release assets.
- Team Kit не запускает `officecli install`, `install.sh`, `install.ps1` или
  `officecli mcp <target>`, не вызывает bare `officecli` и не меняет
  `PATH`/shell profile.
- OfficeCLI MCP предоставляет один широкий инструмент, способный читать и
  изменять Office-файлы. Это ожидаемая функциональная граница; командный
  allowlist внутри этого единственного инструмента в релиз не добавляется.

## Проверка

TDD-покрытие расширяет существующие catalog, Hermes renderer, strict
managed-state, bootstrap lifecycle и ordered operation-contract тесты. Новый
provisioner получает отдельные табличные unit-тесты для cache hit, download,
oversize, checksum mismatch, tamper, permissions, redirected path, updater
sibling cleanup/rejection, fixed config argv, config failure и readiness при
`autoUpdate=true/false`.

Локально в worktree от GitLab `master` выполняются focused RED/GREEN циклы,
затем один `go vet ./...`, один `go test ./...`, сборка двух команд и
`git diff --check`. Race остаётся в существующей нативной GitHub-матрице. Task 0
квалифицирует immutable source/asset pin, license и accepted persisted-autoUpdate
policy без запуска Windows binary; только такой record
`QUALIFIED_PINNED_AUTOUPDATE_DISABLED_SOURCE` открывает Tasks 1–5. Task 6
обязателен для runtime qualification и Release: missing или failed Windows smoke
rejects Release. После успешного Task 6 record становится distinct
`QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME`; он не заменяет Task 10/Release
corporate Windows policy/equivalence evidence либо formal waiver. Для immutable
pin `v1.0.144` matrix Task 6 первой
OfficeCLI-командой выполняет
`config autoUpdate false`, затем command read-back exact `false`, проверку
persisted JSON и только после этого `--version` и два последовательных MCP
запуска с `initialize`/`tools/list`; SHA binary должен остаться неизменным. В
изолированном Unix/macOS clean home допускается только
`.officecli/config.json`; Windows проверяет pre/post delta всего disposable
effective profile и допускает тот же exact config delta, не требуя отсутствия
pre-existing runner files. Новые agent/skill identities и файлы
`.update/.partial/.old` запрещены. Отдельный disposable qualification fixture Task 6
preseed-ит OfficeCLI skill identity с `SKILL.md`, фиксирует её manifest и после
config set/read-back выполняет exact MCP дважды без промежуточного `--version`.
Любые bounded refresh-записи остаются внутри этой существующей identity; новый
agent/sub-skill запрещён, а второй start после сохранённого marker не меняет
manifest. Успех best-effort refresh не является readiness-gate, но любая запись
за пределами preseed identity отклоняет technical smoke. Windows smoke выполняется только под disposable OS account/VM
или GitHub-hosted runner и использует effective Known Folder profile; изменение
`HOME`/`USERPROFILE` само по себе не считается изоляцией. ALT выполняет Linux
asset в закреплённом p11 userspace. Windows smoke остаётся Task 6 technical
release gate, а corporate-Windows policy/equivalence evidence либо formal waiver
остаётся Task 10/Release gate; ни один не заменяется source/asset record Task 0.

GitHub run обязан сообщать `head_sha`, равный проверяемому GitLab SHA. Сначала
это exact feature SHA, а после merge — новый exact SHA GitLab `master`; feature
evidence не заменяет повторную проверку merge commit. GitLab pipeline также
должен относиться к тому же SHA. В MR и release evidence сохраняются
qualification record, GitHub run URL, GitLab pipeline URL и полные SHA.

## Не входит в объём

- fork или пересборка OfficeCLI;
- Windows ARM64 и Linux ARM64;
- собственный updater или поиск latest release;
- opt-in режим self-update binary или динамический runtime path `current`;
- установка OfficeCLI skills;
- очистка старых версий;
- поддержка OfficeCLI в Cursor, Codex и других клиентах;
- отдельный release workflow вместо существующего CI/release контура;
- публикация GitHub release;
- синхронизация GitLab `master` из GitHub `main` или перенос unrelated GitHub
  commits в production branch;
- изменение, перетегирование или повторная публикация `v0.1.3`.

## Оценка

Полный сценарий занимает 6,7–9,9 часа активной работы; до merge — 6,2–9,1
часа. Совокупное пассивное ожидание GitLab и GitHub CI — ориентировочно 45–120
минут. Ожидание нового upstream OfficeCLI release удалено из критического пути.
Ожидание исправления GitHub billing, корпоративного Windows evidence и MR
approval в активную оценку не входит.
