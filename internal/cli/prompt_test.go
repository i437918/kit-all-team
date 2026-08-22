package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/gitx"
)

func TestErrorIdentity_WrappedGitErrorKeepsTypedCodeAndExit(t *testing.T) {
	err := fmt.Errorf("service: %w", &gitx.Error{Code: "LOCAL_CHANGES_DETECTED", Err: errors.New("dirty")})

	code, exit := errorIdentity(err)

	if code != "LOCAL_CHANGES_DETECTED" || exit != ExitLocalChanges {
		t.Fatalf("code=%q exit=%d", code, exit)
	}
}

func TestErrorIdentity_OtherWrappedGitErrorKeepsGenericPublicIdentity(t *testing.T) {
	err := fmt.Errorf("service: %w", &gitx.Error{Code: "GIT_COMMAND_FAILED", Err: errors.New("failed")})

	code, exit := errorIdentity(err)

	if code != "TEAMKIT_FAILED" || exit != ExitFailure {
		t.Fatalf("code=%q exit=%d", code, exit)
	}
}

func TestQuestionnaireApplyModeAndToolchainCopyIsExact(t *testing.T) {
	var output bytes.Buffer
	q := newQuestionnaire(strings.NewReader("2\n1\n"), &output)
	mode, err := q.askApplyMode(context.Background())
	if err != nil || mode != "update" {
		t.Fatalf("mode=%q err=%v", mode, err)
	}
	var toolchain string
	if err := q.askChoice(context.Background(), &toolchain, "Выберите набор skills", toolchainChoices()); err != nil {
		t.Fatal(err)
	}
	want := "Что вы хотите сделать:\n  1. Добавить новое окружение\n  2. Обновить существующее окружение\nВведите номер ответа: " +
		"Выберите набор skills:\n  1. cc_1c_skills от Широкова\n  2. ai_rules_1c от Филиппова\nВведите номер ответа: "
	if output.String() != want {
		t.Fatalf("output=%q want=%q", output.String(), want)
	}
}

func TestParseOptions_RemembersExplicitEmptySelectors(t *testing.T) {
	opts, err := parseOptions([]string{"apply", "--toolchain=", "--update=", "--kit-home="}, io.Discard)
	if err != nil || !opts.toolchainSet || !opts.updateSet || !opts.kitHomeSet {
		t.Fatalf("opts=%#v err=%v", opts, err)
	}
}

func TestQuestionnaireNonHermesAbsenceStopsBeforeProjectQuestions(t *testing.T) {
	for _, value := range []string{"false", "0", "False", "FALSE"} {
		t.Run(value, func(t *testing.T) {
			var output bytes.Buffer
			q := newQuestionnaire(strings.NewReader(""), &output)
			opts := options{
				operatingSystem: string(domain.OSWindows),
				application:     string(domain.AppCodex),
				appInstalled:    value,
			}

			err := q.completeApplication(context.Background(), &opts)

			code, exit := errorIdentity(err)
			if code != "AI_APP_REQUIRED" || exit != ExitApplicationRequired {
				t.Fatalf("err=%v code=%q exit=%d", err, code, exit)
			}
			if output.Len() != 0 {
				t.Fatalf("unexpected later prompt output=%q", output.String())
			}
		})
	}
}

func TestQuestionnaireExplicitToolchainHandlingIsStable(t *testing.T) {
	t.Run("empty explicit value is rejected before prompts", func(t *testing.T) {
		var output bytes.Buffer
		q := newQuestionnaire(strings.NewReader(""), &output)
		opts := options{toolchainSet: true}

		err := q.completeProject(context.Background(), &opts)

		code, exit := errorIdentity(err)
		if code != "TOOLCHAIN_UNKNOWN" || exit != ExitUsage {
			t.Fatalf("err=%v code=%q exit=%d", err, code, exit)
		}
		if output.Len() != 0 {
			t.Fatalf("unexpected prompt output=%q", output.String())
		}
	})

	t.Run("valid explicit value skips skills prompt", func(t *testing.T) {
		var output bytes.Buffer
		q := newQuestionnaire(strings.NewReader(""), &output)
		opts := options{
			project:      string(domain.ProjectAPA),
			role:         string(domain.RoleDeveloper),
			toolchain:    string(domain.ToolchainCC1CSkills),
			toolchainSet: true,
			update:       "none",
		}

		if err := q.completeProject(context.Background(), &opts); err != nil {
			t.Fatal(err)
		}
		if output.Len() != 0 {
			t.Fatalf("unexpected prompt output=%q", output.String())
		}
	})

	t.Run("invalid explicit value reaches desired state validation", func(t *testing.T) {
		opts := options{
			operatingSystem: string(domain.OSWindows),
			application:     string(domain.AppCodex),
			appInstalled:    "true",
			kitHome:         `C:\TeamKit\apa`,
			project:         string(domain.ProjectAPA),
			role:            string(domain.RoleDeveloper),
			toolchain:       "invalid",
			toolchainSet:    true,
		}

		_, err := opts.desiredState()
		code, exit := errorIdentity(err)
		if code != "TOOLCHAIN_UNKNOWN" || exit != ExitUsage {
			t.Fatalf("err=%v code=%q exit=%d", err, code, exit)
		}
	})
}

