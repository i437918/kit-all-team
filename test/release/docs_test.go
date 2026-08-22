package release_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadmeAndConfluenceGuideDescribeOnlyCurrentV013Journey(t *testing.T) {
	readme := strings.ReplaceAll(readRepositoryFile(t, "README.md"), "\r\n", "\n")
	for _, forbidden := range []string{
		"## Архив выпуска v0.1.0",
		"## Архивная установка v0.1.0",
		"## Исторический архив RC2",
		"## Историческая инструкция: опубликованный v0.1.0-rc.2",
		"teamkit-v0.1.0-rc.2-windows-amd64.exe",
		"Показатели рассчитаны по закреплённым коммитам",
		"e01688e764a3cf1c1b4a0ad5069ea885837cfb2e",
		"f33d2405207cf325f893dc8ca2789157d887db81",
	} {
		if strings.Contains(readme, forbidden) {
			t.Errorf("README retains obsolete installation content %q", forbidden)
		}
	}
	for _, required := range []string{
		"## Содержание",
		"[Что такое 1C Team Kit](#что-такое-1c-team-kit)",
		"[Установка в Windows](#установка-в-windows)",
		"[macOS](#macos)",
		"[Linux](#linux)",
		"[ALT Linux](#alt-linux)",
		"[Безопасность и ограничения](#безопасность-и-ограничения)",
		"## Что такое 1C Team Kit",
		"## Что подготовить до установки",
		"## Что будет создано",
		"## Установка в Windows",
		"### Ветка A. Новое окружение",
		"### Ветка B. Обновление окружения",
		"### Ветка C. Восстановление незавершённой установки",
		"## Проверка результата",
		"## Устранение ошибок",
		"docs/CONFLUENCE-INSTALL-v0.1.3.md",
		"https://github.com/Nikolay-Shirokov/cc-1c-skills",
		"https://github.com/comol/ai_rules_1c",
		"  - `cc_1c_skills от Широкова`; [Nikolay-Shirokov/cc-1c-skills](https://github.com/Nikolay-Shirokov/cc-1c-skills)",
		"  - `ai_rules_1c от Филиппова`; [comol/ai_rules_1c](https://github.com/comol/ai_rules_1c)",
		"| Набор | Основной акцент | Что содержит |",
		"`72` каталога skills (`303` файла)",
		"`11` каталогов skills (`115` файлов)",
		"`34` rules, `13` описаний субагентов и `13` команд",
		"**`generic-development`**",
		"отличается от GitLab token",
		"https://docs.example.invalid/spaces/DAT/pages/323493884/Получение+персонального+токен+в+GitLab",
		"**GitLab Release**",
		"**Hermes**",
		"**Team Kit**",
		"`KIT_ALL_TEAM_HOME`",
		"https://gitlab.example.invalid/1c/aisuz/ai/-/tree/content-apa?ref_type=heads",
		"команда проекта адаптирует",
		"самостоятельно публикует проверенный комплект",
		"следующие установки и обновления",
		"> **Важно: развитие проектной ветки**",
		"не устанавливает сертификаты в системное хранилище",
		"https://v8std.ru/mcp/",
		"https://github.com/zeegin/v8std",
		"https://ai.v8std.ru/mcp",
		"не отправляйте закрытый код",
	} {
		if !strings.Contains(readme, required) {
			t.Errorf("README missing current user journey fragment %q", required)
		}
	}
	contents := strings.Index(readme, "## Содержание")
	product := strings.Index(readme, "## Что такое 1C Team Kit")
	if contents < 0 || product <= contents {
		t.Errorf("README contents must precede the product description: contents=%d product=%d", contents, product)
	}
	comparisonStart := strings.Index(readme, "Сравнение доступных наборов:")
	if comparisonStart < 0 {
		t.Fatal("README comparison table start is missing")
	}
	comparisonEnd := strings.Index(readme[comparisonStart:], "Если выбран **Hermes**")
	if comparisonEnd < 0 {
		t.Fatal("README comparison table end is missing")
	}
	comparison := readme[comparisonStart : comparisonStart+comparisonEnd]
	for _, removedColumn := range []string{"Когда выбирать", "Репозиторий"} {
		if strings.Contains(comparison, removedColumn) {
			t.Errorf("README comparison table retains removed column %q", removedColumn)
		}
	}
	safety := strings.Index(readme, "**Team Kit** не изменяет системное хранилище сертификатов")
	projectBranch := strings.Index(readme, "> **Важно: развитие проектной ветки**")
	prerequisites := strings.Index(readme, "## Что подготовить до установки")
	if safety < 0 || projectBranch <= safety || prerequisites <= projectBranch {
		t.Errorf("README project-branch callout must follow the Team Kit safety boundary: safety=%d callout=%d prerequisites=%d", safety, projectBranch, prerequisites)
	}

	confluence := strings.ReplaceAll(readRepositoryFile(t, "docs/CONFLUENCE-INSTALL-v0.1.3.md"), "\r\n", "\n")
	for _, required := range []string{
		"# Установка 1C Team Kit v0.1.3",
		"## Содержание",
		"[Назначение](#назначение)",
		"## Чек-лист перед установкой",
		"## Подробная установка в Windows",
		"## Матрица веток установки",
		"## Проверка после установки",
		"## macOS",
		"## Linux",
		"## ALT Linux",
		"https://docs.example.invalid/spaces/DAT/pages/323493884/Получение+персонального+токен+в+GitLab",
		"`cc_1c_skills от Широкова`; [Nikolay-Shirokov/cc-1c-skills](https://github.com/Nikolay-Shirokov/cc-1c-skills)",
		"`ai_rules_1c от Филиппова`; [comol/ai_rules_1c](https://github.com/comol/ai_rules_1c)",
		"| Набор | Основной акцент | Что содержит |",
		"`72` каталога skills (`303` файла)",
		"`11` каталогов skills (`115` файлов)",
		"**`generic-development`**",
		"Подключение к LLM через API (IDE, SDK)",
		"[zeegin/v8std](https://github.com/zeegin/v8std)",
		"> **Важно: развитие проектной ветки**",
		"[`content-apa`](https://gitlab.example.invalid/1c/aisuz/ai/-/tree/content-apa?ref_type=heads)",
	} {
		if !strings.Contains(confluence, required) {
			t.Errorf("Confluence guide missing fragment %q", required)
		}
	}
	confluenceContents := strings.Index(confluence, "## Содержание")
	confluencePurpose := strings.Index(confluence, "## Назначение")
	if confluenceContents < 0 || confluencePurpose <= confluenceContents {
		t.Errorf("Confluence contents must precede purpose: contents=%d purpose=%d", confluenceContents, confluencePurpose)
	}
	confluenceComparison := markdownSection(t, confluence, "### Сравнение доступных наборов")
	for _, removedColumn := range []string{"Когда выбирать", "Репозиторий"} {
		if strings.Contains(confluenceComparison, removedColumn) {
			t.Errorf("Confluence comparison table retains removed column %q", removedColumn)
		}
	}
	confluenceSafety := strings.Index(confluence, "**Team Kit** не изменяет системное хранилище сертификатов")
	confluenceProjectBranch := strings.Index(confluence, "> **Важно: развитие проектной ветки**")
	if confluenceSafety < 0 || confluenceProjectBranch <= confluenceSafety {
		t.Errorf("Confluence project-branch callout must follow the Team Kit safety boundary: safety=%d callout=%d", confluenceSafety, confluenceProjectBranch)
	}
}

func TestReadmePlatformJourneysAreActionable(t *testing.T) {
	readme := strings.ReplaceAll(readRepositoryFile(t, "README.md"), "\r\n", "\n")
	for _, platform := range []struct {
		heading  string
		required []string
	}{
		{
			heading: "## macOS",
			required: []string{
				"uname -m",
				"teamkit-v0.1.3-darwin-amd64",
				"teamkit-v0.1.3-darwin-arm64",
				"shasum -a 256",
				"выберите `macOS`",
				"$HOME/TeamKit/apa",
				" apply",
				"status --kit-home",
				"`ready`",
			},
		},
		{
			heading: "## Linux",
			required: []string{
				"uname -m",
				"x86_64",
				"teamkit-v0.1.3-linux-amd64",
				"sha256sum --check",
				"выберите `Linux`",
				"$HOME/teamkit/apa",
				" apply",
				"status --kit-home",
				"`ready`",
			},
		},
		{
			heading: "## ALT Linux",
			required: []string{
				"/etc/os-release",
				"ALT Linux p11",
				"teamkit-v0.1.3-linux-amd64",
				"sha256sum --check",
				"выберите `ALT Linux`",
				"$HOME/teamkit/apa",
				" apply",
				"status --kit-home",
				"`ready`",
			},
		},
	} {
		t.Run(platform.heading, func(t *testing.T) {
			section := markdownSection(t, readme, platform.heading)
			for _, required := range platform.required {
				if !strings.Contains(section, required) {
					t.Errorf("%s journey missing %q", platform.heading, required)
				}
			}
		})
	}
}

