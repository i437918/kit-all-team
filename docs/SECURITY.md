# Security Model

Team Kit separates non-secret desired state from application-local credentials.
`KIT_ALL_TEAM_HOME/.env` stores selections only; GitLab and CustomLLM credentials
belong in the chosen application's `.env`. Secrets must enter through masked input,
must not appear in URLs or process arguments, and are passed to Git only through a
temporary `GIT_ASKPASS` environment.

The workspace `.gitignore` always contains `.env`, `/db/`, and `/.teamkit/`.
Plans, receipts, diagnostics, CI logs, archives, and command captures are scanned
with unique secret canaries. Status reports only whether a secret store exists and
its path.

The release gate runs `teamkit-security-audit` against tracked paths, every Git
object reachable from the exact candidate commit, exact candidate files, raw
platform evidence, and nested ZIP/TAR evidence. Scoping release history to that
commit makes evidence independent of unrelated refs present in different mirrors;
the default local audit remains repository-wide and scans all reachable refs.
It detects known token formats, credential assignments, private keys, forbidden
tracked paths, unsafe archive entries, and unscannable oversized inputs. Reports
contain only rule names, coverage counters, and one-way location digests—never the
matched value. `SECURITY-AUDIT.json` covers the candidate. Final `v0.1.0`
validation is read-only on GitHub; the protected tag and Release are published
only in the private GitLab from the same exact candidate, without rebuilding the
selected files. Durable publication evidence is recorded after release in
`docs/releases/v0.1.0.md`.

All remote URLs and externally supplied payloads are allowlisted. Windows Hermes
installation requires both the pinned SHA-256 and a trusted Authenticode signer.
Certificate archives reject absolute paths, traversal, and symlinks. Corporate CA
files stay under `HERMES_HOME/certs`; Team Kit never changes the system trust store.

## Граница безопасности финального v0.1.0

`teamkit v0.1.0 (unsigned internal release)` — финальный non-prerelease только для приватного GitLab. Бинарники Team Kit не имеют платформенной подписи; файлы macOS не подписаны Apple и не notarized. SHA-256 подтверждает целостность полученного файла, но не происхождение в смысле платформенной подписи. Если организационная политика запрещает неподписанные внутренние файлы, пользователь должен остановиться и обратиться к администратору, а не отключать системную защиту.

Trusted corporate network probe для `v0.1.0` не подтверждён: GitHub-hosted runner не разрешает внутренний DNS `gitlab.tools.enterprise.ru`. Такая проверка требует eligible self-hosted runner внутри корпоративной сети/VPN. Автономные allowlist- и security-тесты не являются заменой живому сетевому подтверждению.

ALT Linux подтверждена только в pinned p11 userspace. Нативная ALT Linux и QEMU/VM не подтверждены и не являются release gates этого внутреннего выпуска.

В Windows Hermes устанавливается через ручной графический мастер. Проверка SHA-256 и Authenticode-подписи `Hermes-Setup.exe` подтверждает конкретный файл и издателя, но не доказывает автоматическую или unattended-установку, завершение GUI либо выбранный `HERMES_HOME`.

OfficeCLI и офисные документы не поддерживаются; Team Kit не должен принимать их как входные данные или включать в release evidence.

## Граница безопасности подготовленного v0.1.5

Историческая граница `v0.1.0` выше не переписывается. В подготовленном `v0.1.5` OfficeCLI добавляется только в профиль Hermes и получает широкие прикладные возможности: tool `officecli` может читать и изменять документы Office. Пользователь должен ограничивать его рабочими документами, для которых разрешены обе операции; Team Kit не превращает эту возможность в read-only sandbox.

Разрешён только exact `v1.0.144` с платформенным SHA pin. Исполняемый файл хранится в `${HERMES_HOME}/.teamkit/officecli/v1.0.144/officecli` (`officecli.exe` в Windows); PATH и системные каталоги не изменяются. Team Kit не добавляет новый installer/updater, не принимает `latest` и не удаляет старые pinned versions.

Политика обновления сохраняется user-global в `${UserProfile}/.officecli/config.json`: сначала выполняется `officecli config autoUpdate false`, затем обязательный `officecli config autoUpdate` read-back и независимый JSON read-back. `OFFICECLI_SKIP_UPDATE` не является MCP control и не используется. До точного `false` любой updater residue блокирует конфигурацию. После подтверждения cleanup остаётся fail-closed и может касаться только `.update`, `.update.partial`, `.old` внутри доказанного owned managed parent.

