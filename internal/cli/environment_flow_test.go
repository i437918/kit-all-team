package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/registry"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

func TestRunInteractiveApply_AddIsFirstAndRejectsExistingBeforePlan(t *testing.T) {
	service := &fakeService{}
	inspector := &fakeInspector{addState: environment.AddWorkspaceExists}
	var out, errOut bytes.Buffer
	runner := Runner{Service: service, Environments: inspector, In: strings.NewReader(interactiveAddAnswers(t)), Out: &out, Err: &errOut, HermesDiscovery: installedHermes(t)}
	code := runner.Run(context.Background(), []string{"apply"})
	if code == ExitOK || !strings.Contains(errOut.String(), "WORKSPACE_EXISTS_USE_UPDATE") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	if service.plans != 0 || service.applies != 0 {
		t.Fatalf("plans=%d applies=%d", service.plans, service.applies)
	}
	if !strings.HasPrefix(out.String(), "Что вы хотите сделать:") {
		t.Fatalf("out=%q", out.String())
	}
}

func TestRunInteractiveApply_AddRejectsActionfulUpdateScopeAfterModeOnly(t *testing.T) {
	runner := Runner{Service: &fakeService{}, In: strings.NewReader("1\n"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	code := runner.Run(context.Background(), []string{"apply", "--update", "both"})
	if code == ExitOK || !strings.Contains(runner.Err.(*bytes.Buffer).String(), "UPDATE_CHOICE_NOT_APPLICABLE") {
		t.Fatalf("code=%d", code)
	}
}

func TestRunInteractiveApply_AddClassificationAndFlags(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		input        string
		addState     environment.AddState
		addErr       error
		cancel       bool
		wantExit     int
		wantIdentity string
		wantPlans    int
		wantApplies  int
		wantAddCalls int
		wantPromotes int
	}{
		{"ready", []string{"apply"}, interactiveAddAnswers(t), environment.AddTargetReady, nil, false, ExitOK, "", 1, 1, 1, 1},
		{"existing", []string{"apply"}, interactiveAddAnswers(t), environment.AddWorkspaceExists, nil, false, ExitFailure, "WORKSPACE_EXISTS_USE_UPDATE", 0, 0, 1, 0},
		{"foreign", []string{"apply"}, interactiveAddAnswers(t), environment.AddTargetReady, &environment.Error{State: environment.Foreign, Detail: "foreign"}, false, ExitFailure, "FOREIGN_WORKSPACE", 0, 0, 1, 0},
		{"inspection", []string{"apply"}, interactiveAddAnswers(t), environment.AddTargetReady, &environment.Error{State: environment.InspectionFailed, Detail: "io"}, false, ExitFailure, "WORKSPACE_INSPECTION_FAILED", 0, 0, 1, 0},
		{"explicit none", []string{"apply", "--update", "none"}, interactiveAddAnswers(t), environment.AddTargetReady, nil, false, ExitOK, "", 1, 1, 1, 1},
		{"explicit actionful", []string{"apply", "--update", "both"}, "1\n", environment.AddTargetReady, nil, false, ExitUsage, "UPDATE_CHOICE_NOT_APPLICABLE", 0, 0, 0, 0},
		{"explicit empty toolchain", []string{"apply", "--toolchain="}, interactiveAddAnswers(t), environment.AddTargetReady, nil, false, ExitUsage, "TOOLCHAIN_UNKNOWN", 0, 0, 0, 0},
		{"canceled", []string{"apply"}, interactiveAddAnswers(t), environment.AddTargetReady, nil, true, ExitInterrupted, "INTERRUPTED", 0, 0, 0, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			actionful := oneActionPlan()
			service := &fakeService{plan: actionful, hasPlan: true, applyResult: &actionful, status: reconcile.StatusReady}
			inspector := &fakeInspector{addState: test.addState, addErr: test.addErr}
			registrySpy := &fakeRegistry{state: registry.LoadMissing}
			var out, errOut bytes.Buffer
			runner := Runner{Service: service, Registry: registrySpy, Environments: inspector, In: strings.NewReader(test.input), Out: &out, Err: &errOut, HermesDiscovery: installedHermes(t)}
			code := runner.Run(ctx, test.args)
			if code != test.wantExit {
				t.Fatalf("exit=%d want=%d stderr=%q", code, test.wantExit, errOut.String())
			}
			if test.wantIdentity != "" && !strings.HasPrefix(errOut.String(), test.wantIdentity+": ") {
				t.Fatalf("stderr=%q", errOut.String())
			}
			if service.plans != test.wantPlans || service.applies != test.wantApplies || inspector.addCalls != test.wantAddCalls {
				t.Fatalf("service=%#v addCalls=%d registry=%#v", service, inspector.addCalls, registrySpy)
			}
			if registrySpy.promotes != test.wantPromotes {
				t.Fatalf("promotes=%d want=%d", registrySpy.promotes, test.wantPromotes)
			}
			if test.wantIdentity != "" && (registrySpy.loads != 0 || registrySpy.promotes != 0) {
				t.Fatalf("failed add touched registry: %#v", registrySpy)
			}
		})
	}
}