func TestCurrentV013Documentation_DescribesAlwaysOnAtlassianCredentials(t *testing.T) {
	for _, path := range []string{
		"README.md",
		"docs/INSTALL.md",
		"docs/CONFLUENCE-INSTALL-v0.1.3.md",
	} {
		t.Run(path, func(t *testing.T) {
			text := readRepositoryFile(t, path)
			for _, required := range []string{
				"v0.1.3",
				"HERMES_CUSTOM_LLM_API_KEY",
				"HERMES_CUSTOM_ISSUE_TRACKER_TOKEN",
				"HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN",
				"Jira personal token",
				"Confluence personal token",
				"owner-only",
				"https://llm.example.invalid/jira/mcp",
				"https://llm.example.invalid/confluence/mcp",
				"x-mcp-jira-authorization",
				"x-mcp-confluence-authorization",
				"retry",
				"https://docs.example.invalid/spaces/BLOKL/pages/1005956195/",
				"обязательн",
			} {
				if !strings.Contains(text, required) {
					t.Errorf("current documentation does not contain %q", required)
				}
			}
		})
	}
}

func TestCurrentV013Documentation_ExplainsJiraAndConfluenceMCPs(t *testing.T) {
	for _, path := range []string{"README.md", "docs/CONFLUENCE-INSTALL-v0.1.3.md"} {
		t.Run(path, func(t *testing.T) {
			text := readRepositoryFile(t, path)
			for _, server := range []struct {
				heading  string
				required []string
			}{
				{heading: "## MCP Jira", required: []string{"HERMES_CUSTOM_ISSUE_TRACKER_TOKEN", "https://llm.example.invalid/jira/mcp", "x-litellm-api-key", "x-mcp-jira-authorization", "задач", "проект"}},
				{heading: "## MCP Confluence", required: []string{"HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN", "https://llm.example.invalid/confluence/mcp", "x-litellm-api-key", "x-mcp-confluence-authorization", "страниц", "пространств"}},
			} {
				section := markdownSection(t, text, server.heading)
				for _, required := range server.required {
					if !strings.Contains(strings.ToLower(section), strings.ToLower(required)) {
						t.Errorf("%s section missing %q", server.heading, required)
					}
				}
			}
			for _, required := range []string{"`v8std`", "`customllm-jira`", "`customllm-confluence`"} {
				if !strings.Contains(text, required) {
					t.Errorf("Hermes verification does not identify required MCP %q", required)
				}
			}
		})
	}
}

func TestPublishedV013Documentation_LinksTheGitLabRelease(t *testing.T) {
	for _, path := range []string{"README.md", "docs/INSTALL.md", "docs/CONFLUENCE-INSTALL-v0.1.3.md"} {
		t.Run(path, func(t *testing.T) {
			text := readRepositoryFile(t, path)
			for _, required := range []string{"v0.1.3", "https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3"} {
				if !strings.Contains(text, required) {
					t.Errorf("published v0.1.3 documentation does not contain %q", required)
				}
			}
			for _, forbidden := range []string{"готовящийся", "после публикации", "Страница после публикации"} {
				if strings.Contains(text, forbidden) {
					t.Errorf("published v0.1.3 documentation retains prospective wording %q", forbidden)
				}
			}
		})
	}
}

func TestConfluenceV013SecretPrompt_ListsAllMaskedHermesCredentials(t *testing.T) {
	guide := readRepositoryFile(t, "docs/CONFLUENCE-INSTALL-v0.1.3.md")
	start := strings.Index(guide, "#### Запрос секретов")
	end := strings.Index(guide, "\n### 6B.")
	if start < 0 || end <= start {
		t.Fatalf("cannot isolate the Confluence secret prompt: start=%d end=%d", start, end)
	}
	prompt := guide[start:end]
	for _, credential := range []string{
		"HERMES_CUSTOM_LLM_API_KEY",
		"HERMES_CUSTOM_ISSUE_TRACKER_TOKEN",
		"HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN",
	} {
		if !strings.Contains(prompt, credential) {
			t.Errorf("Confluence secret prompt does not list masked Hermes credential %q", credential)
		}
	}
}

func TestInstallGuideCurrentV013PrecedesHistoricalReleases(t *testing.T) {
	for _, path := range []string{"docs/INSTALL.md"} {
		t.Run(path, func(t *testing.T) {
			text := strings.ReplaceAll(readRepositoryFile(t, path), "\r\n", "\n")
			currentStart := strings.Index(text, "## Актуальный выпуск v0.1.3")
			v010Archive := strings.Index(text, "## Архив выпуска v0.1.0")
			rc2Archive := strings.Index(text, "## Исторический архив RC2")
			if currentStart < 0 || v010Archive <= currentStart || rc2Archive <= v010Archive {
				t.Fatalf("release sections are missing or out of order: current=%d v0.1.0=%d RC2=%d", currentStart, v010Archive, rc2Archive)
			}

			current := text[currentStart:v010Archive]
			for _, required := range []string{
				"teamkit-v0.1.3-windows-amd64.exe",
				"teamkit-v0.1.3-linux-amd64",
				"teamkit-v0.1.3-darwin-amd64",
				"teamkit-v0.1.3-darwin-arm64",
				"SHA256SUMS",
				"SECURITY-AUDIT.json",
				"встроенные skills Hermes",
				"ровно один внешний набор",
				"cc_1c_skills от Широкова",
				"ai_rules_1c от Филиппова",
				"skills opt-in --sync",
				"не выполняет общий `hermes update`",
				"schema 34 или 37",
				"MCP `v8std`",
				"SECRET_FILE_PERMISSIONS_UNSAFE",
				"HERMES_BUNDLED_SKILLS_USER_OPT_OUT",
				"HERMES_CONFIG_SCHEMA_UNSUPPORTED",
				"retry --kit-home",
				"Assets → Other",
				"Hermes-Setup.exe",
				"certs.zip",
			} {
				if !strings.Contains(current, required) {
					t.Errorf("current v0.1.3 section does not contain %q", required)
				}
			}
			for _, forbidden := range []string{
				"docs/releases/v0.1.3.md",
				"jobs/UNKNOWN",
				"pipeline/UNKNOWN",
				"SHA256: TBD",
			} {
				if strings.Contains(current, forbidden) {
					t.Errorf("current v0.1.3 section contains prospective evidence %q", forbidden)
				}
			}
		})
	}
}

func TestInstallGuide_StatesCurrentEntrypointAndSecretBoundary(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "docs", "INSTALL.md"))
	if err != nil {
		t.Fatalf("read installation guide: %v", err)
	}
	text := string(data)
	for _, fragment := range []string{"--version", "локальном `.env` выбранного приложения", "GIT_ASKPASS", "неподписан", "teamkit plan", "status --kit-home", "retry --kit-home", "update --kit-home"} {
		if !strings.Contains(text, fragment) {
			t.Errorf("installation guide does not contain %q", fragment)
		}
	}
}

func TestUserDocsDescribeEnvironmentModesAndRegistrySafety(t *testing.T) {
	for _, document := range []struct {
		path    string
		heading string
	}{
		{path: "README.md", heading: "## Установка в Windows"},
		{path: "docs/INSTALL.md", heading: "## Мастер v0.1.0: пошаговый выбор add/update"},
	} {
		t.Run(document.path, func(t *testing.T) {
			fullText := readRepositoryFile(t, document.path)
			text := markdownSection(t, fullText, document.heading)
			for _, compatibilityContract := range []string{
				"меню `Добавить/Обновить` появляется только при интерактивном `teamkit apply`",
				"`apply --non-interactive`, `plan`, `status`, `retry` и `update`",
				"сохраняют обратную совместимость",
				"машиночитаемые форматы",
				"`apply --non-interactive` и `plan` не используют обнаружение через локальный реестр",
				"`plan` и `status` не записывают локальный реестр",
			} {
				if !strings.Contains(text, compatibilityContract) {
					t.Fatalf("%s add/update section missing compatibility contract %q", document.path, compatibilityContract)
				}
			}
			if document.path == "README.md" {
				for _, contract := range []string{
					"`--kit-home` имеет наивысший приоритет",
					"ошибка явного `--kit-home` завершает команду без перехода к другим источникам",
					"локальный реестр",
					"`KIT_ALL_TEAM_HOME` из переменной среды",
					"ручной ввод",
				} {
					if !strings.Contains(text, contract) {
						t.Fatalf("README add/update section missing environment source contract %q", contract)
					}
				}
			}
			disclaimers := []string{"финальн", "v0.1.0", "историческ", "v0.1.0-rc.2"}
			if document.path == "README.md" {
				disclaimers = []string{"v0.1.3", "текущ"}
			}
			for _, disclaimer := range disclaimers {
				if !strings.Contains(strings.ToLower(text), strings.ToLower(disclaimer)) {
					t.Fatalf("%s add/update section missing final-release boundary %q", document.path, disclaimer)
				}
			}
			required := []string{"1. `Добавить новое окружение`", "2. `Обновить существующее окружение`", "RETRY_REQUIRED", "cc_1c_skills от Широкова", "ai_rules_1c от Филиппова", ".teamkit/handoff.txt", "AI_APP_REQUIRED"}
			last := -1
			for _, fragment := range required {
				position := strings.Index(text, fragment)
				if position < 0 {
					t.Fatalf("%s add/update section missing %q", document.path, fragment)
				}
				if document.path != "README.md" && (fragment == "1. `Добавить новое окружение`" || fragment == "2. `Обновить существующее окружение`" || fragment == "RETRY_REQUIRED" || fragment == "cc_1c_skills от Широкова" || fragment == ".teamkit/handoff.txt") {
					if position < last {
						t.Fatalf("%s scenario fragment %q is out of order", document.path, fragment)
					}
					last = position
				}
			}
		})
	}
	security := readRepositoryFile(t, "docs/SECURITY.md")
	for _, text := range []string{
		`%LOCALAPPDATA%\TeamKit\environments.json`,
		`~/Library/Application Support/TeamKit/environments.json`,
		`${XDG_CONFIG_HOME:-~/.config}/teamkit/environments.json`,
		"schema_version", "65536", "64", "не содержит", "не переписывается", "не сканирует", "owner-only", "atomic replace", ".teamkit/handoff.txt",
	} {
		if !strings.Contains(security, text) {
			t.Fatalf("SECURITY missing %q", text)
		}
	}
	changelog := readRepositoryFile(t, "CHANGELOG.md")
	unreleased := markdownSection(t, changelog, "## v0.1.0 — 2026-08-17")
	for _, text := range []string{"Добавить новое окружение", "Обновить существующее окружение", "локальный MRU хранит только абсолютные пути"} {
		if !strings.Contains(unreleased, text) {
			t.Fatalf("v0.1.0 changelog missing %q", text)
		}
	}
	rc2 := markdownSection(t, changelog, "## v0.1.0-rc.2 — 2026-08-16")
	for _, text := range []string{"Добавить новое окружение", "Обновить существующее окружение", "локальный MRU"} {
		if strings.Contains(rc2, text) {
			t.Fatalf("published RC2 changelog contains future behavior %q", text)
		}
	}
}

