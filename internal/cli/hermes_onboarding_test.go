package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestPrepareHermesHome_PromptedAndExplicitOrder(t *testing.T) {
	root := testutil.TempDir(t)
	hermesHome := filepath.Join(root, "hermes")
	kitHome := filepath.Join(root, "kit")
	for _, test := range []struct {
		name         string
		opts         options
		input        string
		wantPrompted bool
	}{
		{
			name:  "prompted Hermes home before project catalog",
			opts:  options{operatingSystem: "windows", application: string(domain.AppHermes)},
			input: hermesHome + "\n" + kitHome + "\n", wantPrompted: true,
		},
		{
			name:  "explicit Hermes home still completes project catalog",
			opts:  options{operatingSystem: "windows", application: string(domain.AppHermes), hermesHome: hermesHome, hermesHomeSet: true},
			input: kitHome + "\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			configured := ""
			runner := Runner{ConfigureHermesHome: func(value string) error { configured = value; return nil }}
			q := newQuestionnaire(strings.NewReader(test.input), &out)
			if err := runner.prepareHermesHome(context.Background(), &test.opts, q); err != nil {
				t.Fatal(err)
			}
			if test.opts.hermesHome != hermesHome || test.opts.kitHome != kitHome || configured != hermesHome {
				t.Fatalf("opts=%#v configured=%q", test.opts, configured)
			}
			hermesPrompt := strings.Index(out.String(), "Введите короткий каталог установки Hermes")
			kitPrompt := strings.Index(out.String(), "Введите каталог для проектов")
			if test.wantPrompted {
				if hermesPrompt < 0 || kitPrompt < 0 || hermesPrompt >= kitPrompt {
					t.Fatalf("prompt order=%q", out.String())
				}
			} else if hermesPrompt >= 0 || kitPrompt < 0 {
				t.Fatalf("explicit prompts=%q", out.String())
			}
		})
	}
}

func TestPrepareHermesHome_RejectsInvalidAndOverlappingPaths(t *testing.T) {
	root := testutil.TempDir(t)
	hermesHome := filepath.Join(root, "hermes")
	kitHome := filepath.Join(root, "kit")
	nonCleanHermes := hermesHome + string(filepath.Separator) + ".." + string(filepath.Separator) + "hermes"
	nonCleanKit := kitHome + string(filepath.Separator) + ".." + string(filepath.Separator) + "kit"
	for _, test := range []struct {
		name, hermesHome, kitHome string
	}{
		{"relative Hermes", "hermes", kitHome},
		{"non-clean Hermes", nonCleanHermes, kitHome},
		{"relative kit", hermesHome, "kit"},
		{"non-clean kit", hermesHome, nonCleanKit},
		{"equal", hermesHome, hermesHome},
		{"Hermes ancestor", root, hermesHome},
		{"Hermes descendant", filepath.Join(kitHome, "hermes"), kitHome},
	} {
		t.Run(test.name, func(t *testing.T) {
			configured := 0
			runner := Runner{ConfigureHermesHome: func(string) error { configured++; return nil }}
			opts := options{operatingSystem: "windows", application: "hermes", hermesHome: test.hermesHome, kitHome: test.kitHome}
			err := runner.prepareHermesHome(context.Background(), &opts, newQuestionnaire(strings.NewReader(""), &bytes.Buffer{}))
			if err == nil || configured != 0 {
				t.Fatalf("err=%v configured=%d", err, configured)
			}
		})
	}
}

