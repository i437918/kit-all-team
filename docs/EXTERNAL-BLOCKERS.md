# Внешние ограничения и release gates

## v0.1.5: OFFICECLI_RUNTIME_QUALIFICATION_EXTERNAL_EVIDENCE

Исходная запись нейтральна к результату. Итог runtime определяется только на основании external exact-SHA evidence, привязанного к SHA кандидата; успешный инженерный результат сам по себе не публикует `v0.1.5` и не закрывает отдельные corporate Windows/release gates. Exact-SHA GitHub dispatch должен успешно завершить четыре native lanes и отдельный ALT p11 smoke, а matching GitLab push pipeline — пройти для того же SHA; evidence bundle с `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME`, exact candidate SHA и URL прикладывается к MR/release record. Task 10 отдельно требует corporate Windows policy/equivalence evidence или формальный waiver.

OfficeCLI exact `v1.0.144` допускается только в профиле Hermes. Инструмент может читать и изменять документы Office; это принятая широкая mutation boundary, а не отсутствие поддержки. ALT использует qualified Linux amd64 asset в pinned p11 userspace и не подтверждает нативную ALT или QEMU/VM.

Package-first publisher публикует `v0.1.5` только в GitLab. GitHub не получает Team Kit tag, Release или assets. Existing package, tag или Release блокирует операцию без overwrite/delete; уже опубликованные releases, tags и assets остаются неизменяемыми.

## Исторические ограничения v0.1.0

`teamkit v0.1.0 (unsigned internal release)` — финальный внутренний non-prerelease в приватном GitLab. Перечисленные ниже ограничения приняты для этого выпуска и не являются доказательством отсутствующей проверки. Они запрещают представлять `v0.1.0` как публичный, подписанный или полностью подтверждённый на всех корпоративных платформах продукт.

Точный release SHA, номера заданий, контрольные суммы и ссылки будут добавлены после публикации в `docs/releases/v0.1.0.md`. Этот файл намеренно не использует подтверждения RC2 как подтверждения финального бинарника.

## TRUSTED_NETWORK_PROBE_UNVERIFIED

Trusted corporate network probe не подтверждён. GitHub-hosted runner не разрешает внутренний DNS `gitlab.tools.enterprise.ru`, поэтому он не может выполнить живую проверку внутренних GitLab, CustomLLM и v8std. Для такого подтверждения нужен eligible self-hosted runner внутри корпоративной сети/VPN. Это ограничение не является ошибкой GitHub-аутентификации и не отменяет автономные тесты с локальными фикстурами.

## ALT_NATIVE_RUNNER_UNAVAILABLE

ALT Linux подтверждена только в pinned p11 userspace-контейнере. Нативная ALT Linux не проверена: подходящего доверенного self-hosted runner нет. Контейнерная проверка не должна называться нативной.

## ALT_QEMU_VM_NOT_VERIFIED

Запуск финального бинарника в ALT Linux QEMU/VM не подтверждён и не является gate этого выпуска. Не используйте старое или информационное QEMU evidence как подтверждение `v0.1.0`.

## UNSIGNED_BINARIES

Бинарники Team Kit не подписаны. Файлы macOS не подписаны Apple и не notarized, поэтому Gatekeeper может остановить запуск. Пользователь не должен отключать системную защиту целиком; если политика организации запрещает внутренние неподписанные файлы, необходимо обратиться к администратору.

## HERMES_WINDOWS_INSTALL_DIR_UNVERIFIED

Первая установка Hermes в Windows выполняется пользователем через ручной графический мастер. Проверка SHA-256 резервного `Hermes-Setup.exe` и действительной Authenticode-подписи `Nous Research Inc.` подтверждает только целостность конкретного файла и издателя. Экспериментальный процесс `hermes-windows-installer-contract-experimental` проверяет статический контракт, но не подтверждает автоматическую установку. Эти проверки не доказывают unattended-установку, завершение GUI либо выбранный каталог `HERMES_HOME`. Team Kit не устанавливает Hermes автоматически в Windows.

## OFFICE_DOCUMENTS_UNSUPPORTED

OfficeCLI и работа с Word, Excel, PowerPoint и другими офисными документами не входят в `v0.1.0`.

## Закрытые прежние локальные блокеры

Локально подтверждён совместимый Hermes runtime `0.20.1`; поддерживаемый контракт `v0.1.0` — `>= 0.20.1 и < 0.21.0`. Системный Go `1.26.6` также подтверждён. Эти локальные статусы не устраняют перечисленные выше внешние ограничения.