func markdownSection(t *testing.T, document, heading string) string {
	t.Helper()
	start := strings.Index(document, heading)
	if start < 0 {
		t.Fatalf("missing markdown heading %q", heading)
	}
	section := document[start:]
	if next := strings.Index(section[len(heading):], "\n## "); next >= 0 {
		section = section[:len(heading)+next]
	}
	return section
}

func TestDocumentation_RecordsALTOfficeCLIImageEvidenceWithoutPromotingRuntime(t *testing.T) {
	root := repositoryRoot(t)
	qualification := readContractFile(t, root, "docs/OFFICECLI-QUALIFICATION.md")
	matrix := readContractFile(t, root, "docs/TEST-MATRIX.md")
	design := readContractFile(t, root, "docs/superpowers/specs/2026-08-22-alt-p11-officecli-bounded-finish-design.md")

	const sourceStatus = "Source-record status: **outcome-neutral; runtime acceptance is conditional on external exact-SHA evidence**."
	const historicalQualificationDigest = "ghcr.io/dmitry-m1man/kit-all-team/alt-p11-officecli@sha256:fe1aef6ae65d887389aa11bd9f9bdf99a924b4f6f587edaeb37307b3e2e99a48"
	const activeQualificationDigest = "ghcr.io/i437918/kit-all-team/alt-p11-officecli@sha256:5ee493c6c7edbdb8d68fb0ab9af2847bae855c9042bc5f13f5fd6b3d0965a825"
	const baseDigest = "registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea"
	const librtNEVRA = "glibc-pthread-6:2.38.0.223.f053ff-alt1.p11.1.x86_64"
	const lddNEVRA = "glibc-utils-6:2.38.0.223.f053ff-alt1.p11.1.x86_64"
	const icuNEVRA = "libicu74-1:7.4.2-alt1.x86_64"
	const officeCLISHA = "32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8"
	const nonPromotion = "It does not establish `QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME`, does not qualify the final feature candidate, and does not replace the required exact-SHA GitHub native/ALT run or the matching GitLab pipeline."
	const publicALTIsolation = "Pull requests and ordinary pushes use the exact public ALT base only; they neither authenticate to GHCR nor run OfficeCLI and do not constitute runtime qualification evidence."
	const publicALTMatrixRule = "The normal one-argument ALT script path is the public-base PR/push check: it pulls only the exact public base, uses no GHCR credentials, and does not run OfficeCLI."
	const approvedDesignStatus = "**Status:** approved on 2026-08-22; bounded finalization is governed by external exact-SHA evidence and is not self-certified by this document."
	const externalEvidenceRule = "Исходная запись нейтральна к результату. Итог runtime определяется только на основании external exact-SHA evidence, привязанного к SHA кандидата; успешный инженерный результат сам по себе не публикует `v0.1.5` и не закрывает отдельные corporate Windows/release gates."
	const officeCLIAltRow = "| `OFFICECLI_ALT_USERSPACE_COMPATIBLE` | userspace-only ALT p11; exact image `" + activeQualificationDigest + "`; providers `" + librtNEVRA + "`, `" + lddNEVRA + "`, `" + icuNEVRA + "` | Manual dispatch bound to exact SHA | Required acceptance gate |"
	const publicAltRow = "| `ALT_USERSPACE_COMPATIBLE` | public base `" + baseDigest + "`; no GHCR or OfficeCLI | PR/push | Required |"

	requiredQualification := []string{
		sourceStatus,
		"https://github.com/dmitry-m1man/kit-all-team/actions/runs/32541524174",
		"Event: `push`",
		"9964ba4dd9f30cf115da223fa7554345e8a8bdfe",
		historicalQualificationDigest,
		baseDigest,
		librtNEVRA,
		lddNEVRA,
		icuNEVRA,
		"35316133",
		officeCLISHA,
		"image-construction/userspace evidence",
		nonPromotion,
	}
	for _, required := range requiredQualification {
		if !strings.Contains(qualification, required) {
			t.Errorf("OfficeCLI qualification record lacks %q", required)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(qualification), " "), publicALTIsolation) {
		t.Errorf("OfficeCLI qualification record lacks public ALT registry isolation rule %q", publicALTIsolation)
	}
	if strings.Count(qualification, "Source-record status: **") != 1 ||
		strings.Count(qualification, sourceStatus) != 1 {
		t.Fatal("OfficeCLI qualification record has an ambiguous source status")
	}
	staleQualificationClaims := []string{
		"Status: **pending trusted exact-SHA dispatch evidence**",
		"the new ALT diagnostics have not yet run",
		"A future successful record must add",
		"Until pinned ALT diagnostics identify",
		"pending runtime status above",
	}
	for _, stale := range staleQualificationClaims {
		if strings.Contains(qualification, stale) {
			t.Errorf("OfficeCLI qualification record retains stale claim %q", stale)
		}
	}
	if strings.Contains(qualification, "Status: **") {
		t.Fatal("OfficeCLI qualification record retains a legacy Status field")
	}

	requiredMatrix := []string{
		baseDigest, activeQualificationDigest, librtNEVRA, lddNEVRA, icuNEVRA,
		"подготовленный, но не опубликованный контракт `v0.1.5`", "userspace-only",
		officeCLIAltRow, publicAltRow,
	}
	for _, required := range requiredMatrix {
		if !strings.Contains(matrix, required) {
			t.Errorf("ALT test matrix lacks immutable evidence %q", required)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(matrix), " "), publicALTMatrixRule) {
		t.Errorf("ALT test matrix lacks public ALT registry isolation rule %q", publicALTMatrixRule)
	}

	requiredDesign := []string{
		baseDigest, historicalQualificationDigest, librtNEVRA, lddNEVRA, icuNEVRA,
		approvedDesignStatus,
		"final task completion report",
		"does not claim that an MR has already been created or updated",
		"No evidence-only source commit",
		"partial evidence bundle",
		"last confirmed delivery stage",
	}
	for _, required := range requiredDesign {
		if !strings.Contains(design, required) {
			t.Errorf("bounded design lacks immutable evidence %q", required)
		}
	}

	statusDocs := []string{
		"README.md",
		"CHANGELOG.md",
		"docs/EXTERNAL-BLOCKERS.md",
		"docs/CONFLUENCE-INSTALL-v0.1.5.md",
		"docs/RELEASE-CHECKLIST.md",
	}
	staleClaims := []string{
		"не полученный runtime PASS",
		"Runtime qualification остаётся pending",
		"runtime qualification этого exact SHA пока не заявлена",
		"и пока не заявлены",
		"не имеет live runtime PASS",
		"не заявляет runtime PASS",
		"оно pending",
		"До этого runtime PASS",
	}
	for _, path := range statusDocs {
		body := readContractFile(t, root, path)
		if strings.Count(body, externalEvidenceRule) != 1 {
			t.Errorf("%s must contain the exact outcome-neutral rule once", path)
		}
		for _, stale := range staleClaims {
			if strings.Contains(body, stale) {
				t.Errorf("%s retains time-dependent claim %q", path, stale)
			}
		}
	}
	externalBlockers := readContractFile(t, root, "docs/EXTERNAL-BLOCKERS.md")
	if !strings.Contains(externalBlockers, "## v0.1.5: OFFICECLI_RUNTIME_QUALIFICATION_EXTERNAL_EVIDENCE") ||
		strings.Contains(externalBlockers, "OFFICECLI_RUNTIME_QUALIFICATION_PENDING") {
		t.Fatal("external blockers retain a time-dependent OfficeCLI heading")
	}
}

