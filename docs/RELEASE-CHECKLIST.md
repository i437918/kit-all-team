# Чек-лист выпусков

## Подготовка v0.1.5: package-first GitLab-only

- [x] Active version contracts, build defaults и четыре platform filenames переведены на exact `v0.1.5` отдельным small commit в том же MR; второй MR и вторая CI-матрица не требуются.
- [x] `scripts/publish-v0.1.5.ps1` остаётся тонким version entry point над общим bounded publisher; исторический `scripts/publish-v0.1.3.ps1` не изменён.
- [x] Publisher принимает exact GitHub run/artifact IDs и GitLab pipeline/verify job IDs, повторно привязывает их к CandidateSha и не dispatch-ит дублирующую CI.
- [x] Direct push в `master` отсутствует: branch refs только read-back проверяются после доставки release candidate через MR.
- [x] Package identity exact: name `teamkit`, version `v0.1.5`, четыре platform binaries, `SHA256SUMS`, `SECURITY-AUDIT.json`.
- [x] До первого upload authenticated API перечисляет все страницы и все статусы exact package `teamkit/v0.1.5` и требует zero records. Каждый из шести файлов получает ровно один PUT без retry; принимается только exact HTTP `201 Created`.
- [x] После каждого PUT publisher повторно перечисляет package/files и требует ровно один package record с exact distinct загруженным prefix, без duplicate/extra и с exact `file_sha256`, когда GitLab его возвращает. После шестого PUT и непосредственно перед tag повторяется exact-six inventory; каждый файл authenticated повторно скачивается и сравнивается по SHA-256.
- [x] Existing или concurrent package state, non-201/ambiguous upload, изменившийся branch ref, tag или Release останавливает publisher без tag/Release/delete. Частичный unlinked package не считается успехом, блокирует resume и требует ручного расследования; legacy metadata не используется как byte/hash evidence.
- [x] GitHub workflows имеют read-only contents permissions и не создают Team Kit tag, Release или assets.
- [x] Уже опубликованные releases, tags и assets, включая исторический `v0.1.3`, остаются неизменяемыми.
- [ ] Exact-SHA dispatch должен завершить зелёными четыре native lanes: Windows, Linux, macOS Intel и macOS Apple Silicon.
- [ ] Отдельный ALT p11 smoke должен повторно использовать qualified Linux amd64 asset и завершиться успешно.
- [ ] Приложить к MR/release record external evidence bundle с `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME`, exact candidate SHA, GitHub run URL и matching GitLab pipeline URL; отдельный post-CI source-only commit не создавать. Исходная запись нейтральна к результату. Итог runtime определяется только на основании external exact-SHA evidence, привязанного к SHA кандидата; успешный инженерный результат сам по себе не публикует `v0.1.5` и не закрывает отдельные corporate Windows/release gates.
- [ ] Task 10 corporate Windows policy/equivalence evidence или формальный waiver должны быть приняты отдельно.

## Финальный v0.1.0: исторический контракт продвижения

- [x] Классификация зафиксирована как `teamkit v0.1.0 (unsigned internal release)`: финальный non-prerelease для приватного GitLab, не публичный production-релиз.
- [x] Активные build/CI defaults и имена четырёх бинарников переведены на `v0.1.0`.
- [x] GitHub workflow выполняет только read-only проверку exact candidate; tag и Release публикуются только в GitLab.
- [x] GitLab CI пропускает tag pipelines, чтобы создание `v0.1.0` не пересобирало выбранные артефакты.
- [x] Ограничение trusted corporate network probe принято: hosted runner не разрешает внутренний DNS; нужен eligible self-hosted runner в корпоративной сети/VPN.
- [x] Ограничение ALT принято: подтверждён только pinned p11 userspace, нативная ALT Linux и QEMU/VM не подтверждены.
- [x] Зафиксировано отсутствие подписи бинарников Team Kit; macOS не подписана Apple и не notarized.
- [x] Windows Hermes описан как ручная GUI-установка; SHA-256 и Authenticode не выдаются за доказательство unattended-установки.
- [x] OfficeCLI и офисные документы исключены из продукта.
- [x] Exact release SHA одинаково опубликован в GitHub `main` и GitLab `master`.
- [x] GitHub exact-SHA validation и GitLab pipeline/job завершены успешно для одного SHA; шесть файлов сравнены побайтно.
- [x] GitLab job сохранён без срока удаления артефактов; защищённый тег `v0.1.0` и GitLab Release указывают на exact SHA.
- [x] Итоговые SHA, CI run, GitLab pipeline/job и ссылки записаны без подмены RC2 в [`docs/releases/v0.1.0.md`](releases/v0.1.0.md).

## Исторический чек-лист внутреннего RC2

- [x] Внутренний неподписанный prerelease `v0.1.0-rc.2` опубликован в [GitLab Release](https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.0-rc.2) из коммита `2a5e3cb517d7b60d666ccde8e95dab57e1012ffb`.
- [x] GitLab pipeline/job `2172635` / `13349176` опубликованы для RC2.
- [x] В Release опубликованы четыре бинарника, `SHA256SUMS`, `SECURITY-AUDIT.json`, `certs.zip` и `Hermes-Setup.exe`.
- [x] Нативные CI-проверки Windows 2025, Ubuntu 24.04, macOS 15 Intel и macOS 15 Apple Silicon успешны; см. [GitHub CI](https://github.com/mi1man-cmd/kit-all-team/actions/runs/31910283357).
- [x] ALT p11 userspace evidence получено для закреплённого контейнера.
- [ ] ALT native runner и QEMU/VM-подтверждение всё ещё отсутствуют.
- [ ] Trusted live network probe из корпоративной сети/VPN ещё не получен: GitHub-hosted runner не разрешает внутренний DNS GitLab.
- [x] Hermes Windows описан как ручная GUI-установка; автоматическая установка не заявляется.
- [x] Системный Go `1.26.6` и Hermes runtime `0.20.1` подтверждены; прежние локальные блокеры закрыты.
- [x] OfficeCLI и работа с офисными документами исключены.

Незакрытые trusted-network и native ALT/QEMU пункты исторического RC2 переносятся в `v0.1.0` как явно принятые ограничения, а не как выполненные проверки. Не заявляйте production-grade полный платформенный паритет.

`HERMES_WINDOWS_INSTALLER_STATIC_CONTRACT_PASSED` означает только проверку статического контракта и не подтверждает unattended-установку или каталог установки: `HERMES_WINDOWS_INSTALLER_STATIC_CONTRACT_PASSED` is informational only and must not be cited as unattended-installation or install-root evidence.
