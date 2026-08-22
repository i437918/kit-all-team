# Дизайн интерактивных режимов add/update и реестра окружений

## Цель и границы

Интерактивный `teamkit apply` получает явный выбор намерения перед остальными вопросами мастера. Выбор `1` создаёт новое окружение через существующий questionnaire, выбор `2` находит и обновляет уже созданное окружение по проверенному публичному состоянию. Реестр является только локальным MRU-списком абсолютных путей и не становится источником данных о проекте или секретах.

Изменение касается только интерактивного `apply` и его внутренних вызовов. Прямые `status`, `retry`, `update`, неинтерактивные `apply` и `plan` сохраняют текущие флаги, коды, JSON-формат и порядок действий. Полный поиск по дискам, сетевым ресурсам, `PATH` или другим неявным местам не выполняется.

Термины в этой спецификации:

- `KIT_ALL_TEAM_HOME` — абсолютный корень окружения;
- Team Kit окружение — root с безопасным `.teamkit/owner` и согласованным публичным `.env`;
- кандидат — путь, предложенный источником и прошедший bounded-инспекцию без записи;
- displayable-кандидат — кандидат с проверенной идентичностью, который можно показать в списке; он может иметь маркер незавершённой операции;
- готовый кандидат — displayable-кандидат без незавершённого receipt, разрешённый для обычного update;
- повреждённый реестр — отсутствующий допустимый JSON-контракт (отсутствие файла само по себе не считается повреждением).

## Первое меню apply

До вопросов об окружении интерактивный `apply` печатает ровно такой выбор:

```text
Что вы хотите сделать:
  1. Добавить новое окружение
  2. Обновить существующее окружение
Введите номер ответа:
```

Неверный или пустой номер получает повторный prompt того же меню и не вызывает команды инициализации. EOF/закрытие stdin возвращает `INPUT_REQUIRED`; отмена через context/сигнал возвращает `INTERRUPTED`. После выбора `1` применяется add-поток, после выбора `2` — update-поток. Никакого третьего пункта и скрытого автоопределения намерения нет.

### Матрица интерактивных флагов и конфликтов

| Вызов | Поведение | Конфликты и границы |
|---|---|---|
| `teamkit apply` | Интерактивный apply; первым показывается mode menu | Никакой режим не выводится из наличия/отсутствия `KIT_ALL_TEAM_HOME` или registry |
| `teamkit apply --non-interactive` | Сохраняет прежний неинтерактивный apply и обязательные selectors | Registry discovery не используется; best-effort promotion выполняется только после успешного выполнения непустого плана действий |
| `teamkit apply [--update none\|content\|database\|both]` | Интерактивный apply всё равно первым показывает mode menu; при выборе update явно переданный scope, включая `none`, пропускает только вопрос scope | `--update` — scope, а не mode; при выборе add отсутствие флага или `--update none` разрешено, а `content|database|both` возвращает `UPDATE_CHOICE_NOT_APPLICABLE` |
| `teamkit apply --non-interactive [--update none\|content\|database\|both]` | Сохраняет неинтерактивный apply; `--update` задаёт scope без mode menu | Registry discovery не используется; best-effort promotion выполняется только если успешный apply действительно выполнил непустой план действий; готовый no-op с `none` реестр не меняет |
| `teamkit update --kit-home H [--target T]` | Сохраняет прямой неинтерактивный update | `--target` принимает `content`, `database`, `both`, `none`; если отсутствует, сохраняется текущий default `none`; registry discovery не используется |
| `teamkit apply` с обычными add selectors | Selectors остаются прежними и не заменяют mode menu | `--update` не является mode selector; explicit `--kit-home` в interactive update трактуется как единственный candidate только после выбора пункта `2` |

Таким образом, первый экран interactive `apply` всегда явен, а прямой `update` и неинтерактивный apply не получают скрытого auto-mode. Для неинтерактивного apply registry discovery не используется; best-effort promotion разрешён только после успешного выполнения непустого плана действий. `--update` никогда не выбирает режим и принимает только scope: explicit scope в update honored, а add с `none`/unset продолжает add-поток.

## Поток add

### Вопросы и обязательный путь

Add запускает текущую последовательность вопросов: OS, AI-приложение, признак установки приложения, `KIT_ALL_TEAM_HOME`, Hermes home (если требуется текущим questionnaire), проект, роль, toolchain и действующие проверки Hermes. Порядок уже поддерживаемых вопросов не меняется; отображаемые названия наборов skills уточняются по следующему контракту.

### Выбор набора skills

Если флаг `--toolchain` отсутствует, мастер показывает ровно один нумерованный вопрос. Двоеточие является частью точного заголовка, который печатает CLI:

```text
Выберите набор skills:
  1. cc_1c_skills от Широкова
  2. ai_rules_1c от Филиппова
Введите номер ответа:
```