func TestDocumentation_DescribesOfficeCLIProductAndReleaseBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	currentDocs := strings.Join([]string{
		readContractFile(t, root, "README.md"),
		readContractFile(t, root, "docs/INSTALL.md"),
		readContractFile(t, root, "docs/SECURITY.md"),
		readContractFile(t, root, "docs/OFFICECLI-QUALIFICATION.md"),
		readContractFile(t, root, "docs/CONFLUENCE-INSTALL-v0.1.5.md"),
	}, "\n")

	for _, required := range []string{
		"v1.0.144",
		"${HERMES_HOME}/.teamkit/officecli/v1.0.144/officecli",
		"${UserProfile}/.officecli/config.json",
		"officecli config autoUpdate false",
		"officecli config autoUpdate",
		"officecli-pptx",
		"officecli-docx",
		"officecli-xlsx",
		"load_skill",
		"`.update`, `.update.partial`, `.old`",
		"v8std, Jira, Confluence и OfficeCLI",
	} {
		if !strings.Contains(currentDocs, required) {
			t.Errorf("current OfficeCLI documentation does not contain %q", required)
		}
	}

	for _, row := range []string{
		"| Windows x64 | `officecli-win-x64.exe` |",
		"| Linux x64 | `officecli-linux-x64` |",
		"| macOS Intel | `officecli-mac-x64` |",
		"| macOS Apple Silicon | `officecli-mac-arm64` |",
		"| ALT Linux p11 x64 | `officecli-linux-x64` |",
	} {
		if !strings.Contains(currentDocs, row) {
			t.Errorf("current OfficeCLI documentation does not contain supported-platform row %q", row)
		}
	}

	for _, required := range []string{
		"только в профиль Hermes",
		"не изменяет PATH",
		"user-global",
		"best-effort",
		"ранее установленных OfficeCLI skills",
		"перезаписать локальные изменения",
		"Team Kit не устанавливает on-disk skills",
		"не полагается на default Hermes skill directory",
		"читать и изменять документы Office",
		"не устанавливает и не обновляет OfficeCLI произвольным installer/updater",
		"`retry` повторно использует существующий `configure_application`",
		"не удаляет старые pinned versions",
		"`OFFICECLI_SKIP_UPDATE` не используется",
	} {
		if !strings.Contains(currentDocs, required) {
			t.Errorf("current OfficeCLI documentation does not contain behavior boundary %q", required)
		}
	}

	changelog := readContractFile(t, root, "CHANGELOG.md")
	unreleased := markdownSection(t, changelog, "## Unreleased")
	for _, required := range []string{
		"v0.1.5",
		"только в GitLab",
		"уже опубликованные releases, tags и assets остаются неизменяемыми",
		"QUALIFIED_PINNED_AUTOUPDATE_DISABLED_RUNTIME",
		"CI run URL",
		"четырёх native lanes",
		"ALT p11 smoke",
	} {
		if !strings.Contains(unreleased, required) {
			t.Errorf("Unreleased boundary does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"OfficeCLI и офисные документы не поддерживаются", "OfficeCLI исключён"} {
		if strings.Contains(unreleased, forbidden) {
			t.Errorf("Unreleased retains superseded OfficeCLI exclusion %q", forbidden)
		}
	}
}

func readRepositoryFile(t *testing.T, path string) string {
	t.Helper()
	root := repositoryRoot(t)
	return readContractFile(t, root, path)
}

func TestFinalV010_PublicationEvidenceRecord(t *testing.T) {
	evidence := readRepositoryFile(t, "docs/releases/v0.1.0.md")

	for _, required := range []string{
		"# Подтверждение публикации v0.1.0",
		"https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.0",
		"4f838e0c701dbd16a006652e74b75cbfa9355370",
		"8f06652c1d3ff97701e0e19b52f22967a7321d9e",
		"https://github.com/mi1man-cmd/kit-all-team/actions/runs/31999757006",
		"https://gitlab.example.invalid/1c/aisuz/ai/-/pipelines/2174086",
		"https://gitlab.example.invalid/1c/aisuz/ai/-/jobs/13355334",
		"`artifacts_expire_at=null`",
		"Коммит документации создан после защищённого тега и не изменяет выпущенные файлы",
		"Защищённый аннотированный тег — сильная идентичность выпуска",
		"GitLab Release и метаданные сохранённого задания могут изменяться",
		"trusted corporate-network evidence недоступно для точного коммита выпуска",
		"проверен только pinned p11 userspace; нативная ALT Linux и QEMU/VM не проверены",
	} {
		if !strings.Contains(evidence, required) {
			t.Errorf("v0.1.0 evidence record does not contain %q", required)
		}
	}

	for _, row := range []string{
		"| `teamkit-v0.1.0-windows-amd64.exe` | 8 082 432 | `b42cd0b46fbfef75e6191973e407be76fede635d7b6a09a2c28364a5462eb331` | [скачать](https://gitlab.example.invalid/1c/aisuz/ai/-/jobs/13355334/artifacts/raw/dist/teamkit-v0.1.0-windows-amd64.exe) |",
		"| `teamkit-v0.1.0-linux-amd64` | 7 745 698 | `376d269e39fd1cee2a88d1ed9cb5e6d2365f6efbc29b1576be6db26446474937` | [скачать](https://gitlab.example.invalid/1c/aisuz/ai/-/jobs/13355334/artifacts/raw/dist/teamkit-v0.1.0-linux-amd64) |",
		"| `teamkit-v0.1.0-darwin-amd64` | 7 886 352 | `b1a6b3b979020e087bf55cdf1be9bed6771e4eda004c5cdd141108d180941ea2` | [скачать](https://gitlab.example.invalid/1c/aisuz/ai/-/jobs/13355334/artifacts/raw/dist/teamkit-v0.1.0-darwin-amd64) |",
		"| `teamkit-v0.1.0-darwin-arm64` | 7 249 970 | `f96ae3370ce95cf13cc5acce691d49990228b6976a0f63c5b2b469f15e165450` | [скачать](https://gitlab.example.invalid/1c/aisuz/ai/-/jobs/13355334/artifacts/raw/dist/teamkit-v0.1.0-darwin-arm64) |",
		"| `SHA256SUMS` | 380 | `efe3e765bb8b552ad46ed0acd94d8df7e25d61c29fdb9afa32e6ec1948d3f555` | [скачать](https://gitlab.example.invalid/1c/aisuz/ai/-/jobs/13355334/artifacts/raw/dist/SHA256SUMS) |",
		"| `SECURITY-AUDIT.json` | 531 | `2792871b44ab04e684aafe234d36ce9f033ea75b456bf22b6ce22d4cdaca7aec` | [скачать](https://gitlab.example.invalid/1c/aisuz/ai/-/jobs/13355334/artifacts/raw/dist/SECURITY-AUDIT.json) |",
		"| `Hermes-Setup.exe` | 7 597 376 | `505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5` | [скачать](https://gitlab.example.invalid/-/project/12087/uploads/0f99502ae0755ee2648473811338b66f/Hermes-Setup.exe) |",
		"| `certs.zip` | 136 410 | `88d85e7e7d64c061c195f93c517500bdc91fccfb9b5a8115da9f6a5a17e689f8` | [скачать](https://gitlab.example.invalid/-/project/12087/uploads/d775983a3143a0556c0d4665896e1b38/certs.zip) |",
	} {
		if !strings.Contains(evidence, row) {
			t.Errorf("v0.1.0 evidence record does not bind an exact file, size, hash and URL: %q", row)
		}
	}

	for _, required := range []string{
		"Authenticode: `Valid`",
		"CN=Nous Research Inc., O=Nous Research Inc., L=Austin, S=Texas, C=US",
		"56B82832D278967F2C13F34C7C5C6518BA3BF120",
	} {
		if !strings.Contains(evidence, required) {
			t.Errorf("v0.1.0 evidence record does not contain Hermes signer evidence %q", required)
		}
	}

	for _, document := range []struct {
		path string
		link string
	}{
		{path: "docs/INSTALL.md", link: "[Подтверждение публикации v0.1.0](releases/v0.1.0.md)"},
	} {
		current := markdownSection(t, readRepositoryFile(t, document.path), "## Актуальный выпуск v0.1.0")
		if !strings.Contains(current, document.link) {
			t.Errorf("%s current release section does not link the durable evidence record", document.path)
		}
		for _, prospective := range []string{"будут записаны после выпуска", "появятся после публикации"} {
			if strings.Contains(current, prospective) {
				t.Errorf("%s still describes published evidence prospectively: %q", document.path, prospective)
			}
		}
	}

	checklist := readRepositoryFile(t, "docs/RELEASE-CHECKLIST.md")
	for _, completed := range []string{
		"- [x] Exact release SHA одинаково опубликован в GitHub `main` и GitLab `master`.",
		"- [x] GitHub exact-SHA validation и GitLab pipeline/job завершены успешно для одного SHA; шесть файлов сравнены побайтно.",
		"- [x] GitLab job сохранён без срока удаления артефактов; защищённый тег `v0.1.0` и GitLab Release указывают на exact SHA.",
		"- [x] Итоговые SHA, CI run, GitLab pipeline/job и ссылки записаны без подмены RC2 в [`docs/releases/v0.1.0.md`](releases/v0.1.0.md).",
	} {
		if !strings.Contains(checklist, completed) {
			t.Errorf("v0.1.0 release checklist does not record completed publication step %q", completed)
		}
	}
}

func TestFinalV010_UserDocsStateLimitationsWithoutInventedEvidence(t *testing.T) {
	for _, path := range []string{"docs/INSTALL.md"} {
		t.Run(path, func(t *testing.T) {
			current := markdownSection(t, readRepositoryFile(t, path), "## Актуальный выпуск v0.1.0")
			for _, required := range []string{
				"teamkit v0.1.0 (unsigned internal release)",
				"приватн",
				"https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.0",
				"teamkit-v0.1.0-windows-amd64.exe",
				"teamkit-v0.1.0-linux-amd64",
				"teamkit-v0.1.0-darwin-amd64",
				"teamkit-v0.1.0-darwin-arm64",
				"SHA256SUMS",
				"GitHub-hosted runner не разрешает внутренний DNS `gitlab.tools.enterprise.ru`",
				"self-hosted runner внутри корпоративной сети/VPN",
				"только в pinned p11 userspace",
				"не подтверждена на нативной ALT Linux или в QEMU/VM",
				"бинарники Team Kit не подписаны",
				"macOS не подписаны Apple и не notarized",
				"ручной графический мастер",
				"не доказывают автоматическую или unattended-установку",
				"офисные документы не поддерживаются",
				"docs/releases/v0.1.0.md",
			} {
				if !strings.Contains(current, required) {
					t.Errorf("current v0.1.0 section does not contain %q", required)
				}
			}
			for _, forbidden := range []string{
				"текущий внутренний кандидат",
				"ещё не выпущена",
				"/-/jobs/",
			} {
				if strings.Contains(current, forbidden) {
					t.Errorf("current v0.1.0 section contains prospective or invented evidence %q", forbidden)
				}
			}
		})
	}

	changelog := readRepositoryFile(t, "CHANGELOG.md")
	final := markdownSection(t, changelog, "## v0.1.0 — 2026-08-17")
	for _, required := range []string{
		"Добавить новое окружение",
		"Обновить существующее окружение",
		"локальный MRU хранит только абсолютные пути",
		"cc_1c_skills от Широкова",
		"ai_rules_1c от Филиппова",
		"trusted corporate network probe",
		"ALT Linux",
		"неподписан",
	} {
		if !strings.Contains(final, required) {
			t.Errorf("v0.1.0 changelog section does not contain %q", required)
		}
	}
	unreleased := markdownSection(t, changelog, "## Unreleased")
	if strings.Contains(unreleased, "Добавить новое окружение") || strings.Contains(unreleased, "финальный `v0.1.0`") {
		t.Error("released v0.1.0 changes remain in Unreleased")
	}
}

func TestFinalV010_CurrentWindowsGuidePrecedesRC2Archive(t *testing.T) {
	for _, path := range []string{"docs/INSTALL.md"} {
		t.Run(path, func(t *testing.T) {
			text := strings.ReplaceAll(readRepositoryFile(t, path), "\r\n", "\n")
			const currentHeading = "## Установка v0.1.0 в Windows: пошагово"
			currentStart := strings.Index(text, currentHeading)
			historicalStart := strings.Index(text, "## Исторический архив RC2")
			if currentStart < 0 {
				t.Fatal("complete current v0.1.0 Windows guide is missing")
			}
			if historicalStart <= currentStart {
				t.Fatal("current v0.1.0 Windows guide must precede the historical RC2 archive")
			}
			current := text[currentStart:historicalStart]

			for _, required := range []string{
				"https://gitlab.example.invalid/1c/aisuz/ai/-/jobs/13355334/artifacts/raw/dist/teamkit-v0.1.0-windows-amd64.exe",
				"teamkit-v0.1.0-windows-amd64.exe",
				"b42cd0b46fbfef75e6191973e407be76fede635d7b6a09a2c28364a5462eb331",
				"PowerShell",
				`& 'C:\TeamKitInstaller\teamkit-v0.1.0-windows-amd64.exe' --version`,
				`"version":"v0.1.0"`,
				`"commit":"8f06652c1d3ff97701e0e19b52f22967a7321d9e"`,
				"НЕ запускайте и не переименовывайте `teamkit-v0.1.0-rc.2-windows-amd64.exe`",
				`& 'C:\TeamKitInstaller\teamkit-v0.1.0-windows-amd64.exe' apply`,
				"1. `Добавить новое окружение`",
				"2. `Обновить существующее окружение`",
				"Hermes уже установлен",
				"Hermes ещё не установлен",
				"первый `hermes` в `PATH`",
				"не задаёт вопрос `AI-приложение уже установлено?`",
				"не просит вводить `HERMES_HOME`",
				"официального сайта Hermes",
				"снова запустите `apply`",
				"только для автоматизации, администрирования или исключительной ситуации",
				"показывает RC2",
			} {
				if !strings.Contains(current, required) {
					t.Errorf("current Windows guide does not contain %q", required)
				}
			}
			for _, forbidden := range []string{
				`& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' --version`,
				`& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply`,
				"будут записаны после публикации",
				"появятся после публикации",
			} {
				if strings.Contains(current, forbidden) {
					t.Errorf("current Windows guide contains obsolete instruction %q", forbidden)
				}
			}
			if !strings.Contains(text[historicalStart:], "teamkit-v0.1.0-rc.2-windows-amd64.exe") {
				t.Fatal("historical RC2 archive lost its exact Windows executable")
			}
			for _, prospective := range []string{"будут записаны после публикации", "появятся после публикации"} {
				if strings.Contains(text, prospective) {
					t.Errorf("published documentation still describes evidence prospectively: %q", prospective)
				}
			}
		})
	}
}

func TestFinalV010_ProfileSecretDACLRecoveryUsesRetry(t *testing.T) {
	for _, path := range []string{"docs/INSTALL.md"} {
		t.Run(path, func(t *testing.T) {
			text := strings.ReplaceAll(readRepositoryFile(t, path), "\r\n", "\n")
			for _, required := range []string{
				"ACTION_FAILED 50-configure-application: SECRET_FILE_PERMISSIONS_UNSAFE",
				`HERMES_HOME\profiles\<identity>\.env`,
				`G:\.hermes\profiles\1c-apa-developer-cc_1c_skills\.env`,
				"`config.yaml` ещё не создан",
				"останавливается до рендеринга профиля",
				"`needs_apply`",
				"`50-configure-application`",
				"`90-verify-state`",
				"SetAccessRuleProtection($true, $false)",
				"FullControl",
				`& 'C:\TeamKitInstaller\teamkit-v0.1.0-windows-amd64.exe' retry --kit-home 'C:\TeamKit\apa'`,
				"не запускайте `apply` повторно",
				"не удаляйте `.env`",
				"не создавайте MCP",
				"перезапустите Hermes",
				"выберите профиль",
			} {
				if !strings.Contains(text, required) {
					t.Errorf("profile-secret recovery does not contain %q", required)
				}
			}
			if strings.Contains(text, "Это не ошибка установки Team Kit") {
				t.Error("documentation incorrectly says the Action 50 failure is unrelated to Team Kit installation")
			}
		})
	}
}

func TestDocumentation_DescribesFinalHermesDetectionAndPreservesRC2History(t *testing.T) {
	root := repositoryRoot(t)
	for _, document := range []struct {
		path          string
		wizardHeading string
	}{
		{path: "docs/INSTALL.md", wizardHeading: "## Мастер v0.1.0: пошаговый выбор add/update"},
	} {
		t.Run(document.path, func(t *testing.T) {
			text := readContractFile(t, root, document.path)
			const heading = "## Актуальный выпуск v0.1.0"
			start := strings.Index(text, heading)
			if start < 0 {
				t.Fatal("current final Hermes section missing")
			}
			section := text[start:]
			if next := strings.Index(section[len(heading):], "\n## "); next >= 0 {
				section = section[:len(heading)+next]
			}
			for _, required := range []string{
				"teamkit v0.1.0 (unsigned internal release)",
				"финальный",
				"### Hermes уже установлен",
				"### Hermes нужно установить",
				"Hermes определяется автоматически",
				"не спрашивает",
				"HERMES_HOME",
				">= 0.20.1 и < 0.21.0",
				"Hermes-Setup.exe` не нужен",
				"официального сайта",
				"HERMES_HOME_AUTO_DETECT_FAILED",
				"HERMES_VERSION_UNSUPPORTED",
				"--hermes-home",
				"--app-installed",
				"только для автоматизации, администрирования или исключительной ситуации",
			} {
				if !strings.Contains(section, required) {
					t.Errorf("current Hermes section missing %q", required)
				}
			}
			installed := strings.Index(section, "### Hermes уже установлен")
			needsInstall := strings.Index(section, "### Hermes нужно установить")
			if installed < 0 || needsInstall <= installed {
				t.Fatal("current Hermes section must branch from installed to needs-install")
			}
			installedSection := section[installed:needsInstall]
			for _, forbidden := range []string{
				"Hermes Agent v0.20.1 (2026.8.13)",
				"ответьте `1. Да`",
				"укажите тот же `HERMES_HOME`",
			} {
				if strings.Contains(installedSection, forbidden) {
					t.Errorf("current installed-Hermes path retains obsolete manual instruction %q", forbidden)
				}
			}
			if strings.Contains(section, `teamkit-v0.1.0-rc.2-windows-amd64.exe' apply`) {
				t.Error("current section tells users to run RC2 for auto-detection")
			}
			wizardStart := strings.Index(text, document.wizardHeading)
			if wizardStart < 0 {
				t.Fatalf("current wizard section %q is missing", document.wizardHeading)
			}
			currentUnreleased := section + "\n" + text[wizardStart:]
			for _, required := range []string{
				"В Windows установка Hermes остаётся ручной",
				"В macOS, Linux и ALT Linux Team Kit выполняет управляемую автоматическую установку Hermes из закреплённого исходного кода",
				"Вопрос `AI-приложение уже установлено` задаётся только для приложения не Hermes",
				"Для Hermes мастер не запрашивает `HERMES_HOME`",
			} {
				if !strings.Contains(currentUnreleased, required) {
					t.Errorf("current unreleased documentation missing %q", required)
				}
			}
			for _, line := range strings.Split(currentUnreleased, "\n") {
				if strings.Contains(line, "AI-приложение уже установлено") && !strings.Contains(line, "не Hermes") {
					t.Errorf("current unreleased documentation presents installed question as unconditional: %q", line)
				}
			}
			for _, forbidden := range []string{
				"- `HERMES_HOME` — только для Hermes",
				"Для Hermes требуется также `--hermes-home`",
				"Когда мастер попросит `HERMES_HOME`",
				"укажите тот же `HERMES_HOME`",
			} {
				if strings.Contains(currentUnreleased, forbidden) {
					t.Errorf("current unreleased documentation retains manual Hermes prompt %q", forbidden)
				}
			}

			const historicalHeading = "## Историческая инструкция: опубликованный v0.1.0-rc.2 в Windows"
			historicalStart := strings.Index(text, historicalHeading)
			if historicalStart < 0 {
				t.Fatal("published RC2 Windows instructions are not explicitly historical")
			}
			if historicalStart >= wizardStart {
				t.Fatal("historical RC2 instructions must precede current wizard documentation")
			}
			historical := text[historicalStart:wizardStart]
			for _, required := range []string{
				`teamkit-v0.1.0-rc.2-windows-amd64.exe' apply`,
				"Hermes Agent v0.20.1 (2026.8.13)",
				"тот же `HERMES_HOME`",
			} {
				if !strings.Contains(historical, required) {
					t.Errorf("historical RC2 instructions missing %q", required)
				}
			}
		})
	}
}

func TestInstallGuide_LinksCorporateLLMPreparation(t *testing.T) {
	root := repositoryRoot(t)
	text := readContractFile(t, root, "docs/INSTALL.md")
	for _, fragment := range []string{
		`[«Начало работы»](https://docs.example.invalid/spaces/IDP/pages/1017637995/%D0%9F%D0%BE%D0%B4%D0%BA%D0%BB%D1%8E%D1%87%D0%B5%D0%BD%D0%B8%D0%B5+%D0%BA+LLM+%D1%87%D0%B5%D1%80%D0%B5%D0%B7+API+IDE+SDK#id-%D0%9F%D0%BE%D0%B4%D0%BA%D0%BB%D1%8E%D1%87%D0%B5%D0%BD%D0%B8%D0%B5%D0%BALLM%D1%87%D0%B5%D1%80%D0%B5%D0%B7API(IDE,SDK)-%D0%9F%D0%B5%D1%80%D0%B5%D0%B4%D0%BF%D0%BE%D0%B4%D0%BA%D0%BB%D1%8E%D1%87%D0%B5%D0%BD%D0%B8%D0%B5%D0%BALLM%D1%83%D0%B1%D0%B5%D0%B4%D0%B8%D1%82%D0%B5%D1%81%D1%8C,%D1%87%D1%82%D0%BE%D1%83%D0%B2%D0%B0%D1%81%D0%B2%D1%8B%D0%BF%D0%BE%D0%BB%D0%BD%D0%B5%D0%BD%D1%8B%D0%B4%D0%B5%D0%B9%D1%81%D1%82%D0%B2%D0%B8%D1%8F,%D0%BE%D0%BF%D0%B8%D1%81%D0%B0%D0%BD%D1%8B%D0%B5%D0%B2%D0%B8%D0%BD%D1%81%D1%82%D1%80%D1%83%D0%BA%D1%86%D0%B8%D0%B8)`,
		"моделью `generic-development`",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("installation guide does not contain corporate LLM preparation fragment %q", fragment)
		}
	}
}