func interactiveAddAnswers(t *testing.T) string {
	t.Helper()
	kit := filepath.Join(testutil.TempDir(t), "kit")
	return strings.Join([]string{"1", "3", "1", kit, "2", "2", "1", ""}, "\n")
}

func installedHermes(t *testing.T) func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
	t.Helper()
	home := filepath.Join(testutil.TempDir(t), "hermes")
	return func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
		return hermes.DiscoveryResult{Installed: true, Home: home, Executable: filepath.Join(home, "hermes"), Version: "0.20.2"}, nil
	}
}

type inspectResult struct {
	verified environment.VerifiedEnvironment
	state    environment.InspectionState
	err      error
}

type fakeInspector struct {
	addState     environment.AddState
	addErr       error
	addCalls     int
	inspectCalls int
	byHome       map[string]inspectResult
}

func (f *fakeInspector) ClassifyAdd(context.Context, string) (environment.AddState, error) {
	f.addCalls++
	return f.addState, f.addErr
}

func (f *fakeInspector) Inspect(_ context.Context, home string) (environment.VerifiedEnvironment, environment.InspectionState, error) {
	f.inspectCalls++
	result := f.byHome[home]
	return result.verified, result.state, result.err
}

type fakeRegistry struct {
	snapshot     registry.Registry
	state        registry.LoadState
	loadErr      error
	promoteErr   error
	loads        int
	promotes     int
	promotedHome string
}

func (f *fakeRegistry) Load(context.Context) (registry.Registry, registry.LoadState, error) {
	f.loads++
	copied := registry.Registry{SchemaVersion: f.snapshot.SchemaVersion, Homes: append([]string(nil), f.snapshot.Homes...)}
	return copied, f.state, f.loadErr
}

func (f *fakeRegistry) Promote(_ context.Context, home string) error {
	f.promotes++
	f.promotedHome = home
	return f.promoteErr
}

func TestRunExistingRunnerWithoutRegistryRemainsBackwardCompatible(t *testing.T) {
	actionful := oneActionPlan()
	service := &fakeService{plan: actionful, hasPlan: true, applyResult: &actionful, status: reconcile.StatusReady}
	var errOut bytes.Buffer
	runner := Runner{Service: service, In: strings.NewReader(""), Out: io.Discard, Err: &errOut}
	if code := runner.Run(context.Background(), linuxArgs("apply")); code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "реестр") {
		t.Fatalf("nil registry emitted warning: %q", errOut.String())
	}
}

func TestSelectEnvironment_OneReadyAutoSelectsWithoutPathQuestion(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "apa")
	registrySpy := &fakeRegistry{snapshot: registry.Registry{SchemaVersion: 1, Homes: []string{home}}, state: registry.LoadValid}
	inspector := &fakeInspector{byHome: map[string]inspectResult{home: {verified: verifiedEnvironment(t, home, "apa"), state: environment.Ready}}}
	var out bytes.Buffer
	runner := Runner{Registry: registrySpy, Environments: inspector, Out: &out, Err: io.Discard, GOOS: runtime.GOOS, Executable: os.Executable}
	q := newQuestionnaire(strings.NewReader(""), &out)
	session := &registrySession{store: registrySpy}
	got, err := runner.selectEnvironment(context.Background(), q, options{}, session)
	if err != nil || got.Home != home {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if strings.Contains(out.String(), "Введите KIT_ALL_TEAM_HOME") || strings.Contains(out.String(), "Выберите окружение") {
		t.Fatalf("out=%q", out.String())
	}
}