Принят upstream best-effort refresh только ранее установленных OfficeCLI skills во всех обнаруженных agent homes. Он может перезаписать local edits внутри существующего skill; новые skill identities не создаются. Team Kit не устанавливает on-disk skills и не полагается на default Hermes skill directory: `officecli-pptx`, `officecli-docx`, `officecli-xlsx` и другие instruction/reference packs загружаются встроенной командой `load_skill` единственного MCP-инструмента `officecli`.

Публикация `v0.1.5` package-first и только в GitLab. До первого PUT authenticated API перечисляет все страницы и статусы exact `teamkit/v0.1.5` и требует zero records. Каждый из шести файлов отправляется ровно одним PUT без retry; успехом считается только HTTP `201 Created`. После каждого PUT publisher требует один exact package record и exact distinct prefix package files без duplicate/extra, сверяя `file_sha256`, когда API его возвращает. После шестого PUT и непосредственно перед tag повторяется exact-six inventory, затем каждый файл authenticated повторно скачивается для SHA-256 comparison; Release links ведут только на package URLs. Любой existing/concurrent package state, ambiguous response, изменившийся production ref, tag или Release останавливает flow без tag/Release/delete. Частичный unlinked package является failed external state, блокирует resume и требует ручного расследования. GitHub workflows не создают Team Kit tag/Release/assets. Уже опубликованные releases, tags и assets остаются неизменяемыми.

Runtime qualification пока pending: `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME` и CI run URL отсутствуют до trusted exact-SHA dispatch. Без зелёных Windows, Linux, macOS Intel, macOS Apple Silicon lanes и ALT p11 smoke выпуск запрещён.

Database Git operations are limited to clone, clean-state verification, fetch, and
fast-forward. Managed hooks reject commit or push from `develop`. Team Kit never
resets, stashes, deletes local changes, creates business commits, or pushes them.

Report suspected exposure without attaching the credential. Rotate it in the
authoritative service, remove any affected artifact, and preserve only sanitized
timestamps and operation IDs for investigation.

## Локальный реестр окружений

Реестр нужен только для показа ранее использованных абсолютных путей в порядке MRU. Team Kit не сканирует диски, сетевые каталоги, `PATH` или историю команд и повторно проверяет каждый путь перед показом.

Расположение файла:

- Windows: `%LOCALAPPDATA%\TeamKit\environments.json`;
- macOS: `~/Library/Application Support/TeamKit/environments.json`;
- Linux и ALT Linux: `${XDG_CONFIG_HOME:-~/.config}/teamkit/environments.json`.

Единственный допустимый JSON-контракт:

```json
{
  "schema_version": 1,
  "homes": [
    "C:\\TeamKit\\apa",
    "C:\\TeamKit\\wms"
  ]
}
```

Файл хранится в UTF-8, имеет размер не более `65536` байт и содержит не более `64` уникальных абсолютных канонических путей. Он **не содержит** проект, роль, операционную систему, AI-приложение, toolchain, credentials, токен, ключ, receipt, статус, время или историю ошибок. Любое неизвестное поле, неверный тип, версия, путь, дубликат либо превышение лимита делает реестр повреждённым.

Только каталог реестра, сам `environments.json` и временный файл атомарной замены имеют owner-only защиту: каталог `0700` и файл `0600` в POSIX; DACL с полным доступом только текущего пользователя в Windows. Запись выполняется bounded atomic replace в том же защищённом каталоге. Symlink, junction и reparse point запрещены; если безопасность владельца, типа или замены нельзя доказать, запись отклоняется.

Повреждённый или недоступный реестр вызывает одно ограниченное предупреждение. В текущем запуске он игнорируется и **не переписывается**; доступны только `KIT_ALL_TEAM_HOME` из среды или ручной ввод. Ошибка best-effort продвижения пути после уже успешной основной операции тоже даёт предупреждение, но не отменяет результат и не запускает повтор автоматически.

Workspace-файлы `.env`, `.teamkit/owner` и operation envelope/receipt являются публичными служебными метаданными, а не secret stores. Для них действуют ограничение размера, строгий разбор и запрет symlink/junction/reparse, но существующие права не мигрируют в owner-only DACL/mode. Значения секретов остаются в приватном хранилище выбранного приложения.

Для приложения не Hermes файл `.teamkit/handoff.txt` также не содержит секретов: в нём только инструкция для одного выбранного закреплённого репозитория/commit и MCP v8std. Если приложение не установлено, handoff не создаётся.
