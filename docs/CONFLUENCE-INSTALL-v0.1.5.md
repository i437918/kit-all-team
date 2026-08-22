# Подготовленная установка 1C Team Kit v0.1.5

> Статус: `v0.1.5` ещё не опубликован. Эта страница фиксирует проверяемый контракт, но не является ссылкой на живой Release. Исходная запись нейтральна к результату. Итог runtime определяется только на основании external exact-SHA evidence, привязанного к SHA кандидата; успешный инженерный результат сам по себе не публикует `v0.1.5` и не закрывает отдельные corporate Windows/release gates. До появления GitLab Release используйте опубликованный `v0.1.3`.

## Граница выпуска

`v0.1.5` — первый полностью проверяемый выпуск с OfficeCLI. Он публикуется только в приватном GitLab и использует package-first flow: до первого upload все страницы и статусы exact `teamkit/v0.1.5` должны быть пусты; каждый из шести файлов получает один PUT без retry и только exact HTTP `201 Created` принимается как успех. После каждого PUT проверяется exact distinct uploaded prefix, после шестого и непосредственно перед tag — exact-six inventory; authenticated API повторно скачивает каждый файл и сравнивает SHA-256. GitLab Release links ведут только на package URLs. GitHub выполняет validation/evidence и не получает Team Kit tag, Release или assets.

Уже опубликованные releases, tags и assets остаются неизменяемыми. Любой existing/concurrent package record или file, ambiguous response, изменившийся production ref, tag или Release `v0.1.5` останавливает publisher без tag/Release/delete. Частичный unlinked package является failed external state, блокирует resume и требует ручного расследования. Legacy metadata не является источником bytes или hash evidence.

Exact набор файлов:

- `teamkit-v0.1.5-windows-amd64.exe`;
- `teamkit-v0.1.5-linux-amd64`;
- `teamkit-v0.1.5-darwin-amd64`;
- `teamkit-v0.1.5-darwin-arm64`;
- `SHA256SUMS`;
- `SECURITY-AUDIT.json`.

После публикации скачивайте файлы только по package URLs из GitLab Release `v0.1.5`, а выбранный бинарник проверяйте по `SHA256SUMS`. Не переименовывайте файл другого выпуска.

## Что OfficeCLI добавляет в Hermes

OfficeCLI добавляется только в профиль Hermes. После настройки профиль содержит четыре MCP: v8std, Jira, Confluence и OfficeCLI. Для другого AI-приложения Team Kit не устанавливает OfficeCLI и не добавляет его в handoff.

Инструмент `officecli` может читать и изменять документы Office — Word, Excel, PowerPoint и другие поддерживаемые форматы. Это широкая mutation boundary, а не read-only просмотр. Передавайте только разрешённые рабочие файлы и сохраняйте резервную копию.

## Поддерживаемые платформы и pins

Принята exact версия `v1.0.144` с upstream commit `1ced45e900782c5083ed550ddf328ee974e425e7`. Она выбрана потому, что исходники, release identity, размеры и SHA-256 четырёх assets сопоставлены с immutable evidence. `latest` и будущие версии автоматически не принимаются.

| Platform | Qualified asset |
| --- | --- |
| Windows x64 | `officecli-win-x64.exe` |
| Linux x64 | `officecli-linux-x64` |
| macOS Intel | `officecli-mac-x64` |
| macOS Apple Silicon | `officecli-mac-arm64` |
| ALT Linux p11 x64 | `officecli-linux-x64` |

| Asset | SHA-256 |
| --- | --- |
| `officecli-win-x64.exe` | `e780cc6a5385f84b4d54d71b0c179904ed534125ec33fe39b1a8711fa80e387e` |
| `officecli-linux-x64` | `32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8` |
| `officecli-mac-x64` | `366100643d757b0da24829422897ca74768a894b5ecd1a471a1336f8e2a0787d` |
| `officecli-mac-arm64` | `04757163428c5bde8d91e8f838517818e74722157722ca5f3877b6716b77bd45` |

ALT использует уже qualified Linux amd64 asset и имеет отдельный p11 smoke. Это не подтверждает нативную ALT или QEMU/VM.

## Управляемая установка и обновления

Executable хранится в `${HERMES_HOME}/.teamkit/officecli/v1.0.144/officecli`; в Windows имя файла — `officecli.exe`. Team Kit не изменяет PATH, не пишет в системные каталоги, не устанавливает и не обновляет OfficeCLI произвольным installer/updater и не удаляет старые pinned versions.

До добавления MCP порядок фиксирован:

1. выполнить `officecli config autoUpdate false`;
2. выполнить `officecli config autoUpdate` и потребовать exact `false`;
3. независимо прочитать user-global `${UserProfile}/.officecli/config.json` и снова потребовать `autoUpdate=false`;
4. проверить exact version и SHA-256;
5. только затем записать абсолютный command и аргумент `mcp` в профиль Hermes.

`OFFICECLI_SKIP_UPDATE` не используется как MCP control: upstream обрабатывает команду `mcp` раньше этого guard. До подтверждённого `autoUpdate=false` остатки updater блокируют операцию fail-closed. После read-back cleanup узко ограничен `.update`, `.update.partial`, `.old` внутри доказанного owned managed parent.

При устранимой ошибке `retry` повторно использует существующий `configure_application`. Он не начинает новый installer flow, не создаёт второй профиль и не меняет PATH.

## OfficeCLI skills

Upstream может выполнить best-effort refresh только ранее установленных OfficeCLI skills во всех обнаруженных agent homes. Refresh не создаёт новый agent home или skill identity, но может перезаписать локальные изменения внутри уже существующего skill.

`officecli-pptx`, `officecli-docx`, `officecli-xlsx` и другие skills — файловые instruction/reference packs, а не дополнительные MCP-серверы. Team Kit не устанавливает on-disk skills, не полагается на default Hermes skill directory и использует встроенную команду `load_skill` единственного MCP-инструмента `officecli`.

## Проверка после установки

После опубликованного и квалифицированного `v0.1.5`:

1. выполните `status --kit-home <путь>` и получите `ready`;
2. откройте профиль Hermes, созданный Team Kit;
3. проверьте четыре MCP: `v8std`, `customllm-jira`, `customllm-confluence`, `officecli`;
4. убедитесь, что OfficeCLI command абсолютный и находится под managed path;
5. не создавайте MCP, config или skills вручную.

Release запрещён без external exact-SHA evidence для четырёх native lanes (`windows-2025`, `ubuntu-24.04`, `macos-15-intel`, `macos-15`), отдельного ALT p11 smoke и matching GitLab push pipeline. Runtime evidence bundle содержит `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME`, exact candidate SHA и CI run URL и прикладывается к MR/release record без post-CI source-only commit.
