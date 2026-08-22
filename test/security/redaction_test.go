package security

import (
	"bytes"
	"context"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/cli"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/engine"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/state"
)

type failingService struct{}

func (failingService) Plan(context.Context, domain.DesiredState, reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	return reconcile.OperationPlan{Actions: []reconcile.Action{{Kind: reconcile.ActionSyncContent}}}, nil
}
func (failingService) Apply(context.Context, domain.DesiredState, reconcile.UpdateChoice, cli.ApplyInputs) (reconcile.OperationPlan, error) {
	return reconcile.OperationPlan{}, errors.New("provider rejected TEAMKIT_CANARY")
}
func (failingService) Status(context.Context, string) (reconcile.PlanStatus, reconcile.OperationPlan, error) {
	return "", reconcile.OperationPlan{}, nil
}
func (failingService) Retry(context.Context, string) error { return nil }
func (failingService) Update(context.Context, string, reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	return reconcile.OperationPlan{}, nil
}
func (failingService) UpdateVerified(context.Context, environment.VerifiedEnvironment, reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	return reconcile.OperationPlan{}, nil
}

type credentials struct{}

func (credentials) Resolve(context.Context, domain.DesiredState, bool) (map[string]string, error) {
	return map[string]string{"KEY": "TEAMKIT_CANARY"}, nil
}

func TestApplyFailure_RedactsCredentialCanary(t *testing.T) {
	var out, err bytes.Buffer
	r := cli.Runner{Service: failingService{}, Credentials: credentials{}, Out: &out, Err: &err}
	code := r.Run(context.Background(), []string{"apply", "--non-interactive", "--os", "linux", "--app", "hermes", "--app-installed", "true", "--kit-home", "/kit", "--hermes-home", "/hermes", "--project", "aisuz", "--role", "developer", "--toolchain", "cc_1c_skills"})
	if code == 0 || strings.Contains(out.String()+err.String(), "TEAMKIT_CANARY") {
		t.Fatalf("code=%d output=%s%s", code, out.String(), err.String())
	}
}

type runtimeCanaryService struct {
	argv       [][]string
	log        bytes.Buffer
	canaryList []string
}

func (s *runtimeCanaryService) Plan(_ context.Context, desired domain.DesiredState, _ reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	return reconcile.Plan(desired, runtimeObservedState())
}

func (s *runtimeCanaryService) Apply(ctx context.Context, desired domain.DesiredState, update reconcile.UpdateChoice, inputs cli.ApplyInputs) (reconcile.OperationPlan, error) {
	persisted, err := state.New(desired.KitHome())
	if err != nil {
		return reconcile.OperationPlan{}, err
	}
	secrets := make([]string, 0, len(inputs.Secrets))
	for _, value := range inputs.Secrets {
		secrets = append(secrets, value)
	}
	s.canaryList = append([]string(nil), secrets...)
	operation := engine.Engine{Effects: runtimeCanaryEffects{service: s}, Store: persisted, Secrets: secrets}
	return operation.Apply(ctx, desired, update)
}

func (*runtimeCanaryService) Status(context.Context, string) (reconcile.PlanStatus, reconcile.OperationPlan, error) {
	return reconcile.StatusReady, reconcile.OperationPlan{}, nil
}
func (*runtimeCanaryService) Retry(context.Context, string) error { return nil }
func (*runtimeCanaryService) Update(context.Context, string, reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	return reconcile.OperationPlan{}, nil
}
func (*runtimeCanaryService) UpdateVerified(context.Context, environment.VerifiedEnvironment, reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
	return reconcile.OperationPlan{}, nil
}

type runtimeCanaryEffects struct{ service *runtimeCanaryService }

func (runtimeCanaryEffects) Observe(context.Context, domain.DesiredState, reconcile.UpdateChoice) (reconcile.ObservedState, error) {
	return runtimeObservedState(), nil
}

func (e runtimeCanaryEffects) Apply(_ context.Context, _ domain.DesiredState, _ reconcile.Action) error {
	e.service.argv = append(e.service.argv, []string{"git", "fetch", "--no-tags", "origin", "content-wms"})
	e.service.log.WriteString("git fetch failed; diagnostic redacted by caller\n")
	return errors.New("upstream rejected " + strings.Join(e.service.canaryList, " / "))
}