Пользователь обязан выбрать ровно один вариант. Набора по умолчанию нет; неверный или пустой номер повторно показывает тот же вопрос. Одновременный выбор или установка обоих наборов не поддерживается.

Если `--toolchain` явно передан со стабильным значением `cc_1c_skills` или `ai_rules_1c`, мастер не показывает этот вопрос и использует заданный вариант. Любое другое явно переданное значение, включая пустую строку и человекочитаемую подпись, возвращает `TOOLCHAIN_UNKNOWN`; fallback к интерактивному вопросу или другому набору запрещён.

Человекочитаемые подписи не меняют стабильные внутренние идентификаторы:

| Подпись в мастере | Стабильный идентификатор | Закрепляемый репозиторий |
|---|---|---|
| `cc_1c_skills от Широкова` | `cc_1c_skills` | `Nikolay-Shirokov/cc-1c-skills` |
| `ai_rules_1c от Филиппова` | `ai_rules_1c` | `comol/ai_rules_1c` |

В `.env`, CLI-флагах, operation contracts, receipt и существующих окружениях продолжают использоваться только идентификаторы `cc_1c_skills` и `ai_rules_1c`. Их переименование или автоматическая миграция не выполняются, поэтому обратная совместимость сохраняется.

Для Hermes профиль выбранной роли создаётся либо используется уже существующий; в этот профиль устанавливается только выбранный набор. Для любого поддерживаемого AI-приложения не Hermes доступны те же два варианта и также выбирается ровно один без значения по умолчанию, но только после подтверждения пользователя, что приложение установлено. При ответе «Нет» команда немедленно возвращает `AI_APP_REQUIRED`: вопрос toolchain не задаётся, `.teamkit/handoff.txt` и любые другие workspace-файлы не создаются.

При подтверждении «Да» Team Kit не устанавливает набор напрямую в non-Hermes приложение: он создаёт не содержащий секретов файл `.teamkit/handoff.txt`. В handoff выбранному приложению передаётся инструкция настроить только выбранный закреплённый репозиторий и точный commit этого набора, а также MCP v8std. Невыбранный набор в handoff не добавляется.

Значение `KIT_ALL_TEAM_HOME` в ответе должно быть непустым абсолютным путём. Пустой ответ — `INPUT_REQUIRED`; «missing/empty» относится к целевому каталогу: путь может отсутствовать или существовать, но быть пустым. Такой каталог принимается как новая цель создания. До проверки пути add не вызывает credentials, сеть или запись.

### Классификация цели

После всех текущих вопросов путь проверяется без изменения файлов:

1. Отсутствующий root при безопасных родительских компонентах — допустимая новая цель; создать его можно только в обычном успешном apply.
2. Существующий пустой root — допустимая новая цель.
3. Существующий Team Kit root с валидными `owner` и публичным `.env` — `WORKSPACE_EXISTS_USE_UPDATE` с подсказкой выбрать «Обновить существующее окружение».
4. Непустой чужой, повреждённый, небезопасный, symlink/junction/reparse root или root с неясным состоянием — `FOREIGN_WORKSPACE` (при I/O-сбое — `WORKSPACE_INSPECTION_FAILED`).

Add не предлагает выбрать существующий путь и не запускает update автоматически. При успешном add после полного применения путь регистрируется как первый элемент MRU. При ошибке, no-op, `plan` или преждевременном завершении реестр не меняется.

## Поток update

После выбора `2` мастер не задаёт вопросы OS, AI-приложения, проекта, роли или toolchain. Он только собирает пути, инспектирует их, загружает проверенные `owner` и публичный `.env`, показывает summary и спрашивает область обновления.

### Источники и строгий приоритет

Если CLI передал непустой `--kit-home`, существует ровно один источник: этот путь. Он валидируется как явный override; при любой ошибке нет fallback к реестру или `KIT_ALL_TEAM_HOME`.

Если `--kit-home` не передан, кандидаты собираются в фиксированном порядке:

1. пути из реестра в порядке MRU;
2. одно значение `KIT_ALL_TEAM_HOME` из среды, добавленное после реестра.

В каждой группе сохраняется исходный порядок, затем выполняется дедупликация. На Windows сравнение путей регистронезависимое, на macOS, Linux и ALT Linux — точное сравнение канонической строки. Никаких других источников и дискового сканирования нет.

Каждый собранный путь обязан пройти одну и ту же полную read-only инспекцию. Реестр никогда не считается доказательством существования окружения.

### Проверка кандидата

Инспектор принимает `Candidate{Home, Source}` и возвращает `VerifiedEnvironment` либо классифицированную ошибку. Проверки выполняются в таком порядке:

