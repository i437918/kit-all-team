# Проект ограниченного по времени GitLab Release-оркестратора

## Цель

Создать неинтерактивный PowerShell-скрипт `scripts/publish-v0.1.3.ps1`, который
публикует полный внутренний GitLab Release `v0.1.3` из заранее подготовленного
точного commit SHA. Скрипт обязан завершиться не позднее 180 минут после старта,
никогда не запрашивать подтверждение пользователя и не раскрывать секреты.

Готовый скрипт сам по себе не является завершением работы. Проект считается
завершённым только после подготовки TeamKit `v0.1.3` с обязательными Jira и
Confluence MCP, успешного выполнения скрипта и фактической публикации Release.
Подготовка candidate выполняется по утверждённой спецификации
`docs/superpowers/specs/2026-08-18-v0.1.3-atlassian-mcp-port-design.md`; обе
спецификации образуют один обязательный end-to-end результат.

Ограничение времени является гарантией завершения процесса, а не гарантией
успешной публикации. Недоступный runner, недостаточные права, конфликтующий ref
или исчерпанный deadline завершают скрипт раньше с неизменяемым кодом ошибки и
машиночитаемой причиной.

## Обязательный конечный результат

Успех разработки означает одновременное выполнение всех условий:

- в GitLab существует Release
  `https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3`;
- protected annotated tag `v0.1.3` ссылается на точный проверенный commit;
- тот же commit опубликован в GitHub `main` и GitLab `master`;
- Jira и Confluence MCP всегда включены в Hermes и используют точные URL,
  header names и `${ENV}` placeholders из Atlassian MCP-спецификации;
- `HERMES_CUSTOM_ISSUE_TRACKER_TOKEN`, `HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN` и
  `HERMES_CUSTOM_LLM_API_KEY` получаются и хранятся только штатным
  защищённым механизмом TeamKit;
- GitHub и GitLab CI успешно проверили один SHA;
- шесть создаваемых publication inputs совпадают между CI побайтно;
- Release предоставляет восемь пользовательских файлов:
  `teamkit-v0.1.3-windows-amd64.exe`,
  `teamkit-v0.1.3-linux-amd64`,
  `teamkit-v0.1.3-darwin-amd64`,
  `teamkit-v0.1.3-darwin-arm64`, `SHA256SUMS`,
  `SECURITY-AUDIT.json`, `Hermes-Setup.exe` и `certs.zip`;
- post-publication download подтверждает имена, размеры и SHA-256;
- итоговый JSON содержит `status: "published"`, release URL, tag-object SHA,
  peeled commit SHA, GitHub run ID, GitLab pipeline/job IDs, восемь SHA-256 и
  фактическую длительность без значений секретов.

Статусы `failed` и `deadline_exceeded` являются корректным поведением скрипта,
но не считаются выполненным релизом. После них работа продолжается
неинтерактивным повторным запуском с тем же SHA до `status: "published"` либо до
подтверждённого внешнего блокера, который невозможно устранить кодом.

## Входной контракт

Обязательные параметры:

- `CandidateSha` — ровно 40 шестнадцатеричных символов;
- `Version` — по умолчанию и для этого выпуска только `v0.1.3`;
- `MaxMinutes` — по умолчанию 180, допустимый диапазон 1–180;
- GitHub repository — по умолчанию `mi1man-cmd/kit-all-team`;
- GitLab base URL — `https://gitlab.example.invalid`;
- GitLab project ID — `12087`;
- GitHub release branch — `main`;
- GitLab release branch — `master`.

Секреты поступают только через `GH_TOKEN` и `GITLAB_TOKEN`. Git-аутентификация
использует уже настроенный credential helper; `GIT_TERMINAL_PROMPT=0` запрещает
интерактивный fallback. Токены нельзя помещать в аргументы процессов, URL,
созданные файлы или диагностический вывод.

## Предварительные условия

До первого внешнего изменения скрипт одним preflight-блоком доказывает:

- PowerShell 7+, Git, Go и HTTPS API доступны;
- рабочее дерево чистое, `HEAD` равен `CandidateSha`;
- встроенная версия candidate равна `v0.1.3`, а build metadata содержит точный
  SHA;
- локальные release/security gates переданы скрипту как существующее exact-SHA
  подтверждение либо повторно выполняются в оставшемся бюджете;
- GitHub `main` и GitLab `master` могут быть продвинуты к candidate обычным
  fast-forward без force и без потери уникальных коммитов;
- версия, tag и Release отсутствуют, либо уже существуют в точности для того же
  SHA и могут быть идемпотентно проверены;
- principal имеет права читать Actions, запускать workflow, сохранять GitLab
  artifacts, создавать protected tag и GitLab Release;
- статические project uploads доступны и имеют закреплённые размеры и SHA-256:
  `Hermes-Setup.exe` — 7 597 376 байт,
  `505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5`;
  `certs.zip` — 136 410 байт,
  `88d85e7e7d64c061c195f93c517500bdc91fccfb9b5a8115da9f6a5a17e689f8`.

Любая ошибка preflight завершает процесс до push, tag и Release.

Перед запуском publication-скрипта end-to-end workflow обязан завершить
Atlassian candidate: исправить `enabled:true`, подключить обязательные токены и
operation contract, обновить активную версию и документацию до `v0.1.3`, пройти
полные Go/release/security tests, собрать четыре бинарника и выполнить Hermes
live smoke без вывода секретов. Эти действия не перекладываются на пользователя
и не требуют от него промежуточных подтверждений.