func TestPrepareHermesHome_ReportsPersistenceWithoutMutationHeader(t *testing.T) {
	root := testutil.TempDir(t)
	for _, test := range []struct {
		name    string
		persist error
		want    string
	}{
		{name: "success", want: "Переменная HERMES_HOME для текущего пользователя .. сохранение\nПеременная HERMES_HOME для текущего пользователя .. готово\n"},
		{name: "failure", persist: errors.New("persist rejected"), want: "Переменная HERMES_HOME для текущего пользователя .. сохранение\nПеременная HERMES_HOME для текущего пользователя .. ошибка\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			runner := Runner{Out: &out, ConfigureHermesHome: func(string) error { return test.persist }}
			opts := options{operatingSystem: "windows", application: "hermes", hermesHome: filepath.Join(root, "hermes"), kitHome: filepath.Join(root, "kit")}
			err := runner.prepareHermesHome(context.Background(), &opts, newQuestionnaire(strings.NewReader(""), &out))
			if (err != nil) != (test.persist != nil) {
				t.Fatalf("err=%v", err)
			}
			if out.String() != test.want || strings.Contains(out.String(), "Обработка данных") {
				t.Fatalf("out=%q want=%q", out.String(), test.want)
			}
		})
	}
}

func TestPrepareHermesHome_JSONHasNoPersistenceProgress(t *testing.T) {
	root := testutil.TempDir(t)
	var out bytes.Buffer
	runner := Runner{Out: &out, ConfigureHermesHome: func(string) error { return nil }}
	opts := options{operatingSystem: "windows", application: "hermes", hermesHome: filepath.Join(root, "hermes"), kitHome: filepath.Join(root, "kit"), jsonOutput: true}
	if err := runner.prepareHermesHome(context.Background(), &opts, newQuestionnaire(strings.NewReader(""), &out)); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("json preflight output=%q", out.String())
	}
}

type progressPanicWriter struct{ calls int }

func (w *progressPanicWriter) Write([]byte) (int, error) {
	w.calls++
	panic("progress writer panic")
}

type progressErrorWriter struct{ calls int }

func (w *progressErrorWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, errors.New("progress writer rejected")
}

func TestPrepareHermesHome_ProgressWriterFailureDoesNotChangePersistenceResult(t *testing.T) {
	root := testutil.TempDir(t)
	persistFailure := errors.New("persist rejected")
	for _, test := range []struct {
		name   string
		writer interface {
			Write([]byte) (int, error)
		}
		persistErr error
	}{
		{name: "panic writer success", writer: &progressPanicWriter{}},
		{name: "panic writer persistence error", writer: &progressPanicWriter{}, persistErr: persistFailure},
		{name: "error writer success", writer: &progressErrorWriter{}},
		{name: "error writer persistence error", writer: &progressErrorWriter{}, persistErr: persistFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			configured := 0
			runner := Runner{Out: test.writer, ConfigureHermesHome: func(string) error { configured++; return test.persistErr }}
			opts := options{operatingSystem: "windows", application: "hermes", hermesHome: filepath.Join(root, "hermes"), kitHome: filepath.Join(root, "kit")}
			err := runner.prepareHermesHome(context.Background(), &opts, newQuestionnaire(strings.NewReader(""), test.writer))
			if configured != 1 {
				t.Fatalf("ConfigureHermesHome calls=%d want=1", configured)
			}
			if test.persistErr == nil && err != nil {
				t.Fatalf("prepareHermesHome() error=%v want=nil", err)
			}
			if test.persistErr != nil && !errors.Is(err, test.persistErr) {
				t.Fatalf("prepareHermesHome() error=%v want wrapped %v", err, test.persistErr)
			}
			switch writer := test.writer.(type) {
			case *progressPanicWriter:
				if writer.calls != 1 {
					t.Fatalf("writer calls=%d want=1", writer.calls)
				}
			case *progressErrorWriter:
				if writer.calls != 1 {
					t.Fatalf("writer calls=%d want=1", writer.calls)
				}
			}
		})
	}
}