1. путь абсолютный, очищен платформенным `filepath.Clean` и не содержит недопустимых компонентов;
2. каждый существующий компонент root и сам root проверены через `Lstat`; symlink, Windows junction и любой reparse point отклоняются;
3. root существует как каталог и доступен для чтения; для add отдельно допускается отсутствующий или пустой root;
4. `.teamkit` существует как обычный каталог без symlink/reparse;
5. bounded-инспекция operation envelope/receipt выполняется **до** чтения `owner` и `.env`. Envelope/receipt обязан быть обычным файлом без symlink/reparse и проходить строгий разбор ограниченного формата: неизвестные поля, неверные типы, превышение размера и недопустимые state/operation values отклоняются. Незавершённый envelope, включая частично созданный receipt первого запуска до появления `owner` или `.env`, классифицируется как `RETRY_REQUIRED`;
6. `.teamkit/owner` существует как обычный публичный metadata-файл без symlink/reparse, читается bounded-режимом и содержит ожидаемый Team Kit owner/project marker;
7. для обычного кандидата `.env` существует как обычный публичный файл без symlink/reparse, читается bounded-режимом и проходит строгий разбор ожидаемых ключей без вывода значений секретных ключей;
8. owner и `.env` согласованы и описывают одно Team Kit окружение, которое сервис может загрузить без мутаций. Workspace-файлы не требуют owner-only mode/DACL: это ограничение применяется только к registry и secret stores. Переход существующих DACL workspace-файлов на owner-only — non-goal.

Кандидат с отсутствующим owner, повреждённым `.env`, несовпадением owner/project, небезопасным путём или неясным состоянием получает внутреннюю причину `Foreign`/`InspectionFailed` и не попадает в список выбора. Для registry/env source это только bounded warning + skip; для explicit/manual source это fatal `FOREIGN_WORKSPACE` либо `WORKSPACE_INSPECTION_FAILED` согласно причине. Инспектор не чинит путь и не пишет в него.

Незавершённый receipt — отдельный displayable-кандидат с маркером `незавершённая операция`: update запрещён, receipt не изменяется, credentials/сеть/план/применение не вызываются. Если он единственный displayable-кандидат, он выбирается автоматически и команда возвращает `RETRY_REQUIRED`. Если displayable-кандидатов несколько, receipt-кандидат показывается в numbered list; выбор его пункта возвращает `RETRY_REQUIRED` и готовую retry-команду. Receipt-кандидаты не исключаются из списка и не маскируются ручным вводом.

Невалидные пути из registry или `KIT_ALL_TEAM_HOME` не являются fatal для discovery: Team Kit печатает одно предупреждение на путь, пропускает его и продолжает с остальными кандидатами; если displayable-кандидатов не осталось, используется ручной ввод. Невалидный explicit `--kit-home` и невалидный путь, введённый в ручном пункте, возвращают классифицированную ошибку (`FOREIGN_WORKSPACE` или `WORKSPACE_INSPECTION_FAILED`) без fallback. Пустой explicit/manual ввод возвращает `INPUT_REQUIRED`.

### Выбор пути

Ровно один displayable-кандидат выбирается автоматически без дополнительного вопроса. Если он имеет receipt marker, немедленно возвращается `RETRY_REQUIRED`; ready-кандидат продолжает обычный update. При двух и более displayable-кандидатах печатается:

```text
Выберите окружение:
  1. apa — C:\TeamKit\apa
  2. wms — C:\TeamKit\wms
  3. Указать другой путь
Введите номер ответа:
```

Подпись ready-кандидата `<PROJECT> — <KIT_ALL_TEAM_HOME>` строится только из уже проверенных `owner`/`.env`; receipt-кандидат отображается как `<KIT_ALL_TEAM_HOME> — незавершённая операция`. Реестр не хранит и не подставляет проект. Пункт `Указать другой путь` запускает тот же bounded-инспектор для введённого непустого абсолютного `KIT_ALL_TEAM_HOME`. Пустой или отсутствующий путь, пустой root и root без подтверждённого Team Kit состояния не являются существующим окружением и завершаются `INPUT_REQUIRED` или fatal `FOREIGN_WORKSPACE` согласно источнику. Ручной путь с незавершённым receipt возвращает `RETRY_REQUIRED`.

Неверный или пустой номер в списке окружений повторно печатает список; EOF возвращает `INPUT_REQUIRED`, отмена через context/сигнал — `INTERRUPTED`. После выбора `Указать другой путь` те же правила применяются к ручному path prompt.

Если displayable-путей нет (все registry/env-кандидаты пропущены с предупреждениями), мастер предлагает ручной ввод:

```text
Введите KIT_ALL_TEAM_HOME:
```

Введённый путь проходит абсолютно ту же инспекцию; доверие к ручному вводу не расширяет права.

### Summary без повторных вопросов

После выбора verified environment мастер печатает только значения, прочитанные и проверенные из публичного состояния:

```text
Найдено окружение:
  KIT_ALL_TEAM_HOME: C:\TeamKit\apa
  Проект: apa
  AI-приложение: Hermes
  Роль: developer
  Набор skills: cc_1c_skills
```

В summary не выводятся секретные значения, токены, пароли, ключи или содержимое receipt. Вопросов OS, app, project, role и toolchain после выбора нет.