## Последовательность публикации

1. Обычными non-force push продвинуть точный SHA в GitHub `main` и GitLab
   `master`; после каждого push перечитать remote ref. Если второй ref успел
   измениться, остановиться без отката первого.
2. Запустить либо найти свежие GitHub CI и GitLab pipeline для точного SHA.
   Ожидать их параллельно. GitHub должен подтвердить candidate build, Windows,
   Linux, macOS Intel, macOS ARM, ALT userspace и все security audits. GitLab
   должен подтвердить job `verify` и шесть publication inputs.
3. Скачать GitHub и GitLab наборы и потребовать побайтовое совпадение четырёх
   бинарников, `SHA256SUMS` и `SECURITY-AUDIT.json`.
4. Проверить встроенные version/commit, четыре строки checksum manifest и
   `passed=true`, zero findings для security audit.
5. Вызвать GitLab Keep endpoint, доказать `artifacts_expire_at=null` и повторно
   скачать шесть файлов из kept job.
6. Запустить существующий GitHub `final-release-validation` на `main`, передав
   exact CI run, artifact digest и шесть GitLab hashes; дождаться успеха.
7. Повторить отсутствие конфликтующего tag/Release, точность обоих branch refs,
   kept job и статических payloads.
8. Потребовать минимум 20 минут остаточного бюджета. Только после этого создать
   exact protected-tag rule, локальный annotated tag `v0.1.3`, проверить tag
   object и peeled SHA, затем выполнить один non-force push тега в GitLab.
9. Создать GitLab Release `1C Team Kit v0.1.3`. Notes содержат exact SHA,
   GitHub run, GitLab pipeline/job, шесть job-bound browser links и шесть
   SHA-256. `assets.links` содержит только закреплённые project uploads
   `Hermes-Setup.exe` и `certs.zip`.
10. Перечитать protected annotated tag и Release, скачать все восемь файлов,
    проверить имена, размеры и хеши, затем вывести итоговый JSON без секретов.

## Deadline и polling

Один `System.Diagnostics.Stopwatch` является источником истины. Каждая операция
получает `remaining = deadline - elapsed`; ни одна функция не создаёт собственный
неограниченный timeout. HTTP timeout не превышает меньшего из 30 секунд и
остатка. Poll interval ограничен 10 секундами и также учитывает остаток.

Внешние процессы запускаются без shell-интерполяции, ожидаются с bounded timeout
и уничтожаются вместе с дочерним деревом при превышении. Exit code `124`
означает общий deadline. Отдельные стабильные exit codes различают preflight,
CI, byte mismatch, security, tag и Release failures.

Стадийные бюджеты являются верхними ориентирами внутри общего deadline:

- preflight и синхронизация refs — 20 минут;
- параллельные GitHub/GitLab CI — 90 минут;
- artifacts, compare и final validation — 40 минут;
- tag, Release и post-verification — 20 минут;
- 10 минут остаются общим резервом.

Неиспользованное время одной стадии доступно следующим стадиям. Скрипт никогда
не продлевает общий deadline.

## Идемпотентность и восстановление

До tag push повторный запуск безопасно переиспользует только exact-SHA успешные
CI/job/artifacts. После tag push действует forward-only recovery:

- совпадающий annotated tag принимается;
- отсутствующий Release создаётся после повторной проверки tag, refs, kept job и
  payloads;
- совпадающий Release повторно проверяется;
- конфликтующий tag или Release никогда не удаляется, не перемещается и не
  перезаписывается.

Если deadline наступил после tag push, скрипт завершается кодом `124` и пишет
только несекретный recovery state. Следующий запуск продолжает вперёд с той же
версией и SHA.

## Реализация и тестирование

Основной файл — `scripts/publish-v0.1.3.ps1`. Внешние Git/API/process операции
проходят через узкие функции, чтобы тестовый adapter мог моделировать успех,
timeout, конфликт refs, несовпадение байтов и сбой после tag.

Автоматические тесты должны доказать:

- отсутствие `Read-Host`, интерактивных credential prompts и token-bearing argv;
- отказ до mutation при любой preflight-ошибке;
- параллельное ожидание двух CI;
- общий deadline и exit `124` при зависшем API/process;
- запрет tag при остатке менее 20 минут;
- порядок keep → byte compare → final validation → protected tag → Release;
- отсутствие удаления/force/move операций;
- идемпотентный exact-SHA повтор и forward recovery;
- редактирование/конфликт чужого tag или Release приводит к отказу;
- логи и итоговый JSON не содержат тестовые значения токенов.

Реальный публикационный smoke выполняется только для подготовленного release
SHA. Dry-run проверяет весь preflight и план вызовов, но никогда не считается
доказательством опубликованного Release.

End-to-end acceptance дополнительно требует реального запуска для `v0.1.3` и
проверки итогового `status: "published"`. Отчёт о dry-run, локальный candidate,
зелёный PR либо созданный tag без Release не удовлетворяют acceptance criteria.

## Вне области работ

- Force push, reset, удаление или перемещение существующих refs/releases.
- Автоматическая выдача Maintainer permissions или создание токенов.
- Обход branch protection, protected-tag policy либо security gates.
- Гарантия успешной публикации при недоступной внешней инфраструктуре.

Внутренняя реализация Jira/Confluence MCP описана отдельной связанной
спецификацией, но её завершение и подготовка candidate SHA являются обязательной
частью общего результата, а не исключением из области работ.