func TestRunHermesHomePersistenceFailure_StopsBeforeAllEffects(t *testing.T) {
	root := testutil.TempDir(t)
	hermesHome := filepath.Join(root, "hermes")
	kitHome := filepath.Join(root, "kit")
	service := &fakeService{}
	discoveries := 0
	credentials := &countingCredentialSource{}
	var out, errOut bytes.Buffer
	runner := Runner{
		Service: service, Credentials: credentials, In: strings.NewReader(""), Out: &out, Err: &errOut,
		ConfigureHermesHome: func(string) error { return errors.New("persist rejected") },
		HermesDiscovery: func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
			discoveries++
			return hermes.DiscoveryResult{}, nil
		},
	}
	if code := runner.Run(context.Background(), continuationArgs(kitHome, hermesHome)); code == ExitOK {
		t.Fatalf("unexpected success: out=%q err=%q", out.String(), errOut.String())
	}
	if discoveries != 0 || credentials.calls != 0 || service.plans != 0 || service.applies != 0 || service.statuses != 0 {
		t.Fatalf("discovery=%d credentials=%d service=%+v", discoveries, credentials.calls, service)
	}
	for _, path := range []string{hermesHome, kitHome} {
		if _, err := filepath.Abs(path); err != nil {
			t.Fatal(err)
		}
		if exists, err := pathExists(path); err != nil || exists {
			t.Fatalf("path side effect %q exists=%t err=%v", path, exists, err)
		}
	}
}

func TestRunHermesHomeMissingPersistenceDependency_StopsBeforeAllEffects(t *testing.T) {
	root := testutil.TempDir(t)
	hermesHome := filepath.Join(root, "hermes")
	kitHome := filepath.Join(root, "kit")
	service := &fakeService{}
	credentials := &countingCredentialSource{}
	discoveries := 0
	var out, errOut bytes.Buffer
	runner := Runner{
		Service: service, Credentials: credentials, In: strings.NewReader(""), Out: &out, Err: &errOut,
		HermesDiscovery: func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
			discoveries++
			return hermes.DiscoveryResult{Installed: true, Home: hermesHome}, nil
		},
	}
	if code := runner.Run(context.Background(), continuationArgs(kitHome, hermesHome)); code != ExitFailure {
		t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "HERMES_HOME_PERSIST_REQUIRED") {
		t.Fatalf("err=%q", errOut.String())
	}
	if discoveries != 0 || credentials.calls != 0 || service.plans != 0 || service.applies != 0 || service.statuses != 0 {
		t.Fatalf("discoveries=%d credentials=%d service=%+v", discoveries, credentials.calls, service)
	}
	for _, path := range []string{hermesHome, kitHome} {
		if exists, err := pathExists(path); err != nil || exists {
			t.Fatalf("path side effect %q exists=%t err=%v", path, exists, err)
		}
	}
}

func TestRunHermesContinuation_SkipsWizardAndDoesNotGuessMissingSelectors(t *testing.T) {
	root := testutil.TempDir(t)
	hermesHome := filepath.Join(root, "hermes")
	kitHome := filepath.Join(root, "kit")
	newRunner := func(input string, out, errOut *bytes.Buffer) (Runner, *fakeService) {
		service := &fakeService{hasPlan: true}
		return Runner{
			Service: service, In: strings.NewReader(input), Out: out, Err: errOut,
			ConfigureHermesHome: func(string) error { return nil },
			HermesDiscovery: func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
				return hermes.DiscoveryResult{Installed: true, Home: hermesHome}, nil
			},
		}, service
	}

	var completeOut, completeErr bytes.Buffer
	runner, service := newRunner("", &completeOut, &completeErr)
	if code := runner.Run(context.Background(), continuationArgs(kitHome, hermesHome)); code != ExitOK {
		t.Fatalf("complete exit=%d out=%q err=%q", code, completeOut.String(), completeErr.String())
	}
	for _, forbidden := range []string{"Что вы хотите сделать", "Выберите проект", "Выберите роль", "Выберите набор skills"} {
		if strings.Contains(completeOut.String(), forbidden) {
			t.Fatalf("complete continuation repeated %q in %q", forbidden, completeOut.String())
		}
	}
	if service.plans != 1 {
		t.Fatalf("plans=%d", service.plans)
	}

	var incompleteOut, incompleteErr bytes.Buffer
	incomplete, incompleteService := newRunner("1\n", &incompleteOut, &incompleteErr)
	args := continuationArgs(kitHome, hermesHome)
	args = removeFlag(args, "--project")
	if code := incomplete.Run(context.Background(), args); code != ExitUsage {
		t.Fatalf("incomplete exit=%d out=%q err=%q", code, incompleteOut.String(), incompleteErr.String())
	}
	for _, want := range []string{"Что вы хотите сделать", "Выберите проект"} {
		if !strings.Contains(incompleteOut.String(), want) {
			t.Fatalf("incomplete command guessed selector; missing %q in %q", want, incompleteOut.String())
		}
	}
	if incompleteService.plans != 0 {
		t.Fatalf("incomplete plans=%d", incompleteService.plans)
	}
}