func TestSelectEnvironment_MultipleShowsProjectsPendingMarkerAndManual(t *testing.T) {
	base := testutil.TempDir(t)
	ready := verifiedEnvironment(t, filepath.Join(base, "apa"), "apa")
	pending := verifiedEnvironment(t, filepath.Join(base, "wms"), "wms")
	pending.Pending = true
	manual := verifiedEnvironment(t, filepath.Join(base, "manual"), "wms")
	registrySpy := &fakeRegistry{snapshot: registry.Registry{SchemaVersion: 1, Homes: []string{ready.Home, pending.Home}}, state: registry.LoadValid}
	inspector := &fakeInspector{byHome: map[string]inspectResult{
		ready.Home:   {verified: ready, state: environment.Ready},
		pending.Home: {verified: pending, state: environment.RetryRequired, err: &environment.Error{State: environment.RetryRequired, Detail: "pending"}},
		manual.Home:  {verified: manual, state: environment.Ready},
	}}
	var out bytes.Buffer
	runner := Runner{Registry: registrySpy, Environments: inspector, Out: &out, Err: io.Discard, GOOS: runtime.GOOS, Executable: os.Executable}
	q := newQuestionnaire(strings.NewReader("3\n"+manual.Home+"\n"), &out)
	got, err := runner.selectEnvironment(context.Background(), q, options{}, &registrySession{store: registrySpy})
	if err != nil || got.Home != manual.Home {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	want := "Выберите окружение:\n  1. apa — " + environment.DisplayPath(ready.Home) + "\n  2. " + environment.DisplayPath(pending.Home) + " — незавершённая операция\n  3. Указать другой путь\nВведите номер ответа: "
	if !strings.Contains(out.String(), want) || inspector.inspectCalls != 3 {
		t.Fatalf("calls=%d output=%q", inspector.inspectCalls, out.String())
	}
}

func TestWriteEnvironmentSummary_ContainsOnlyPublicSelections(t *testing.T) {
	var out bytes.Buffer
	home := filepath.Join(testutil.TempDir(t), "apa")
	env := verifiedEnvironment(t, home, "apa")
	if err := writeEnvironmentSummary(&out, env); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Найдено окружение:", "KIT_ALL_TEAM_HOME: " + environment.DisplayPath(home), "Проект: apa", "AI-приложение: Hermes", "Роль: developer", "Набор skills: cc_1c_skills"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %q", want, out.String())
		}
	}
	if strings.Contains(out.String(), "TOKEN") {
		t.Fatalf("secret-like output=%q", out.String())
	}
}

func TestWriteEnvironmentSummaryEscapesTerminalUnsafeAcceptedHome(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), `apa"`) + "\n\x1b\u0085\u202e\n  Проект: wms"
	env := verifiedEnvironment(t, home, "apa")
	var out bytes.Buffer
	if err := writeEnvironmentSummary(&out, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "KIT_ALL_TEAM_HOME: "+environment.DisplayPath(home)) || strings.Count(out.String(), "\n  Проект:") != 1 {
		t.Fatalf("summary field boundary was spoofed: %q", out.String())
	}
	for _, forbidden := range []rune{'\x1b', '\u0085', '\u202e'} {
		if strings.ContainsRune(out.String(), forbidden) {
			t.Fatalf("summary contains raw %U: %q", forbidden, out.String())
		}
	}
}

func verifiedEnvironment(t *testing.T, home, project string) environment.VerifiedEnvironment {
	t.Helper()
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
		KitHome: home, HermesHome: filepath.Join(filepath.Dir(home), "hermes"), HermesVersion: "0.20.2",
		Project: domain.ProjectID(project), Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	return environment.VerifiedEnvironment{Home: home, Desired: desired}
}

