package cli

import (
	"context"
	"fmt"
	"io"
)

func (r Runner) runUserCheck(ctx context.Context) int {
	q := newQuestionnaire(r.In, r.Out)
	for _, section := range []struct {
		prompt  string
		choices []choice
	}{{"Операционные системы", operatingSystemChoices()}, {"AI-приложения", applicationChoices()}} {
		if err := writeChoices(r.Out, section.prompt, section.choices); err != nil {
			return r.fail(options{}, err, nil)
		}
	}
	for _, app := range applicationChoices() {
		if app.value != "hermes" {
			if err := writeUserCheckLine(r.Out, fmt.Sprintf("%s: не установлен", app.label)); err != nil {
				return r.fail(options{}, err, nil)
			}
			if err := writeUserCheckLine(r.Out, fmt.Sprintf("%s: установлен", app.label)); err != nil {
				return r.fail(options{}, err, nil)
			}
		}
	}
	for _, line := range []string{"Hermes: отсутствует", "Hermes: установлен", "Hermes: продолжение после установки"} {
		if err := writeUserCheckLine(r.Out, line); err != nil {
			return r.fail(options{}, err, nil)
		}
	}
	if err := readAndDiscardVisible(ctx, q, "Введите короткий каталог установки Hermes (например, C:\\Hermes)"); err != nil {
		return r.fail(options{}, err, nil)
	}
	sample := options{
		operatingSystem: "windows", application: "hermes", appInstalled: "true",
		kitHome: `C:\TeamKit`, hermesHome: `C:\Hermes`, project: "asku", role: "analyst",
		toolchain: "cc_1c_skills", update: "none",
	}
	command, err := formatHermesContinuation(`C:\TeamKit\teamkit.exe`, sample)
	if err != nil {
		return r.fail(options{}, err, nil)
	}
	for _, line := range []string{"Пример двух команд продолжения:", command} {
		if err := writeUserCheckLine(r.Out, line); err != nil {
			return r.fail(options{}, err, nil)
		}
	}
	for _, section := range []struct {
		prompt  string
		choices []choice
	}{{"Проекты", projectChoices()}, {"Роли", roleChoices()}, {"Наборы skills", toolchainChoices()}} {
		if err := writeChoices(r.Out, section.prompt, section.choices); err != nil {
			return r.fail(options{}, err, nil)
		}
	}
	if err := readAndDiscardVisible(ctx, q, "Каталог для проектов"); err != nil {
		return r.fail(options{}, err, nil)
	}
	for _, key := range []string{"GITLAB_USERNAME", "TEAMKIT_SOURCE_TOKEN", "AI_TOKEN", "TEAMKIT_PUBLIC_ISSUES_KEY", "TEAMKIT_PUBLIC_WIKI_KEY"} {
		if err := writeUserCheckLine(r.Out, fmt.Sprintf("%s: использовать сохранённое значение", key)); err != nil {
			return r.fail(options{}, err, nil)
		}
		if err := writeUserCheckLine(r.Out, fmt.Sprintf("%s: ввести новое значение", key)); err != nil {
			return r.fail(options{}, err, nil)
		}
		if err := readAndDiscardVisible(ctx, q, key); err != nil {
			return r.fail(options{}, err, nil)
		}
	}
	for _, line := range []string{
		"Профиль Hermes",
		"Skills Hermes",
		"MCP: v8std, Jira, Confluence, OfficeCLI",
		"Провайдер: Почта Тех",
		"Модель: public-development",
		"Проверка интерфейса завершена. Изменения не вносились.",
	} {
		if err := writeUserCheckLine(r.Out, line); err != nil {
			return r.fail(options{}, err, nil)
		}
	}
	return ExitOK
}

func writeUserCheckLine(writer io.Writer, line string) error {
	_, err := fmt.Fprintln(writer, line)
	return err
}

func readAndDiscardVisible(ctx context.Context, q *questionnaire, label string) error {
	value := ""
	return q.askText(ctx, &value, label+" (тестовый ввод виден)")
}