Если interactive mode `2` выбран и `--update` уже передан, его scope (`none|content|database|both`) считается выбранным и это меню scope не печатается. При отсутствии `--update` затем печатается ровно меню:

```text
Что обновить в существующем окружении:
  1. Ничего
  2. Только файлы окружения
  3. Только файлы базы данных
  4. Файлы окружения и базы данных
Введите номер ответа:
```

Отображаемые варианты маппятся на существующие значения: `1 -> none`, `2 -> content`, `3 -> database`, `4 -> both`.

`1. Ничего` немедленно завершает `apply` с кодом `0`. Read-only discovery, validation и summary-read могли быть выполнены до показа этого меню; после выбора `1` не выполняются новые reads или эффекты: network, credential resolver, `Plan`, `Apply`, receipt/status write, любые файловые записи или изменения в `.env`/`.teamkit`, запись реестра и MRU promotion. В частности, выбранный путь не продвигается в MRU.

Варианты `2–4` передаются в существующий `service.Plan`/`service.Apply` pipeline с verified desired state. Финальный сервис повторно валидирует root перед эффектами; рассинхронизация summary и финальной проверки останавливает команду существующей ошибкой.

Неверный или пустой номер update-menu повторно печатает это меню; EOF возвращает `INPUT_REQUIRED`, отмена через context/сигнал — `INTERRUPTED`. Для no-op разрешены уже выполненные read-only discovery, validation и summary-read; после выбора `1` запрещены любые новые reads/effects, включая network, credential resolution, mutations, файловые записи или изменения и registry promotion.

## Локальный реестр

### Расположение и контракт

Реестр находится вне KIT workspace:

- Windows: `%LOCALAPPDATA%\TeamKit\environments.json`;
- macOS: `~/Library/Application Support/TeamKit/environments.json`;
- Linux и ALT Linux: `${XDG_CONFIG_HOME:-~/.config}/teamkit/environments.json`.

Файл — UTF-8 JSON размером не более `65536` байт с единственным контрактом версии:

```json
{
  "schema_version": 1,
  "homes": [
    "C:\\TeamKit\\apa",
    "C:\\TeamKit\\wms"
  ]
}
```

`schema_version` обязателен и равен `1`; `homes` обязателен и является массивом строк. Каждая строка — абсолютный канонический путь безопасного Team Kit root. Порядок — MRU: первый путь был последним успешно применённым/обновлённым. Максимум — `64` записи; дубликаты запрещены. Других полей нет: в частности, нет project, role, app, toolchain, credential, secret, receipt, статуса или истории ошибок.

Файл с неизвестным полем, неверным типом/версией, невалидным путём, дубликатом, превышением размера или числа записей считается повреждённым. Отсутствующий файл означает пустой реестр и не является ошибкой.

### Защищённое чтение и запись

Только каталог реестра, файл registry и его временный файл создаются с owner-only правами: POSIX каталог `0700`, файл `0600`; Windows — DACL с полным доступом только текущего пользователя. Workspace `.env`, `.teamkit/owner` и operation envelope/receipt остаются regular public metadata files: для них обязательны отсутствие symlink/junction/reparse, bounded read и строгая проверка содержимого, но не owner-only mode/DACL. Ни каталог, ни registry-файл, ни временный файл не могут быть symlink/junction/reparse. Миграция существующих workspace DACL/mode в owner-only не выполняется и относится к non-goals.

Запись — bounded atomic replace: временный файл создаётся в том же owner-only каталоге, записывается полностью, синхронизируется и заменяет целевой файл атомарным rename/replace. Нельзя следовать symlink/reparse к каталогу, временному файлу или цели. При невозможности доказать владельца, тип, границы или атомарную замену запись отклоняется fail-closed.

### MRU-политика и моменты записи

Best-effort promotion выполняется только после успешного выполнения непустого плана действий интерактивным или неинтерактивным `apply`, после интерактивного update с выбором `2–4` (`content|database|both`), после прямого `update --target content|database|both`, а также после успешно завершённого `retry` для регистрации или backfill пути. Путь добавляется в начало, существующая запись перемещается в начало, дубликаты удаляются, хвост обрезается до `64`.

Неуспешный apply/update/retry, no-op (`none`), пустой план apply/update, `status`, `plan`, отмена ввода и ошибка до полного успеха реестр не изменяют. Прямые `status`, `retry` и `update` не используют реестр для discovery; только успешный `retry` выполняет регистрацию/backfill, а прямой update промотирует путь лишь для `content|database|both`.

Если реестр повреждён при чтении, недоступен из-за I/O/permission ошибки или имеет неподдерживаемый формат, Team Kit один раз печатает предупреждение, игнорирует его до конца запуска и использует только `KIT_ALL_TEAM_HOME` из среды либо ручной ввод:

```text
Предупреждение: локальный реестр Team Kit повреждён, недоступен или имеет неподдерживаемый формат и будет проигнорирован.
```

