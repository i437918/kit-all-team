package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/registry"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestPublicCatalog_ReturnsOnlyCompiledStableSelectors(t *testing.T) {
	result := publicCatalog()
	if result.SchemaVersion != 1 {
		t.Fatalf("schema=%d", result.SchemaVersion)
	}
	if result.Applications[0].ID != "hermes" {
		t.Fatalf("apps=%#v", result.Applications)
	}
	if got := choiceIDs(result.Toolchains); !reflect.DeepEqual(got, []string{"cc_1c_skills", "ai_rules_1c"}) {
		t.Fatalf("toolchains=%v", got)
	}
	for _, group := range [][]PublicChoice{result.Applications, result.Projects, result.Roles, result.Toolchains} {
		for _, choice := range group {
			if choice.ID == "" || choice.Label == "" {
				t.Fatalf("public choice=%#v", choice)
			}
		}
	}
}

func TestRunPublicDetection_UsesExactDetectorWithoutOperationalAdapters(t *testing.T) {
	service := &fakeService{}
	credentials := &planCredentials{}
	inspector := &fakeInspector{}
	hermesCalled := false
	var stdout, stderr bytes.Buffer
	runner := Runner{
		Service: service, Credentials: credentials, Environments: inspector,
		ApplicationLookPath: func(name string) (string, error) {
			if name != "codex" {
				t.Fatalf("look path=%q", name)
			}
			return `C:\\Tools\\codex.exe`, nil
		},
		HermesDiscovery: func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
			hermesCalled = true
			return hermes.DiscoveryResult{}, errors.New("must not discover Hermes")
		},
		Out: &stdout, Err: &stderr,
	}

	code := runner.Run(context.Background(), []string{"detect-app", "--json", "--app", "codex"})
	if code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	var result PublicDetection
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result != (PublicDetection{SchemaVersion: 1, ApplicationID: "codex", Installed: true}) {
		t.Fatalf("result=%#v", result)
	}
	if hermesCalled || inspector.inspectCalls != 0 || inspector.addCalls != 0 || service.command != "" || credentials.calls != 0 {
		t.Fatalf("hermes=%t inspector=%#v service=%#v credentials=%d", hermesCalled, inspector, service, credentials.calls)
	}
}

func TestRunPublicDetection_WindowsAcceptsExplicitNonHermesInstalledConfirmationWithoutPATHClaim(t *testing.T) {
	var stdout, stderr bytes.Buffer
	lookPathCalls := 0
	runner := Runner{
		GOOS: "windows",
		ApplicationLookPath: func(string) (string, error) {
			lookPathCalls++
			return "", errors.New("private PATH diagnostic must not be consulted")
		},
		Out: &stdout,
		Err: &stderr,
	}

	code := runner.Run(context.Background(), []string{"detect-app", "--json", "--app", "codex", "--app-installed=true"})
	if code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	var result PublicDetection
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	want := PublicDetection{SchemaVersion: 1, ApplicationID: "codex", Installed: true}
	if result != want || lookPathCalls != 0 {
		t.Fatalf("result=%#v lookPathCalls=%d", result, lookPathCalls)
	}
	if result.Home != "" || result.Version != "" {
		t.Fatalf("confirmation falsely claimed executable metadata: %#v", result)
	}
}

func TestRunPublicDetection_HermesUsesDiscoveryAndReturnsMetadata(t *testing.T) {
	service := &fakeService{}
	credentials := &planCredentials{}
	called := false
	var stdout, stderr bytes.Buffer
	runner := Runner{
		Service: service, Credentials: credentials, Out: &stdout, Err: &stderr,
		HermesDiscovery: func(_ context.Context, request hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
			called = true
			if request.OS != domain.OSWindows || request.ExplicitHome != "" || request.InstalledOverride != nil || request.KitHome != "" {
				t.Fatalf("request=%#v", request)
			}
			return hermes.DiscoveryResult{Installed: true, Home: `C:\\Hermes`, Version: "0.20.2"}, nil
		},
		GOOS: "windows",
	}

	code := runner.Run(context.Background(), []string{"detect-app", "--json", "--app", "hermes"})
	if code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	var result PublicDetection
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	want := PublicDetection{SchemaVersion: 1, ApplicationID: "hermes", Installed: true, Home: `C:\\Hermes`, Version: "0.20.2"}
	if result != want || !called || service.command != "" || credentials.calls != 0 {
		t.Fatalf("result=%#v called=%t service=%#v credentials=%d", result, called, service, credentials.calls)
	}
}