func TestRunHermesContinuation_ReappliesHermesHomeIdempotently(t *testing.T) {
	root := testutil.TempDir(t)
	hermesHome := filepath.Join(root, "hermes")
	kitHome := filepath.Join(root, "kit")
	service := &fakeService{hasPlan: true}
	var configured []string
	discoveries := 0
	runner := Runner{
		Service: service, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		ConfigureHermesHome: func(value string) error { configured = append(configured, value); return nil },
		HermesDiscovery: func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
			discoveries++
			return hermes.DiscoveryResult{Installed: true, Home: hermesHome}, nil
		},
	}
	args := continuationArgs(kitHome, hermesHome)
	for attempt := 0; attempt < 2; attempt++ {
		if code := runner.Run(context.Background(), args); code != ExitOK {
			t.Fatalf("attempt=%d exit=%d", attempt+1, code)
		}
	}
	if !reflect.DeepEqual(configured, []string{hermesHome, hermesHome}) || discoveries != 2 || service.plans != 2 {
		t.Fatalf("configured=%v discoveries=%d plans=%d", configured, discoveries, service.plans)
	}
}

func TestRunHermesContinuation_NonInteractiveShapeRejectedBeforeEffects(t *testing.T) {
	root := testutil.TempDir(t)
	hermesHome := filepath.Join(root, "hermes")
	kitHome := filepath.Join(root, "kit")
	service := &fakeService{}
	credentials := &countingCredentialSource{}
	configured, discoveries := 0, 0
	var out, errOut bytes.Buffer
	runner := Runner{
		Service: service, Credentials: credentials, In: strings.NewReader(""), Out: &out, Err: &errOut,
		ConfigureHermesHome: func(string) error { configured++; return nil },
		HermesDiscovery: func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
			discoveries++
			return hermes.DiscoveryResult{Installed: true, Home: hermesHome}, nil
		},
	}
	args := append(continuationArgs(kitHome, hermesHome), "--non-interactive")
	if code := runner.Run(context.Background(), args); code != ExitUsage {
		t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if configured != 0 || discoveries != 0 || credentials.calls != 0 || service.plans != 0 || service.applies != 0 || service.statuses != 0 {
		t.Fatalf("configured=%d discoveries=%d credentials=%d service=%+v", configured, discoveries, credentials.calls, service)
	}
	for _, path := range []string{hermesHome, kitHome} {
		if exists, err := pathExists(path); err != nil || exists {
			t.Fatalf("path side effect %q exists=%t err=%v", path, exists, err)
		}
	}
}