func TestInstallGuide_VerifiesAndExecutesOnlyTheSelectedUnixBinary_RC2(t *testing.T) {
	root := repositoryRoot(t)
	text := readContractFile(t, root, "docs/INSTALL.md")
	for _, platform := range []struct {
		name     string
		required []string
	}{
		{
			name: "Linux",
			required: []string{
				`artifact="teamkit-v0.1.0-rc.2-linux-amd64"`,
				`expected="ba634fc2c760b4c2e144dc7cd457e9d19c06b87bb8705aa21e5947264f945ea3"`,
				`sha256sum --check --strict`,
			},
		},
		{
			name: "macOS Apple Silicon",
			required: []string{
				`artifact="teamkit-v0.1.0-rc.2-darwin-arm64"`,
				`expected="972b3cb259440834bd10c65b5987061b97d9cb9db975aae36cf43e66f0bc3814"`,
				`shasum -a 256 "$artifact"`,
			},
		},
	} {
		t.Run(platform.name, func(t *testing.T) {
			for _, required := range platform.required {
				if !strings.Contains(text, required) {
					t.Errorf("installation guide does not contain selected-binary step %q", required)
				}
			}
		})
	}
	for _, required := range []string{`chmod 700 "$artifact"`, `./"$artifact" --version`} {
		if !strings.Contains(text, required) {
			t.Errorf("installation guide does not contain selected-binary step %q", required)
		}
	}
	if strings.Contains(text, "sha256sum --check --strict SHA256SUMS") {
		t.Error("installation guide checks all manifest entries even when only one binary was downloaded")
	}
}

