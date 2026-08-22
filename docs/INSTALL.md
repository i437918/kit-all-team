# Установка 1C Team Kit

## Актуальный выпуск v0.1.3

Страница опубликованного выпуска: [GitLab Release v0.1.3](https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3).

`v0.1.3` — текущий опубликованный patch установки Hermes. Используйте только файлы приватной страницы [GitLab Release v0.1.3](https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3):

- `teamkit-v0.1.3-windows-amd64.exe`;
- `teamkit-v0.1.3-linux-amd64`;
- `teamkit-v0.1.3-darwin-amd64`;
- `teamkit-v0.1.3-darwin-arm64`;
- `SHA256SUMS`;
- `SECURITY-AUDIT.json`.

### Чистая установка в Windows

1. Создайте `C:\TeamKitInstaller` и скачайте туда Windows-бинарник и `SHA256SUMS`.
2. Проверьте, что SHA-256 бинарника совпадает со строкой для `teamkit-v0.1.3-windows-amd64.exe` в `SHA256SUMS`.
3. В новом PowerShell проверьте версию и запустите мастер:

```powershell
& 'C:\TeamKitInstaller\teamkit-v0.1.3-windows-amd64.exe' --version
& 'C:\TeamKitInstaller\teamkit-v0.1.3-windows-amd64.exe' apply
```

4. Первым ответом выберите `1 — Добавить новое окружение`, затем Windows, Hermes, проект, роль и один набор: `cc_1c_skills от Широкова` или `ai_rules_1c от Филиппова`.
5. Не вводите `HERMES_HOME`: Team Kit обнаруживает Hermes `>= 0.20.1 и < 0.21.0` автоматически.

Новый профиль сохраняет встроенные skills Hermes и добавляет ровно один внешний набор. Эти наборы взаимоисключающие. Team Kit не копирует Learned skills и не удаляет пользовательские skills.

### Обновление v0.1.0 и восстановление незавершённой установки

Для зарегистрированного окружения запустите `apply` и выберите `2 — Обновить существующее окружение`. Если состояние уже `needs_apply` и остались Action 50/90, не начинайте новую установку — выполните:

```powershell
& 'C:\TeamKitInstaller\teamkit-v0.1.3-windows-amd64.exe' retry --kit-home 'C:\TeamKit\apa'
```

Точный старый marker в доказанном профиле Team Kit мигрируется только официальной командой `hermes -p <identity> skills opt-in --sync`. Team Kit не выполняет общий `hermes update`. Изменённый marker считается пользовательским opt-out: `HERMES_BUNDLED_SKILLS_USER_OPT_OUT` возвращается без записи.

`SECRET_FILE_PERMISSIONS_UNSAFE` означает, что DACL локального `.env` небезопасна и профиль ещё не опубликован. `v0.1.3` при `retry --kit-home` исправляет права только того staging `.env`, который доказан текущей операцией. Не удаляйте `.env`, не включайте наследование и не создавайте `config.yaml` или MCP вручную.

### Hermes: always-on Jira и Confluence