func TestFormatHermesContinuation_QuotesGraphicUnicodeAndRejectsControls(t *testing.T) {
	opts := options{
		operatingSystem: "windows", application: "hermes", appInstalled: "true",
		kitHome: `C:\Проекты O'Kit`, hermesHome: `C:\Гермес O'Neil`,
		project: "проект O'дин", role: "роль O'два", toolchain: "набор O'три", update: "none",
		installerPath: "installer-path-canary", certificates: "certificate-path-canary",
	}
	command, err := formatHermesContinuation(`C:\Инструменты O'Kit\teamkit.exe`, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`$env:HERMES_HOME = 'C:\Гермес O''Neil'`,
		`& 'C:\Инструменты O''Kit\teamkit.exe' apply`,
		`--kit-home 'C:\Проекты O''Kit'`,
		`--project 'проект O''дин'`,
		`--role 'роль O''два'`,
		`--toolchain 'набор O''три'`,
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("command=%q missing %q", command, want)
		}
	}
	for _, canary := range []string{"saved-secret-canary", "replacement-secret-canary", "installer-path-canary", "certificate-path-canary"} {
		if strings.Contains(command, canary) {
			t.Fatalf("command leaked %q: %q", canary, command)
		}
	}
	invalidScope := opts
	invalidScope.update = "both"
	if got, err := formatHermesContinuation(`C:\teamkit.exe`, invalidScope); err == nil || got != "" {
		t.Fatalf("non-none continuation scope rendered command=%q err=%v", got, err)
	}

	for _, field := range []string{"executable", "kit-home", "hermes-home", "project", "role", "toolchain"} {
		t.Run("reject "+field, func(t *testing.T) {
			unsafe := opts
			executable := `C:\teamkit.exe`
			value := "safe\nWrite-Output injected"
			switch field {
			case "executable":
				executable = value
			case "kit-home":
				unsafe.kitHome = value
			case "hermes-home":
				unsafe.hermesHome = value
			case "project":
				unsafe.project = value
			case "role":
				unsafe.role = value
			case "toolchain":
				unsafe.toolchain = value
			}
			got, err := formatHermesContinuation(executable, unsafe)
			if err == nil || got != "" {
				t.Fatalf("unsafe %s rendered command=%q err=%v", field, got, err)
			}
		})
	}
}