func TestWindowsInstructions_RequirePowerShellAndUsePasteSafeChecksum_RC2(t *testing.T) {
	root := repositoryRoot(t)

	for _, path := range []string{"docs/INSTALL.md"} {
		t.Run(path, func(t *testing.T) {
			text := strings.ReplaceAll(readContractFile(t, root, path), "\r\n", "\n")
			windowsHeading := "## Историческая инструкция: опубликованный v0.1.0-rc.2 в Windows"
			windowsStart := strings.Index(text, windowsHeading)
			if windowsStart < 0 {
				t.Fatal("Windows section is missing")
			}
			windowsJourney := text[windowsStart:]
			if end := strings.Index(windowsJourney[len(windowsHeading):], "\n## "); end >= 0 {
				windowsJourney = windowsJourney[:len(windowsHeading)+end]
			}
			firstPowerShellBlock := strings.Index(windowsJourney, "```powershell")
			if firstPowerShellBlock < 0 {
				t.Fatal("Windows PowerShell block is missing")
			}
			preamble := windowsJourney[:firstPowerShellBlock]
			for _, fragment := range []string{
				"именно PowerShell",
				`PS C:\TeamKitInstaller>`,
				`C:\TeamKitInstaller>`,
				"cmd.exe",
				"`cmd.exe` не подходит",
				"`$file`",
				"`Get-FileHash`",
				"`if (...) { ... }`",
				"`powershell`",
				"многострочной вставке",
			} {
				if !strings.Contains(preamble, fragment) {
					t.Errorf("Windows preamble does not contain %q", fragment)
				}
			}
			windowsIntro := windowsJourney
			if hermesStart := strings.Index(windowsIntro, "### A. Hermes уже установлен"); hermesStart >= 0 {
				windowsIntro = windowsIntro[:hermesStart]
			}
			checksumLine := singleLinePowerShellCommand(t, windowsIntro, `$file = 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe'`)
			for _, fragment := range []string{
				"Test-Path -LiteralPath $file -PathType Leaf",
				"0d3d8baa48fe1ecc42518793a8451081fa9fa454223f45bd945a4908d6b22711",
				"Get-FileHash -LiteralPath $file -Algorithm SHA256",
				"if ($actual -ne $expected)",
				"Write-Host",
			} {
				if !strings.Contains(checksumLine, fragment) {
					t.Errorf("single-line Windows checksum command does not contain %q", fragment)
				}
			}
			if strings.Index(checksumLine, "Test-Path") > strings.Index(checksumLine, "Get-FileHash") {
				t.Error("Windows checksum command hashes the file before checking that it exists")
			}
			for _, command := range []string{
				`Set-Location -LiteralPath 'C:\TeamKitInstaller'`,
				`& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' --version`,
				`& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply`,
			} {
				powerShellFenceContaining(t, windowsJourney, command)
			}
		})
	}

	readme := strings.ReplaceAll(readContractFile(t, root, "README.md"), "\r\n", "\n")
	for _, fragment := range []string{
		`Set-Location -LiteralPath 'C:\TeamKitInstaller'`,
		"HERMES_EXECUTABLE_UNVERIFIED",
		"LOCAL_CHANGES_DETECTED",
		"FOREIGN_WORKSPACE",
	} {
		if !strings.Contains(readme, fragment) {
			t.Errorf("README does not contain current Windows troubleshooting fragment %q", fragment)
		}
	}
}

