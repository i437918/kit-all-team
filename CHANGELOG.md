# Changelog

## Unreleased

- Подготовлен `v0.1.5` — первый полностью проверяемый выпуск с OfficeCLI, package-first и только в GitLab; GitHub остаётся validation/evidence authority без Team Kit tag или Release.
- OfficeCLI exact `v1.0.144` добавляется только в профиль Hermes по managed path, с закреплёнными SHA-256, persisted user-global `autoUpdate=false`, обязательным read-back и без PATH/installer/updater changes.
- Принят best-effort refresh только ранее установленных OfficeCLI skills; Team Kit не устанавливает on-disk skills, а встроенный `load_skill` работает через единственный MCP tool `officecli`, который может читать и изменять документы Office.
- Активные build/CI/version contracts `v0.1.5` входят отдельным small commit в тот же MR, чтобы не запускать второй MR и вторую CI-матрицу.
- Release запрещён без external exact-SHA evidence для четырёх native lanes и ALT p11 smoke; evidence bundle должен содержать `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME`, exact candidate SHA и CI run URL.
- Исходная запись нейтральна к результату. Итог runtime определяется только на основании external exact-SHA evidence, привязанного к SHA кандидата; успешный инженерный результат сам по себе не публикует `v0.1.5` и не закрывает отдельные corporate Windows/release gates.
- Граница неизменяемости: уже опубликованные releases, tags и assets остаются неизменяемыми; исторические `v0.1.3` и legacy metadata не используются как bytes/hash evidence нового выпуска.

## v0.1.3 — 2026-08-18

- Опубликованы четыре платформенных бинарника и проверочные артефакты в GitLab Release.
- Для профиля Hermes добавлены обязательные always-on MCP Jira и Confluence с персональными `HERMES_CUSTOM_ISSUE_TRACKER_TOKEN` и `HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN`, защищённым хранением и продолжением через `retry`.
- Пользовательские инструкции дополнены назначением обоих MCP, точными URL и заголовками авторизации.

## v0.1.2 — 2026-08-17

- Исправлен разбор вывода `hermes doctor`: служебное обновление progress-строки больше не приводит к `HERMES_PROFILE_DOCTOR_FAILED`.
- Для ролей аналитика, программиста и архитектора добавлены отдельные шаблоны `SOUL.md`.
- Выбранный ролевой шаблон устанавливается до Doctor, включая продолжение уже остановленной операции через `retry`.

## v0.1.1 — 2026-08-17

- Новые профили Hermes сохраняют встроенные skills и получают ровно один выбранный внешний набор 1С.
- Доказанные профили Team Kit от `v0.1.0` мигрируют через `skills opt-in --sync`; пользовательский opt-out не изменяется.
- Исправлена нормализация DACL созданного Hermes staging `.env`, поэтому `retry` продолжает Action 50/90 без ручного ослабления прав.
- `config.yaml` рендерится по schema 34/37, доказанной exact runtime, и проверяет MCP `v8std`.
- Активные build/CI-контракты и шесть имён публикации переключены на `v0.1.1`; GitLab остаётся единственным источником публикации, GitHub выполняет exact-SHA byte comparison.

## v0.1.0 — 2026-08-17

- Активные build/CI-контракты переключены на финальный `v0.1.0`: GitHub выполняет только read-only проверку exact candidate, а публикация остаётся только в GitLab.
- Добавить новое окружение и Обновить существующее окружение теперь являются первым явным выбором интерактивного мастера; update поддерживает проверенный выбор пути, безопасное `Ничего` и готовую команду `RETRY_REQUIRED`.
- локальный MRU хранит только абсолютные пути и не содержит секретов или сведений проекта.
- Два взаимоисключающих варианта отображаются как `cc_1c_skills от Широкова` и `ai_rules_1c от Филиппова`; внутренние ID сохранены, а приложения не Hermes получают secret-free handoff.
- Финальный выпуск остаётся внутренним и неподписанным; trusted corporate network probe не подтверждён из-за недоступного внутреннего DNS на hosted runner.
- ALT Linux подтверждена только как pinned p11 userspace, без нативной ALT Linux и QEMU/VM; сборки macOS не подписаны Apple и не notarized.
- Windows-установка Hermes остаётся ручной через GUI; OfficeCLI и офисные документы не поддерживаются.

## v0.1.0-rc.2 — 2026-08-16

- Add the Russian numbered setup wizard for predictable non-technical use.
- Publish corporate certificates and the signed Hermes fallback installer as release assets.
- Expand Russian installation guidance, prerequisites, and recovery instructions.

## v0.1.0-rc.1 — 2026-08-15

- Introduce the cross-platform `teamkit` Go CLI and deterministic desired-state reconciler.
- Add a closed 11-project catalog, three roles, two pinned toolchains, and four OS families.
- Add read-only database acquisition, safe workspace publication, Hermes/provider setup, and non-Hermes handoff.
- Add reproducible four-target builds, native GitHub test lanes, ALT p11 container checks, and QEMU evidence.
- Add a manually confirmed immutable private-prerelease workflow that promotes exact tested binaries, network/platform evidence, checksums, and release attestations without rebuilding.
- Exclude OfficeCLI, office documents, GUI, signing, notarization, and public release.