func TestRegistrySession_LoadFailureWarnsOnceAndDisablesPromotion(t *testing.T) {
	for _, state := range []registry.LoadState{registry.LoadCorrupt, registry.LoadUnavailable} {
		t.Run(fmt.Sprint(state), func(t *testing.T) {
			store := &fakeRegistry{state: state, loadErr: errors.New("registry unavailable")}
			session := &registrySession{store: store}
			var errOut bytes.Buffer
			if _, writable := session.ensureLoaded(context.Background(), &errOut); writable {
				t.Fatal("registry unexpectedly writable")
			}
			if _, writable := session.ensureLoaded(context.Background(), &errOut); writable {
				t.Fatal("registry unexpectedly writable on second load")
			}
			session.promote(context.Background(), &errOut, filepath.Join(testutil.TempDir(t), "apa"))
			const warning = "Предупреждение: локальный реестр Team Kit повреждён, недоступен или имеет неподдерживаемый формат и будет проигнорирован."
			if strings.Count(errOut.String(), warning) != 1 || store.loads != 1 || store.promotes != 0 {
				t.Fatalf("loads=%d promotes=%d stderr=%q", store.loads, store.promotes, errOut.String())
			}
		})
	}
}

func TestSelectEnvironment_PendingPrintsExecutableRetryAndStopsEffects(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "apa")
	executable := filepath.Join(testutil.TempDir(t), "teamkit")
	pending := verifiedEnvironment(t, home, "apa")
	pending.Pending = true
	inspector := &fakeInspector{byHome: map[string]inspectResult{home: {verified: pending, state: environment.RetryRequired, err: &environment.Error{State: environment.RetryRequired, Detail: "pending"}}}}
	service := &fakeService{}
	registrySpy := &fakeRegistry{}
	var out, errOut bytes.Buffer
	runner := Runner{
		Service: service, Registry: registrySpy, Environments: inspector,
		In: strings.NewReader("2\n"), Out: &out, Err: &errOut, GOOS: runtime.GOOS,
		Executable: func() (string, error) { return executable, nil },
	}
	code := runner.Run(context.Background(), []string{"apply", "--kit-home", home})
	if code != ExitFailure || !strings.Contains(errOut.String(), "RETRY_REQUIRED") || !strings.Contains(errOut.String(), executable) || !strings.Contains(errOut.String(), home) {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	if service.plans != 0 || service.applies != 0 || service.statuses != 0 || registrySpy.promotes != 0 {
		t.Fatalf("service=%#v registry=%#v", service, registrySpy)
	}
}

func TestFormatRetryCommand_POSIXEscapesSingleQuotes(t *testing.T) {
	got, err := formatRetryCommand("linux", "/opt/teamkit", "/srv/team'kit")
	want := `'/opt/teamkit' retry --kit-home '/srv/team'\''kit'`
	if err != nil || got != want {
		t.Fatalf("got=%q want=%q err=%v", got, want, err)
	}
}

func TestFormatRetryCommand_WindowsEscapesPowerShellSingleQuotes(t *testing.T) {
	got, err := formatRetryCommand("windows", `C:\Program Files\O'Brien\teamkit.exe`, `C:\Team's\apa`)
	want := `& 'C:\Program Files\O''Brien\teamkit.exe' retry --kit-home 'C:\Team''s\apa'`
	if err != nil || got != want {
		t.Fatalf("got=%q want=%q err=%v", got, want, err)
	}
}

func TestFormatRetryCommandRejectsTerminalUnsafePathsWithoutRenderingThem(t *testing.T) {
	for _, test := range []struct {
		name       string
		executable string
		home       string
	}{
		{name: "home", executable: "/opt/teamkit", home: "/srv/apa\n\x1b\u202e"},
		{name: "executable", executable: "/opt/teamkit\n\x1b\u202e", home: "/srv/apa"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command, err := formatRetryCommand("linux", test.executable, test.home)
			if command != "" || !errors.Is(err, environment.ErrTerminalUnsafePath) {
				t.Fatalf("command=%q err=%T %v", command, err, err)
			}
		})
	}
}