Для профиля Hermes Team Kit автоматически добавляет включённые MCP Jira и Confluence. Обязательны собственные персональные `HERMES_CUSTOM_ISSUE_TRACKER_TOKEN` (метка `Jira personal token`) и `HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN` (метка `Confluence personal token`); получите их по корпоративной [инструкции подключения mcp-atlassian](https://docs.example.invalid/spaces/BLOKL/pages/1005956195/Инструкция+по+подключению+MCP-сервера+mcp-atlassian+в+AI-ассистент+Cline). Мастер маскированно запрашивает `HERMES_CUSTOM_LLM_API_KEY`, `HERMES_CUSTOM_ISSUE_TRACKER_TOKEN` и `HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN`. Значения сохраняются только в защищённом owner-only profile `.env`; не передавайте их в командной строке или URL. Jira использует `https://llm.example.invalid/jira/mcp`, `x-litellm-api-key` и `x-mcp-jira-authorization`; Confluence — `https://llm.example.invalid/confluence/mcp`, `x-litellm-api-key` и `x-mcp-confluence-authorization`. После установки во вкладке MCP должны быть включены `v8std`, `customllm-jira`, `customllm-confluence`. При устранимой ошибке используйте `retry --kit-home`: он продолжает незавершённую операцию без повторного `apply`.

После успеха проверьте выбранный профиль Hermes:

- встроенные skills Hermes присутствуют вместе с ровно одним внешним набором;
- `config.yaml` использует доказанную exact runtime schema 34 или 37;
- настроен MCP `v8std`;
- секрет находится в owner-only `.env`, а не в `config.yaml`.

`HERMES_CONFIG_SCHEMA_UNSUPPORTED` означает, что schema exact runtime не относится к поддержанным 34/37. Collision внешнего skill возвращает `HERMES_TOOLCHAIN_NAME_COLLISION`; непроверенный bundled catalog — `HERMES_BUNDLED_SKILLS_CATALOG_UNVERIFIED`. Эти ошибки не следует обходить ручным копированием каталогов.

GitLab kept job является единственным источником шести публикуемых файлов; exact-SHA GitHub CI обязан сверить их byte-for-byte. В `Assets → Other` допускаются только `Hermes-Setup.exe` и `certs.zip`.

Ограничения: выпуск внутренний и неподписанный; macOS не подписана/notarized; ALT Linux подтверждена только в pinned p11 userspace; Windows-установка Hermes выполняется вручную через GUI; trusted corporate-network probe требует корпоративного runner/VPN; офисные документы не поддерживаются.

## Подготовленный контракт v0.1.5: OfficeCLI для Hermes

`v0.1.5` ещё не опубликован: до появления GitLab Release и exact runtime evidence продолжайте использовать опубликованный `v0.1.3`. Подготовленный выпуск будет GitLab-only и package-first; GitHub остаётся местом read-only validation/evidence и не получает Team Kit tag, Release или assets. Полная пользовательская версия этой инструкции находится в [`CONFLUENCE-INSTALL-v0.1.5.md`](CONFLUENCE-INSTALL-v0.1.5.md).

OfficeCLI добавляется только в профиль Hermes. Закреплены exact `v1.0.144`, commit `1ced45e900782c5083ed550ddf328ee974e425e7` и SHA-256 из [`OFFICECLI-QUALIFICATION.md`](OFFICECLI-QUALIFICATION.md). Управляемый executable — `${HERMES_HOME}/.teamkit/officecli/v1.0.144/officecli` либо `officecli.exe` в Windows; PATH не изменяется, произвольный installer/updater не запускается, старые pinned versions не удаляются.

До записи MCP Team Kit выполняет `officecli config autoUpdate false`, затем `officecli config autoUpdate` и независимо читает user-global `${UserProfile}/.officecli/config.json`; точное `false` обязательно. `OFFICECLI_SKIP_UPDATE` не используется как MCP control. После подтверждения политики fail-closed cleanup допускает только `.update`, `.update.partial`, `.old` внутри owned managed parent.

Upstream может выполнять best-effort refresh только ранее установленных OfficeCLI skills во всех обнаруженных agent homes и перезаписать local edits внутри существующего skill. `officecli-pptx`, `officecli-docx`, `officecli-xlsx` и другие skills — файловые instruction/reference packs, а не дополнительные MCP-серверы. Team Kit не устанавливает on-disk skills, не полагается на default Hermes skill directory и вызывает встроенную команду `load_skill` единственного MCP-инструмента `officecli`.

Инструмент `officecli` может читать и изменять документы Office. После настройки профиль содержит четыре MCP: v8std, Jira, Confluence и OfficeCLI. При устранимой ошибке `retry` повторно использует существующий `configure_application`, не создаёт второй профиль и не начинает новый installer flow.

Release запрещён, пока exact-SHA dispatch не завершит зелёными четыре native lanes и отдельный ALT p11 smoke. Только запись `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME` и CI run URL могут служить runtime release evidence; сейчас такой PASS не заявлен.

## Архив выпуска v0.1.0

<!-- Compatibility anchor for immutable v0.1.0 evidence tests: ## Актуальный выпуск v0.1.0 -->

`teamkit v0.1.0 (unsigned internal release)` — финальный, не предварительный внутренний выпуск. Он доступен только авторизованным сотрудникам в приватном GitLab. Откройте [GitLab Release v0.1.0](https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.0), раскройте `Assets` и скачайте один бинарник для своей системы:

- Windows x64 — `teamkit-v0.1.0-windows-amd64.exe`;
- Linux или ALT Linux x64 — `teamkit-v0.1.0-linux-amd64`;
- macOS Intel — `teamkit-v0.1.0-darwin-amd64`;
- macOS Apple Silicon — `teamkit-v0.1.0-darwin-arm64`.

Скачайте с той же страницы `SHA256SUMS` и проверьте SHA-256 выбранного бинарника до запуска. Значения от `v0.1.0-rc.2` для финальных файлов не подходят. Точный коммит, номера заданий, размеры, контрольные суммы и прямые ссылки приведены в документе [Подтверждение публикации v0.1.0](releases/v0.1.0.md), который хранится в дереве исходников как `docs/releases/v0.1.0.md`.

Перед установкой учтите границы подтверждённого:

- trusted corporate network probe не подтверждён: GitHub-hosted runner не разрешает внутренний DNS `gitlab.tools.enterprise.ru`; нужен eligible self-hosted runner внутри корпоративной сети/VPN;
- ALT Linux проверена только в pinned p11 userspace; работа не подтверждена на нативной ALT Linux или в QEMU/VM;
- бинарники Team Kit не подписаны; файлы macOS не подписаны Apple и не notarized;
- Windows-установка Hermes использует ручной графический мастер. Правильные SHA-256 и Authenticode-подпись подтверждают файл и издателя, но не доказывают автоматическую или unattended-установку, её завершение либо выбранный каталог;
- офисные документы не поддерживаются.

Если правила вашей организации запрещают запуск неподписанных внутренних файлов, остановитесь и обратитесь к администратору. Не отключайте системную защиту целиком.

### Hermes уже установлен

`Hermes-Setup.exe` не нужен. Запустите выбранный бинарник `v0.1.0` с командой `apply` и выберите Hermes. Hermes определяется автоматически: мастер не спрашивает, установлено ли приложение, не просит `HERMES_HOME` или версию. Team Kit показывает найденные каталог и версию и повторно проверяет их перед изменениями.

Поддерживаются проверенные версии `>= 0.20.1 и < 0.21.0`, а не только точная `0.20.1`. Для более новых `0.20.x` дополнительно выполняются безопасные read-only проверки совместимости.

### Hermes нужно установить

В Windows установка Hermes остаётся ручной: скачайте установщик с [официального сайта Hermes](https://hermes-agent.nousresearch.com/), проверьте подпись `Nous Research Inc.` и завершите графическую установку. Если официальный сайт недоступен, резервный `Hermes-Setup.exe` находится в `Assets` → `Other` GitLab Release. Его контрольная сумма относится только к этому файлу. После установки снова запустите Team Kit — каталог и версия будут обнаружены автоматически.

В macOS, Linux и ALT Linux Team Kit выполняет управляемую автоматическую установку Hermes из закреплённого исходного кода в безопасный автоматически выбранный каталог. Для загрузки нужен доступ к разрешённым внешним источникам; при сетевой ошибке исправьте доступ и повторите `apply`, не подбирая случайный путь.

При `HERMES_HOME_AUTO_DETECT_FAILED` не подбирайте случайный каталог: убедитесь, что установка завершена и путь безопасен. `HERMES_VERSION_UNSUPPORTED` означает версию вне диапазона `>= 0.20.1 и < 0.21.0`. Явные `--hermes-home` и `--app-installed` используйте только для автоматизации, администрирования или исключительной ситуации; они не отключают проверки безопасности и совместимости.

## Архивная установка v0.1.0 в Windows: пошагово

<!-- Compatibility anchor for immutable v0.1.0 guide tests: ## Установка v0.1.0 в Windows: пошагово -->

Это основной маршрут для обычного пользователя Windows. Не переходите в исторический архив RC2: сначала полностью выполните подходящую ветку этой инструкции.

### Шаг 1. Скачайте финальный Team Kit

1. Создайте папку `C:\TeamKitInstaller`.
2. Скачайте [финальный файл Windows x64](https://gitlab.example.invalid/1c/aisuz/ai/-/jobs/13355334/artifacts/raw/dist/teamkit-v0.1.0-windows-amd64.exe).
3. Сохраните его с исходным именем `C:\TeamKitInstaller\teamkit-v0.1.0-windows-amd64.exe`.

**Важно:** НЕ запускайте и не переименовывайте `teamkit-v0.1.0-rc.2-windows-amd64.exe`. Это старая предварительная версия. Переименование RC2 не добавляет в неё новое меню и автоматическое обнаружение Hermes.

Если планируете использовать Hermes, скачайте `certs.zip` из `Assets` того же [Release v0.1.0](https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.0) и положите рядом с EXE. Если Hermes уже установлен, `Hermes-Setup.exe` не скачивайте. Резервный установщик понадобится только в ветке «Hermes ещё не установлен», когда официальный сайт недоступен.

### Шаг 2. Проверьте финальный файл в PowerShell

Откройте PowerShell. Приглашение должно начинаться с `PS`; обычная командная строка `cmd.exe` не подходит. Перейдите в папку:

```powershell
Set-Location -LiteralPath 'C:\TeamKitInstaller'
```

Скопируйте и выполните всю следующую строку целиком:

```powershell
$file = 'C:\TeamKitInstaller\teamkit-v0.1.0-windows-amd64.exe'; if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Файл не найден: $file" }; $expected = 'b42cd0b46fbfef75e6191973e407be76fede635d7b6a09a2c28364a5462eb331'; $actual = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant(); if ($actual -ne $expected) { throw "Контрольная сумма не совпадает: ожидалась $expected, получена $actual" }; Write-Host "SHA-256 совпадает: $actual"
```

После сообщения `SHA-256 совпадает` проверьте версию:

```powershell
& 'C:\TeamKitInstaller\teamkit-v0.1.0-windows-amd64.exe' --version
```

В полученной строке JSON найдите два точных фрагмента:

- `"version":"v0.1.0"`;
- `"commit":"8f06652c1d3ff97701e0e19b52f22967a7321d9e"`.

Если хеш не совпал или в версии есть `-rc.2`, не продолжайте. Скачайте финальный файл заново по ссылке из шага 1.

### Шаг 3. Подготовьте AI-приложение

Выберите только одну ветку.

#### Вариант A — использовать установленный Hermes

Hermes уже установлен на компьютере.

- Не скачивайте `Hermes-Setup.exe`.
- Положите `certs.zip` рядом с Team Kit.
- Team Kit проверит безопасные стандартные каталоги установки и первый `hermes` в `PATH`.
- Мастер не задаёт вопрос `AI-приложение уже установлено?` и не просит вводить `HERMES_HOME`: найденные путь и версия показываются автоматически.

Переходите к шагу 4.

#### Вариант B — сначала установить Hermes

Hermes ещё не установлен.

1. Скачайте Hermes с [официального сайта Hermes](https://hermes-agent.nousresearch.com/).
2. Если официальный сайт недоступен, откройте `Assets` → `Other` [Release v0.1.0](https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.0) и скачайте `Hermes-Setup.exe`. Для резервного файла ожидается SHA-256 `505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5` и действительная Authenticode-подпись `Nous Research Inc.`.
3. Пройдите графический мастер до сообщения о завершении установки.
4. Закройте PowerShell и откройте новое окно, чтобы обновился `PATH`.
5. Положите `certs.zip` рядом с Team Kit и снова запустите `apply` по шагу 4.

Не угадывайте `HERMES_HOME`: после установки финальный Team Kit должен обнаружить Hermes самостоятельно. Если установка не завершена, закончите её и снова запустите `apply`.

#### Вариант C — использовать другое AI-приложение

`Hermes-Setup.exe` и `certs.zip` не нужны. Установите выбранное приложение заранее. После установки откройте новое окно PowerShell, чтобы команда приложения появилась в `PATH`. Только для приложения не Hermes мастер задаёт вопрос `AI-приложение уже установлено?`.

### Шаг 4. Запустите `apply`

```powershell
& 'C:\TeamKitInstaller\teamkit-v0.1.0-windows-amd64.exe' apply
```

Первым должно быть меню:

1. `Добавить новое окружение`;
2. `Обновить существующее окружение`.

Если вместо этого первым появляется `Выберите операционную систему`, экран показывает RC2. Это симптом запуска `teamkit-v0.1.0-rc.2-windows-amd64.exe`: нажмите `Ctrl+C`, вернитесь к шагу 1 и запустите финальный EXE без `-rc.2` в имени.

### Шаг 5. Завершите выбранное действие

Для новой установки выберите `1 — Добавить новое окружение`. Укажите новую пустую папку, например `C:\TeamKit\apa`, затем выберите систему, приложение, проект, роль и один набор skills. При выборе Hermes мастер сам найдёт установленный runtime: вопроса об установленном приложении и запроса `HERMES_HOME` не будет.

Для ранее созданного окружения выберите `2 — Обновить существующее окружение`. Одно известное окружение выбирается автоматически, а несколько выводятся нумерованным списком. После сводки выберите:

1. `Ничего` — безопасная отмена;
2. `Только файлы окружения`;
3. `Только файлы базы данных`;
4. `Файлы окружения и базы данных`.

Если установленный Hermes не найден, сначала проверьте завершение установки и перезапустите PowerShell. Явный `--hermes-home` используйте только для автоматизации, администрирования или исключительной ситуации; обычный мастер не должен просить этот путь.

## Исторический архив RC2

Ниже без изменений сохранены команды, имена файлов и контрольные суммы опубликованного `v0.1.0-rc.2`. Они нужны только для воспроизведения RC2 и не подходят для установки `v0.1.0`.

`v0.1.0-rc.2` собран из коммита `2a5e3cb517d7b60d666ccde8e95dab57e1012ffb`.
Это внутренний неподписанный RC, а не публичный production-релиз. Открывайте
[страницу GitLab Release](https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.0-rc.2): это рекомендуемая точка загрузки для авторизованных пользователей приватного GitLab.

## Выберите и проверьте один файл

Не нужно загружать или проверять все четыре бинарника. Выберите только свой:

| Система | Файл | SHA-256 |
|---|---|---|
| Windows x64 | `teamkit-v0.1.0-rc.2-windows-amd64.exe` | `0d3d8baa48fe1ecc42518793a8451081fa9fa454223f45bd945a4908d6b22711` |
| Linux/ALT Linux x64 | `teamkit-v0.1.0-rc.2-linux-amd64` | `ba634fc2c760b4c2e144dc7cd457e9d19c06b87bb8705aa21e5947264f945ea3` |
| macOS Intel | `teamkit-v0.1.0-rc.2-darwin-amd64` | `5f9b17dafc0e0d8f1a248a54ee9cd2f12b57433c72a37039ffd948ffb377d305` |
| macOS Apple Silicon | `teamkit-v0.1.0-rc.2-darwin-arm64` | `972b3cb259440834bd10c65b5987061b97d9cb9db975aae36cf43e66f0bc3814` |

Также в Release есть `SHA256SUMS` (SHA-256 самого файла: `12c133779f7b0382760d23468aa29860461431256b4561cc44f4904fe33cda31`) и `SECURITY-AUDIT.json` (`66299c1e98b24b357b16518a3660e873bd6cc829a70e1578a9e7db7b65881948`). При использовании Hermes загрузите `certs.zip`: `88d85e7e7d64c061c195f93c517500bdc91fccfb9b5a8115da9f6a5a17e689f8`. Это локальный fallback сертификатов из assets Release, его не нужно запрашивать у администратора.

## Историческая инструкция: опубликованный v0.1.0-rc.2 в Windows

После загрузки Team Kit процесс состоит из двух частей: сначала выполните общую часть, затем выберите один сценарий — A «Hermes уже установлен», B «Hermes ещё не установлен» или C «Другое AI-приложение».

Сначала один раз выполните общие шаги. Создайте `C:\TeamKitInstaller` и сохраните там EXE Team Kit.

Откройте именно PowerShell. Перед выполнением команд убедитесь, что приглашение начинается с `PS`, например `PS C:\TeamKitInstaller>`. Приглашение `C:\TeamKitInstaller>` без `PS` означает, что открыт `cmd.exe`: там `$file`, `Get-FileHash` и `if (...) { ... }` не работают. `cmd.exe` не подходит для этой инструкции. Если уже открыт `cmd.exe`, введите `powershell` и дождитесь приглашения с `PS` либо откройте новое окно PowerShell.

Windows Terminal может показать предупреждение о многострочной вставке. Это ожидаемо: проверьте содержимое и подтверждайте вставку только в том случае, если оно совпадает с инструкцией.

Перейдите в папку установки и проверьте, что приглашение стало `PS C:\TeamKitInstaller>`:

```powershell
Set-Location -LiteralPath 'C:\TeamKitInstaller'
```

Проверьте выбранный EXE. Однострочная команда сначала проверяет наличие файла по абсолютному пути, затем SHA-256 и явно сообщает результат:

```powershell
$file = 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe'; if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Файл не найден: $file" }; $expected = '0d3d8baa48fe1ecc42518793a8451081fa9fa454223f45bd945a4908d6b22711'; $actual = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant(); if ($actual -ne $expected) { throw "Контрольная сумма не совпадает: ожидалась $expected, получена $actual" }; Write-Host "SHA-256 совпадает: $actual"
```

Если Windows предупреждает о неизвестном издателе, сначала обязательно проверьте SHA-256. Не обходите запрет политики организации самостоятельно.

После успешной проверки запросите версию по абсолютному пути:

```powershell
& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' --version
```

Сначала завершите все общие шаги выше; затем выберите ровно один сценарий:

- [A. Hermes уже установлен](#a-hermes-уже-установлен)
- [B. Hermes ещё не установлен](#b-hermes-ещё-не-установлен)
- [C. Другое AI-приложение](#c-другое-ai-приложение)

Перейдите только к выбранному сценарию; возвращаться и повторять общую часть не нужно.

### A. Hermes уже установлен

`Hermes-Setup.exe` не нужен, скачивать его не требуется. Для Hermes нужен `certs.zip` из assets того же Release; сохраните его в `C:\TeamKitInstaller`.

Проверьте существующий runtime:

```powershell
hermes --version
```

Если команда не найдена, используйте абсолютный путь:

```powershell
& '<HERMES_HOME>\hermes-agent\venv\Scripts\hermes.exe' --version
```

Подставьте фактический `HERMES_HOME`. Первая строка должна быть `Hermes Agent v0.20.1 (2026.8.13)`; `0.20.0` не подходит. Затем запустите Team Kit:

```powershell
& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply
```

Выберите Hermes, ответьте `1. Да` — Hermes уже установлен — и укажите тот же `HERMES_HOME`.

### B. Hermes ещё не установлен

До начала установки заранее скачайте установщик с [официального сайта](https://hermes-agent.nousresearch.com/) — это рекомендуемый источник. Если он недоступен, откройте GitLab Release `v0.1.0-rc.2` → `Assets` → `Other`, скачайте резервный `Hermes-Setup.exe` и сохраните его как `C:\TeamKitInstaller\Hermes-Setup.exe`. Для Hermes также нужен `certs.zip` из assets того же Release; сохраните его в `C:\TeamKitInstaller` рядом с Team Kit.

Кнопка Copy над каждым блоком копирует только этот блок. Выполняйте блоки по порядку; блок подписи сам проверяет наличие файла, но не повторяет проверку SHA-256.

Сначала проверьте SHA-256 одной строкой:

```powershell
$hermesInstaller = 'C:\TeamKitInstaller\Hermes-Setup.exe'; if (-not (Test-Path -LiteralPath $hermesInstaller -PathType Leaf)) { throw "Файл не найден: $hermesInstaller" }; $expected = '505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5'; $actual = (Get-FileHash -LiteralPath $hermesInstaller -Algorithm SHA256).Hash.ToLowerInvariant(); if ($actual -ne $expected) { throw "Контрольная сумма Hermes-Setup.exe не совпадает: ожидалась $expected, получена $actual" }; Write-Host "SHA-256 Hermes-Setup.exe совпадает: $actual"
```

Затем проверьте Authenticode-подпись одной строкой:

```powershell
$hermesInstaller = 'C:\TeamKitInstaller\Hermes-Setup.exe'; if (-not (Test-Path -LiteralPath $hermesInstaller -PathType Leaf)) { throw "Файл не найден: $hermesInstaller" }; $signature = Get-AuthenticodeSignature -LiteralPath $hermesInstaller; $signature | Select-Object Status, @{Name='Publisher'; Expression={ $_.SignerCertificate.Subject }}; if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'Nous Research Inc\.') { throw 'Подпись Hermes-Setup.exe недействительна или издатель не Nous Research Inc.' }; Write-Host 'Подпись Hermes-Setup.exe подтверждена'
```

Нормальный результат: Status: `Valid`, Publisher содержит `Nous Research Inc.`. При другом статусе, издателе или хеше остановитесь и не запускайте установщик.

Запустите `C:\TeamKitInstaller\Hermes-Setup.exe` двойным щелчком и пройдите окна графического мастера до конца. Team Kit не устанавливает Hermes автоматически; дождитесь завершения ручной установки.

В новом PowerShell выполните `hermes --version`. Если команда не найдена, используйте абсолютный путь:

```powershell
& '<HERMES_HOME>\hermes-agent\venv\Scripts\hermes.exe' --version
```

Вместо `<HERMES_HOME>` подставьте фактический путь к выбранной рабочей папке Hermes; не предполагайте путь по умолчанию. Первая строка должна быть `Hermes Agent v0.20.1 (2026.8.13)`. Версия `0.20.0` не подходит. После успешной проверки запустите Team Kit:

```powershell
& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply
```

Выберите Hermes и ответьте `1. Да` — Hermes уже установлен. Укажите тот же `HERMES_HOME`, который проверили выше. При ошибке `HERMES_EXECUTABLE_UNVERIFIED` остановитесь: Team Kit не нашёл подтверждённый исполняемый файл либо получил неподходящую версию.

Экспериментальный ручной процесс проверяет хеш и издателя установщика, но не доказывает завершение установки через графический интерфейс или правильность выбранного `HERMES_HOME`. Подтвердите результат отдельной командой версии, как описано выше.

### C. Другое AI-приложение

`Hermes-Setup.exe` и `certs.zip` не нужны. Альтернативное AI-приложение должно быть установлено заранее, а его команда должна быть доступна через `PATH`. Затем запустите Team Kit:

```powershell
& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply
```

В мастере выберите своё приложение.

## macOS

В Terminal выберите `darwin-arm64` для Apple Silicon либо `darwin-amd64` для Intel и подставьте соответствующий хеш из таблицы:

```bash
artifact="teamkit-v0.1.0-rc.2-darwin-arm64"
expected="972b3cb259440834bd10c65b5987061b97d9cb9db975aae36cf43e66f0bc3814"
actual="$(shasum -a 256 "$artifact" | awk '{print $1}')"
test "$actual" = "$expected" || { echo "Контрольная сумма не совпадает"; exit 1; }
chmod 700 "$artifact"
./"$artifact" --version
./"$artifact" apply
```

Нативные CI-проверки macOS 15 для Intel и Apple Silicon успешны. Бинарники не подписаны Apple и не notarized; при Gatekeeper используйте разрешённый организацией путь запуска или обратитесь к администратору, не отключая защиту всей системы.

## Linux и ALT Linux

В терминале проверьте выбранный Linux-файл и запустите его:

```bash
artifact="teamkit-v0.1.0-rc.2-linux-amd64"
expected="ba634fc2c760b4c2e144dc7cd457e9d19c06b87bb8705aa21e5947264f945ea3"
printf '%s  %s\n' "$expected" "$artifact" | sha256sum --check --strict
chmod 700 "$artifact"
./"$artifact" --version
./"$artifact" apply
```

Нативная CI-проверка Ubuntu 24.04 успешна. В ALT Linux используется тот же бинарник, но подтверждён только pinned p11 userspace-контейнер: нативный runner и QEMU/VM не подтверждены. Первый запуск ALT выполняйте как контролируемую проверку вместе с администратором.

## Мастер v0.1.0: пошаговый выбор add/update

Следующие шаги относятся к финальному `v0.1.0`. Исторический RC2 ещё не показывает первым вопросом выбор между добавлением и обновлением, поэтому не используйте бинарник `v0.1.0-rc.2` для этого сценария.

Новое меню `Добавить/Обновить` появляется только при интерактивном `teamkit apply`, когда пользователь отвечает на вопросы мастера. Существующие команды `apply --non-interactive`, `plan`, `status`, `retry` и `update` сохраняют обратную совместимость: их параметры и машиночитаемые форматы не меняются.

Обнаружение окружений через локальный реестр используется только интерактивным мастером обновления. `apply --non-interactive` и `plan` не используют обнаружение через локальный реестр. Команды `plan` и `status` не записывают локальный реестр.

### 1. Запустите `apply`

В Windows используйте PowerShell и абсолютный путь к проверенному EXE. В macOS/Linux/ALT Linux используйте Terminal и команду `./<проверенный-файл> apply`. Первым появится меню:

1. `Добавить новое окружение`;
2. `Обновить существующее окружение`.

Введите номер ответа. До выбора операция не обращается к сети и ничего не записывает.

### 2. Новое окружение: `1 — Добавить новое окружение`

Выберите этот пункт для нового проекта или нового пути. Ответьте на вопросы об операционной системе и AI-приложении, укажите отсутствующую либо пустую папку `KIT_ALL_TEAM_HOME`, затем выберите проект, роль и ровно один набор skills. Для существующего Team Kit root будет показан `WORKSPACE_EXISTS_USE_UPDATE`; повторите запуск и выберите обновление.

### 3. Существующее окружение: `2 — Обновить существующее окружение`

В update-режиме ответы из окружения загружаются автоматически: вопросы об OS, приложении, проекте, роли и skills повторно не задаются.

- Один найденный путь выбирается автоматически.
- Несколько путей показываются нумерованным списком в MRU-порядке; рядом указан проект.
- Пункт `Указать другой путь` позволяет ввести полный абсолютный путь вручную.
- Если подходящих путей нет, ручной путь запрашивается сразу.

Источники имеют строгий порядок: явный `--kit-home`, затем локальный реестр, затем `KIT_ALL_TEAM_HOME` из среды. После ошибки явного пути fallback нет. Диски, сетевые каталоги и история оболочки не сканируются. Повреждённый или недоступный реестр даёт одно предупреждение и не перезаписывается в этом запуске; продолжайте через переменную среды или ручной путь.

### 4. Сводка и выбор обновления

Сначала проверьте сводку выбранного окружения. Затем введите номер:

1. `Ничего` — безопасная отмена без последующих чтений, записи, сети и запроса учётных данных;
2. `Только файлы окружения` (`content`);
3. `Только файлы базы данных` (`database`);
4. `Файлы окружения и базы данных` (`both`).

### 5. Незавершённая операция: `RETRY_REQUIRED`

Обновление не началось. Не удаляйте `.teamkit/`. Скопируйте и выполните **всю** готовую команду `retry`, которую напечатал Team Kit: в PowerShell для Windows или в POSIX-оболочке для macOS/Linux/ALT Linux. Не переносите из неё только отдельные параметры.

### 6. Подписи наборов skills

Доступны ровно два одиночных варианта: `1 — cc_1c_skills от Широкова` и `2 — ai_rules_1c от Филиппова`. Оба одновременно не устанавливаются, значения по умолчанию нет. В `.env`, CLI и служебных контрактах сохраняются прежние внутренние идентификаторы `cc_1c_skills` и `ai_rules_1c`.

### 7. Hermes и другие AI-приложения

Hermes в `v0.1.0` обнаруживается автоматически. При версии `>= 0.20.1 и < 0.21.0` Team Kit создаёт или использует профиль роли и подключает только выбранный toolchain. Если Hermes не найден, в Windows установка остаётся ручной, а в macOS, Linux и ALT Linux Team Kit выполняет управляемую установку из закреплённого исходного кода.

Вопрос `AI-приложение уже установлено` задаётся только для приложения не Hermes. Для Hermes мастер не запрашивает `HERMES_HOME`: безопасный каталог и версия определяются автоматически. Явные `--app-installed` и `--hermes-home` предназначены только для автоматизации, администрирования или исключительной ситуации.

Для любого поддерживаемого приложения не Hermes прямая установка не выполняется. При подтверждённой установке Team Kit записывает одну не содержащую секретов инструкцию `.teamkit/handoff.txt` для выбранного pinned-набора и MCP v8std. Выполните эту инструкцию в своём AI-приложении. При ответе, что приложение не установлено, возвращается `AI_APP_REQUIRED` до вопроса о skills и до создания workspace-файлов.

## Hermes, сеть и секреты

Для Hermes Team Kit автоматически настраивает CustomLLM с моделью `generic-development`. До запуска получите токен CustomLLM и выполните подготовку по корпоративной инструкции [«Начало работы»](https://docs.example.invalid/spaces/IDP/pages/1017637995/%D0%9F%D0%BE%D0%B4%D0%BA%D0%BB%D1%8E%D1%87%D0%B5%D0%BD%D0%B8%D0%B5+%D0%BA+LLM+%D1%87%D0%B5%D1%80%D0%B5%D0%B7+API+IDE+SDK#id-%D0%9F%D0%BE%D0%B4%D0%BA%D0%BB%D1%8E%D1%87%D0%B5%D0%BD%D0%B8%D0%B5%D0%BALLM%D1%87%D0%B5%D1%80%D0%B5%D0%B7API(IDE,SDK)-%D0%9F%D0%B5%D1%80%D0%B5%D0%B4%D0%BF%D0%BE%D0%B4%D0%BA%D0%BB%D1%8E%D1%87%D0%B5%D0%BD%D0%B8%D0%B5%D0%BALLM%D1%83%D0%B1%D0%B5%D0%B4%D0%B8%D1%82%D0%B5%D1%81%D1%8C,%D1%87%D1%82%D0%BE%D1%83%D0%B2%D0%B0%D1%81%D0%B2%D1%8B%D0%BF%D0%BE%D0%BB%D0%BD%D0%B5%D0%BD%D1%8B%D0%B4%D0%B5%D0%B9%D1%81%D1%82%D0%B2%D0%B8%D1%8F,%D0%BE%D0%BF%D0%B8%D1%81%D0%B0%D0%BD%D1%8B%D0%B5%D0%B2%D0%B8%D0%BD%D1%81%D1%82%D1%80%D1%83%D0%BA%D1%86%D0%B8%D0%B8). Положите `certs.zip` рядом с бинарником Team Kit; архив используется локально и не устанавливает сертификаты в системное хранилище.

GitLab и CustomLLM могут быть недоступны вне VPN или корпоративной сети. Неподтверждённый trusted live network probe на GitHub-hosted runner вызван тем, что он не разрешает внутренний DNS `gitlab.tools.enterprise.ru`; это не ошибка GitHub-аутентификации. Для подтверждения нужен self-hosted runner внутри корпоративной сети/VPN.

Не передавайте токены в аргументах, URL, сообщениях или скриншотах. При неинтерактивном запуске они хранятся в локальном `.env` выбранного приложения; Git использует временный `GIT_ASKPASS`, не помещающий учётные данные в URL или аргументы. Один `KIT_ALL_TEAM_HOME` соответствует одному проекту и окружению; храните его отдельно от папки с загруженными бинарниками и `certs.zip`.

## Если установка остановилась на Action 50 и небезопасных правах `.env`

Сообщение `ACTION_FAILED 50-configure-application: SECRET_FILE_PERMISSIONS_UNSAFE` означает, что Team Kit остановил настройку, потому что приватный файл наследует доступ от родительской папки. Для Hermes это может быть общий `HERMES_HOME\.env` либо файл профиля `HERMES_HOME\profiles\<identity>\.env`. Например, для проекта `apa`, роли `developer` и skills `cc_1c_skills` путь будет `G:\.hermes\profiles\1c-apa-developer-cc_1c_skills\.env`.

На действии `50-configure-application` секреты профиля проверяются раньше остальной конфигурации. Поэтому `config.yaml` ещё не создан: настройка останавливается до рендеринга профиля и MCP. Состояние `needs_apply` с оставшимися действиями `50-configure-application` и `90-verify-state` в этой ситуации ожидаемо.

Откройте PowerShell от имени того же пользователя, который запускает Hermes. В первой строке используйте точный путь, указанный в ошибке:

```powershell
$secretFile = 'G:\.hermes\profiles\1c-apa-developer-cc_1c_skills\.env'
if (-not (Test-Path -LiteralPath $secretFile -PathType Leaf)) { throw "Файл не найден: $secretFile" }
$currentSid = [System.Security.Principal.WindowsIdentity]::GetCurrent().User

$acl = [System.Security.AccessControl.FileSecurity]::new()
$acl.SetOwner($currentSid)
$acl.SetAccessRuleProtection($true, $false)
$rule = [System.Security.AccessControl.FileSystemAccessRule]::new(
    $currentSid,
    [System.Security.AccessControl.FileSystemRights]::FullControl,
    [System.Security.AccessControl.AccessControlType]::Allow
)
$acl.AddAccessRule($rule)
Set-Acl -LiteralPath $secretFile -AclObject $acl
```

Проверьте, что наследование выключено и осталась одна ненаследуемая запись `FullControl` текущего пользователя:

```powershell
$acl = Get-Acl -LiteralPath 'G:\.hermes\profiles\1c-apa-developer-cc_1c_skills\.env'
$acl.AreAccessRulesProtected
$acl.Access | Format-Table IdentityReference, FileSystemRights, AccessControlType, IsInherited
```

После исправления не запускайте `apply` повторно, не удаляйте `.env`, `.teamkit` или профиль и не создавайте MCP или `config.yaml` вручную. Продолжите уже сохранённую операцию. Если ваш `KIT_ALL_TEAM_HOME` отличается от примера, замените только последний путь:

```powershell
& 'C:\TeamKitInstaller\teamkit-v0.1.0-windows-amd64.exe' retry --kit-home 'C:\TeamKit\apa'
```

После успешного `retry` выполните `status --kit-home` и убедитесь, что получен результат `ready`. Затем перезапустите Hermes и выберите профиль `1c-apa-developer-cc_1c_skills` либо профиль с именем, показанным Team Kit для ваших проекта, роли и skills.

## План, проверка и повтор

До изменения файлов можно получить план, а после установки проверить состояние или безопасно продолжить незавершённую операцию:

```text
teamkit plan --non-interactive --os windows --app hermes --app-installed=true --kit-home C:\TeamKit\aisuz --hermes-home C:\Hermes --project aisuz --role developer --toolchain cc_1c_skills --certs C:\TeamKitInstaller\certs.zip
teamkit status --kit-home <абсолютный путь к KIT_ALL_TEAM_HOME>
teamkit retry --kit-home <абсолютный путь к KIT_ALL_TEAM_HOME>
teamkit update --kit-home <абсолютный путь к KIT_ALL_TEAM_HOME> --target content|database|both|none
```

При сбое не удаляйте `.teamkit/`: сохранённая квитанция позволяет продолжить незавершённые действия. Рабочая копия базы данных доступна только для чтения; Team Kit может выполнить `fetch` и `fast-forward`, но не делает `reset`, `stash`, `commit` или `push`.