func TestRunPublicDetection_MissingApplicationReturnsRedactedStableJSONError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{
		ApplicationLookPath: func(string) (string, error) { return "", exec.ErrNotFound },
		Out:                 &stdout,
		Err:                 &stderr,
	}

	code := runner.Run(context.Background(), []string{"detect-app", "--json", "--app", "codex"})
	if code != ExitApplicationRequired {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || stderr.String() != "{\"error\":\"AI_APP_REQUIRED\",\"message\":\"selected AI application is not installed\"}\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunPublicQueries_UnknownOrMissingApplicationKeepsStableRedactedJSONError(t *testing.T) {
	for _, command := range []string{"detect-app", "environments"} {
		for _, application := range []string{"", "unknown-app-canary"} {
			t.Run(command+"/"+application, func(t *testing.T) {
				service := &fakeService{}
				credentials := &planCredentials{}
				candidate := testutil.TempDir(t)
				store := &fakeRegistry{snapshot: registry.Registry{SchemaVersion: registry.SchemaVersion, Homes: []string{candidate}}, state: registry.LoadValid}
				inspector := &fakeInspector{}
				lookPathCalls, hermesCalls := 0, 0
				var stdout, stderr bytes.Buffer
				runner := Runner{
					Service: service, Credentials: credentials, Registry: store, Environments: inspector,
					ApplicationLookPath: func(string) (string, error) {
						lookPathCalls++
						return "", errors.New("invalid public app reached platform detector")
					},
					HermesDiscovery: func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
						hermesCalls++
						return hermes.DiscoveryResult{}, errors.New("invalid public app reached Hermes discovery")
					},
					Out: &stdout, Err: &stderr,
				}
				args := []string{command, "--json"}
				if application != "" {
					args = append(args, "--app", application)
				}

				code := runner.Run(context.Background(), args)
				if code != ExitApplicationRequired {
					t.Errorf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				const want = "{\"error\":\"AI_APP_REQUIRED\",\"message\":\"selected AI application is not installed\"}\n"
				echoed := application != "" && strings.Contains(stdout.String()+stderr.String(), application)
				if stdout.Len() != 0 || stderr.String() != want || echoed {
					t.Errorf("stdout=%q stderr=%q", stdout.String(), stderr.String())
				}
				if service.command != "" || credentials.calls != 0 || store.loads != 0 || lookPathCalls != 0 || hermesCalls != 0 || inspector.inspectCalls != 0 || inspector.addCalls != 0 {
					t.Fatalf("service=%#v credentials=%d registry=%#v lookPath=%d hermes=%d inspector=%#v", service, credentials.calls, store, lookPathCalls, hermesCalls, inspector)
				}
			})
		}
	}
}

func TestRunPublicQueries_AdapterFailuresUseStableBoundedMessages(t *testing.T) {
	secret := strings.Repeat("private-adapter-token-", 200)
	cases := []struct {
		name string
		args []string
		run  Runner
		want string
	}{
		{
			name: "non-Hermes detector",
			args: []string{"detect-app", "--json", "--app", "codex"},
			run:  Runner{ApplicationLookPath: func(string) (string, error) { return "", errors.New(secret) }},
			want: "{\"error\":\"AI_APP_INSPECTION_FAILED\",\"message\":\"selected AI application could not be verified\"}\n",
		},
		{
			name: "Hermes discovery",
			args: []string{"detect-app", "--json", "--app", "hermes"},
			run: Runner{HermesDiscovery: func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
				return hermes.DiscoveryResult{}, errors.New(secret)
			}},
			want: "{\"error\":\"AI_APP_INSPECTION_FAILED\",\"message\":\"selected AI application could not be verified\"}\n",
		},
		{
			name: "environment discovery",
			args: []string{"environments", "--json", "--app", "hermes"},
			run: Runner{
				Registry:     &fakeRegistry{snapshot: registry.Registry{SchemaVersion: registry.SchemaVersion, Homes: []string{filepath.Join(testutil.TempDir(t), "candidate")}}, state: registry.LoadValid},
				Environments: &fakeInspector{byHome: map[string]inspectResult{}},
			},
			want: "{\"error\":\"WORKSPACE_INSPECTION_FAILED\",\"message\":\"environment discovery failed\"}\n",
		},
	}
	// Make the environment inspector violate the Ready contract with the private
	// adapter error so Discover returns it instead of converting it to a warning.
	environmentCase := &cases[2]
	home := environmentCase.run.Registry.(*fakeRegistry).snapshot.Homes[0]
	environmentCase.run.Environments = &fakeInspector{byHome: map[string]inspectResult{
		home: {state: environment.Ready, err: errors.New(secret)},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			tc.run.Out, tc.run.Err = &stdout, &stderr
			code := tc.run.Run(context.Background(), tc.args)
			if code != ExitFailure || stdout.Len() != 0 || stderr.String() != tc.want {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if strings.Contains(stderr.String(), "private-adapter-token") || len(stderr.String()) > 256 {
				t.Fatalf("public error leaked or was unbounded: %q", stderr.String())
			}
		})
	}
}