func TestFormatRetryCommand_NormalSpacesQuotesAndUnicodeRemainExact(t *testing.T) {
	posix, err := formatRetryCommand("linux", `/opt/Команда "Team Kit"`, `/srv/Проект O'Brien "apa"`)
	if err != nil || posix != `'/opt/Команда "Team Kit"' retry --kit-home '/srv/Проект O'\''Brien "apa"'` {
		t.Fatalf("posix=%q err=%v", posix, err)
	}
	powerShell, err := formatRetryCommand("windows", `C:\Program Files\Команда "Team Kit".exe`, `C:\Проект O'Brien "apa"`)
	if err != nil || powerShell != `& 'C:\Program Files\Команда "Team Kit".exe' retry --kit-home 'C:\Проект O''Brien "apa"'` {
		t.Fatalf("powershell=%q err=%v", powerShell, err)
	}
}

func TestSelectEnvironmentUnsafeRegistryAndEnvironmentPathsWarnThenUseManual(t *testing.T) {
	unsafe := filepath.Join(testutil.TempDir(t), "apa\n\x1b\u0085\u202e3. Поддельный пункт")
	manual := filepath.Join(testutil.TempDir(t), "manual")
	for _, test := range []struct {
		name     string
		registry registry.Registry
		env      string
	}{
		{name: "registry", registry: registry.Registry{SchemaVersion: 1, Homes: []string{unsafe}}},
		{name: "environment", registry: registry.Registry{SchemaVersion: 1}, env: unsafe},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("KIT_ALL_TEAM_HOME", test.env)
			store := &fakeRegistry{snapshot: test.registry, state: registry.LoadValid}
			inspector := &fakeInspector{byHome: map[string]inspectResult{manual: {verified: verifiedEnvironment(t, manual, "apa"), state: environment.Ready}}}
			var out, errOut bytes.Buffer
			runner := Runner{Registry: store, Environments: inspector, Out: &out, Err: &errOut}
			q := newQuestionnaire(strings.NewReader(manual+"\n"), &out)
			got, err := runner.selectEnvironment(context.Background(), q, options{}, &registrySession{store: store})
			if err != nil || got.Home != manual || inspector.inspectCalls != 1 {
				t.Fatalf("got=%#v calls=%d err=%v", got, inspector.inspectCalls, err)
			}
			if !strings.Contains(errOut.String(), environment.DisplayPath(unsafe)) || strings.Contains(errOut.String(), "\n3. Поддельный пункт") {
				t.Fatalf("unsafe warning output=%q", errOut.String())
			}
		})
	}
}

func TestRunInteractiveUpdateExplicitAndManualUnsafePathsFailWithoutTerminalInjection(t *testing.T) {
	unsafeExplicit := filepath.Join(testutil.TempDir(t), "apa\n\x1b\u0085\u202eTEAMKIT_OK: fake")
	unsafeManual := filepath.Join(testutil.TempDir(t), "apa\x1b\u0085\u202eTEAMKIT_OK: fake")
	for _, test := range []struct {
		name  string
		args  []string
		input string
	}{
		{name: "explicit", args: []string{"apply", "--kit-home", unsafeExplicit}, input: "2\n"},
		{name: "manual", args: []string{"apply"}, input: "2\n" + unsafeManual + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspector := &fakeInspector{}
			var out, errOut bytes.Buffer
			runner := Runner{Service: &fakeService{}, Environments: inspector, In: strings.NewReader(test.input), Out: &out, Err: &errOut}
			code := runner.Run(context.Background(), test.args)
			if code != ExitFailure || !strings.Contains(errOut.String(), "FOREIGN_WORKSPACE") || strings.Contains(errOut.String(), "\nTEAMKIT_OK: fake") || strings.ContainsRune(errOut.String(), '\x1b') || strings.ContainsRune(errOut.String(), '\u0085') || strings.ContainsRune(errOut.String(), '\u202e') {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			if inspector.inspectCalls != 0 {
				t.Fatalf("unsafe path reached inspector: %d", inspector.inspectCalls)
			}
		})
	}
}