func TestQuestionnaireModeRepromptsAndEOFIsTyped(t *testing.T) {
	var out bytes.Buffer
	q := newQuestionnaire(strings.NewReader("\n9\n2\n"), &out)
	mode, err := q.askApplyMode(context.Background())
	if err != nil || mode != "update" {
		t.Fatalf("mode=%q err=%v", mode, err)
	}
	if got := strings.Count(out.String(), "Что вы хотите сделать:"); got != 3 {
		t.Fatalf("menu count=%d output=%q", got, out.String())
	}

	q = newQuestionnaire(strings.NewReader(""), io.Discard)
	_, err = q.askApplyMode(context.Background())
	var operational *operationalError
	if !errors.As(err, &operational) || operational.Code != codeInputRequired {
		t.Fatalf("err=%T %v", err, err)
	}
	code, exit := errorIdentity(err)
	if code != "INPUT_REQUIRED" || exit != ExitUsage {
		t.Fatalf("code=%q exit=%d", code, exit)
	}
}

func TestOperationalErrorExitMappingIsExact(t *testing.T) {
	tests := []struct {
		code     operationalCode
		wantExit int
	}{
		{codeInputRequired, ExitUsage}, {codeUpdateChoiceNotApplicable, ExitUsage},
		{codeWorkspaceExistsUseUpdate, ExitFailure}, {codeForeignWorkspace, ExitFailure},
		{codeRetryRequired, ExitFailure}, {codeWorkspaceInspectionFailed, ExitFailure},
	}
	for _, test := range tests {
		identity, exit := errorIdentity(newOperationalError(test.code, "detail", nil))
		if identity != string(test.code) || exit != test.wantExit {
			t.Fatalf("code=%q identity=%q exit=%d", test.code, identity, exit)
		}
	}
}

func TestErrorIdentity_DomainAndOperationalExitMappingIsExact(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
		wantExit int
	}{
		{"toolchain", domain.NewValidationError(domain.ToolchainUnknown, "toolchain", "bad"), "TOOLCHAIN_UNKNOWN", ExitUsage},
		{"application", domain.NewValidationError(domain.AIAppRequired, "application", "codex"), "AI_APP_REQUIRED", ExitApplicationRequired},
		{"foreign", newOperationalError(codeForeignWorkspace, "foreign", nil), "FOREIGN_WORKSPACE", ExitFailure},
		{"inspection", newOperationalError(codeWorkspaceInspectionFailed, "io", nil), "WORKSPACE_INSPECTION_FAILED", ExitFailure},
		{"retry", newOperationalError(codeRetryRequired, "retry", nil), "RETRY_REQUIRED", ExitFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, exit := errorIdentity(test.err)
			if code != test.wantCode || exit != test.wantExit {
				t.Fatalf("code=%q exit=%d", code, exit)
			}
		})
	}
}

func TestEnvironmentChoicesRenderReadyPendingAndManualPaths(t *testing.T) {
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS:           domain.OSWindows,
		Application:  domain.AppCodex,
		AppInstalled: true,
		KitHome:      `C:\TeamKit\apa`,
		Project:      domain.ProjectAPA,
		Role:         domain.RoleDeveloper,
		Toolchain:    domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := environmentChoices([]environment.VerifiedEnvironment{
		{Home: `C:\TeamKit\apa`, Desired: desired},
		{Home: `C:\TeamKit\pending`, Pending: true},
	})
	want := []choice{
		{value: "0", label: `apa — ` + environment.DisplayPath(`C:\TeamKit\apa`)},
		{value: "1", label: environment.DisplayPath(`C:\TeamKit\pending`) + ` — незавершённая операция`},
		{value: manualEnvironmentChoice, label: "Указать другой путь"},
	}
	if len(got) != len(want) {
		t.Fatalf("choices=%#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("choice[%d]=%#v want=%#v", i, got[i], want[i])
		}
	}
}