func TestRunHermesMissingExactExecutable_InitialAndContinuationUseSameHandoff(t *testing.T) {
	root := testutil.TempDir(t)
	hermesHome := filepath.Join(root, "Гермес O'Neil")
	kitHome := filepath.Join(root, "Проекты O'Kit")
	if err := os.MkdirAll(hermesHome, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "Инструменты O'Kit", "teamkit.exe")
	newRunner := func(input string, out, errOut *bytes.Buffer) (Runner, *fakeService, *countingCredentialSource) {
		service := &fakeService{}
		credentialSource := &countingCredentialSource{}
		return Runner{
			Service: service, Credentials: credentialSource, In: strings.NewReader(input), Out: out, Err: errOut,
			Executable:          func() (string, error) { return executable, nil },
			ConfigureHermesHome: func(string) error { return nil },
			HermesDiscovery: func(ctx context.Context, request hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
				return hermes.Discover(ctx, request, hermes.DiscoveryDependencies{})
			},
		}, service, credentialSource
	}

	initialInput := strings.Join([]string{
		choiceAnswer("add", applyModeChoices()),
		choiceAnswer("windows", operatingSystemChoices()),
		choiceAnswer("hermes", applicationChoices()),
		hermesHome,
		kitHome,
		choiceAnswer("asku", projectChoices()),
		choiceAnswer("analyst", roleChoices()),
		choiceAnswer("cc_1c_skills", toolchainChoices()),
	}, "\n") + "\n"
	var initialOut, initialErr bytes.Buffer
	initial, initialService, initialCredentials := newRunner(initialInput, &initialOut, &initialErr)
	if code := initial.Run(context.Background(), []string{"apply"}); code != ExitApplicationRequired {
		t.Fatalf("initial exit=%d out=%q err=%q", code, initialOut.String(), initialErr.String())
	}

	var continuationOut, continuationErr bytes.Buffer
	continuation, continuationService, continuationCredentials := newRunner("", &continuationOut, &continuationErr)
	if code := continuation.Run(context.Background(), continuationArgs(kitHome, hermesHome)); code != ExitApplicationRequired {
		t.Fatalf("continuation exit=%d out=%q err=%q", code, continuationOut.String(), continuationErr.String())
	}

	for name, transcript := range map[string]string{"initial": initialOut.String(), "continuation": continuationOut.String()} {
		last := -1
		for _, fragment := range []string{
			"Отключите почтовый VPN только на время установки Hermes.",
			"Установите Hermes в выбранный HERMES_HOME: " + hermesHome,
			"После установки включите почтовый VPN для доступа к почтовым сервисам.",
			"Только после включения VPN выполните в PowerShell:",
			"$env:HERMES_HOME = " + powerShellQuote(hermesHome),
			"& " + powerShellQuote(executable) + " apply",
			"--app-installed=true", "--os windows", "--app hermes",
			"--kit-home " + powerShellQuote(kitHome), "--hermes-home " + powerShellQuote(hermesHome),
			"--project " + powerShellQuote("asku"), "--role " + powerShellQuote("analyst"),
			"--toolchain " + powerShellQuote("cc_1c_skills"), "--update none",
		} {
			index := strings.Index(transcript, fragment)
			if index < 0 || index < last {
				t.Fatalf("%s transcript order failed at %q: %q", name, fragment, transcript)
			}
			last = index
		}
	}
	combined := initialOut.String() + initialErr.String() + continuationOut.String() + continuationErr.String()
	for _, forbidden := range []string{"TEAMKIT_FAILED", "HERMES_EXECUTABLE_UNVERIFIED", "RETRY_REQUIRED", "saved-secret-canary", "replacement-secret-canary"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("handoff contains %q: %q", forbidden, combined)
		}
	}
	if initialErr.Len() != 0 || continuationErr.Len() != 0 || initialService.plans != 0 || continuationService.plans != 0 || initialCredentials.calls != 0 || continuationCredentials.calls != 0 {
		t.Fatalf("initial err=%q service=%+v creds=%d; continuation err=%q service=%+v creds=%d", initialErr.String(), initialService, initialCredentials.calls, continuationErr.String(), continuationService, continuationCredentials.calls)
	}
}

func TestRunHermesInitialWizard_ExplicitInstalledMissingExecutableUsesHandoff(t *testing.T) {
	root := testutil.TempDir(t)
	hermesHome := filepath.Join(root, "hermes")
	kitHome := filepath.Join(root, "kit")
	if err := os.MkdirAll(hermesHome, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "teamkit.exe")
	input := strings.Join([]string{
		choiceAnswer("add", applyModeChoices()),
		hermesHome,
		kitHome,
		choiceAnswer("asku", projectChoices()),
		choiceAnswer("analyst", roleChoices()),
		choiceAnswer("cc_1c_skills", toolchainChoices()),
	}, "\n") + "\n"
	service := &fakeService{}
	credentials := &countingCredentialSource{}
	configured := 0
	var out, errOut bytes.Buffer
	runner := Runner{
		Service: service, Credentials: credentials, In: strings.NewReader(input), Out: &out, Err: &errOut,
		Executable: func() (string, error) { return executable, nil },
		ConfigureHermesHome: func(value string) error {
			configured++
			if value != hermesHome {
				t.Fatalf("configured=%q want=%q", value, hermesHome)
			}
			return nil
		},
		HermesDiscovery: func(ctx context.Context, request hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
			return hermes.Discover(ctx, request, hermes.DiscoveryDependencies{})
		},
	}
	args := []string{"apply", "--app-installed=true", "--os", "windows", "--app", "hermes"}
	if code := runner.Run(context.Background(), args); code != ExitApplicationRequired {
		t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	for _, want := range []string{"Выберите проект", "Отключите почтовый VPN только на время установки Hermes.", "$env:HERMES_HOME = " + powerShellQuote(hermesHome), "& " + powerShellQuote(executable) + " apply", "--app-installed=true", "--update none"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("out=%q missing %q", out.String(), want)
		}
	}
	if errOut.Len() != 0 || configured != 1 || credentials.calls != 0 || service.plans != 0 || service.applies != 0 || service.statuses != 0 {
		t.Fatalf("err=%q configured=%d credentials=%d service=%+v", errOut.String(), configured, credentials.calls, service)
	}
}