func TestLineContaining_SelectsCommandInsidePowerShellFence(t *testing.T) {
	const marker = `$file = 'C:\TeamKitInstaller\teamkit.exe'`
	fixture := marker + " outside a code block\n\n```powershell\n" + marker + "; Test-Path -LiteralPath $file\n```\n"

	line := lineContaining(t, fixture, marker)
	if !strings.Contains(line, "Test-Path -LiteralPath $file") {
		t.Error("selected command marker outside its PowerShell fence")
	}
}

func TestWindowsJourney_OffersThreeExclusiveScenarios_RC2(t *testing.T) {
	root := repositoryRoot(t)
	for _, path := range []string{"docs/INSTALL.md"} {
		t.Run(path, func(t *testing.T) {
			text := strings.ReplaceAll(readContractFile(t, root, path), "\r\n", "\n")
			heading := "## Историческая инструкция: опубликованный v0.1.0-rc.2 в Windows"
			start := strings.Index(text, heading)
			if start < 0 {
				t.Fatal("Windows journey is missing")
			}
			journey := text[start+len(heading):]
			if end := strings.Index(journey, "\n## "); end >= 0 {
				journey = journey[:end]
			}

			headings := []string{
				"### A. Hermes уже установлен",
				"### B. Hermes ещё не установлен",
				"### C. Другое AI-приложение",
			}
			positions := make([]int, len(headings))
			missing := false
			for i, scenarioHeading := range headings {
				positions[i] = strings.Index(journey, scenarioHeading)
				if positions[i] < 0 {
					t.Errorf("Windows journey does not contain scenario heading %q", scenarioHeading)
					missing = true
				}
			}
			for _, link := range []string{
				"[A. Hermes уже установлен](#a-hermes-уже-установлен)",
				"[B. Hermes ещё не установлен](#b-hermes-ещё-не-установлен)",
				"[C. Другое AI-приложение](#c-другое-ai-приложение)",
			} {
				if !strings.Contains(journey, link) {
					t.Errorf("Windows journey does not contain scenario link %q", link)
				}
			}
			if missing {
				return
			}
			if !(positions[0] < positions[1] && positions[1] < positions[2]) {
				t.Fatal("Windows scenario headings are out of order")
			}
			for _, problem := range scenarioLinkPlacementProblems(journey) {
				t.Error(problem)
			}

			common := journey[:positions[0]]
			for _, fragment := range []string{
				`C:\TeamKitInstaller`,
				"именно PowerShell",
				`PS C:\TeamKitInstaller>`,
				"cmd.exe",
				`Set-Location -LiteralPath 'C:\TeamKitInstaller'`,
				"Get-FileHash -LiteralPath $file -Algorithm SHA256",
				`& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' --version`,
				"только к выбранному сценарию",
			} {
				if !strings.Contains(common, fragment) {
					t.Errorf("common Windows steps do not contain %q", fragment)
				}
			}
			for _, forbidden := range []string{"Hermes-Setup.exe", "certs.zip", " apply"} {
				if strings.Contains(common, forbidden) {
					t.Errorf("common Windows steps contain scenario-specific fragment %q", forbidden)
				}
			}
			if count := strings.Count(journey, `Set-Location -LiteralPath 'C:\TeamKitInstaller'`); count != 1 {
				t.Errorf("Windows journey must contain exactly one Set-Location command, got %d", count)
			}
			if count := strings.Count(journey, `$file = 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe'`); count != 1 {
				t.Errorf("Windows journey must contain exactly one Team Kit checksum command, got %d", count)
			}
			if count := strings.Count(journey, `& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' --version`); count != 1 {
				t.Errorf("Windows journey must contain exactly one Team Kit version command, got %d", count)
			}

			scenarioA := journey[positions[0]:positions[1]]
			scenarioB := journey[positions[1]:positions[2]]
			scenarioC := journey[positions[2]:]
			for _, fragment := range []string{"Hermes-Setup.exe", "скачивать его не требуется", "нужен `certs.zip`", "Hermes Agent v0.20.1 (2026.8.13)", "Hermes уже установлен", "тот же `HERMES_HOME`"} {
				if !strings.Contains(scenarioA, fragment) {
					t.Errorf("installed-Hermes scenario does not contain %q", fragment)
				}
			}
			aVersion := strings.Index(scenarioA, "Hermes Agent v0.20.1 (2026.8.13)")
			aApply := strings.Index(scenarioA, `& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply`)
			if aVersion < 0 || aApply < aVersion {
				t.Error("installed-Hermes scenario must verify runtime before Team Kit apply")
			}
			singleLinePowerShellCommand(t, scenarioA, "hermes --version")
			singleLinePowerShellCommand(t, scenarioA, `& '<HERMES_HOME>\hermes-agent\venv\Scripts\hermes.exe' --version`)
			singleLinePowerShellCommand(t, scenarioA, `& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply`)

			for _, fragment := range []string{"официального сайта", "Assets", "Other", "нужен `certs.zip`", `C:\TeamKitInstaller\Hermes-Setup.exe`, "Get-AuthenticodeSignature", "графического мастера до конца", "Hermes Agent v0.20.1 (2026.8.13)", "Hermes уже установлен"} {
				if !strings.Contains(scenarioB, fragment) {
					t.Errorf("missing-Hermes scenario does not contain %q", fragment)
				}
			}
			bPreparation := strings.Index(scenarioB, "До начала установки")
			bSignature := strings.Index(scenarioB, "Get-AuthenticodeSignature")
			bGUI := strings.Index(scenarioB, "графического мастера до конца")
			bVersion := strings.Index(scenarioB, "Hermes Agent v0.20.1 (2026.8.13)")
			bApply := strings.Index(scenarioB, `& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply`)
			if bPreparation < 0 || bSignature < bPreparation || bGUI < bSignature || bVersion < bGUI || bApply < bVersion {
				t.Error("missing-Hermes scenario must verify installer, finish GUI, verify runtime, then apply")
			}

			for _, fragment := range []string{"Hermes-Setup.exe", "`certs.zip` не нужны", "должно быть установлено", `& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply`, "выберите своё приложение"} {
				if !strings.Contains(scenarioC, fragment) {
					t.Errorf("alternative-app scenario does not contain %q", fragment)
				}
			}
			singleLinePowerShellCommand(t, scenarioC, `& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply`)
		})
	}
}

func TestScenarioLinkPlacementContract_RejectsEarlyLinks(t *testing.T) {
	fixture := strings.Join([]string{
		"Процесс: сначала выполните общую часть, затем выберите один сценарий.",
		"[A. Hermes уже установлен](#a-hermes-уже-установлен)",
		"[B. Hermes ещё не установлен](#b-hermes-ещё-не-установлен)",
		"[C. Другое AI-приложение](#c-другое-ai-приложение)",
		"",
		"```powershell",
		`& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' --version`,
		"```",
		"",
		"Сначала завершите все общие шаги выше; затем выберите ровно один сценарий.",
		"",
		"### A. Hermes уже установлен",
	}, "\n")
	if problems := scenarioLinkPlacementProblems(fixture); len(problems) == 0 {
		t.Error("scenario link placement contract accepted links before the common version step")
	}
}

func scenarioLinkPlacementProblems(journey string) []string {
	var problems []string
	introEnd := strings.Index(journey, "```powershell")
	if introEnd < 0 {
		return []string{"Windows journey does not contain a common PowerShell block"}
	}
	intro := journey[:introEnd]
	for _, fragment := range []string{"сначала выполните общую часть", "затем выберите один сценарий"} {
		if !strings.Contains(intro, fragment) {
			problems = append(problems, "Windows intro does not explain common-then-scenario flow: "+fragment)
		}
	}

	const versionCommand = `& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' --version`
	versionPosition := strings.Index(journey, versionCommand)
	if versionPosition < 0 {
		return append(problems, "Windows journey does not contain the common Team Kit version command")
	}
	closeOffset := strings.Index(journey[versionPosition:], "\n```")
	if closeOffset < 0 {
		return append(problems, "Team Kit version PowerShell fence is not closed")
	}
	afterVersionFence := versionPosition + closeOffset + len("\n```")
	firstScenario := strings.Index(journey, "### A. Hermes уже установлен")
	if firstScenario < 0 {
		return append(problems, "Windows journey does not contain the first scenario heading")
	}

	const instruction = "Сначала завершите все общие шаги выше; затем выберите ровно один сценарий"
	instructionPosition := strings.Index(journey, instruction)
	if instructionPosition < afterVersionFence || instructionPosition > firstScenario {
		problems = append(problems, "scenario selection instruction must follow the common version fence")
	}
	for _, link := range []string{
		"[A. Hermes уже установлен](#a-hermes-уже-установлен)",
		"[B. Hermes ещё не установлен](#b-hermes-ещё-не-установлен)",
		"[C. Другое AI-приложение](#c-другое-ai-приложение)",
	} {
		position := strings.Index(journey, link)
		if position < afterVersionFence || position < instructionPosition || position > firstScenario {
			problems = append(problems, "actionable scenario link must follow common steps and precede scenarios: "+link)
		}
	}
	return problems
}

