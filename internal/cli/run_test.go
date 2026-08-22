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
	"time"

	"github.com/mi1man-cmd/kit-all-team/internal/apps"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/registry"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

type fakeService struct {
	command                   string
	desired                   domain.DesiredState
	update                    reconcile.UpdateChoice
	secrets                   map[string]string
	err, applyErr             error
	plan                      reconcile.OperationPlan
	hasPlan                   bool
	applyResult, updateResult *reconcile.OperationPlan
	plans, applies, updates   int
	verifiedUpdates           int
	verified                  environment.VerifiedEnvironment
	retries, statuses         int
	status                    reconcile.PlanStatus
	statusPlan                reconcile.OperationPlan
	statusErr                 error
}

func copyPlan(plan reconcile.OperationPlan) reconcile.OperationPlan {
	copied := plan
	copied.Actions = append([]reconcile.Action(nil), plan.Actions...)
	return copied
}

func (f *fakeService) Plan(_ context.Context, desired domain.DesiredState, update reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	f.command, f.desired, f.update, f.plans = "plan", desired, update, f.plans+1
	if f.hasPlan {
		return f.plan, f.err
	}
	return oneActionPlan(), f.err
}

func (f *fakeService) Apply(_ context.Context, desired domain.DesiredState, update reconcile.UpdateChoice, inputs ApplyInputs) (reconcile.OperationPlan, error) {
	f.command, f.desired, f.update, f.secrets, f.applies = "apply", desired, update, inputs.Secrets, f.applies+1
	if f.applyErr != nil {
		return oneActionPlan(), f.applyErr
	}
	if f.applyResult != nil {
		return copyPlan(*f.applyResult), f.err
	}
	return oneActionPlan(), f.err
}

func (f *fakeService) Status(context.Context, string) (reconcile.PlanStatus, reconcile.OperationPlan, error) {
	f.statuses++
	if f.command == "" {
		f.command = "status"
	}
	status := f.status
	if status == "" {
		status = reconcile.StatusReady
	}
	return status, f.statusPlan, firstError(f.statusErr, f.err)
}

func (f *fakeService) Retry(context.Context, string) error {
	f.command, f.retries = "retry", f.retries+1
	return f.err
}

func (f *fakeService) Update(_ context.Context, _ string, update reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	f.command, f.update, f.updates = "update", update, f.updates+1
	return f.updateResponse()
}

func (f *fakeService) UpdateVerified(_ context.Context, verified environment.VerifiedEnvironment, update reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	f.command, f.update, f.updates = "update", update, f.updates+1
	f.verifiedUpdates++
	f.verified = verified
	return f.updateResponse()
}

func (f *fakeService) updateResponse() (reconcile.OperationPlan, error) {
	if f.updateResult != nil {
		return copyPlan(*f.updateResult), f.err
	}
	return oneActionPlan(), f.err
}

type fixedCredentials map[string]string

func (f fixedCredentials) Resolve(context.Context, domain.DesiredState, bool) (map[string]string, error) {
	return f, nil
}

type planCredentials struct {
	actions []reconcile.Action
	calls   int
}

func (p *planCredentials) Resolve(context.Context, domain.DesiredState, bool) (map[string]string, error) {
	p.calls++
	return map[string]string{}, nil
}

func (p *planCredentials) ResolveForPlan(_ context.Context, _ domain.DesiredState, actions []reconcile.Action, _ bool) (map[string]string, error) {
	p.calls++
	p.actions = append([]reconcile.Action(nil), actions...)
	return map[string]string{}, nil
}