func TestRunPublicEnvironments_FiltersSelectedApplicationAndSortsWindowsPaths(t *testing.T) {
	base := testutil.TempDir(t)
	alpha := filepath.Join(base, "Alpha")
	zeta := filepath.Join(base, "zeta")
	other := filepath.Join(base, "Other")
	inspector := &fakeInspector{byHome: map[string]inspectResult{
		zeta:  {verified: publicQueryEnvironment(t, zeta, domain.AppHermes, false), state: environment.Ready},
		alpha: {verified: publicQueryEnvironment(t, alpha, domain.AppHermes, true), state: environment.RetryRequired, err: &environment.Error{State: environment.RetryRequired, Detail: "pending"}},
		other: {verified: publicQueryEnvironment(t, other, domain.AppCodex, false), state: environment.Ready},
	}}
	store := &fakeRegistry{snapshot: registry.Registry{SchemaVersion: registry.SchemaVersion, Homes: []string{zeta, other, alpha}}, state: registry.LoadValid}
	service := &fakeService{}
	credentials := &planCredentials{}
	var stdout, stderr bytes.Buffer
	runner := Runner{
		Service: service, Credentials: credentials, Environments: inspector, Registry: store,
		Out: &stdout, Err: &stderr, GOOS: "windows",
	}

	code := runner.Run(context.Background(), []string{"environments", "--json", "--app", "hermes"})
	if code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	var result []PublicEnvironment
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if got := []string{result[0].Home, result[1].Home}; !reflect.DeepEqual(got, []string{alpha, zeta}) {
		t.Fatalf("homes=%v", got)
	}
	if result[0].Status != "RETRY_REQUIRED" || result[1].Status != "READY" || result[0].ApplicationID != "hermes" || store.loads != 1 || service.command != "" || credentials.calls != 0 {
		t.Fatalf("result=%#v registry=%#v service=%#v credentials=%d", result, store, service, credentials.calls)
	}
}

func publicQueryEnvironment(t *testing.T, home string, application domain.AIApplication, pending bool) environment.VerifiedEnvironment {
	t.Helper()
	input := domain.DesiredStateInput{
		OS: domain.OSWindows, Application: application, AppInstalled: true,
		KitHome: home,
		Project: domain.ProjectWMS, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	}
	if application == domain.AppHermes {
		input.HermesHome = filepath.Join(filepath.Dir(home), "hermes")
		input.HermesVersion = "0.20.2"
	}
	desired, err := domain.NewDesiredState(input)
	if err != nil {
		t.Fatal(err)
	}
	return environment.VerifiedEnvironment{Home: home, Desired: desired, Pending: pending}
}

func TestParseOptions_PublicQueriesRequireExactJSONFlags(t *testing.T) {
	for _, args := range [][]string{
		{"catalog", "--json"},
		{"detect-app", "--json", "--app", "hermes"},
		{"detect-app", "--json", "--app", "codex", "--app-installed=true"},
		{"environments", "--json", "--app", "hermes"},
	} {
		opts, err := parseOptions(args, &bytes.Buffer{})
		if err != nil || !opts.jsonOutput {
			t.Fatalf("args=%v opts=%#v err=%v", args, opts, err)
		}
	}
	for _, args := range [][]string{
		{"catalog", "--app", "hermes", "--json"},
		{"environments", "--json", "--app", "hermes", "extra"},
	} {
		if _, err := parseOptions(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("args=%v accepted", args)
		}
	}
}

func TestRunPublicCatalog_EmitsOnlyPublicJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := Runner{Out: &stdout, Err: &stderr}
	if code := runner.Run(context.Background(), []string{"catalog", "--json"}); code != ExitOK {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "https://") || strings.Contains(stdout.String(), "commit") {
		t.Fatalf("catalog leaked non-selector fields: %q", stdout.String())
	}
}