type multiCredentials map[string]string

func (c multiCredentials) Resolve(context.Context, domain.DesiredState, bool) (map[string]string, error) {
	return c, nil
}

func TestApplyFailure_MultiSecretCanariesStayOutOfEveryObservableSink(t *testing.T) {
	kit := filepath.Join(testutil.TempDir(t), "kit")
	gitlabUser := strings.Join([]string{"employee", "-secret-", "canary"}, "")
	gitlabToken := strings.Join([]string{"gl", "pat-", strings.Repeat("D", 24)}, "")
	providerKey := strings.Join([]string{"sk-", "proj-", strings.Repeat("E", 24)}, "")
	jiraCanary := "jira-personal-canary-7xQ2mN9pL4vK8dR6"
	confluenceCanary := "confluence-personal-canary-3wF8sT5yH2cJ9nM7"
	credentials := multiCredentials{
		"GITLAB_USERNAME":                  gitlabUser,
		"GITLAB_TOKEN":                     gitlabToken,
		"HERMES_CUSTOM_LLM_API_KEY": providerKey,
		"HERMES_CUSTOM_ISSUE_TRACKER_TOKEN":                       jiraCanary,
		"HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN":                 confluenceCanary,
	}
	service := &runtimeCanaryService{}
	var stdout, stderr bytes.Buffer
	runner := cli.Runner{Service: service, Credentials: credentials, Out: &stdout, Err: &stderr}
	code := runner.Run(context.Background(), []string{
		"apply", "--non-interactive", "--os", "linux", "--app", "cursor", "--app-installed=true",
		"--kit-home", kit, "--project", "wms", "--role", "developer", "--toolchain", "ai_rules_1c",
	})
	if code == 0 {
		t.Fatal("apply unexpectedly succeeded")
	}

	operation, err := os.ReadFile(filepath.Join(kit, ".teamkit", "operation.json"))
	if err != nil {
		t.Fatalf("read operation: %v", err)
	}
	plan, receipt := operation, operation
	log := append(append([]byte{}, service.log.Bytes()...), stdout.Bytes()...)
	log = append(log, stderr.Bytes()...)
	if err := os.WriteFile(filepath.Join(kit, ".teamkit", "execution.log"), log, 0o600); err != nil {
		t.Fatal(err)
	}
	argv := make([]byte, 0)
	for _, command := range service.argv {
		argv = append(argv, strings.Join(command, "\x00")...)
	}
	if len(argv) == 0 || !bytes.Contains(receipt, []byte("[REDACTED]")) {
		t.Fatalf("runtime evidence missing argv or redaction: argv=%d receipt=%s", len(argv), receipt)
	}
	config, err := (hermes.Profile{
		Project: "wms", Role: "developer", KitHome: kit,
		Toolchain:     hermes.Toolchain{Name: "ai_rules_1c", Origin: "https://example.test/toolchain", Version: "test"},
		V8StdEndpoint: catalog.V8StdMCP().Endpoint,
	}).RenderForSchema(hermes.CustomLLMProvider(), hermes.HermesConfigVersion)
	if err != nil {
		t.Fatalf("render Hermes config: %v", err)
	}

	sinks := map[string][]byte{
		"stdout": stdout.Bytes(), "stderr": stderr.Bytes(), "plan": plan,
		"receipt": receipt, "execution_log": log, "config": config, "captured_hermes_argv": argv, "audit_output": service.log.Bytes(),
	}
	for _, secret := range []string{gitlabUser, gitlabToken, providerKey, jiraCanary, confluenceCanary} {
		for name, contents := range sinks {
			if bytes.Contains(contents, []byte(secret)) {
				t.Fatalf("%s leaked a runtime secret canary", name)
			}
		}
	}
}

func runtimeObservedState() reconcile.ObservedState {
	return reconcile.ObservedState{
		WorkspaceReady: true, ContentReady: false, DatabaseReady: true,
		ToolchainReady: true, ApplicationReady: true,
	}
}