func TestRunPlanNonInteractiveEmitsJSONAndValidatedDesiredState(t *testing.T) {
	service := &fakeService{}
	var stdout, stderr bytes.Buffer
	runner := Runner{Service: service, In: strings.NewReader(""), Out: &stdout, Err: &stderr}
	code := runner.Run(context.Background(), []string{
		"plan", "--non-interactive", "--json", "--os", "windows", "--app", "hermes",
		"--app-installed=true", "--kit-home", `C:\kit`, "--hermes-home", `C:\hermes`,
		"--project", "wms", "--role", "developer", "--toolchain", "ai_rules_1c",
	})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if service.command != "plan" || service.desired.Project() != domain.ProjectWMS {
		t.Fatalf("service call = %q desired=%+v", service.command, service.desired)
	}
	if !strings.Contains(stdout.String(), `"command":"plan"`) || !strings.Contains(stdout.String(), `"20-sync-content"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunApplyNeverAcceptsSecretFlagsAndRedactsServiceError(t *testing.T) {
	const (
		gitLabCanary     = "TEAMKIT_TOKEN_CANARY"
		jiraCanary       = "jira-personal-canary-7xQ2mN9pL4vK8dR6"
		confluenceCanary = "confluence-personal-canary-3wF8sT5yH2cJ9nM7"
	)
	service := &fakeService{applyErr: errors.New("provider rejected " + strings.Join([]string{gitLabCanary, jiraCanary, confluenceCanary}, ","))}
	var stdout, stderr bytes.Buffer
	runner := Runner{
		Service: service, Credentials: fixedCredentials{
			"GITLAB_TOKEN": gitLabCanary, "HERMES_CUSTOM_ISSUE_TRACKER_TOKEN": jiraCanary, "HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN": confluenceCanary,
		},
		In: strings.NewReader(""), Out: &stdout, Err: &stderr,
	}
	for _, jsonOutput := range []bool{false, true} {
		t.Run(fmt.Sprintf("json-%t", jsonOutput), func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			args := linuxArgs("apply")
			if jsonOutput {
				args = append(args, "--json")
			}
			code := runner.Run(context.Background(), args)
			if code == 0 {
				t.Fatal("exit=0, want failure")
			}
			for _, canary := range []string{gitLabCanary, jiraCanary, confluenceCanary} {
				if strings.Contains(stdout.String()+stderr.String(), canary) {
					t.Fatalf("secret leaked: stdout=%q stderr=%q", stdout.String(), stderr.String())
				}
			}
		})
	}
	if service.secrets["GITLAB_TOKEN"] != gitLabCanary || service.secrets["HERMES_CUSTOM_ISSUE_TRACKER_TOKEN"] != jiraCanary || service.secrets["HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN"] != confluenceCanary {
		t.Fatalf("credential source was not passed to service: %#v", service.secrets)
	}

	stderr.Reset()
	code := runner.Run(context.Background(), append(linuxArgs("apply"), "--gitlab-token", gitLabCanary))
	if code != ExitUsage || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("secret flag exit=%d stderr=%q", code, stderr.String())
	}
}

func TestRunApplyPlansBeforeResolvingPlanAwareCredentials(t *testing.T) {
	service := &fakeService{hasPlan: true, plan: reconcile.OperationPlan{Actions: []reconcile.Action{{Kind: reconcile.ActionConfigureApplication}}}}
	credentials := &planCredentials{}
	runner := Runner{Service: service, Credentials: credentials, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if code := runner.Run(context.Background(), linuxArgs("apply")); code != ExitOK {
		t.Fatalf("exit=%d", code)
	}
	if service.plans != 1 || service.applies != 1 || service.statuses != 1 || credentials.calls != 1 {
		t.Fatalf("plans=%d applies=%d statuses=%d credential calls=%d", service.plans, service.applies, service.statuses, credentials.calls)
	}
	if !reflect.DeepEqual(credentials.actions, service.plan.Actions) {
		t.Fatalf("credential actions=%#v plan=%#v", credentials.actions, service.plan.Actions)
	}
}

func TestRunApplyReportsReobservedReadyState(t *testing.T) {
	service := &fakeService{
		hasPlan:    true,
		plan:       oneActionPlan(),
		status:     reconcile.StatusReady,
		statusPlan: reconcile.OperationPlan{},
	}
	var stdout, stderr bytes.Buffer
	runner := Runner{Service: service, In: strings.NewReader(""), Out: &stdout, Err: &stderr}
	if code := runner.Run(context.Background(), linuxArgs("apply")); code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if service.statuses != 1 || !strings.Contains(stdout.String(), "apply: ready") || strings.Contains(stdout.String(), "20-sync-content") {
		t.Fatalf("statuses=%d stdout=%q", service.statuses, stdout.String())
	}
}

func TestRunMutationFailsWhenFinalObservationFails(t *testing.T) {
	service := &fakeService{hasPlan: true, plan: oneActionPlan(), statusErr: errors.New("FINAL_STATE_DRIFT")}
	var stderr bytes.Buffer
	runner := Runner{Service: service, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &stderr}
	if code := runner.Run(context.Background(), linuxArgs("apply")); code == ExitOK || !strings.Contains(stderr.String(), "FINAL_STATE_DRIFT") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestRunRegistryPromotionPolicy(t *testing.T) {
	actionful := oneActionPlan()
	empty := reconcile.OperationPlan{}
	tests := []struct {
		name         string
		args         func(*testing.T) []string
		configure    func(*fakeService)
		wantPromotes int
	}{
		{"apply returned action and final ready", nativeApplyArgs, func(s *fakeService) {
			s.plan = actionful
			s.hasPlan = true
			s.applyResult = &actionful
			s.status = reconcile.StatusReady
		}, 1},
		{"preliminary action but apply returned empty", nativeApplyArgs, func(s *fakeService) {
			s.plan = actionful
			s.hasPlan = true
			s.applyResult = &empty
			s.status = reconcile.StatusReady
		}, 0},
		{"apply returned action but final needs apply", nativeApplyArgs, func(s *fakeService) {
			s.plan = actionful
			s.hasPlan = true
			s.applyResult = &actionful
			s.status = reconcile.StatusNeedsApply
		}, 0},
		{"direct update returned action and final ready", func(t *testing.T) []string {
			return []string{"update", "--kit-home", filepath.Join(testutil.TempDir(t), "kit"), "--target", "both"}
		}, func(s *fakeService) { s.updateResult = &actionful; s.status = reconcile.StatusReady }, 1},
		{"direct update returned empty", func(t *testing.T) []string {
			return []string{"update", "--kit-home", filepath.Join(testutil.TempDir(t), "kit"), "--target", "both"}
		}, func(s *fakeService) { s.updateResult = &empty; s.status = reconcile.StatusReady }, 0},
		{"direct update final needs apply", func(t *testing.T) []string {
			return []string{"update", "--kit-home", filepath.Join(testutil.TempDir(t), "kit"), "--target", "both"}
		}, func(s *fakeService) { s.updateResult = &actionful; s.status = reconcile.StatusNeedsApply }, 0},
		{"direct update none", func(t *testing.T) []string {
			return []string{"update", "--kit-home", filepath.Join(testutil.TempDir(t), "kit"), "--target", "none"}
		}, func(s *fakeService) { s.updateResult = &actionful; s.status = reconcile.StatusReady }, 0},
		{"retry final ready", func(t *testing.T) []string {
			return []string{"retry", "--kit-home", filepath.Join(testutil.TempDir(t), "kit")}
		}, func(s *fakeService) { s.status = reconcile.StatusReady }, 1},
		{"retry final needs apply", func(t *testing.T) []string {
			return []string{"retry", "--kit-home", filepath.Join(testutil.TempDir(t), "kit")}
		}, func(s *fakeService) { s.status = reconcile.StatusNeedsApply }, 0},
		{"status never promotes", func(t *testing.T) []string {
			return []string{"status", "--kit-home", filepath.Join(testutil.TempDir(t), "kit")}
		}, func(s *fakeService) { s.status = reconcile.StatusReady }, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeService{}
			test.configure(service)
			store := &fakeRegistry{state: registry.LoadMissing}
			runner := Runner{Service: service, Registry: store, In: strings.NewReader(""), Out: io.Discard, Err: io.Discard}
			if code := runner.Run(context.Background(), test.args(t)); code != ExitOK {
				t.Fatalf("exit=%d", code)
			}
			if store.loads != test.wantPromotes || store.promotes != test.wantPromotes {
				t.Fatalf("loads=%d promotes=%d want=%d", store.loads, store.promotes, test.wantPromotes)
			}
		})
	}
}

func TestRunPlanNeverLoadsRegistry(t *testing.T) {
	base := testutil.TempDir(t)
	tests := []struct {
		name  string
		args  []string
		input string
	}{
		{"noninteractive", append([]string{"plan"}, nativeApplyArgs(t)[1:]...), ""},
		{"interactive", []string{"plan"}, strings.Join([]string{"3", "1", filepath.Join(base, "kit"), "2", "2", "1", ""}, "\n")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeRegistry{state: registry.LoadValid}
			runner := Runner{Service: &fakeService{}, Registry: store, In: strings.NewReader(test.input), Out: io.Discard, Err: io.Discard, HermesDiscovery: installedHermes(t)}
			if code := runner.Run(context.Background(), test.args); code != ExitOK {
				t.Fatalf("exit=%d", code)
			}
			if store.loads != 0 || store.promotes != 0 {
				t.Fatalf("registry=%#v", store)
			}
		})
	}
}

func TestRunPromotionFailureWarnsButKeepsSuccessfulExit(t *testing.T) {
	actionful := oneActionPlan()
	registrySpy := &fakeRegistry{state: registry.LoadValid, promoteErr: errors.New(strings.Repeat("x", 2000) + "\n\x1b[31m")}
	service := &fakeService{hasPlan: true, plan: actionful, applyResult: &actionful, status: reconcile.StatusReady}
	var out, errOut bytes.Buffer
	runner := Runner{Service: service, Registry: registrySpy, In: strings.NewReader(""), Out: &out, Err: &errOut}
	if code := runner.Run(context.Background(), nativeApplyArgs(t)); code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	const prefix = "Предупреждение: не удалось обновить локальный реестр Team Kit:"
	if strings.Count(errOut.String(), prefix) != 1 || len(errOut.String()) > 768 || strings.ContainsRune(errOut.String(), '\x1b') {
		t.Fatalf("stderr len=%d %q", len(errOut.String()), errOut.String())
	}
	if service.applies != 1 || registrySpy.loads != 1 || registrySpy.promotes != 1 {
		t.Fatalf("service=%#v registry=%#v", service, registrySpy)
	}
}

type guardingRegistry struct {
	fakeRegistry
	path string
}

func (g *guardingRegistry) Promote(ctx context.Context, home string) error {
	if err := os.WriteFile(g.path, []byte("REWRITTEN"), 0o600); err != nil {
		return err
	}
	return g.fakeRegistry.Promote(ctx, home)
}

func TestRunCorruptAndUnavailableRegistryNeverRewriteAcrossMutationCommands(t *testing.T) {
	actionful := oneActionPlan()
	entries := []struct {
		name        string
		args        func(*testing.T, string) []string
		input       string
		interactive bool
	}{
		{"noninteractive apply", func(t *testing.T, _ string) []string { return nativeApplyArgs(t) }, "", false},
		{"direct update", func(_ *testing.T, home string) []string {
			return []string{"update", "--kit-home", home, "--target", "both"}
		}, "", false},
		{"retry", func(_ *testing.T, home string) []string { return []string{"retry", "--kit-home", home} }, "", false},
		{"interactive update", func(_ *testing.T, _ string) []string { return []string{"apply", "--update", "both"} }, "2\n", true},
	}
	for _, loadState := range []registry.LoadState{registry.LoadCorrupt, registry.LoadUnavailable} {
		for _, entry := range entries {
			t.Run(fmt.Sprintf("%d/%s", loadState, entry.name), func(t *testing.T) {
				home := filepath.Join(testutil.TempDir(t), "kit")
				registryPath := filepath.Join(testutil.TempDir(t), "environments.json")
				original := []byte("USER-REGISTRY-BYTES\n")
				if err := os.WriteFile(registryPath, original, 0o600); err != nil {
					t.Fatal(err)
				}
				store := &guardingRegistry{fakeRegistry: fakeRegistry{state: loadState, loadErr: errors.New("load disabled")}, path: registryPath}
				service := &fakeService{plan: actionful, hasPlan: true, applyResult: &actionful, updateResult: &actionful, status: reconcile.StatusReady}
				inspector := &fakeInspector{}
				if entry.interactive {
					t.Setenv("KIT_ALL_TEAM_HOME", home)
					inspector.byHome = map[string]inspectResult{home: {verified: verifiedEnvironment(t, home, "apa"), state: environment.Ready}}
				}
				var errOut bytes.Buffer
				runner := Runner{Service: service, Registry: store, Environments: inspector, In: strings.NewReader(entry.input), Out: io.Discard, Err: &errOut, GOOS: runtime.GOOS, Executable: os.Executable}
				if exit := runner.Run(context.Background(), entry.args(t, home)); exit != ExitOK {
					t.Fatalf("exit=%d stderr=%q", exit, errOut.String())
				}
				const warning = "Предупреждение: локальный реестр Team Kit повреждён, недоступен или имеет неподдерживаемый формат и будет проигнорирован."
				after, err := os.ReadFile(registryPath)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Count(errOut.String(), warning) != 1 || store.loads != 1 || store.promotes != 0 || !bytes.Equal(after, original) {
					t.Fatalf("loads=%d promotes=%d stderr=%q bytes=%q", store.loads, store.promotes, errOut.String(), after)
				}
				if service.applies+service.updates+service.retries != 1 {
					t.Fatalf("product operation did not succeed: %#v", service)
				}
			})
		}
	}
}

func TestRunApplyNoOpDoesNotResolveCredentialsOrApply(t *testing.T) {
	service := &fakeService{hasPlan: true, plan: reconcile.OperationPlan{}}
	credentials := &planCredentials{}
	runner := Runner{Service: service, Credentials: credentials, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if code := runner.Run(context.Background(), linuxArgs("apply")); code != ExitOK {
		t.Fatalf("exit=%d", code)
	}
	if service.plans != 1 || service.applies != 0 || credentials.calls != 0 {
		t.Fatalf("plans=%d applies=%d credential calls=%d", service.plans, service.applies, credentials.calls)
	}
}

func TestRunApplyPrintsSecretFreeAlternativeApplicationHandoff(t *testing.T) {
	service := &fakeService{hasPlan: true, plan: reconcile.OperationPlan{Actions: []reconcile.Action{{Kind: reconcile.ActionConfigureApplication}}}}
	var stdout, stderr bytes.Buffer
	runner := Runner{Service: service, Credentials: fixedCredentials{"GITLAB_TOKEN": "TEAMKIT_HANDOFF_CANARY"}, In: strings.NewReader(""), Out: &stdout, Err: &stderr}
	args := []string{
		"apply", "--non-interactive", "--os", "linux", "--app", "codex", "--app-installed=true",
		"--kit-home", "/tmp/kit", "--project", "wms", "--role", "developer", "--toolchain", "cc_1c_skills",
	}
	if code := runner.Run(context.Background(), args); code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "handoff: ") || !strings.Contains(stdout.String(), "codex") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "TEAMKIT_HANDOFF_CANARY") {
		t.Fatalf("handoff leaked a secret: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunMapsMissingAlternativeApplicationToStableExit(t *testing.T) {
	service := &fakeService{err: apps.ErrApplicationRequired}
	var stderr bytes.Buffer
	runner := Runner{Service: service, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &stderr}
	code := runner.Run(context.Background(), linuxArgs("plan"))
	if code != ExitApplicationRequired || !strings.Contains(stderr.String(), "AI_APP_REQUIRED") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestRunNonInteractiveMissingAlternativeApplicationStopsBeforeEffects(t *testing.T) {
	for _, command := range []string{"plan", "apply"} {
		for _, installed := range []string{"false", "0", "False", "FALSE"} {
			t.Run(command+"/"+installed, func(t *testing.T) {
				service := &fakeService{}
				credentials := &planCredentials{}
				var stdout, stderr bytes.Buffer
				runner := Runner{Service: service, Credentials: credentials, In: strings.NewReader(""), Out: &stdout, Err: &stderr}
				code := runner.Run(context.Background(), []string{
					command, "--non-interactive", "--os", "linux", "--app", "codex", "--app-installed=" + installed,
					"--kit-home", "/must-not-be-inspected", "--project", "wms", "--role", "developer", "--toolchain", "cc_1c_skills",
				})

				if code != ExitApplicationRequired || !strings.Contains(stderr.String(), "AI_APP_REQUIRED") {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				if service.plans != 0 || service.applies != 0 || service.statuses != 0 || service.command != "" || credentials.calls != 0 {
					t.Fatalf("service=%+v credential calls=%d", service, credentials.calls)
				}
			})
		}
	}
}

func TestRunInteractiveAdd_AllMissingAlternativeApplicationsExitBeforeWorkspace(t *testing.T) {
	applications := apps.SupportedApplications()
	if len(applications) != 10 {
		t.Fatalf("supported alternative applications = %d, want 10", len(applications))
	}
	for index, application := range applications {
		t.Run(string(application), func(t *testing.T) {
			parent := testutil.TempDir(t)
			root := filepath.Join(parent, "must-not-exist")
			service := &fakeService{}
			inspector := &fakeInspector{}
			credentials := &planCredentials{}
			store := &fakeRegistry{state: registry.LoadMissing}
			input := fmt.Sprintf("1\n3\n%d\n2\n", index+2)
			var out, errOut bytes.Buffer
			runner := Runner{Service: service, Credentials: credentials, Registry: store, Environments: inspector, In: strings.NewReader(input), Out: &out, Err: &errOut}
			code := runner.Run(context.Background(), []string{"apply", "--kit-home", root})
			if code != ExitApplicationRequired || !strings.HasPrefix(errOut.String(), "AI_APP_REQUIRED: ") {
				t.Fatalf("exit=%d stderr=%q", code, errOut.String())
			}
			if strings.Contains(out.String(), "Выберите набор skills:") {
				t.Fatalf("toolchain prompt reached: %q", out.String())
			}
			if inspector.addCalls != 0 || inspector.inspectCalls != 0 || service.plans != 0 || service.applies != 0 || service.statuses != 0 || service.updates != 0 || service.retries != 0 || credentials.calls != 0 || store.loads != 0 || store.promotes != 0 {
				t.Fatalf("inspector=%#v service=%#v credentials=%d registry=%#v", inspector, service, credentials.calls, store)
			}
			if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("root created: %v", err)
			}
		})
	}
}

func TestRunNonInteractiveInvalidApplicationInstalledKeepsStableError(t *testing.T) {
	service := &fakeService{}
	credentials := &planCredentials{}
	var stderr bytes.Buffer
	runner := Runner{Service: service, Credentials: credentials, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &stderr}
	code := runner.Run(context.Background(), []string{
		"plan", "--non-interactive", "--os", "linux", "--app", "codex", "--app-installed=not-a-bool",
		"--kit-home", "/must-not-be-inspected", "--project", "wms", "--role", "developer", "--toolchain", "cc_1c_skills",
	})

	if code != ExitFailure || !strings.Contains(stderr.String(), `APP_INSTALLED_INVALID: "not-a-bool"`) {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if service.command != "" || service.plans != 0 || service.applies != 0 || service.statuses != 0 || credentials.calls != 0 {
		t.Fatalf("service=%+v credential calls=%d", service, credentials.calls)
	}
}

func TestRunInteractiveQuestionnaireCollectsAllSelectors(t *testing.T) {
	root := testutil.TempDir(t)
	kitHome := filepath.Join(root, "kit")
	hermesHome := filepath.Join(root, "hermes")
	input := strings.Join([]string{
		"3", "1", kitHome, "11",
		"3", "1", "",
	}, "\n")
	service := &fakeService{}
	var stdout, stderr bytes.Buffer
	runner := Runner{Service: service, In: strings.NewReader(input), Out: &stdout, Err: &stderr, HermesDiscovery: func(_ context.Context, request hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
		if request.InstalledOverride != nil {
			t.Fatalf("interactive override=%v, want nil", *request.InstalledOverride)
		}
		return hermes.DiscoveryResult{Installed: true, Home: hermesHome, Executable: filepath.Join(hermesHome, "hermes"), Version: "0.20.2"}, nil
	}}
	if code := runner.Run(context.Background(), []string{"plan"}); code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if service.desired.OS() != domain.OSLinux || service.desired.Role() != domain.RoleArchitect || service.update != reconcile.UpdateNone {
		t.Fatalf("desired=%+v update=%q", service.desired, service.update)
	}
	for _, forbidden := range []string{"AI-приложение уже установлено", "Введите HERMES_HOME", "Введите версию Hermes"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("unexpected prompt %q in %q", forbidden, stdout.String())
		}
	}
	if service.desired.HermesVersion() != "0.20.2" {
		t.Fatalf("version=%q", service.desired.HermesVersion())
	}
}

func TestRunHermesJSONContainsDiscoveryMetadataWithoutProse(t *testing.T) {
	service := &fakeService{}
	var stdout, stderr bytes.Buffer
	runner := Runner{Service: service, In: strings.NewReader(""), Out: &stdout, Err: &stderr, HermesDiscovery: func(_ context.Context, req hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
		if req.ExplicitHome != "/explicit" || req.InstalledOverride == nil || !*req.InstalledOverride {
			t.Fatalf("request=%#v", req)
		}
		return hermes.DiscoveryResult{Installed: true, Home: "/explicit", Executable: "/explicit/hermes", Version: "0.20.2"}, nil
	}}
	args := []string{"plan", "--non-interactive", "--json", "--os", "linux", "--app", "hermes", "--app-installed=true", "--kit-home", "/kit", "--hermes-home", "/explicit", "--project", "wms", "--role", "developer", "--toolchain", "cc_1c_skills"}
	if code := runner.Run(context.Background(), args); code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "{") || !strings.Contains(stdout.String(), `"hermes":{"installed":true,"home":"/explicit","version":"0.20.2"}`) || strings.Contains(stdout.String(), "Hermes найден") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunHermesDiscoveryFailurePreventsServiceCall(t *testing.T) {
	service := &fakeService{}
	var stderr bytes.Buffer
	runner := Runner{Service: service, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &stderr, HermesDiscovery: func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
		return hermes.DiscoveryResult{}, errors.New("HERMES_HOME_AUTO_DETECT_FAILED")
	}}
	code := runner.Run(context.Background(), []string{"plan", "--non-interactive", "--os", "linux", "--app", "hermes", "--kit-home", "/kit", "--project", "wms", "--role", "developer", "--toolchain", "cc_1c_skills"})
	if code == ExitOK || service.plans != 0 {
		t.Fatalf("exit=%d plans=%d", code, service.plans)
	}
}

func TestRunHermesExplicitFalseOverrideReachesDiscovery(t *testing.T) {
	service := &fakeService{}
	runner := Runner{Service: service, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, HermesDiscovery: func(_ context.Context, request hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
		if request.InstalledOverride == nil || *request.InstalledOverride {
			t.Fatalf("override=%v, want false", request.InstalledOverride)
		}
		return hermes.DiscoveryResult{Home: "/target"}, nil
	}}
	args := []string{"plan", "--non-interactive", "--os", "linux", "--app", "hermes", "--app-installed=false", "--kit-home", "/kit", "--hermes-home", "/target", "--project", "wms", "--role", "developer", "--toolchain", "cc_1c_skills"}
	if code := runner.Run(context.Background(), args); code != ExitOK {
		t.Fatalf("exit=%d", code)
	}
}

func TestRunInteractiveQuestionnairePromptsUpdateOnlyForNonemptyWorkspace(t *testing.T) {
	root := testutil.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("managed fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		"3", "2", "1", root, "11", "2", "2", "4", "",
	}, "\n")
	service := &fakeService{}
	var stdout, stderr bytes.Buffer
	runner := Runner{Service: service, In: strings.NewReader(input), Out: &stdout, Err: &stderr}
	if code := runner.Run(context.Background(), []string{"plan"}); code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if service.update != reconcile.UpdateBoth || !strings.Contains(stdout.String(), "Что обновить в существующем окружении") {
		t.Fatalf("update=%q stdout=%q", service.update, stdout.String())
	}
}

func TestRunCancellationInterruptsBlockedQuestionnaire(t *testing.T) {
	input, output := io.Pipe()
	defer input.Close()
	defer output.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- (Runner{Service: &fakeService{}, In: input, Out: &bytes.Buffer{}, Err: &stderr}).Run(ctx, []string{"plan"})
	}()
	cancel()
	select {
	case code := <-done:
		if code != ExitInterrupted || !strings.Contains(stderr.String(), "INTERRUPTED") {
			t.Fatalf("exit=%d stderr=%q", code, stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("questionnaire did not stop after cancellation")
	}
}

func TestRunDispatchesStatusRetryAndUpdate(t *testing.T) {
	for _, test := range []struct {
		args    []string
		command string
	}{
		{[]string{"status", "--kit-home", "/tmp/kit"}, "status"},
		{[]string{"retry", "--kit-home", "/tmp/kit"}, "retry"},
		{[]string{"update", "--kit-home", "/tmp/kit", "--target", "both"}, "update"},
	} {
		service := &fakeService{}
		runner := Runner{Service: service, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
		if code := runner.Run(context.Background(), test.args); code != 0 {
			t.Fatalf("%s exit=%d", test.command, code)
		}
		if service.command != test.command {
			t.Fatalf("dispatch=%q, want %q", service.command, test.command)
		}
	}
}

func TestRunBareHelpExitsSuccessfully(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{Out: &stdout, Err: &stderr}
	if code := runner.Run(context.Background(), []string{"--help"}); code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "teamkit plan|apply|status|retry|update|version") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunBareVersionEmitsOnlyBuildMetadata(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{Out: &stdout, Err: &stderr}
	if code := runner.Run(context.Background(), []string{"--version"}); code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), `{"version":`) || strings.Contains(stdout.String(), "version: ") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunCancelledCommandUsesInterruptedExit(t *testing.T) {
	service := &fakeService{err: context.Canceled}
	var stderr bytes.Buffer
	runner := Runner{Service: service, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &stderr}
	code := runner.Run(context.Background(), linuxArgs("plan"))
	if code != ExitInterrupted || !strings.Contains(stderr.String(), "INTERRUPTED") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func oneActionPlan() reconcile.OperationPlan {
	return reconcile.OperationPlan{Actions: []reconcile.Action{{ID: "20-sync-content", Kind: reconcile.ActionSyncContent, Idempotent: true}}}
}

func firstError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func linuxArgs(command string) []string {
	return []string{
		command, "--non-interactive", "--os", "linux", "--app", "hermes", "--app-installed=true",
		"--kit-home", "/tmp/kit", "--hermes-home", "/tmp/hermes", "--project", "wms",
		"--role", "developer", "--toolchain", "ai_rules_1c",
	}
}

func nativeApplyArgs(t *testing.T) []string {
	t.Helper()
	base := testutil.TempDir(t)
	return []string{"apply", "--non-interactive", "--os", nativeOS(), "--app", "hermes", "--app-installed=true", "--kit-home", filepath.Join(base, "kit"), "--hermes-home", filepath.Join(base, "hermes"), "--project", "wms", "--role", "developer", "--toolchain", "ai_rules_1c"}
}

func nativeOS() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return "linux"
}