func TestSelectEnvironmentManualInspectorCannotSubstituteTerminalUnsafeHome(t *testing.T) {
	manual := filepath.Join(testutil.TempDir(t), "manual")
	unsafeVerified := filepath.Join(testutil.TempDir(t), "unsafe\n\x1b\u0085\u202e")
	inspector := &fakeInspector{byHome: map[string]inspectResult{
		manual: {verified: verifiedEnvironment(t, unsafeVerified, "apa"), state: environment.Ready},
	}}
	var out bytes.Buffer
	runner := Runner{Environments: inspector, Out: &out, Err: io.Discard}
	q := newQuestionnaire(strings.NewReader(manual+"\n"), &out)
	_, err := runner.selectEnvironment(context.Background(), q, options{}, &registrySession{})
	var operational *operationalError
	if !errors.As(err, &operational) || operational.Code != codeForeignWorkspace || inspector.inspectCalls != 1 {
		t.Fatalf("calls=%d err=%T %v", inspector.inspectCalls, err, err)
	}
}

func TestRunInteractivePendingRejectsTerminalUnsafeVerifiedHomeOrExecutable(t *testing.T) {
	base := testutil.TempDir(t)
	candidate := filepath.Join(base, "candidate")
	unsafe := filepath.Join(base, "unsafe\n\x1b\u0085\u202eRETRY_REQUIRED: fake")
	for _, test := range []struct {
		name       string
		verified   string
		executable string
		wantCode   string
	}{
		{name: "verified home", verified: unsafe, executable: filepath.Join(base, "teamkit"), wantCode: "FOREIGN_WORKSPACE"},
		{name: "executable", verified: candidate, executable: unsafe, wantCode: "WORKSPACE_INSPECTION_FAILED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pending := verifiedEnvironment(t, test.verified, "apa")
			pending.Pending = true
			inspector := &fakeInspector{byHome: map[string]inspectResult{
				candidate: {verified: pending, state: environment.RetryRequired, err: &environment.Error{State: environment.RetryRequired, Detail: "pending"}},
			}}
			var out, errOut bytes.Buffer
			runner := Runner{
				Service: &fakeService{}, Environments: inspector, In: strings.NewReader("2\n"), Out: &out, Err: &errOut,
				Executable: func() (string, error) { return test.executable, nil },
			}
			code := runner.Run(context.Background(), []string{"apply", "--kit-home", candidate})
			if code != ExitFailure || !strings.Contains(errOut.String(), test.wantCode) || strings.ContainsRune(errOut.String(), '\x1b') || strings.ContainsRune(errOut.String(), '\u0085') || strings.ContainsRune(errOut.String(), '\u202e') || strings.Contains(errOut.String(), "\nRETRY_REQUIRED: fake") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}

func TestRunInteractiveUpdate_NoneStopsAfterSummaryWithoutEffects(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "apa")
	ready := verifiedEnvironment(t, home, "apa")
	inspector := &fakeInspector{byHome: map[string]inspectResult{home: {verified: ready, state: environment.Ready}}}
	service := &fakeService{}
	credentials := &planCredentials{}
	registrySpy := &fakeRegistry{}
	var out, errOut bytes.Buffer
	runner := Runner{Service: service, Credentials: credentials, Registry: registrySpy, Environments: inspector, In: strings.NewReader("2\n1\n"), Out: &out, Err: &errOut, GOOS: runtime.GOOS, Executable: os.Executable}
	code := runner.Run(context.Background(), []string{"apply", "--kit-home", home})
	if code != ExitOK || !strings.Contains(out.String(), "Найдено окружение:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if inspector.inspectCalls != 1 || service.plans != 0 || service.applies != 0 || service.statuses != 0 || credentials.calls != 0 || registrySpy.loads != 0 || registrySpy.promotes != 0 {
		t.Fatalf("inspect=%d service=%#v credentials=%d registry=%#v", inspector.inspectCalls, service, credentials.calls, registrySpy)
	}
}

func TestRunInteractiveApply_UpdateScopesCallExistingUpdateFlow(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		input string
		want  reconcile.UpdateChoice
	}{
		{"content", []string{"apply"}, "2\n2\n", reconcile.UpdateContent},
		{"database", []string{"apply"}, "2\n3\n", reconcile.UpdateDatabase},
		{"both", []string{"apply"}, "2\n4\n", reconcile.UpdateBoth},
		{"explicit", []string{"apply", "--update", "content"}, "2\n", reconcile.UpdateContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := filepath.Join(testutil.TempDir(t), "apa")
			test.args = append(test.args, "--kit-home", home)
			ready := verifiedEnvironment(t, home, "apa")
			inspector := &fakeInspector{byHome: map[string]inspectResult{home: {verified: ready, state: environment.Ready}}}
			updatePlan := oneActionPlan()
			service := &fakeService{updateResult: &updatePlan, status: reconcile.StatusReady}
			registrySpy := &fakeRegistry{state: registry.LoadMissing}
			var out, errOut bytes.Buffer
			runner := Runner{Service: service, Registry: registrySpy, Environments: inspector, In: strings.NewReader(test.input), Out: &out, Err: &errOut}
			code := runner.Run(context.Background(), test.args)
			if code != ExitOK || service.updates != 1 || service.verifiedUpdates != 1 || service.update != test.want || service.statuses != 1 || service.plans != 0 || service.applies != 0 || registrySpy.promotes != 1 {
				t.Fatalf("code=%d service=%#v registry=%#v stdout=%q stderr=%q", code, service, registrySpy, out.String(), errOut.String())
			}
			if service.verified.Home != ready.Home || !reflect.DeepEqual(service.verified.Desired, ready.Desired) {
				t.Fatalf("verified=%#v want=%#v", service.verified, ready)
			}
			if !strings.Contains(out.String(), "update: ready") {
				t.Fatalf("stdout=%q", out.String())
			}
		})
	}
}