func TestEnvironmentChoicesEscapesTerminalUnsafeAcceptedHomes(t *testing.T) {
	unsafe := `C:\TeamKit\apa"` + "\n\x1b\u0085\u202e3. Поддельный пункт"
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSWindows, Application: domain.AppCodex, AppInstalled: true,
		KitHome: unsafe, Project: domain.ProjectAPA, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	choices := environmentChoices([]environment.VerifiedEnvironment{
		{Home: unsafe, Desired: desired},
		{Home: unsafe, Pending: true},
	})
	for index, item := range choices[:2] {
		if !strings.Contains(item.label, environment.DisplayPath(unsafe)) {
			t.Fatalf("choice[%d] did not use shared display: %q", index, item.label)
		}
		for _, forbidden := range []rune{'\n', '\x1b', '\u0085', '\u202e'} {
			if strings.ContainsRune(item.label, forbidden) {
				t.Fatalf("choice[%d] contains raw %U: %q", index, forbidden, item.label)
			}
		}
	}
}

func TestUpdateChoicesCopyAndStableValuesAreExact(t *testing.T) {
	got := updateChoices()
	want := []choice{
		{value: "none", label: "Ничего"},
		{value: "content", label: "Только файлы окружения"},
		{value: "database", label: "Только файлы базы данных"},
		{value: "both", label: "Файлы окружения и базы данных"},
	}
	if len(got) != len(want) {
		t.Fatalf("choices=%#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("choice[%d]=%#v want=%#v", i, got[i], want[i])
		}
	}
}

func TestQuestionnaireAskChoiceMapsNumberAndRendersRussianMenu(t *testing.T) {
	var output bytes.Buffer
	q := newQuestionnaire(strings.NewReader("2\n"), &output)
	var selected string

	err := q.askChoice(context.Background(), &selected, "Выберите операционную систему", []choice{
		{value: "windows", label: "Windows"},
		{value: "linux", label: "Linux"},
	})

	if err != nil {
		t.Fatal(err)
	}
	if selected != "linux" {
		t.Fatalf("selected=%q, want linux", selected)
	}
	want := "Выберите операционную систему:\n  1. Windows\n  2. Linux\nВведите номер ответа: "
	if output.String() != want {
		t.Fatalf("output=%q, want %q", output.String(), want)
	}
}

func TestQuestionnaireAskChoiceRetriesInvalidNumbers(t *testing.T) {
	var output bytes.Buffer
	q := newQuestionnaire(strings.NewReader("windows\n0\n4\n3\n"), &output)
	var selected string

	err := q.askChoice(context.Background(), &selected, "Выберите вариант", []choice{
		{value: "one", label: "Один"},
		{value: "two", label: "Два"},
		{value: "three", label: "Три"},
	})

	if err != nil {
		t.Fatal(err)
	}
	if selected != "three" {
		t.Fatalf("selected=%q, want three", selected)
	}
	if got := strings.Count(output.String(), "Некорректный номер. Введите число от 1 до 3."); got != 3 {
		t.Fatalf("invalid-message count=%d, output=%q", got, output.String())
	}
}

func TestQuestionnaireAskTextRejectsEmptyEOF(t *testing.T) {
	q := newQuestionnaire(strings.NewReader(""), io.Discard)
	var value string

	err := q.askText(context.Background(), &value, "Введите KIT_ALL_TEAM_HOME")

	var operational *operationalError
	if !errors.As(err, &operational) || operational.Code != codeInputRequired || !errors.Is(err, io.EOF) {
		t.Fatalf("err=%T %v, want typed INPUT_REQUIRED wrapping io.EOF", err, err)
	}
}

func TestQuestionnaireAskChoiceStopsAfterContextCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})
	q := newQuestionnaire(reader, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var selected string

	err := q.askChoice(ctx, &selected, "Выберите вариант", []choice{{value: "one", label: "Один"}})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}

func TestQuestionnaireAskChoicePreCancelledReadyInputDoesNotPromptOrMutate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	q := newQuestionnaire(strings.NewReader("1\n"), &output)
	var selected string

	err := q.askChoice(ctx, &selected, "Выберите вариант", []choice{{value: "one", label: "Один"}})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	if code, exit := errorIdentity(err); code != "INTERRUPTED" || exit != ExitInterrupted {
		t.Fatalf("code=%q exit=%d", code, exit)
	}
	if output.Len() != 0 || selected != "" {
		t.Fatalf("output=%q selected=%q", output.String(), selected)
	}
}
