package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/registry"
)

func TestRunUserCheck_DiscardsVisibleInputWithoutDependencies(t *testing.T) {
	canaries := []string{"kit-canary", "hermes-canary", "user-canary", "token-canary", "provider-canary", "jira-canary", "confluence-canary"}
	var out, errOut bytes.Buffer
	runner := Runner{In: strings.NewReader(strings.Join(canaries, "\n") + "\n"), Out: &out, Err: &errOut}
	if code := runner.Run(context.Background(), []string{"user-check"}); code != ExitOK {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if !strings.HasSuffix(out.String(), "Проверка интерфейса завершена. Изменения не вносились.\n") {
		t.Fatalf("out=%q", out.String())
	}
	for _, canary := range canaries {
		if strings.Contains(out.String()+errOut.String(), canary) {
			t.Fatalf("canary leaked: %q", canary)
		}
	}
}

func TestRunUserCheck_ShowsCompleteSequentialCatalogWithoutDependencies(t *testing.T) {
	canaries := []string{"hermes-home-canary", "kit-home-canary", "username-canary", "token-canary", "provider-canary", "jira-canary", "confluence-canary"}
	service := &fakeService{}
	credentials := &countingCredentialSource{}
	inspector := &userCheckInspector{}
	registryStore := &userCheckRegistry{}
	configureCalls, discoveryCalls, executableCalls := 0, 0, 0
	var out, errOut bytes.Buffer
	runner := Runner{
		Service: service, Credentials: credentials, Environments: inspector, Registry: registryStore,
		In: strings.NewReader(strings.Join(canaries, "\n") + "\n"), Out: &out, Err: &errOut,
		ConfigureHermesHome: func(string) error { configureCalls++; return nil },
		HermesDiscovery: func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
			discoveryCalls++
			return hermes.DiscoveryResult{}, nil
		},
		Executable: func() (string, error) { executableCalls++; return "must-not-run", nil },
	}
	if code := runner.Run(context.Background(), []string{"user-check"}); code != ExitOK {
		t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if service.plans+service.applies+service.statuses+service.updates+service.retries != 0 || credentials.calls != 0 || inspector.calls != 0 || registryStore.calls != 0 || configureCalls != 0 || discoveryCalls != 0 || executableCalls != 0 {
		t.Fatalf("production dependency called: service=%+v credentials=%d inspector=%d registry=%d configure=%d discovery=%d executable=%d", service, credentials.calls, inspector.calls, registryStore.calls, configureCalls, discoveryCalls, executableCalls)
	}

	fragments := []string{
		"Операционные системы:", "Windows", "macOS", "Linux", "ALT Linux",
		"AI-приложения:", "Hermes", "Cursor", "Claude Code", "Codex", "OpenCode", "Kilo Code", "Kimi", "Qwen", "Command Code", "Cline", "Pi",
	}
	for _, app := range []string{"Cursor", "Claude Code", "Codex", "OpenCode", "Kilo Code", "Kimi", "Qwen", "Command Code", "Cline", "Pi"} {
		fragments = append(fragments, app+": не установлен", app+": установлен")
	}
	fragments = append(fragments,
		"Hermes: отсутствует", "Hermes: установлен", "Hermes: продолжение после установки",
		"Введите короткий каталог установки Hermes (например, C:\\Hermes) (тестовый ввод виден):",
		"Пример двух команд продолжения:", "$env:HERMES_HOME = 'C:\\Hermes'", "& 'C:\\TeamKit\\teamkit.exe' apply",
		"--app-installed=true", "--os windows", "--app hermes", "--kit-home 'C:\\TeamKit'", "--hermes-home 'C:\\Hermes'", "--project 'asku'", "--role 'analyst'", "--toolchain 'cc_1c_skills'", "--update none",
		"Проекты:", "aisuz", "apa", "asbnu", "asku", "easr", "eisko", "esed", "uat", "unip", "zup", "wms",
		"Роли:", "Аналитик", "Программист", "Архитектор",
		"Наборы skills:", "cc_1c_skills от Широкова", "ai_rules_1c от Филиппова",
		"Каталог для проектов (тестовый ввод виден):",
	)
	for _, key := range []string{"GITLAB_USERNAME", "TEAMKIT_SOURCE_TOKEN", "AI_TOKEN", "TEAMKIT_PUBLIC_ISSUES_KEY", "TEAMKIT_PUBLIC_WIKI_KEY"} {
		fragments = append(fragments, key+": использовать сохранённое значение", key+": ввести новое значение", key+" (тестовый ввод виден):")
	}
	fragments = append(fragments,
		"Профиль Hermes", "Skills Hermes", "MCP: v8std, Jira, Confluence, OfficeCLI", "Провайдер: Почта Тех", "Модель: public-development",
		"Проверка интерфейса завершена. Изменения не вносились.\n",
	)
	assertFragmentsInOrder(t, out.String(), fragments)
	if strings.Contains(out.String(), "TEAMKIT_PUBLIC_PROVIDER_API_KEY") {
		t.Fatalf("user-check exposed internal credential name: %q", out.String())
	}
	for _, canary := range canaries {
		if strings.Contains(out.String()+errOut.String(), canary) {
			t.Fatalf("canary leaked: %q", canary)
		}
	}
}

func TestRunUserCheck_ParseFailurePrecedesDispatchAndCancellationIsInterrupted(t *testing.T) {
	var parseOut, parseErr bytes.Buffer
	parseRunner := Runner{In: strings.NewReader("must-not-be-read\n"), Out: &parseOut, Err: &parseErr}
	if code := parseRunner.Run(context.Background(), []string{"user-check", "--unknown"}); code != ExitUsage {
		t.Fatalf("parse exit=%d out=%q err=%q", code, parseOut.String(), parseErr.String())
	}
	if strings.Contains(parseOut.String(), "Проверка интерфейса") {
		t.Fatalf("user-check dispatched before parse completed: %q", parseOut.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var cancelOut, cancelErr bytes.Buffer
	cancelRunner := Runner{In: strings.NewReader("ready-input-must-not-be-read\n"), Out: &cancelOut, Err: &cancelErr}
	if code := cancelRunner.Run(ctx, []string{"user-check"}); code != ExitInterrupted {
		t.Fatalf("cancel exit=%d out=%q err=%q", code, cancelOut.String(), cancelErr.String())
	}
	if !strings.Contains(cancelErr.String(), "INTERRUPTED") || strings.Contains(cancelOut.String()+cancelErr.String(), "ready-input-must-not-be-read") {
		t.Fatalf("cancel out=%q err=%q", cancelOut.String(), cancelErr.String())
	}
}

func TestRunUserCheck_PropagatesFinalLineWriterFailure(t *testing.T) {
	writer := &finalLineFailingWriter{}
	var errOut bytes.Buffer
	runner := Runner{
		In:  strings.NewReader(strings.Repeat("visible-input\n", 7)),
		Out: writer,
		Err: &errOut,
	}

	if code := runner.Run(context.Background(), []string{"user-check"}); code != ExitFailure {
		t.Fatalf("code=%d, want %d; stderr=%q", code, ExitFailure, errOut.String())
	}
	if !strings.Contains(errOut.String(), "TEAMKIT_FAILED") {
		t.Fatalf("stderr=%q, want writer failure identity", errOut.String())
	}
	if strings.Contains(writer.String(), "Проверка интерфейса завершена") {
		t.Fatalf("failed final line was reported as written: %q", writer.String())
	}
}

type finalLineFailingWriter struct {
	bytes.Buffer
}

func (w *finalLineFailingWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("Проверка интерфейса завершена")) {
		return 0, errors.New("late output failure")
	}
	return w.Buffer.Write(p)
}

type userCheckInspector struct{ calls int }

func (i *userCheckInspector) Inspect(context.Context, string) (environment.VerifiedEnvironment, environment.InspectionState, error) {
	i.calls++
	return environment.VerifiedEnvironment{}, environment.InspectionFailed, nil
}

func (i *userCheckInspector) ClassifyAdd(context.Context, string) (environment.AddState, error) {
	i.calls++
	return environment.AddTargetReady, nil
}

type userCheckRegistry struct{ calls int }

func (r *userCheckRegistry) Load(context.Context) (registry.Registry, registry.LoadState, error) {
	r.calls++
	return registry.Registry{}, registry.LoadMissing, nil
}

func (r *userCheckRegistry) Promote(context.Context, string) error {
	r.calls++
	return nil
}

func assertFragmentsInOrder(t *testing.T, transcript string, fragments []string) {
	t.Helper()
	cursor := 0
	for _, fragment := range fragments {
		index := strings.Index(transcript[cursor:], fragment)
		if index < 0 {
			t.Fatalf("transcript missing %q after byte %d: %q", fragment, cursor, transcript)
		}
		cursor += index + len(fragment)
	}
}