func TestRunHermesContinuation_InvalidSchemaIsConfigurationFailureWithoutEffects(t *testing.T) {
	for _, test := range []struct {
		name    string
		content *string
	}{
		{"missing", nil},
		{"ambiguous", stringPointer("DEFAULT_CONFIG = {'_config_version': 39}\nDEFAULT_CONFIG = {'_config_version': 40}\n")},
		{"nonpositive", stringPointer("DEFAULT_CONFIG = {'_config_version': 0}\n")},
		{"unparseable", stringPointer("DEFAULT_CONFIG = {'_config_version': 'thirty-nine'}\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := testutil.TempDir(t)
			hermesHome := filepath.Join(root, "hermes")
			kitHome := filepath.Join(root, "kit")
			executable := filepath.Join(hermesHome, "hermes-agent", "venv", "Scripts", "hermes.exe")
			if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(executable, []byte("arbitrary-runtime-bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			if test.content != nil {
				config := filepath.Join(hermesHome, "hermes-agent", "hermes_cli", "config_defaults.py")
				if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(config, []byte(*test.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			service := &fakeService{}
			credentialSource := &countingCredentialSource{}
			var out, errOut bytes.Buffer
			runner := Runner{
				Service: service, Credentials: credentialSource, In: strings.NewReader(""), Out: &out, Err: &errOut,
				ConfigureHermesHome: func(string) error { return nil },
				HermesDiscovery: func(ctx context.Context, request hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
					return hermes.Discover(ctx, request, hermes.DiscoveryDependencies{})
				},
			}
			if code := runner.Run(context.Background(), continuationArgs(kitHome, hermesHome)); code != ExitFailure {
				t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errOut.String())
			}
			if !strings.HasPrefix(errOut.String(), "HERMES_CONFIG_SCHEMA_UNSUPPORTED:") {
				t.Fatalf("schema identity=%q", errOut.String())
			}
			for _, forbidden := range []string{"Отключите почтовый VPN", "TEAMKIT_FAILED", "HERMES_EXECUTABLE_UNVERIFIED", "RETRY_REQUIRED"} {
				if strings.Contains(out.String()+errOut.String(), forbidden) {
					t.Fatalf("schema failure contains %q: out=%q err=%q", forbidden, out.String(), errOut.String())
				}
			}
			if service.plans != 0 || service.applies != 0 || service.statuses != 0 || credentialSource.calls != 0 {
				t.Fatalf("service=%+v credentials=%d", service, credentialSource.calls)
			}
		})
	}
}

type countingCredentialSource struct{ calls int }

func (c *countingCredentialSource) Resolve(context.Context, domain.DesiredState, bool) (map[string]string, error) {
	c.calls++
	return map[string]string{}, nil
}

func continuationArgs(kitHome, hermesHome string) []string {
	return []string{"apply", "--app-installed=true", "--os", "windows", "--app", "hermes", "--kit-home", kitHome, "--hermes-home", hermesHome, "--project", "asku", "--role", "analyst", "--toolchain", "cc_1c_skills", "--update", "none"}
}

func removeFlag(args []string, name string) []string {
	result := make([]string, 0, len(args)-2)
	for index := 0; index < len(args); index++ {
		if args[index] == name {
			index++
			continue
		}
		result = append(result, args[index])
	}
	return result
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func choiceAnswer(value string, choices []choice) string {
	for index, item := range choices {
		if item.value == value {
			return strconv.Itoa(index + 1)
		}
	}
	panic(fmt.Sprintf("choice %q is unavailable", value))
}

func stringPointer(value string) *string {
	return &value
}
