package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

type questionnaire struct {
	reader *bufio.Reader
	out    io.Writer
}

type choice struct {
	value string
	label string
}

func newQuestionnaire(input io.Reader, output io.Writer) *questionnaire {
	reader, ok := input.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(input)
	}
	return &questionnaire{reader: reader, out: output}
}

func (q *questionnaire) complete(ctx context.Context, opts *options) error {
	if err := q.completeApplication(ctx, opts); err != nil {
		return err
	}
	if err := q.completeKitHome(ctx, opts); err != nil {
		return err
	}
	return q.completeProject(ctx, opts)
}

func (q *questionnaire) completeApplication(ctx context.Context, opts *options) error {
	if err := q.askChoice(ctx, &opts.operatingSystem, "Выберите операционную систему", operatingSystemChoices()); err != nil {
		return err
	}
	if err := q.askChoice(ctx, &opts.application, "Выберите AI-приложение", applicationChoices()); err != nil {
		return err
	}
	if opts.application != string(domain.AppHermes) {
		if err := q.askChoice(ctx, &opts.appInstalled, "AI-приложение уже установлено", []choice{{value: "true", label: "Да"}, {value: "false", label: "Нет"}}); err != nil {
			return err
		}
		if _, err := opts.applicationInstalled(); err != nil {
			return err
		}
	}
	return nil
}

func (q *questionnaire) completeKitHome(ctx context.Context, opts *options) error {
	if err := q.askText(ctx, &opts.kitHome, "Введите каталог для проектов"); err != nil {
		return err
	}
	return nil
}

func (q *questionnaire) completeProject(ctx context.Context, opts *options) error {
	if err := q.completeProjectSelectors(ctx, opts); err != nil {
		return err
	}
	return q.completeLegacyPlanScope(ctx, opts)
}

func (q *questionnaire) completeProjectSelectors(ctx context.Context, opts *options) error {
	if opts.toolchainSet && opts.toolchain == "" {
		return domain.NewValidationError(domain.ToolchainUnknown, "toolchain", "")
	}
	if err := q.askChoice(ctx, &opts.project, "Выберите проект", projectChoices()); err != nil {
		return err
	}
	if err := q.askChoice(ctx, &opts.role, "Выберите роль", roleChoices()); err != nil {
		return err
	}
	if err := q.askChoice(ctx, &opts.toolchain, "Выберите набор skills", toolchainChoices()); err != nil {
		return err
	}
	return nil
}

func (q *questionnaire) completeLegacyPlanScope(ctx context.Context, opts *options) error {
	if opts.update == "" {
		state, err := workspace.Classify(opts.kitHome)
		if err != nil {
			return newOperationalError(codeWorkspaceInspectionFailed, err.Error(), err)
		}
		if state == workspace.NonEmpty {
			if err := q.askChoice(ctx, &opts.update, "Что обновить в существующем окружении", updateChoices()); err != nil {
				return err
			}
		} else {
			opts.update = "none"
		}
	}
	return nil
}

func (q *questionnaire) askChoice(ctx context.Context, value *string, question string, choices []choice) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if *value != "" {
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(q.out, "%s:\n", question); err != nil {
			return err
		}
		for index, option := range choices {
			if _, err := fmt.Fprintf(q.out, "  %d. %s\n", index+1, option.label); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(q.out, "Введите номер ответа: "); err != nil {
			return err
		}

		line, err := q.readLine(ctx)
		if err != nil && err != io.EOF {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		line = strings.TrimSpace(line)
		if line == "" && err == io.EOF {
			return newOperationalError(codeInputRequired, question, io.EOF)
		}
		number, parseErr := strconv.Atoi(line)
		if parseErr == nil && number >= 1 && number <= len(choices) {
			*value = choices[number-1].value
			return nil
		}
		if _, writeErr := fmt.Fprintf(q.out, "Некорректный номер. Введите число от 1 до %d.\n", len(choices)); writeErr != nil {
			return writeErr
		}
		if err == io.EOF {
			return newOperationalError(codeInputRequired, question, io.EOF)
		}
	}
}

func (q *questionnaire) askText(ctx context.Context, value *string, prompt string) error {
	if *value != "" {
		return nil
	}
	if _, err := fmt.Fprintf(q.out, "%s: ", prompt); err != nil {
		return err
	}
	line, err := q.readLine(ctx)
	if err != nil && err != io.EOF {
		return err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return newOperationalError(codeInputRequired, prompt, io.EOF)
	}
	*value = line
	return nil
}

func (q *questionnaire) readLine(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type readResult struct {
		line string
		err  error
	}
	result := make(chan readResult, 1)
	go func() {
		line, err := q.reader.ReadString('\n')
		result <- readResult{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case read := <-result:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return read.line, read.err
	}
}

func operatingSystemChoices() []choice {
	items := catalog.OperatingSystems()
	choices := make([]choice, 0, len(items))
	for _, item := range items {
		choices = append(choices, choice{value: string(item.ID), label: item.Label})
	}
	return choices
}

func applicationChoices() []choice {
	items := catalog.AIApplications()
	choices := make([]choice, 0, len(items))
	for _, item := range items {
		choices = append(choices, choice{value: string(item.ID), label: item.Label})
	}
	return choices
}

func projectChoices() []choice {
	items := catalog.Projects()
	choices := make([]choice, 0, len(items))
	for _, item := range items {
		choices = append(choices, choice{value: string(item.ID), label: string(item.ID)})
	}
	return choices
}

func roleChoices() []choice {
	items := catalog.Roles()
	choices := make([]choice, 0, len(items))
	for _, item := range items {
		label := item.Label
		switch item.ID {
		case domain.RoleAnalyst:
			label = "Аналитик"
		case domain.RoleDeveloper:
			label = "Программист"
		case domain.RoleArchitect:
			label = "Архитектор"
		}
		choices = append(choices, choice{value: string(item.ID), label: label})
	}
	return choices
}

func toolchainChoices() []choice {
	return []choice{
		{value: string(domain.ToolchainCC1CSkills), label: "cc_1c_skills от Широкова"},
		{value: string(domain.ToolchainAIRules1C), label: "ai_rules_1c от Филиппова"},
	}
}

func updateChoices() []choice {
	return []choice{
		{value: "none", label: "Ничего"},
		{value: "content", label: "Только файлы окружения"},
		{value: "database", label: "Только файлы базы данных"},
		{value: "both", label: "Файлы окружения и базы данных"},
	}
}

func applyModeChoices() []choice {
	return []choice{
		{value: "add", label: "Добавить новое окружение"},
		{value: "update", label: "Обновить существующее окружение"},
	}
}

func (q *questionnaire) askApplyMode(ctx context.Context) (string, error) {
	var mode string
	if err := q.askChoice(ctx, &mode, "Что вы хотите сделать", applyModeChoices()); err != nil {
		return "", err
	}
	return mode, nil
}

const manualEnvironmentChoice = "manual"

func environmentChoices(environments []environment.VerifiedEnvironment) []choice {
	result := make([]choice, 0, len(environments)+1)
	for index, item := range environments {
		label := fmt.Sprintf("%s — %s", item.Desired.Project(), environment.DisplayPath(item.Home))
		if item.Pending {
			label = environment.DisplayPath(item.Home) + " — незавершённая операция"
		}
		result = append(result, choice{value: strconv.Itoa(index), label: label})
	}
	return append(result, choice{value: manualEnvironmentChoice, label: "Указать другой путь"})
}