func TestRunInteractiveUpdate_WorkspaceChangedStopsBeforeStatusAndPromotion(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "apa")
	ready := verifiedEnvironment(t, home, "apa")
	inspector := &fakeInspector{byHome: map[string]inspectResult{home: {verified: ready, state: environment.Ready}}}
	service := &fakeService{err: fmt.Errorf("%w: public workspace state could not be revalidated", workspace.ErrChanged)}
	registrySpy := &fakeRegistry{state: registry.LoadMissing}
	var out, errOut bytes.Buffer
	runner := Runner{Service: service, Registry: registrySpy, Environments: inspector, In: strings.NewReader("2\n2\n"), Out: &out, Err: &errOut}
	code := runner.Run(context.Background(), []string{"apply", "--kit-home", home})
	if code != ExitFailure || errOut.String() != "WORKSPACE_CHANGED: WORKSPACE_CHANGED: public workspace state could not be revalidated\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if service.verifiedUpdates != 1 || service.statuses != 0 || registrySpy.promotes != 0 {
		t.Fatalf("service=%#v registry=%#v", service, registrySpy)
	}
}

func TestBoundedDiagnostic_IsASCIIControlFreeAndBounded(t *testing.T) {
	got := boundedDiagnostic(strings.Repeat("секрет\n", 100))
	if len(got) > 640 {
		t.Fatalf("len=%d diagnostic=%q", len(got), got)
	}
	for _, r := range got {
		if r > 127 || (r < 32 && r != '\t') {
			t.Fatalf("unsafe rune %U in %q", r, got)
		}
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("raw newline in %q", got)
	}
}

func TestSelectEnvironment_InvalidManualPathIsFatalWithoutFallback(t *testing.T) {
	inspector := &fakeInspector{byHome: map[string]inspectResult{
		"relative": {state: environment.Foreign, err: &environment.Error{State: environment.Foreign, Detail: "relative"}},
	}}
	var out bytes.Buffer
	runner := Runner{Environments: inspector, Out: &out, Err: io.Discard, GOOS: runtime.GOOS, Executable: os.Executable}
	q := newQuestionnaire(strings.NewReader("relative\n"), &out)
	_, err := runner.selectEnvironment(context.Background(), q, options{}, &registrySession{})
	var operational *operationalError
	if !errors.As(err, &operational) || operational.Code != codeForeignWorkspace || inspector.inspectCalls != 1 {
		t.Fatalf("calls=%d err=%T %v", inspector.inspectCalls, err, err)
	}
}