Такой запуск не переписывает повреждённый или недоступный файл даже после успешного apply/update/retry. Временный новый реестр не создаётся: «manual fallback» означает работу без registry write. Это правило предотвращает потерю неизвестных пользовательских данных. `LoadState` сохраняется как `Corrupt` или `Unavailable`, и оба состояния запрещают `Promote` в этом запуске.

Если операция workspace завершилась успешно, но последующий `Promote` registry не удался, Team Kit печатает предупреждение и возвращает `0`: успешный workspace apply/update/retry не откатывается, повтор операции и автоматический retry не запускаются. Ошибка promotion не превращается в failure уже завершённой операции; следующий запуск снова сможет работать через manual/env fallback.

## Ошибки и retry

Для этого сценария закрепляются следующие стабильные коды:

- `WORKSPACE_EXISTS_USE_UPDATE` — add получил существующее Team Kit окружение;
- `FOREIGN_WORKSPACE` — путь не является подтверждённым Team Kit окружением или нарушает safety policy;
- `RETRY_REQUIRED` — в Team Kit окружении найден незавершённый receipt/операция;
- `INPUT_REQUIRED` — отсутствует обязательный непустой путь или номер меню;
- `INTERRUPTED` — интерактивный ввод отменён context/сигналом;
- `UPDATE_CHOICE_NOT_APPLICABLE` — в add-потоке передан `--update content`, `--update database` или `--update both`;
- `WORKSPACE_INSPECTION_FAILED` — проверка остановилась на I/O/permission ошибке.

`RETRY_REQUIRED` всегда сопровождается готовой командой, составленной из фактического `os.Executable()` и проверенного пути:

```text
Windows PowerShell: & '<abs-path-to-teamkit.exe>' retry --kit-home '<KIT_ALL_TEAM_HOME>'
POSIX shell:        '<abs-path-to-teamkit>' retry --kit-home '<KIT_ALL_TEAM_HOME>'
```

Кавычки и экранирование выбираются для текущей оболочки. Пользователь не редактирует команду вручную. До запуска этой команды update не выполняется и registry write не происходит.

## Архитектура и интерфейсы

### Границы компонентов

Изменение раскладывается по существующим слоям:

1. `internal/cli` добавляет первый режимный вопрос, add/update dispatch, summary, меню update и формирование retry-команды. Неинтерактивная ветка и прямые команды не проходят через этот dispatcher.
2. Новый узкий `internal/registry` вычисляет платформенный путь, bounded read/validate, protected atomic write и MRU promotion. Он не знает о project, app, role, toolchain, secrets или сервисном плане.
3. Новый discovery/inspection слой в `internal/cli` или отдельном `internal/environment` собирает кандидатов и использует существующие `pathsafe`, `privatefile`, `config`, `domain` и receipt-проверки. Он только читает и возвращает verified snapshot.
4. `internal/service` остаётся источником истины для повторной проверки и выполнения; `internal/reconcile` продолжает вычислять `none|content|database|both` и actions. Registry promotion вызывается только после результата полного успеха.

### Минимальные контракты

```go
type Registry struct {
    SchemaVersion int
    Homes         []string // MRU, абсолютные безопасные пути
}

type EnvironmentRegistry interface {
    Load(ctx context.Context) (Registry, LoadState, error)
    Promote(ctx context.Context, home string) error
}

type EnvironmentInspector interface {
    Inspect(ctx context.Context, home string) (VerifiedEnvironment, InspectionState, error)
}

type CandidateSource uint8 // explicit, registry, environment

type Candidate struct {
    Home   string
    Source CandidateSource
}
```

`LoadState` различает `Missing`, `Valid`, `Corrupt` и `Unavailable`; `Corrupt` и `Unavailable` запрещают последующую `Promote` в этом запуске и требуют warning/manual fallback. `InspectionState` различает `Ready`, `RetryRequired`, `Foreign` и `InspectionFailed`. Инспектор принимает каждый путь одинаково, а CLI отвечает только за приоритет и UI. Интерфейсы не возвращают секреты и не разрешают запись в workspace на discovery-фазе. `Promote` после успешной основной операции является best-effort: его failure сообщает warning без rollback/retry.

### Поток данных

```text
interactive apply
  -> mode menu
  -> add: current questionnaire -> readonly target classification -> service plan/apply
  -> update: explicit kit-home OR registry MRU + env
            -> dedupe -> inspect operation envelope/receipt, then owner/.env
            -> auto-select | numbered list with receipt markers + manual path
            -> verified summary -> exact 4-option update menu
            -> none: exit 0
            -> content/database/both: existing plan/apply -> success-only MRU promotion
```

При обнаруженном повреждённом или недоступном реестре в поток добавляется warning и флаг `registryWritable=false`; этот флаг сохраняется до конца запуска. Невалидные registry/env candidates получают отдельные bounded warnings и пропускаются. Receipt/envelope проверяется до credentials, сети и плана. Успешный результат сервиса является единственной точкой, после которой допускается best-effort MRU promotion.