func TestMissingHermesScenario_DescribesManualVerifiedInstall_RC2(t *testing.T) {
	root := repositoryRoot(t)

	for _, path := range []string{"docs/INSTALL.md"} {
		t.Run(path, func(t *testing.T) {
			text := strings.ReplaceAll(readContractFile(t, root, path), "\r\n", "\n")
			const historicalHeading = "## Историческая инструкция: опубликованный v0.1.0-rc.2 в Windows"
			historicalStart := strings.Index(text, historicalHeading)
			if historicalStart < 0 {
				t.Fatal("historical RC2 Windows journey is missing")
			}
			text = text[historicalStart:]
			const heading = "### B. Hermes ещё не установлен"
			start := strings.Index(text, heading)
			if start < 0 {
				t.Fatal("missing-Hermes Windows scenario is missing")
			}
			section := text[start:]
			if end := strings.Index(section[len(heading):], "\n### "); end >= 0 {
				section = section[:len(heading)+end]
			}

			hermesChecksum := singleLinePowerShellCommand(t, section, `$hermesInstaller = 'C:\TeamKitInstaller\Hermes-Setup.exe'`)
			for _, fragment := range []string{
				"Test-Path -LiteralPath $hermesInstaller -PathType Leaf",
				"505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5",
				"Get-FileHash -LiteralPath $hermesInstaller -Algorithm SHA256",
			} {
				if !strings.Contains(hermesChecksum, fragment) {
					t.Errorf("Hermes checksum PowerShell command does not contain %q", fragment)
				}
			}
			if strings.Index(hermesChecksum, "Test-Path") > strings.Index(hermesChecksum, "Get-FileHash") {
				t.Error("Hermes checksum command hashes the installer before checking that it exists")
			}
			signatureCommand := singleLinePowerShellCommand(t, section, `$signature = Get-AuthenticodeSignature`)
			for _, problem := range hermesSignatureCommandProblems(signatureCommand) {
				t.Error(problem)
			}
			singleLinePowerShellCommand(t, section, `& '<HERMES_HOME>\hermes-agent\venv\Scripts\hermes.exe' --version`)
			singleLinePowerShellCommand(t, section, `& 'C:\TeamKitInstaller\teamkit-v0.1.0-rc.2-windows-amd64.exe' apply`)
			for _, fragment := range []string{
				"Copy",
				"копирует только этот блок",
				"Выполняйте блоки по порядку",
				"не повторяет проверку SHA-256",
			} {
				if !strings.Contains(section, fragment) {
					t.Errorf("Hermes Windows Copy guidance does not contain %q", fragment)
				}
			}
			copyGuidance := strings.Index(section, "Кнопка Copy")
			shaStep := strings.Index(section, "проверьте SHA-256")
			signatureStep := strings.Index(section, "проверьте Authenticode-подпись")
			if copyGuidance < 0 || shaStep < copyGuidance || signatureStep < shaStep {
				t.Error("Hermes Copy guidance must appear immediately before the ordered SHA and signature steps")
			}

			ordered := []string{
				"официального сайта",
				"Assets",
				"Other",
				`C:\TeamKitInstaller\Hermes-Setup.exe`,
				"проверьте SHA-256",
				"проверьте Authenticode-подпись",
				"двойным щелчком",
				"графического мастера до конца",
				"Team Kit не устанавливает Hermes автоматически",
				"`hermes --version`",
				`<HERMES_HOME>\hermes-agent\venv\Scripts\hermes.exe`,
				"Hermes Agent v0.20.1 (2026.8.13)",
				"запустите Team Kit",
				"Hermes уже установлен",
				"`HERMES_EXECUTABLE_UNVERIFIED`",
			}
			last := -1
			for _, fragment := range ordered {
				index := strings.Index(section, fragment)
				if index < 0 {
					t.Errorf("Hermes Windows section does not contain %q", fragment)
					continue
				}
				if index < last {
					t.Errorf("Hermes Windows step %q is out of order", fragment)
				}
				last = index
			}

			for _, fragment := range []string{
				`C:\TeamKitInstaller\Hermes-Setup.exe`,
				"505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5",
				"Get-FileHash -LiteralPath $hermesInstaller",
				"Get-AuthenticodeSignature -LiteralPath $hermesInstaller",
				"Status: `Valid`",
				"Publisher",
				"Nous Research Inc.",
				"не запускайте установщик",
				"Team Kit не устанавливает Hermes автоматически",
				`& '<HERMES_HOME>\hermes-agent\venv\Scripts\hermes.exe' --version`,
				"фактический путь",
				"`0.20.0` не подходит",
				"тот же `HERMES_HOME`",
			} {
				if !strings.Contains(section, fragment) {
					t.Errorf("Hermes Windows section does not contain safety fragment %q", fragment)
				}
			}
		})
	}

	troubleshooting := readContractFile(t, root, "README.md")
	for _, fragment := range []string{
		">= 0.20.1 и < 0.21.0",
		"первый `hermes` в `PATH`",
		"--hermes-home",
		"автоматизации, администрирования или исключительной ситуации",
	} {
		if !strings.Contains(troubleshooting, fragment) {
			t.Errorf("HERMES_EXECUTABLE_UNVERIFIED explanation does not contain %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"Hermes Agent v0.20.1 (2026.8.13)",
		"укажите тот же `HERMES_HOME`",
	} {
		if strings.Contains(troubleshooting, forbidden) {
			t.Errorf("current troubleshooting retains obsolete manual instruction %q", forbidden)
		}
	}
}

func TestHermesSignatureCommandContract_RejectsUnsafeMutations(t *testing.T) {
	const absolutePath = `$hermesInstaller = 'C:\TeamKitInstaller\Hermes-Setup.exe'`
	const requiredTail = `$signature = Get-AuthenticodeSignature -LiteralPath $hermesInstaller; $signature | Select-Object Status, Publisher; if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'Nous Research Inc\.') { throw 'invalid' }`
	const validPrefix = absolutePath + `; if (-not (Test-Path -LiteralPath $hermesInstaller -PathType Leaf)) { throw 'Файл не найден' }; ` + requiredTail
	tests := map[string]string{
		"path assigned after existence check": `if (-not (Test-Path -LiteralPath $hermesInstaller -PathType Leaf)) { throw 'Файл не найден' }; ` + absolutePath + `; ` + requiredTail,
		"Get-FileHash duplicated":             validPrefix + `; Get-FileHash -LiteralPath $hermesInstaller`,
		"expected hash variable duplicated":   validPrefix + `; $expected = 'unexpected'`,
		"pinned installer hash duplicated":    validPrefix + `; Write-Host '505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5'`,
	}
	for name, command := range tests {
		t.Run(name, func(t *testing.T) {
			if problems := hermesSignatureCommandProblems(command); len(problems) == 0 {
				t.Error("unsafe signature command satisfied the documentation contract")
			}
		})
	}
}

func hermesSignatureCommandProblems(command string) []string {
	var problems []string
	const assignment = `$hermesInstaller = 'C:\TeamKitInstaller\Hermes-Setup.exe'`
	for _, fragment := range []string{
		assignment,
		"Test-Path -LiteralPath $hermesInstaller -PathType Leaf",
		"Файл не найден",
		"Get-AuthenticodeSignature -LiteralPath $hermesInstaller",
		"$signature.Status -ne 'Valid'",
		"Nous Research Inc\\.",
		"Publisher",
	} {
		if !strings.Contains(command, fragment) {
			problems = append(problems, "Hermes signature PowerShell command does not contain "+fragment)
		}
	}
	assignmentPosition := strings.Index(command, assignment)
	existencePosition := strings.Index(command, "Test-Path")
	signaturePosition := strings.Index(command, "Get-AuthenticodeSignature")
	if assignmentPosition >= 0 && existencePosition >= 0 && signaturePosition >= 0 &&
		!(assignmentPosition < existencePosition && existencePosition < signaturePosition) {
		problems = append(problems, "Hermes signature command must assign the absolute path, check existence, then inspect Authenticode")
	}
	for _, forbidden := range []string{
		"Get-FileHash",
		"$expected",
		"505dfb4c2c1052b055e3fc694a76cb7ce093a64962c7713aa294f5549c6734f5",
	} {
		if strings.Contains(command, forbidden) {
			problems = append(problems, "Hermes signature PowerShell command duplicates SHA material "+forbidden)
		}
	}
	return problems
}

func lineContaining(t *testing.T, text, marker string) string {
	t.Helper()
	block := powerShellFenceContaining(t, text, marker)
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	t.Fatalf("PowerShell fence does not contain command line marker %q", marker)
	return ""
}

func singleLinePowerShellCommand(t *testing.T, text, marker string) string {
	t.Helper()
	block := strings.TrimSpace(powerShellFenceContaining(t, text, marker))
	lines := strings.Split(block, "\n")
	if len(lines) != 1 {
		t.Fatalf("PowerShell command %q must occupy one logical line, got %d", marker, len(lines))
	}
	return lines[0]
}

func powerShellFenceContaining(t *testing.T, text, marker string) string {
	t.Helper()
	const opening = "```powershell\n"
	searchFrom := 0
	for searchFrom < len(text) {
		openOffset := strings.Index(text[searchFrom:], opening)
		if openOffset < 0 {
			break
		}
		contentStart := searchFrom + openOffset + len(opening)
		closeOffset := strings.Index(text[contentStart:], "\n```")
		if closeOffset < 0 {
			t.Fatal("unterminated PowerShell fence")
		}
		contentEnd := contentStart + closeOffset
		block := text[contentStart:contentEnd]
		if strings.Contains(block, marker) {
			return block
		}
		searchFrom = contentEnd + len("\n```")
	}
	t.Fatalf("PowerShell fence does not contain %q", marker)
	return ""
}
