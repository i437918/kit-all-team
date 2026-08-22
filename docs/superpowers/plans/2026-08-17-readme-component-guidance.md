# README Component Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Дополнить актуальный README v0.1.1 сравнением skills, пояснениями CustomLLM/certs/Hermes и описанием MCP v8std с каноническими ссылками.

**Architecture:** Изменение ограничено одним пользовательским документом и его существующим release-контрактом. Новая информация размещается рядом с соответствующим решением пользователя, без перестройки README и без изменения Confluence-инструкции.

**Tech Stack:** GitHub-flavored Markdown, Go release contract tests.

## Global Constraints

- Меняется только актуальный README и его контрактный тест.
- Модель оформляется как **`generic-development`**.
- Наборы остаются взаимоисключающими; ни один не объявляется универсально лучшим.
- Канонические репозитории: `Nikolay-Shirokov/cc-1c-skills`, `comol/ai_rules_1c`, `zeegin/v8std`.
- Публичный MCP endpoint: `https://ai.v8std.ru/mcp`.

---

### Task 1: Дополнить README сведениями о компонентах

**Files:**
- Modify: `README.md`
- Test: `test/release/docs_test.go`

**Interfaces:**
- Consumes: текущая структура разделов «Что подготовить до установки» и «Как устанавливаются skills Hermes».
- Produces: проверяемый пользовательский контракт README с каноническими ссылками и пояснениями.

- [ ] **Step 1: Расширить failing contract test**

Добавить в `TestReadmeAndConfluenceGuideDescribeOnlyCurrentV011Journey` отдельный список обязательных фрагментов README:

```go
for _, required := range []string{
	"https://github.com/Nikolay-Shirokov/cc-1c-skills",
	"https://github.com/comol/ai_rules_1c",
	"**`generic-development`**",
	"отличается от GitLab token",
	"не устанавливает сертификаты в системное хранилище",
	"https://v8std.ru/mcp/",
	"https://github.com/zeegin/v8std",
	"https://ai.v8std.ru/mcp",
	"не отправляйте закрытый код",
} {
	if !strings.Contains(readme, required) {
		t.Errorf("README missing component guidance %q", required)
	}
}
```

- [ ] **Step 2: Запустить тест и подтвердить RED**

Run:

```powershell
go test -mod=vendor -count=1 ./test/release -run TestReadmeAndConfluenceGuideDescribeOnlyCurrentV011Journey
```

Expected: FAIL на отсутствующих репозиториях, расширенных пояснениях и MCP-разделе.

- [ ] **Step 3: Добавить таблицу сравнения skills**

После списка предварительного выбора вставить таблицу с колонками:

```markdown
| Набор | Основной акцент | Что содержит | Когда выбирать | Репозиторий |
```

Для `cc_1c_skills` описать навыки полного цикла разработки 1С, работу с XML/CLI и проверками. Для `ai_rules_1c` описать переносимые правила, роли субагентов, команды и адаптеры нескольких AI-клиентов.

- [ ] **Step 4: Расширить prerequisites Hermes**

Оформить модель как **`generic-development`**, сослаться на корпоративную инструкцию получения ключа и отличить ключ от GitLab token. Пояснить локальную TLS-роль `certs.zip` и назначение поддерживаемого Hermes runtime, включая ручную GUI-установку Windows и автоматическое обнаружение пути.

- [ ] **Step 5: Добавить раздел MCP v8std**

После раздела skills описать read-only справочник стандартов и диагностик 1С, официальный endpoint, документацию и репозиторий. Добавить предупреждение о публичной передаче запросов и запрет отправки закрытого кода/коммерческих данных.

- [ ] **Step 6: Запустить GREEN и полные проверки**

Run:

```powershell
gofmt -w test/release/docs_test.go
go test -mod=vendor -count=1 ./test/release
go test -mod=vendor -count=1 ./...
go vet -mod=vendor ./...
git diff --check
```

Expected: все команды exit 0; архивные разделы в README отсутствуют.

- [ ] **Step 7: Зафиксировать пользовательскую документацию**

```powershell
git add README.md docs/CONFLUENCE-INSTALL-v0.1.1.md test/release/docs_test.go docs/superpowers/plans/2026-08-17-readme-component-guidance.md
git commit -m "docs(readme): publish v0.1.1 installation guidance"
```