## Совместимость

- `status --kit-home`, `retry --kit-home` и `update --kit-home [--target ...]` сохраняют прямой неинтерактивный контракт; `status` никогда не пишет реестр, успешный `retry` может выполнить регистрацию/backfill, direct update промотирует только для `content|database|both`, а отсутствие `--target` сохраняет текущий default `none` без записи.
- `apply --non-interactive` не показывает первое меню и требует прежние селекторы/флаги; registry discovery не используется, а best-effort promotion выполняется только после успешного выполнения непустого плана действий.
- Неинтерактивный `plan` не читает и не пишет реестр.
- Интерактивный `plan` не получает первое меню, не читает и не пишет реестр.
- Существующие значения `none`, `content`, `database`, `both`, JSON output и коды отмены не переименовываются.
- `--kit-home` в интерактивном update имеет абсолютный приоритет и не допускает fallback.

## Безопасность

Discovery не сканирует диск, не принимает данные реестра без повторной filesystem validation, не следует symlink/junction/reparse, не читает или не выводит секреты, не изменяет чужое окружение и не продолжает незавершённую операцию автоматически. Owner-only и bounded-проверки применяются к registry и secret stores; public workspace `.teamkit/owner`, `.env` и operation envelope/receipt проходят regular/no-reparse, bounded и strict-content проверки без требования owner-only mode/DACL. Выбор `Ничего` — абсолютный no-write/no-network/no-credentials barrier после завершённых read-only reads. Запись в ветку `develop`, системные trust stores и секретные файлы не входит в дизайн.

## TDD и кроссплатформенная матрица

Сначала добавляются падающие unit tests на контракты, затем минимальная реализация, затем интеграционные проверки CLI. Обязательная матрица:

| Область | Проверки |
|---|---|
| CLI menu | Первое меню содержит ровно два пункта; add и update dispatch; invalid/empty number reprompts; EOF -> `INPUT_REQUIRED`; cancellation -> `INTERRUPTED`; summary не задаёт OS/app/project/role/toolchain повторно; update menu содержит ровно четыре пункта и правильный mapping. |
| Skills selector | При отсутствии `--toolchain` вопрос имеет точный заголовок `Выберите набор skills:` и подписи `cc_1c_skills от Широкова` / `ai_rules_1c от Филиппова`; оба варианта доступны для Hermes и установленного non-Hermes; выбрать можно ровно один; default отсутствует; invalid/empty number reprompts; в `.env`, CLI и operation contracts записываются прежние ID `cc_1c_skills` / `ai_rules_1c`. Валидный explicit ID пропускает prompt; любой иной explicit value -> `TOOLCHAIN_UNKNOWN` без fallback. |
| Hermes profile | Для каждой роли проверяется как создание нового, так и использование существующего профиля; в обоих случаях профиль получает только выбранный набор, а второй одновременно не устанавливается. |
| Non-Hermes handoff | Для каждого из `cursor`, `claude-code`, `codex`, `opencode`, `kilo-code`, `kimi`, `qwen`, `command-code`, `cline`, `pi` при `app-installed=true` выполняется матрица обоих toolchain (`10 × 2 = 20` сценариев): каждый вариант отдельно создаёт secret-free `.teamkit/handoff.txt` только с выбранным pinned repo и точным commit плюс MCP v8std; прямой установки нет; второй набор не появляется; snapshot/контрольные проверки исключают токены и другие секретные значения. Для каждого из десяти приложений `app-installed=false` возвращает `AI_APP_REQUIRED` до toolchain prompt и не создаёт handoff или иной workspace-файл. |
| Add | Отсутствующий и пустой root принимаются; пустой введённый путь отклоняется; Team Kit root -> `WORKSPACE_EXISTS_USE_UPDATE`; foreign/non-safe root -> `FOREIGN_WORKSPACE`; успешный add двигает MRU, ошибка/no-op не двигает. |
| Discovery | Explicit `--kit-home` выбирается первым и не fallback-ится; registry MRU идёт раньше env; dedupe; invalid registry/env candidates warn+skip; нет displayable-кандидатов -> manual; список содержит ready `<project> — <path>`, receipt marker и `Указать другой путь`; explicit/manual invalid path остаётся fatal. |
| Receipt | Operation envelope/partial first-run receipt inspect происходит до owner/.env; неполный receipt отображается marker, один такой candidate auto-selects, при выборе в list -> `RETRY_REQUIRED`; точные PowerShell/POSIX команды, нет credentials/сети/plan/apply/write; ready candidate остаётся обычным update. |
| No-op | Read-only discovery/validation/summary-read могут быть завершены до меню; после выбора `Ничего` нет новых reads/effects, credential resolver, plan, apply, network, файловых записей или изменений, включая registry; код `0`. |
| Registry contract | `schema_version: 1`; только `homes`; UTF-8; 64 entries/65536 bytes; absolute paths; MRU; duplicate rejection; atomic replacement; owner-only permissions; `Missing`/`Valid`/`Corrupt`/`Unavailable`; corrupt/unavailable warning, manual fallback и отсутствие rewrite; Promote failure after successful operation warns, returns 0, no rollback/retry. |
| Windows | `%LOCALAPPDATA%` path; case-insensitive dedupe; DACL current-user-only; junction/reparse rejection; PowerShell retry quoting. |
| macOS | Application Support registry path; exact canonical dedupe; POSIX owner-only mode for registry/secret stores only; regular/no-reparse workspace files; POSIX retry quoting. |
| Linux/ALT Linux | `${XDG_CONFIG_HOME:-~/.config}` path; exact canonical dedupe; `0700/0600`; symlink rejection; POSIX retry quoting. |
| Flags/regression | `apply [--update none|content|database|both]`, `apply --non-interactive [--update ...]`, direct `update --kit-home [--target ...]` (absent target -> `none`), status/retry and plan preserve expected conflicts/contracts; add with unset/`none` succeeds, add with other scopes -> `UPDATE_CHOICE_NOT_APPLICABLE`; interactive menu remains first and explicit update scope skips only scope question; valid explicit `--toolchain=cc_1c_skills|ai_rules_1c` skips only skills prompt, invalid/empty explicit value -> `TOOLCHAIN_UNKNOWN`; existing JSON and current questionnaire tests remain green. |

Файловые операции тестируются через temporary directories и подменные adapters; network, credential resolver, service Plan/Apply и registry writer имеют spies, чтобы доказать no-op barrier. Отдельные spies проверяют `LoadState=Unavailable`, одно warning/manual fallback без rewrite, а также успешную основную операцию с неудачным `Promote`: warning, код `0`, отсутствие rollback/retry. Платформенные тесты с build tags проверяют owner-only права только для registry/secret stores, regular/no-reparse/bounded/strict-content workspace files, DACL/junction/reparse и registry locations; миграция workspace DACL в owner-only не тестируется как non-goal. Общие тесты проверяют одинаковую state machine.

## Изменения документации

После реализации обновляются `README.md` и `docs/INSTALL.md`: первое меню, add/update, источники и приоритет кандидатов, summary, четыре варианта update, готовая команда retry, новые подписи обоих наборов skills и отдельное пояснение non-Hermes handoff без прямой установки. В release notes фиксируется локальный MRU без секретов. `docs/SECURITY.md` перечисляет расположение, `schema_version`, owner-only/atomic политику и отсутствие project/role/app/toolchain/credentials, а также secret-free контракт `.teamkit/handoff.txt`. Прямые команды и неинтерактивная автоматизация описываются как обратно совместимые.

## Самопроверка спецификации

Проверены и зафиксированы все границы:

- первое меню имеет ровно `1 add` и `2 update`; invalid/empty number reprompts, EOF даёт `INPUT_REQUIRED`, cancellation — `INTERRUPTED`;
- add сохраняет текущие вопросы, требует непустой ввод пути и принимает только отсутствующий/пустой целевой root;
- update использует приоритет `--kit-home` > registry MRU > `KIT_ALL_TEAM_HOME`, без fallback после явного override;
- каждый кандидат проверяется по абсолютности, operation envelope/receipt (до owner/.env), публичным owner/.env и no-symlink/reparse policy; partial first-run receipt получает `RETRY_REQUIRED`;
- summary загружен из verified state, а OS/app/project/role/toolchain не спрашиваются повторно;
- при отсутствии `--toolchain` вопрос skills показывает точный заголовок с двоеточием и новые подписи, но сохраняет прежние ID; валидный explicit ID пропускает prompt, invalid explicit value даёт `TOOLCHAIN_UNKNOWN`; для Hermes и подтверждённо установленного non-Hermes доступен выбор ровно одного из двух вариантов без default, а non-Hermes получает только secret-free handoff выбранного pinned repo/commit и MCP v8std; `app-installed=false` даёт `AI_APP_REQUIRED` без prompt/handoff/files;
- update menu содержит ровно `none|content|database|both`, и `none` не вызывает сеть, credentials, plan, apply или записи;
- registry использует только `schema_version: 1` и MRU-массив `homes`, с bounded atomic owner-only storage; `LoadState` включает `Missing|Valid|Corrupt|Unavailable`;
- actionful successful interactive/noninteractive apply, интерактивный update с выбором `2–4`, прямой update с `content|database|both` и успешно завершённый `retry` выполняют только best-effort promotion; error/no-op/пустой plan/status/plan не пишут; повреждённый или недоступный registry предупреждает, даёт manual fallback и не переписывается;
- incomplete receipt возвращает `RETRY_REQUIRED` с готовой командой PowerShell/POSIX;
- нет disk scan и нет скрытых новых источников;
- все требования покрыты архитектурой, интерфейсами, data flow, security, TDD/cross-platform tests и docs impact.

Спецификация не содержит отложенных решений, placeholder-текста или расширения публичного `.env`/registry метаданных.

## Критерии приёмки

Реализация считается соответствующей этой спецификации только если одновременно выполнены следующие условия:

1. Интерактивный `apply` первым показывает ровно меню `Добавить новое окружение` / `Обновить существующее окружение`; invalid/empty number reprompts, EOF возвращает `INPUT_REQUIRED`, cancellation — `INTERRUPTED`, до выбора нет эффектов.
2. При отсутствии `--toolchain` вопрос `Выберите набор skills:` показывает ровно `cc_1c_skills от Широкова` и `ai_rules_1c от Филиппова`, не имеет default и разрешает выбрать только один набор; валидный explicit stable ID пропускает prompt, а любой иной explicit value возвращает `TOOLCHAIN_UNKNOWN`; внутренние значения `.env`, CLI и operation contracts остаются `cc_1c_skills` / `ai_rules_1c`.
3. Для Hermes профиль выбранной роли создаётся или используется существующий и получает только выбранный набор. Для каждого из десяти поддерживаемых AI-приложений не Hermes при подтверждённой установке доступны оба одиночных варианта; Team Kit не выполняет прямую установку, а создаёт secret-free `.teamkit/handoff.txt` только с выбранным pinned repo и точным commit плюс MCP v8std, без невыбранного набора. Ответ «Нет» возвращает `AI_APP_REQUIRED` до toolchain prompt и не создаёт handoff или иной workspace-файл.
4. Add сохраняет остальной существующий questionnaire, принимает только отсутствующий или пустой целевой root; отсутствие `--update` и `--update none` разрешены, а `--update content|database|both` возвращает `UPDATE_CHOICE_NOT_APPLICABLE`; для уже подтверждённого Team Kit root сообщает `WORKSPACE_EXISTS_USE_UPDATE`.
5. Update использует только явный `--kit-home`, registry MRU и `KIT_ALL_TEAM_HOME` в указанном приоритете; invalid registry/env candidates предупреждаются и пропускаются; при одном displayable-кандидате выбирает его автоматически, при нескольких показывает нумерованный список с receipt markers и пунктом `Указать другой путь`, при отсутствии displayable-кандидатов просит ручной путь.
6. Каждый кандидат проходит read-only проверку абсолютного безопасного пути, operation envelope/receipt до `owner`/публичного `.env` и strict-content/no-reparse policy; partial first-run или незавершённый receipt даёт `RETRY_REQUIRED` и готовую команду retry.
7. После выбора update summary строится из validated state и не задаёт повторно вопросы OS, AI-приложения, проекта, роли или toolchain.
8. Меню update содержит ровно `Ничего`, `Только файлы окружения`, `Только файлы базы данных`, `Файлы окружения и базы данных`; `Ничего` возвращает `0` без сети, credentials, plan/apply и любых записей.
9. Registry хранит только `schema_version: 1` и bounded MRU-массив абсолютных путей, использует платформенный путь и owner-only atomic write; best-effort promotion выполняют только actionful successful interactive/noninteractive apply, интерактивный update с выбором `2–4`, direct update с `content|database|both` и успешно завершённый `retry` для регистрации/backfill; `status`, `plan`, `none`, пустой план apply/update, ошибки и отмена никогда не пишут registry.
10. Повреждённый или недоступный registry вызывает ровно одно предупреждение, включает manual fallback и не переписывается в текущем запуске; discovery не выполняет disk scan.
11. Неинтерактивные `apply`/`plan`, а также прямые `status`, `retry` и `update` сохраняют прежние интерфейсы и форматы; `apply [--non-interactive] --update none|content|database|both` трактует `--update` только как scope, interactive update honors explicit scope and skips only scope question, при отсутствии флага `--target` direct `update` сохраняет default `none`; registry discovery не используется в noninteractive apply, а promotion после успеха best-effort и при ошибке лишь предупреждает, возвращая `0` без rollback/retry.

## Не входит в объём (non-goals)

- Поиск окружений сканированием дисков, сетевых томов, `PATH`, shell history или любых иных неявных источников.
- Хранение в registry project, OS, AI-приложения, роли, toolchain, credentials, secrets, receipt, статуса, timestamps или истории ошибок.
- Автоматическое исправление повреждённого registry, `.env`, `owner`, receipt или чужого workspace.
- Автоматический retry, автоматический выбор режима по содержимому каталога или fallback после явно заданного `--kit-home`.
- Переименование стабильных идентификаторов `cc_1c_skills` / `ai_rules_1c`, установка обоих наборов одновременно, неявный default или прямая установка набора в AI-приложение не Hermes.
- Изменение остальных вопросов questionnaire add, публичного JSON-формата команд, системных trust stores, ветки `develop` или секретных файлов.
- Перенос registry в workspace, синхронизация registry между машинами или удалённый сервис обнаружения окружений.
