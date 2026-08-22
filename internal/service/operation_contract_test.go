package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/cli"
	"github.com/mi1man-cmd/kit-all-team/internal/credentials"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/engine"
	"github.com/mi1man-cmd/kit-all-team/internal/gitx"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/state"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

func TestDefaultOperationContract_HermesBindsOrderedMCPServers(t *testing.T) {
	desired := mustDesiredState(t, domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
		KitHome: filepath.Join(testutil.TempDir(t), "workspace"), HermesHome: filepath.Join(testutil.TempDir(t), "hermes"), HermesVersion: "0.20.1",
		Project: domain.ProjectWMS, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	got, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatalf("defaultOperationContract: %v", err)
	}

	// Hand-written canonical contract. A current Hermes operation binds all
	// configured MCPs in the same order as the closed catalog declarations.
	// The fourth entry is the selected, immutable OfficeCLI stdio launch
	// contract; no runtime environment or secret value is part of it.
	executable := "officecli"
	if runtime.GOOS == "windows" {
		executable = "officecli.exe"
	}
	command, err := json.Marshal(filepath.Join(desired.HermesHome(), ".teamkit", "officecli", "1.0.144", executable))
	if err != nil {
		t.Fatal(err)
	}
	contract := fmt.Sprintf(`{"schema_version":1,"project":{"id":"wms","content_repository":"https://gitlab.example.invalid/1c/aisuz/ai.git","content_branch":"content-wms","database_repository":"https://gitlab.example.invalid/1c/fulfillment/wms.git","database_branch":"develop"},"toolchain":{"id":"cc_1c_skills","origin":"https://github.com/Nikolay-Shirokov/cc-1c-skills.git","commit":"e01688e764a3cf1c1b4a0ad5069ea885837cfb2e"},"provider":{"id":"customllm","name":"CustomLLM","base_url":"https://llm.example.invalid/v1","model":"generic-development","api_mode":"chat_completions","api_key_environment":"HERMES_CUSTOM_LLM_API_KEY"},"mcp_servers":[{"id":"v8std","endpoint":"https://ai.v8std.ru/mcp"},{"id":"customllm-jira","endpoint":"https://llm.example.invalid/jira/mcp","headers":{"x-litellm-api-key":"Bearer ${HERMES_CUSTOM_LLM_API_KEY}","x-mcp-jira-authorization":"Token ${HERMES_CUSTOM_ISSUE_TRACKER_TOKEN}"},"connect_timeout":60,"timeout":120,"sampling_enabled":false,"supports_parallel_tool_calls":false},{"id":"customllm-confluence","endpoint":"https://llm.example.invalid/confluence/mcp","headers":{"x-litellm-api-key":"Bearer ${HERMES_CUSTOM_LLM_API_KEY}","x-mcp-confluence-authorization":"Token ${HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN}"},"connect_timeout":60,"timeout":120,"sampling_enabled":false,"supports_parallel_tool_calls":false},{"id":"officecli","command":%s,"args":["mcp"],"asset":{"version":"1.0.144","commit":"1ced45e900782c5083ed550ddf328ee974e425e7","os":"linux","architecture":"amd64","file_name":"officecli-linux-x64","url":"https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-linux-x64","size":35316133,"sha256":"32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8","update_policy":"auto_update_disabled_user_config","skill_refresh_policy":"existing_installed_only_best_effort"}}],"hermes":{"mode":"external-compatible","minimum_version":"0.20.1","maximum_exclusive_version":"0.21.0","observed_version":"0.20.1","certificate_sha256":"88d85e7e7d64c061c195f93c517500bdc91fccfb9b5a8115da9f6a5a17e689f8"}}`, command)
	digest := sha256.Sum256([]byte(contract))
	want := hex.EncodeToString(digest[:])
	if got != want {
		t.Fatalf("contract hash = %q, want %q", got, want)
	}
	firstThreeMCPServers := `[{"id":"v8std","endpoint":"https://ai.v8std.ru/mcp"},{"id":"customllm-jira","endpoint":"https://llm.example.invalid/jira/mcp","headers":{"x-litellm-api-key":"Bearer ${HERMES_CUSTOM_LLM_API_KEY}","x-mcp-jira-authorization":"Token ${HERMES_CUSTOM_ISSUE_TRACKER_TOKEN}"},"connect_timeout":60,"timeout":120,"sampling_enabled":false,"supports_parallel_tool_calls":false},{"id":"customllm-confluence","endpoint":"https://llm.example.invalid/confluence/mcp","headers":{"x-litellm-api-key":"Bearer ${HERMES_CUSTOM_LLM_API_KEY}","x-mcp-confluence-authorization":"Token ${HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN}"},"connect_timeout":60,"timeout":120,"sampling_enabled":false,"supports_parallel_tool_calls":false},`
	if !strings.Contains(contract, firstThreeMCPServers) {
		t.Fatal("first three Hermes MCP contracts changed")
	}
	if strings.Contains(contract, "provider-key") || strings.Contains(contract, "jira-token") || strings.Contains(contract, "confluence-token") || strings.Contains(contract, `"environment"`) {
		t.Fatalf("canonical contract leaks runtime secrets or environment: %s", contract)
	}
	if strings.Contains(contract, "officecli-win-x64.exe") || strings.Contains(contract, "officecli-mac-x64") || strings.Contains(contract, "officecli-mac-arm64") {
		t.Fatalf("canonical contract includes an unselected OfficeCLI asset: %s", contract)
	}
	changedCommand := strings.Replace(string(command), "officecli", "officeclj", 1)
	for name, changed := range map[string]string{
		"asset version":          strings.Replace(contract, `"version":"1.0.144"`, `"version":"1.0.145"`, 1),
		"asset commit":           strings.Replace(contract, "1ced45e900782c5083ed550ddf328ee974e425e7", "2ced45e900782c5083ed550ddf328ee974e425e7", 1),
		"asset operating system": strings.Replace(contract, `"os":"linux"`, `"os":"windows"`, 1),
		"asset architecture":     strings.Replace(contract, `"architecture":"amd64"`, `"architecture":"arm64"`, 1),
		"asset filename":         strings.Replace(contract, "officecli-linux-x64", "officecli-linux-x6y", 1),
		"asset URL":              strings.Replace(contract, "/officecli-linux-x64", "/officecli-linux-x6y", 1),
		"asset size":             strings.Replace(contract, `"size":35316133`, `"size":35316134`, 1),
		"asset SHA-256":          strings.Replace(contract, "32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8", "42ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8", 1),
		"managed command path":   strings.Replace(contract, string(command), changedCommand, 1),
		"args":                   strings.Replace(contract, `"args":["mcp"]`, `"args":["mcp","--verbose"]`, 1),
		"update policy":          strings.Replace(contract, "auto_update_disabled_user_config", "auto_update_enabled_user_config", 1),
		"skill refresh policy":   strings.Replace(contract, "existing_installed_only_best_effort", "never", 1),
		"endpoint":               strings.Replace(contract, "https://llm.example.invalid/jira/mcp", "https://llm.example.invalid/jira/mcq", 1),
		"header":                 strings.Replace(contract, "Token ${HERMES_CUSTOM_ISSUE_TRACKER_TOKEN}", "Token ${JIRA_TOKEO}", 1),
	} {
		t.Run(name, func(t *testing.T) {
			mutatedDigest := sha256.Sum256([]byte(changed))
			if got == hex.EncodeToString(mutatedDigest[:]) {
				t.Fatal("one-byte MCP binding change did not change operation identity")
			}
		})
	}
}

func TestService_PlanContractChangesWithRuntimeObservedForLegacyState(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	desired := testDesired(t, root, domain.AppHermes, true, hermesHome)
	contracts := map[string]string{}

	for _, version := range []string{"0.20.1", "0.20.2"} {
		version := version
		plan, err := New(Options{ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			return hermes.DiscoveryResult{
				Installed: true, Home: hermesHome,
				Executable: filepath.Join(hermesHome, "hermes-agent", "venv", "bin", "hermes"),
				Version:    version,
			}, nil
		}}).Plan(context.Background(), desired, reconcile.UpdateNone)
		if err != nil {
			t.Fatalf("Plan observed Hermes %s: %v", version, err)
		}
		contracts[version] = plan.ContractHash
	}

	if contracts["0.20.1"] == contracts["0.20.2"] {
		t.Fatal("plans for observed Hermes 0.20.1 and 0.20.2 shared one contract")
	}
}

func TestService_ApplyBindsRuntimeObservedVersionIntoContractAndReceipt(t *testing.T) {
	for _, version := range []string{"0.20.1", "0.20.2"} {
		version := version
		t.Run(version, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
			desired := testDesired(t, root, domain.AppHermes, true, hermesHome)
			stopAfterPrepare := errors.New("stop after durable operation")
			secretCalls, effectCalls := 0, 0
			svc := New(Options{
				ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
					return hermes.DiscoveryResult{
						Installed: true, Home: hermesHome,
						Executable: filepath.Join(hermesHome, "hermes-agent", "venv", "bin", "hermes"),
						Version:    version,
					}, nil
				},
				SecretStore: func(string) (credentials.SecretStore, error) {
					secretCalls++
					return nil, stopAfterPrepare
				},
				Effects: func(EffectInputs) engine.Effects {
					effectCalls++
					return failingEffects{}
				},
			})

			plan, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{})
			if !errors.Is(err, stopAfterPrepare) {
				t.Fatalf("Apply error = %v, want stop after durable operation", err)
			}
			if secretCalls != 1 || effectCalls != 0 {
				t.Fatalf("private adapters after prepare: secrets=%d effects=%d, want 1 and 0", secretCalls, effectCalls)
			}

			persisted, err := state.New(root)
			if err != nil {
				t.Fatal(err)
			}
			storedPlan, receipt, err := persisted.LoadOperation()
			if err != nil {
				t.Fatalf("LoadOperation: %v", err)
			}
			bound, err := receipt.DesiredState()
			if err != nil {
				t.Fatalf("receipt desired: %v", err)
			}
			if bound.HermesVersion() != version {
				t.Fatalf("receipt Hermes version = %q, want observed %q", bound.HermesVersion(), version)
			}
			wantContract, err := defaultOperationContract(bound)
			if err != nil {
				t.Fatal(err)
			}
			if plan.ContractHash != wantContract || storedPlan.ContractHash != wantContract {
				t.Fatalf("contracts returned=%q stored=%q want receipt-bound=%q", plan.ContractHash, storedPlan.ContractHash, wantContract)
			}
		})
	}
}

func TestService_ApplyRejectsHermesVersionDriftBeforePrivateAdapters(t *testing.T) {
	t.Run("desired version mismatches runtime", func(t *testing.T) {
		root := filepath.Join(testutil.TempDir(t), "workspace")
		hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
		desired, err := domain.NewDesiredState(domain.DesiredStateInput{
			OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
			KitHome: root, HermesHome: hermesHome, HermesVersion: "0.20.1",
			Project: domain.ProjectWMS, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
		})
		if err != nil {
			t.Fatal(err)
		}
		assertApplyRuntimeDriftBeforePrivateAdapters(t, desired, func(int) string { return "0.20.2" }, 1)
	})

	t.Run("runtime changes while acquiring lock", func(t *testing.T) {
		root := filepath.Join(testutil.TempDir(t), "workspace")
		hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
		desired := testDesired(t, root, domain.AppHermes, true, hermesHome)
		assertApplyRuntimeDriftBeforePrivateAdapters(t, desired, func(call int) string {
			if call == 1 {
				return "0.20.1"
			}
			return "0.20.2"
		}, 2)
	})
}

func assertApplyRuntimeDriftBeforePrivateAdapters(t *testing.T, desired domain.DesiredState, version func(int) string, wantRuntimeCalls int) {
	t.Helper()
	runtimeCalls, privateCalls := 0, 0
	svc := New(Options{
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			runtimeCalls++
			return hermes.DiscoveryResult{
				Installed: true, Home: desired.HermesHome(),
				Executable: filepath.Join(desired.HermesHome(), "hermes-agent", "venv", "bin", "hermes"),
				Version:    version(runtimeCalls),
			}, nil
		},
		StateStore: func(string) (engine.Store, error) {
			privateCalls++
			return nil, errors.New("must not open state store")
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			privateCalls++
			return nil, errors.New("must not open secrets")
		},
		Effects: func(EffectInputs) engine.Effects {
			privateCalls++
			return failingEffects{}
		},
	})

	_, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{})
	if err == nil || !strings.Contains(err.Error(), "HERMES_RUNTIME_DRIFT") {
		t.Fatalf("Apply error = %v, want HERMES_RUNTIME_DRIFT", err)
	}
	if runtimeCalls != wantRuntimeCalls || privateCalls != 0 {
		t.Fatalf("runtime calls=%d private adapters=%d, want %d and 0", runtimeCalls, privateCalls, wantRuntimeCalls)
	}
}

func TestService_ApplyAndUpdateUseFinalVerifiedHermesExecutableWithoutLateProbe(t *testing.T) {
	for _, command := range []string{"apply", "update"} {
		command := command
		t.Run(command, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
			desired := testDesired(t, root, domain.AppHermes, true, hermesHome)
			if command == "update" {
				input := desiredStateInput(desired)
				input.HermesVersion = "0.20.1"
				desired = mustDesiredState(t, input)
				if err := os.MkdirAll(root, 0o700); err != nil {
					t.Fatal(err)
				}
				writeDesired(t, desired)
				if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
					t.Fatal(err)
				}
			}

			firstExecutable := filepath.Join(hermesHome, "runtime", "first-hermes")
			finalExecutable := firstExecutable
			wantRuntimeCalls := 1
			if command == "apply" {
				finalExecutable = filepath.Join(hermesHome, "runtime", "under-lock-hermes")
				wantRuntimeCalls = 2
			}
			runtimeCalls := 0
			var captured EffectInputs
			store := &recordingSecretStore{loaded: map[string]string{
				credentials.GitLabUsername:  "runtime-binding-user",
				credentials.GitLabToken:     "bind-token-canary",
				credentials.CustomLLMAPIKey: "bind-secret-canary",
				credentials.JiraToken:       "bind-jira-token-canary",
				credentials.ConfluenceToken: "bind-confluence-token-canary",
			}}
			svc := New(Options{
				ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
					runtimeCalls++
					executable, version := finalExecutable, "0.20.1"
					if runtimeCalls == 1 {
						executable = firstExecutable
					}
					if runtimeCalls > wantRuntimeCalls {
						version = "0.20.2"
					}
					return hermes.DiscoveryResult{Installed: true, Home: hermesHome, Executable: executable, Version: version}, nil
				},
				ApplicationHome: func(domain.DesiredState) (string, error) { return hermesHome, nil },
				SecretStore:     func(string) (credentials.SecretStore, error) { return store, nil },
				ManagedCertificateBundle: func(string, string) (string, bool, error) {
					return filepath.Join(hermesHome, "certs", "ca-bundle.pem"), true, nil
				},
				AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
					return &recordingAskPass{credentials: input}, nil
				},
				Effects: func(input EffectInputs) engine.Effects {
					captured = input
					return failingEffects{canary: "RUNTIME_BINDING_EFFECT_STOP"}
				},
			})

			var err error
			if command == "apply" {
				_, err = svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{Secrets: map[string]string{
					credentials.CustomLLMAPIKey: "bind-secret-canary",
					credentials.JiraToken:       "bind-jira-token-canary",
					credentials.ConfluenceToken: "bind-confluence-token-canary",
				}})
			} else {
				_, err = svc.Update(context.Background(), root, reconcile.UpdateBoth)
			}
			if err == nil || !strings.Contains(err.Error(), "RUNTIME_BINDING_EFFECT_STOP") {
				t.Fatalf("%s error = %v, want effect stop after verified binding", command, err)
			}
			if strings.Contains(err.Error(), "bind-token-canary") || strings.Contains(err.Error(), "bind-secret-canary") {
				t.Fatalf("%s error exposed a secret canary: %v", command, err)
			}
			if runtimeCalls != wantRuntimeCalls {
				t.Fatalf("%s runtime calls = %d, want %d and no late probe", command, runtimeCalls, wantRuntimeCalls)
			}
			if captured.HermesExecutable != finalExecutable {
				t.Fatalf("%s effect executable = %q, want final verified %q", command, captured.HermesExecutable, finalExecutable)
			}
		})
	}
}

func TestService_UpdateRejectsHermesRuntimeDriftBeforeStateSecretsOrEffects(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	public := testDesired(t, root, domain.AppHermes, true, hermesHome)
	input := desiredStateInput(public)
	input.HermesVersion = "0.20.1"
	public = mustDesiredState(t, input)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, public)
	if err := workspace.EnsureOwner(root, string(public.Project())); err != nil {
		t.Fatal(err)
	}

	stateCalls, secretCalls, effectCalls := 0, 0, 0
	svc := New(Options{
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			return hermes.DiscoveryResult{
				Installed: true, Home: hermesHome,
				Executable: filepath.Join(hermesHome, "runtime", "hermes"), Version: "0.20.2",
			}, nil
		},
		ApplicationHome: func(domain.DesiredState) (string, error) { return hermesHome, nil },
		StateStore: func(string) (engine.Store, error) {
			stateCalls++
			return nil, errors.New("must not open state")
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			secretCalls++
			return nil, errors.New("must not open secrets")
		},
		Effects: func(EffectInputs) engine.Effects {
			effectCalls++
			return failingEffects{}
		},
	})

	_, err := svc.Update(context.Background(), root, reconcile.UpdateBoth)
	if err == nil || !strings.Contains(err.Error(), "HERMES_RUNTIME_DRIFT") {
		t.Fatalf("Update error = %v, want HERMES_RUNTIME_DRIFT", err)
	}
	if stateCalls != 0 || secretCalls != 0 || effectCalls != 0 {
		t.Fatalf("state=%d secrets=%d effects=%d, want 0/0/0", stateCalls, secretCalls, effectCalls)
	}
}

func TestService_StatusRecoversPreparedHermesOperationFromReceiptVersion(t *testing.T) {
	for _, version := range []string{"0.20.1", "0.20.2"} {
		version := version
		t.Run(version, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
			public, bound := interruptedHermesStates(t, root, hermesHome, version)
			plan := currentHermesOperationPlan(t, bound)
			writeInterruptedCurrentOperation(t, public, bound, plan)

			runtimeCalls, privateCalls := 0, 0
			svc := New(Options{
				ResolveHermesRuntime: stableHermesRuntime(hermesHome, version, &runtimeCalls),
				ApplicationHome: func(domain.DesiredState) (string, error) {
					privateCalls++
					return "", errors.New("must not open application home")
				},
				SecretStore: func(string) (credentials.SecretStore, error) {
					privateCalls++
					return nil, errors.New("must not open secrets")
				},
				Effects: func(EffectInputs) engine.Effects {
					privateCalls++
					return failingEffects{}
				},
			})

			status, got, err := svc.Status(context.Background(), root)
			if err != nil {
				t.Fatalf("Status interrupted operation: %v", err)
			}
			if status != reconcile.StatusNeedsApply || !reflect.DeepEqual(got, plan) {
				t.Fatalf("Status = %q, %#v; want needs_apply and %#v", status, got, plan)
			}
			if runtimeCalls != 1 || privateCalls != 0 {
				t.Fatalf("runtime calls=%d private adapters=%d, want 1 and 0", runtimeCalls, privateCalls)
			}
			persistedPublic, err := svc.loadDesiredForRevalidation(root)
			if err != nil {
				t.Fatal(err)
			}
			if persistedPublic.HermesVersion() != "" {
				t.Fatalf("read-only Status backfilled Hermes version %q", persistedPublic.HermesVersion())
			}
		})
	}
}

func TestService_RetryRecoversPreparedHermesOperationAndBackfillsVersion(t *testing.T) {
	for _, version := range []string{"0.20.1", "0.20.2"} {
		version := version
		t.Run(version, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
			public, bound := interruptedHermesStates(t, root, hermesHome, version)
			plan := currentHermesOperationPlan(t, bound)
			writeInterruptedCurrentOperation(t, public, bound, plan)

			secretStore := &recordingSecretStore{loaded: map[string]string{
				credentials.CustomLLMAPIKey: "provider-key", credentials.JiraToken: "jira-token", credentials.ConfluenceToken: "confluence-token",
			}}
			var applied []reconcile.Action
			runtimeCalls := 0
			privateValidationObservedBeforeBackfill := false
			svc := New(Options{
				ResolveHermesRuntime: stableHermesRuntime(hermesHome, version, &runtimeCalls),
				ApplicationHome: func(domain.DesiredState) (string, error) {
					data, err := os.ReadFile(filepath.Join(root, ".env"))
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(data), "HERMES_VERSION=") {
						privateValidationObservedBeforeBackfill = true
					}
					return hermesHome, nil
				},
				SecretStore: func(string) (credentials.SecretStore, error) { return secretStore, nil },
				ManagedCertificateBundle: func(string, string) (string, bool, error) {
					return filepath.Join(hermesHome, "certs", "ca-bundle.pem"), true, nil
				},
				AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
					return &recordingAskPass{credentials: input}, nil
				},
				Effects: func(EffectInputs) engine.Effects { return recordingActionEffects{actions: &applied} },
			})

			if err := svc.Retry(context.Background(), root); err != nil {
				t.Fatalf("Retry interrupted operation: %v", err)
			}
			if runtimeCalls != 2 {
				t.Fatalf("Retry verified runtime %d times, want pre-lock and under-lock", runtimeCalls)
			}
			if !privateValidationObservedBeforeBackfill {
				t.Fatal("Retry never proved private path validation before backfill")
			}
			if !reflect.DeepEqual(applied, plan.Actions) {
				t.Fatalf("Retry applied %#v, want %#v", applied, plan.Actions)
			}
			backfilled, err := svc.loadDesiredForRevalidation(root)
			if err != nil {
				t.Fatal(err)
			}
			if backfilled.HermesVersion() != version {
				t.Fatalf("backfilled Hermes version = %q, want %q", backfilled.HermesVersion(), version)
			}
		})
	}
}

func TestService_RetryRejectsUnsafePrivateHomeWithoutVersionBackfill(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	public, bound := interruptedHermesStates(t, root, hermesHome, "0.20.1")
	plan := currentHermesOperationPlan(t, bound)
	writeInterruptedCurrentOperation(t, public, bound, plan)
	before, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}

	secretCalls, effectCalls, runtimeCalls := 0, 0, 0
	svc := New(Options{
		ResolveHermesRuntime: stableHermesRuntime(hermesHome, "0.20.1", &runtimeCalls),
		ApplicationHome:      func(domain.DesiredState) (string, error) { return root, nil },
		SecretStore: func(string) (credentials.SecretStore, error) {
			secretCalls++
			return nil, errors.New("must not open secrets")
		},
		Effects: func(EffectInputs) engine.Effects {
			effectCalls++
			return failingEffects{}
		},
	})

	err = svc.Retry(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "HOME_PATH_OVERLAP") {
		t.Fatalf("Retry error = %v, want HOME_PATH_OVERLAP", err)
	}
	after, readErr := os.ReadFile(filepath.Join(root, ".env"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("unsafe Retry mutated public .env:\n before=%q\n after=%q", before, after)
	}
	if runtimeCalls != 2 || secretCalls != 0 || effectCalls != 0 {
		t.Fatalf("runtime=%d secrets=%d effects=%d, want 2/0/0", runtimeCalls, secretCalls, effectCalls)
	}
}

func TestService_CurrentHermesRecoveryRejectsDriftBeforePrivateAdapters(t *testing.T) {
	for _, command := range []string{"status", "retry"} {
		command := command
		for _, drift := range []string{"selector", "public_version", "contract", "runtime"} {
			drift := drift
			t.Run(command+"_"+drift, func(t *testing.T) {
				root := filepath.Join(testutil.TempDir(t), "workspace")
				hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
				public, bound := interruptedHermesStates(t, root, hermesHome, "0.20.1")
				switch drift {
				case "selector":
					input := desiredStateInput(bound)
					input.Project = domain.ProjectAPA
					bound = mustDesiredState(t, input)
				case "public_version":
					input := desiredStateInput(public)
					input.HermesVersion = "0.20.2"
					public = mustDesiredState(t, input)
				}
				plan := currentHermesOperationPlan(t, bound)
				if drift == "contract" {
					plan.ContractHash = strings.Repeat("a", sha256.Size*2)
				}
				writeInterruptedCurrentOperation(t, public, bound, plan)

				runtimeVersion := "0.20.1"
				wantRuntimeCalls := 0
				if drift == "runtime" {
					runtimeVersion = "0.20.2"
					wantRuntimeCalls = 1
				}
				runtimeCalls, privateCalls := 0, 0
				svc := New(Options{
					ResolveHermesRuntime: stableHermesRuntime(hermesHome, runtimeVersion, &runtimeCalls),
					ApplicationHome: func(domain.DesiredState) (string, error) {
						privateCalls++
						return "", errors.New("must not open application home")
					},
					SecretStore: func(string) (credentials.SecretStore, error) {
						privateCalls++
						return nil, errors.New("must not open secrets")
					},
					AskPass: func(string, gitx.Credentials) (AskPassSession, error) {
						privateCalls++
						return nil, errors.New("must not open askpass")
					},
					Effects: func(EffectInputs) engine.Effects {
						privateCalls++
						return failingEffects{}
					},
				})

				var err error
				if command == "status" {
					_, _, err = svc.Status(context.Background(), root)
				} else {
					err = svc.Retry(context.Background(), root)
				}
				if err == nil {
					t.Fatal("drifted interrupted operation was accepted")
				}
				if runtimeCalls != wantRuntimeCalls || privateCalls != 0 {
					t.Fatalf("runtime calls=%d private adapters=%d, want %d and 0; error=%v", runtimeCalls, privateCalls, wantRuntimeCalls, err)
				}
			})
		}
	}
}

func TestService_RetryRejectsCurrentHermesRecoveryDriftUnderLock(t *testing.T) {
	for _, drift := range []string{"selector", "receipt_version", "contract", "runtime"} {
		drift := drift
		t.Run(drift, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
			public, initialBound := interruptedHermesStates(t, root, hermesHome, "0.20.1")
			initialPlan := currentHermesOperationPlan(t, initialBound)
			secondBound := initialBound
			if drift == "selector" {
				input := desiredStateInput(secondBound)
				input.Project = domain.ProjectAPA
				secondBound = mustDesiredState(t, input)
			}
			if drift == "receipt_version" {
				input := desiredStateInput(secondBound)
				input.HermesVersion = "0.20.2"
				secondBound = mustDesiredState(t, input)
			}
			secondPlan := currentHermesOperationPlan(t, secondBound)
			if drift == "contract" {
				secondPlan.ContractHash = strings.Repeat("b", sha256.Size*2)
			}

			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			writeDesired(t, public)
			if err := workspace.EnsureOwner(root, string(public.Project())); err != nil {
				t.Fatal(err)
			}
			store := &sequenceOperationStore{
				plans:    []reconcile.OperationPlan{initialPlan, secondPlan},
				receipts: []*reconcile.Receipt{reconcile.NewReceipt(initialBound, initialPlan), reconcile.NewReceipt(secondBound, secondPlan)},
			}
			publicBytes := desiredStateBytes(t, public)
			runtimeCalls, privateCalls := 0, 0
			svc := New(Options{
				ReadFile:   func(string) ([]byte, error) { return append([]byte(nil), publicBytes...), nil },
				StateStore: func(string) (engine.Store, error) { return store, nil },
				ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
					runtimeCalls++
					version := "0.20.1"
					if drift == "runtime" && runtimeCalls == 2 {
						version = "0.20.2"
					}
					return hermes.DiscoveryResult{
						Installed: true, Home: hermesHome,
						Executable: filepath.Join(hermesHome, "hermes-agent", "venv", "bin", "hermes"),
						Version:    version,
					}, nil
				},
				ApplicationHome: func(domain.DesiredState) (string, error) {
					privateCalls++
					return "", errors.New("must not open application home")
				},
				SecretStore: func(string) (credentials.SecretStore, error) {
					privateCalls++
					return nil, errors.New("must not open secrets")
				},
				Effects: func(EffectInputs) engine.Effects {
					privateCalls++
					return failingEffects{}
				},
			})

			err := svc.Retry(context.Background(), root)
			if err == nil {
				t.Fatal("under-lock drift was accepted")
			}
			wantRuntimeCalls := 1
			if drift == "runtime" {
				wantRuntimeCalls = 2
			}
			if runtimeCalls != wantRuntimeCalls || privateCalls != 0 {
				t.Fatalf("runtime calls=%d private adapters=%d, want %d and 0; error=%v", runtimeCalls, privateCalls, wantRuntimeCalls, err)
			}
		})
	}
}

func TestDefaultOperationContract_NonHermesOmitsHermesAndProviderPins(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "workspace"), domain.AppCodex, true, "")

	got, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatalf("defaultOperationContract: %v", err)
	}

	contract := `{"schema_version":1,"project":{"id":"wms","content_repository":"https://gitlab.example.invalid/1c/aisuz/ai.git","content_branch":"content-wms","database_repository":"https://gitlab.example.invalid/1c/fulfillment/wms.git","database_branch":"develop"},"toolchain":{"id":"cc_1c_skills","origin":"https://github.com/Nikolay-Shirokov/cc-1c-skills.git","commit":"e01688e764a3cf1c1b4a0ad5069ea885837cfb2e"},"mcp":{"id":"v8std","endpoint":"https://ai.v8std.ru/mcp"}}`
	digest := sha256.Sum256([]byte(contract))
	want := hex.EncodeToString(digest[:])
	if got != want {
		t.Fatalf("contract hash = %q, want %q", got, want)
	}
}

func TestLegacyRC2OperationContract_PreservesFixtureHash(t *testing.T) {
	desired := rc2Desired(t, filepath.Join(testutil.TempDir(t), "workspace"), filepath.Join(testutil.TempDir(t), "hermes"), domain.ProjectAPA, domain.ToolchainCC1CSkills)

	got, err := legacyRC2InstalledHermesContract(desired)
	if err != nil {
		t.Fatalf("legacyRC2InstalledHermesContract: %v", err)
	}
	if want := rc2FailedToolchainPlan().ContractHash; got != want {
		t.Fatalf("legacy RC2 contract hash = %q, want fixture %q", got, want)
	}
}

func TestDefaultOperationContract_ChangesWithObservedHermesVersion(t *testing.T) {
	base := domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true, KitHome: filepath.Join(testutil.TempDir(t), "kit"), HermesHome: filepath.Join(testutil.TempDir(t), "hermes"), Project: domain.ProjectWMS, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills}
	base.HermesVersion = "0.20.2"
	first, err := domain.NewDesiredState(base)
	if err != nil {
		t.Fatal(err)
	}
	base.HermesVersion = "0.20.3"
	second, err := domain.NewDesiredState(base)
	if err != nil {
		t.Fatal(err)
	}
	a, err := defaultOperationContract(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := defaultOperationContract(second)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("operation contract ignored observed Hermes version")
	}
}

func TestService_RetryRejectsChangedOperationContractBeforePrivateAdapters(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	desired := testDesired(t, root, domain.AppHermes, true, hermesHome)
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)

	oldContract := strings.Repeat("a", sha256.Size*2)
	plan := reconcile.OperationPlan{
		ContractHash: oldContract,
		Actions: []reconcile.Action{{
			ID: "50-configure-application", Kind: reconcile.ActionConfigureApplication, Idempotent: true,
		}},
	}
	receipt := reconcile.NewReceipt(desired, plan)
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, receipt); err != nil {
		t.Fatal(err)
	}

	downloads, privateCalls, writerCalls := 0, 0, 0
	svc := New(Options{
		OperationContract: func(domain.DesiredState) (string, error) {
			return strings.Repeat("b", sha256.Size*2), nil
		},
		ApplicationHome: func(domain.DesiredState) (string, error) {
			privateCalls++
			return hermesHome, nil
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			privateCalls++
			return nil, errors.New("must not open secrets")
		},
		AskPass: func(string, gitx.Credentials) (AskPassSession, error) {
			privateCalls++
			return nil, errors.New("must not create askpass")
		},
		Downloader: DownloadFunc(func(context.Context, string) ([]byte, error) {
			downloads++
			return nil, errors.New("must not download")
		}),
		WritePrivate: func(string, []byte) error {
			writerCalls++
			return errors.New("must not write")
		},
		Effects: func(EffectInputs) engine.Effects {
			privateCalls++
			return failingEffects{}
		},
	})

	err = svc.Retry(context.Background(), root)
	if err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
		t.Fatalf("Retry error = %v, want OPERATION_CONTRACT_MISMATCH", err)
	}
	if downloads != 0 || privateCalls != 0 || writerCalls != 0 {
		t.Fatalf("contract mismatch opened downloader=%d private adapters=%d writer=%d, want none", downloads, privateCalls, writerCalls)
	}
}

func TestService_StatusRejectsExactFailedRC2HermesOperationBeforeRuntimeProbe(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSWindows, Application: domain.AppHermes, AppInstalled: true,
		KitHome: root, HermesHome: hermesHome, Project: domain.ProjectAPA,
		Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeRC2ProfileOwnership(t, desired)

	plan := rc2FailedToolchainPlan()
	receipt := reconcile.NewReceipt(desired, plan)
	for _, actionID := range []string{"10-prepare-workspace", "20-sync-content", "30-sync-database"} {
		if err := receipt.Record(actionID, reconcile.EffectSucceeded, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := receipt.Record("40-install-toolchain", reconcile.EffectFailed, "toolchain skill layout is invalid"); err != nil {
		t.Fatal(err)
	}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, receipt); err != nil {
		t.Fatal(err)
	}

	runtimeCalls := 0
	svc := New(Options{ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
		runtimeCalls++
		return hermes.DiscoveryResult{
			Installed: true, Home: hermesHome,
			Executable: filepath.Join(hermesHome, "hermes-agent", "venv", "Scripts", "hermes.exe"),
			Version:    "0.20.1",
		}, nil
	}})
	if _, _, err := svc.Status(context.Background(), root); err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
		t.Fatalf("Status exact RC2 operation error = %v, want OPERATION_CONTRACT_MISMATCH", err)
	}
	if runtimeCalls != 0 {
		t.Fatalf("Status legacy RC2 operation launched %d runtime probes, want none", runtimeCalls)
	}
}

func TestService_RetryRejectsChangedOperationContract_LegacyRC2BeforePrivateAdapters(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSWindows, Application: domain.AppHermes, AppInstalled: true,
		KitHome: root, HermesHome: hermesHome, Project: domain.ProjectAPA,
		Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeRC2ProfileOwnership(t, desired)

	plan := rc2FailedToolchainPlan()
	receipt := reconcile.NewReceipt(desired, plan)
	for _, actionID := range []string{"10-prepare-workspace", "20-sync-content", "30-sync-database"} {
		if err := receipt.Record(actionID, reconcile.EffectSucceeded, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := receipt.Record("40-install-toolchain", reconcile.EffectFailed, "toolchain skill layout is invalid"); err != nil {
		t.Fatal(err)
	}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, receipt); err != nil {
		t.Fatal(err)
	}

	store := &recordingSecretStore{loaded: map[string]string{
		credentials.CustomLLMAPIKey: "provider-key", credentials.JiraToken: "jira-token", credentials.ConfluenceToken: "confluence-token",
	}}
	downloads, privateCalls, runtimeCalls, writerCalls := 0, 0, 0, 0
	svc := New(Options{
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			runtimeCalls++
			return hermes.DiscoveryResult{
				Installed: true, Home: hermesHome,
				Executable: filepath.Join(hermesHome, "hermes-agent", "venv", "Scripts", "hermes.exe"),
				Version:    "0.20.1",
			}, nil
		},
		ApplicationHome: func(domain.DesiredState) (string, error) {
			privateCalls++
			return hermesHome, nil
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			privateCalls++
			return store, nil
		},
		Downloader: DownloadFunc(func(context.Context, string) ([]byte, error) {
			downloads++
			return nil, errors.New("must not download OfficeCLI")
		}),
		WritePrivate: func(string, []byte) error {
			writerCalls++
			return errors.New("must not write")
		},
		ManagedCertificateBundle: func(string, string) (string, bool, error) {
			return filepath.Join(hermesHome, "certs", "ca-bundle.pem"), true, nil
		},
		AskPass: func(_ string, input gitx.Credentials) (AskPassSession, error) {
			privateCalls++
			return &recordingAskPass{credentials: input}, nil
		},
		Effects: func(EffectInputs) engine.Effects {
			privateCalls++
			return failingEffects{}
		},
	})
	err = svc.Retry(context.Background(), root)
	if err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
		t.Fatalf("Retry legacy RC2 operation error = %v, want OPERATION_CONTRACT_MISMATCH", err)
	}
	if downloads != 0 || privateCalls != 0 || runtimeCalls != 0 || writerCalls != 0 {
		t.Fatalf("Retry legacy RC2 operation opened downloader=%d private adapters=%d runtime=%d writer=%d, want none", downloads, privateCalls, runtimeCalls, writerCalls)
	}
}

func TestService_RetryRejectsRC2BeforeRuntimeAndPrivateAdapters(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSWindows, Application: domain.AppHermes, AppInstalled: true,
		KitHome: root, HermesHome: hermesHome, Project: domain.ProjectAPA,
		Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeRC2ProfileOwnership(t, desired)
	plan := rc2FailedToolchainPlan()
	receipt := reconcile.NewReceipt(desired, plan)
	for _, actionID := range []string{"10-prepare-workspace", "20-sync-content", "30-sync-database"} {
		if err := receipt.Record(actionID, reconcile.EffectSucceeded, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := receipt.Record("40-install-toolchain", reconcile.EffectFailed, "toolchain skill layout is invalid"); err != nil {
		t.Fatal(err)
	}
	persisted, err := state.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, receipt); err != nil {
		t.Fatal(err)
	}

	runtimeCalls, privateCalls := 0, 0
	svc := New(Options{
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			runtimeCalls++
			version := "0.20.1"
			if runtimeCalls == 2 {
				version = "0.20.2"
			}
			return hermes.DiscoveryResult{
				Installed: true, Home: hermesHome,
				Executable: filepath.Join(hermesHome, "hermes-agent", "venv", "Scripts", "hermes.exe"),
				Version:    version,
			}, nil
		},
		ApplicationHome: func(domain.DesiredState) (string, error) {
			privateCalls++
			return "", errors.New("must not open private application home")
		},
		SecretStore: func(string) (credentials.SecretStore, error) {
			privateCalls++
			return nil, errors.New("must not open secrets")
		},
		Effects: func(EffectInputs) engine.Effects {
			privateCalls++
			return failingEffects{}
		},
	})
	err = svc.Retry(context.Background(), root)
	if err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
		t.Fatalf("Retry runtime drift error = %v, want OPERATION_CONTRACT_MISMATCH", err)
	}
	if runtimeCalls != 0 || privateCalls != 0 {
		t.Fatalf("Retry runtime calls=%d private adapters=%d; want none", runtimeCalls, privateCalls)
	}
}

func TestValidateExactLegacyMarker_UsesExactBoundedRegularRead(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "owner")
	expected := []byte("wms\n")
	foreign := errors.New("foreign owner")

	readCalls := 0
	err := validateExactLegacyMarkerWithReader(path, expected, foreign, func(gotPath string, limit int64) ([]byte, error) {
		readCalls++
		if gotPath != path || limit != int64(len(expected)) {
			t.Fatalf("read path=%q limit=%d, want %q/%d", gotPath, limit, path, len(expected))
		}
		return append([]byte(nil), expected...), nil
	})
	if err != nil || readCalls != 1 {
		t.Fatalf("exact marker error=%v reads=%d, want nil/1", err, readCalls)
	}

	for _, test := range []struct {
		name string
		read func(string, int64) ([]byte, error)
	}{
		{name: "different exact-length bytes", read: func(string, int64) ([]byte, error) { return []byte("apa\n"), nil }},
		{name: "oversize bytes", read: func(string, int64) ([]byte, error) { return []byte("wms\nextra"), nil }},
		{name: "unsafe read", read: func(string, int64) ([]byte, error) { return nil, pathsafe.ErrUnsafe }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateExactLegacyMarkerWithReader(path, expected, foreign, test.read); !errors.Is(err, foreign) {
				t.Fatalf("marker error = %v, want foreign owner", err)
			}
		})
	}
}

func TestService_RetryRejectsRC2OperationChangingToCurrentContractUnderLock(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	desired := rc2Desired(t, root, hermesHome, domain.ProjectAPA, domain.ToolchainCC1CSkills)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeRC2ProfileOwnership(t, desired)
	legacyPlan := rc2FailedToolchainPlan()
	legacyReceipt := exactRC2FailedToolchainReceipt(desired, legacyPlan)
	currentHash, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	currentPlan := rc2FailedToolchainPlan()
	currentPlan.ContractHash = currentHash
	currentReceipt := exactRC2FailedToolchainReceipt(desired, currentPlan)
	store := &sequenceOperationStore{
		plans:    []reconcile.OperationPlan{legacyPlan, currentPlan},
		receipts: []*reconcile.Receipt{legacyReceipt, currentReceipt},
	}

	runtimeCalls, privateCalls := 0, 0
	svc := New(Options{
		StateStore: func(string) (engine.Store, error) { return store, nil },
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			runtimeCalls++
			return hermes.DiscoveryResult{
				Installed: true, Home: hermesHome,
				Executable: filepath.Join(hermesHome, "hermes-agent", "venv", "Scripts", "hermes.exe"),
				Version:    "0.20.1",
			}, nil
		},
		ApplicationHome: func(domain.DesiredState) (string, error) { privateCalls++; return "", errors.New("must not open") },
		SecretStore:     func(string) (credentials.SecretStore, error) { privateCalls++; return nil, errors.New("must not open") },
		Effects:         func(EffectInputs) engine.Effects { privateCalls++; return failingEffects{} },
	})
	err = svc.Retry(context.Background(), root)
	if err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
		t.Fatalf("Retry contract mode drift error = %v, want OPERATION_CONTRACT_MISMATCH", err)
	}
	if runtimeCalls != 0 || privateCalls != 0 {
		t.Fatalf("Retry runtime calls=%d private adapters=%d, want 0 and 0", runtimeCalls, privateCalls)
	}
}

func TestService_RetryRejectsRC2DesiredAndReceiptHomeDriftUnderLock(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	initialHome := filepath.Join(testutil.TempDir(t), "hermes-initial")
	driftedHome := filepath.Join(testutil.TempDir(t), "hermes-drifted")
	initialDesired := rc2Desired(t, root, initialHome, domain.ProjectAPA, domain.ToolchainCC1CSkills)
	driftedDesired := rc2Desired(t, root, driftedHome, domain.ProjectAPA, domain.ToolchainCC1CSkills)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, initialDesired)
	if err := workspace.EnsureOwner(root, string(initialDesired.Project())); err != nil {
		t.Fatal(err)
	}
	writeRC2ProfileOwnership(t, initialDesired)
	initialEnv, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	driftedEnv := []byte(strings.Replace(string(initialEnv), "HERMES_HOME="+initialHome, "HERMES_HOME="+driftedHome, 1))
	plan := rc2FailedToolchainPlan()
	store := &sequenceOperationStore{
		plans:    []reconcile.OperationPlan{plan, plan},
		receipts: []*reconcile.Receipt{exactRC2FailedToolchainReceipt(initialDesired, plan), exactRC2FailedToolchainReceipt(driftedDesired, plan)},
	}

	readCalls, runtimeCalls, privateCalls := 0, 0, 0
	svc := New(Options{
		ReadFile: func(string) ([]byte, error) {
			readCalls++
			if readCalls == 1 {
				return append([]byte(nil), initialEnv...), nil
			}
			return append([]byte(nil), driftedEnv...), nil
		},
		StateStore: func(string) (engine.Store, error) { return store, nil },
		ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
			runtimeCalls++
			home := initialHome
			if runtimeCalls > 1 {
				home = driftedHome
			}
			return hermes.DiscoveryResult{
				Installed: true, Home: home,
				Executable: filepath.Join(home, "hermes-agent", "venv", "Scripts", "hermes.exe"),
				Version:    "0.20.1",
			}, nil
		},
		ApplicationHome: func(domain.DesiredState) (string, error) { privateCalls++; return "", errors.New("must not open") },
		SecretStore:     func(string) (credentials.SecretStore, error) { privateCalls++; return nil, errors.New("must not open") },
		Effects:         func(EffectInputs) engine.Effects { privateCalls++; return failingEffects{} },
	})
	err = svc.Retry(context.Background(), root)
	if err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
		t.Fatalf("Retry desired drift error = %v, want OPERATION_CONTRACT_MISMATCH", err)
	}
	if readCalls != 1 || runtimeCalls != 0 || privateCalls != 0 {
		t.Fatalf("Retry reads=%d runtime=%d private=%d; want 1, 0, 0", readCalls, runtimeCalls, privateCalls)
	}
}

func TestService_StatusRejectsRC2BeforeRuntimeAndPrivateAdapters(t *testing.T) {
	for _, test := range []struct {
		name         string
		version      string
		observedHome func(string) string
	}{
		{name: "version_0_20_2", version: "0.20.2", observedHome: func(home string) string { return home }},
		{name: "different_home", version: "0.20.1", observedHome: func(string) string { return filepath.Join(testutil.TempDir(t), "other-hermes") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
			desired := rc2Desired(t, root, hermesHome, domain.ProjectAPA, domain.ToolchainCC1CSkills)
			writeRC2Operation(t, desired, rc2FailedToolchainPlan(), exactRC2FailedToolchainReceipt)

			runtimeCalls, privateCalls := 0, 0
			svc := New(Options{
				ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
					runtimeCalls++
					home := test.observedHome(hermesHome)
					return hermes.DiscoveryResult{
						Installed: true, Home: home,
						Executable: filepath.Join(home, "hermes-agent", "venv", "Scripts", "hermes.exe"),
						Version:    test.version,
					}, nil
				},
				ApplicationHome: func(domain.DesiredState) (string, error) { privateCalls++; return "", errors.New("must not open") },
				SecretStore:     func(string) (credentials.SecretStore, error) { privateCalls++; return nil, errors.New("must not open") },
				Effects:         func(EffectInputs) engine.Effects { privateCalls++; return failingEffects{} },
			})
			if _, _, err := svc.Status(context.Background(), root); err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
				t.Fatalf("Status drift error = %v, want OPERATION_CONTRACT_MISMATCH", err)
			}
			if runtimeCalls != 0 || privateCalls != 0 {
				t.Fatalf("Status runtime calls=%d private adapters=%d, want 0 and 0", runtimeCalls, privateCalls)
			}
		})
	}
}

func TestService_StatusRejectsRC2DesiredContractAndActionDriftBeforeRuntime(t *testing.T) {
	for _, test := range []struct {
		name       string
		current    func(*testing.T, string, string) domain.DesiredState
		mutatePlan func(reconcile.OperationPlan) reconcile.OperationPlan
		receipt    func(domain.DesiredState, reconcile.OperationPlan) *reconcile.Receipt
	}{
		{
			name: "hermes_home",
			current: func(t *testing.T, root, _ string) domain.DesiredState {
				return rc2Desired(t, root, filepath.Join(testutil.TempDir(t), "other-hermes"), domain.ProjectAPA, domain.ToolchainCC1CSkills)
			},
		},
		{
			name: "project",
			current: func(t *testing.T, root, home string) domain.DesiredState {
				return rc2Desired(t, root, home, domain.ProjectWMS, domain.ToolchainCC1CSkills)
			},
		},
		{
			name: "toolchain",
			current: func(t *testing.T, root, home string) domain.DesiredState {
				return rc2Desired(t, root, home, domain.ProjectAPA, domain.ToolchainAIRules1C)
			},
		},
		{
			name:    "contract_hash",
			current: func(_ *testing.T, _ string, _ string) domain.DesiredState { return domain.DesiredState{} },
			mutatePlan: func(plan reconcile.OperationPlan) reconcile.OperationPlan {
				plan.ContractHash = strings.Repeat("b", sha256.Size*2)
				return plan
			},
		},
		{
			name:    "action_id",
			current: func(_ *testing.T, _ string, _ string) domain.DesiredState { return domain.DesiredState{} },
			mutatePlan: func(plan reconcile.OperationPlan) reconcile.OperationPlan {
				plan.Actions[3].ID = "41-install-toolchain"
				return plan
			},
			receipt: exactRC2FailedToolchainReceipt,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
			persistedDesired := rc2Desired(t, root, hermesHome, domain.ProjectAPA, domain.ToolchainCC1CSkills)
			plan := rc2FailedToolchainPlan()
			if test.mutatePlan != nil {
				plan = test.mutatePlan(plan)
			}
			receiptFactory := test.receipt
			if receiptFactory == nil {
				receiptFactory = exactRC2FailedToolchainReceipt
			}
			writeRC2Operation(t, persistedDesired, plan, receiptFactory)
			current := test.current(t, root, hermesHome)
			if current.Project() != "" {
				writeDesired(t, current)
			}

			runtimeCalls := 0
			svc := New(Options{ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
				runtimeCalls++
				return hermes.DiscoveryResult{Installed: true, Home: hermesHome, Executable: filepath.Join(hermesHome, "hermes.exe"), Version: "0.20.1"}, nil
			}})
			if _, _, err := svc.Status(context.Background(), root); err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
				t.Fatalf("Status drift error = %v, want OPERATION_CONTRACT_MISMATCH", err)
			}
			if runtimeCalls != 0 {
				t.Fatalf("structural drift launched %d runtime probes", runtimeCalls)
			}
		})
	}
}

func TestService_StatusRejectsForgedRC2HomeOrRoleWithoutExistingProfileOwnership(t *testing.T) {
	for _, test := range []struct {
		name   string
		forged func(*testing.T, string, string) domain.DesiredState
	}{
		{
			name: "hermes_home",
			forged: func(t *testing.T, root, _ string) domain.DesiredState {
				return rc2Desired(t, root, filepath.Join(testutil.TempDir(t), "forged-hermes"), domain.ProjectAPA, domain.ToolchainCC1CSkills)
			},
		},
		{
			name: "role",
			forged: func(t *testing.T, root, home string) domain.DesiredState {
				desired, err := domain.NewDesiredState(domain.DesiredStateInput{
					OS: domain.OSWindows, Application: domain.AppHermes, AppInstalled: true,
					KitHome: root, HermesHome: home, Project: domain.ProjectAPA,
					Role: domain.RoleAnalyst, Toolchain: domain.ToolchainCC1CSkills,
				})
				if err != nil {
					t.Fatal(err)
				}
				return desired
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(testutil.TempDir(t), "workspace")
			hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
			original := rc2Desired(t, root, hermesHome, domain.ProjectAPA, domain.ToolchainCC1CSkills)
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := workspace.EnsureOwner(root, string(original.Project())); err != nil {
				t.Fatal(err)
			}
			writeRC2ProfileOwnership(t, original)

			forged := test.forged(t, root, hermesHome)
			writeDesired(t, forged)
			plan := rc2FailedToolchainPlan()
			persisted, err := state.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := persisted.SaveOperation(plan, exactRC2FailedToolchainReceipt(forged, plan)); err != nil {
				t.Fatal(err)
			}

			runtimeCalls := 0
			svc := New(Options{ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
				runtimeCalls++
				return hermes.DiscoveryResult{
					Installed: true, Home: forged.HermesHome(),
					Executable: filepath.Join(forged.HermesHome(), "hermes-agent", "venv", "Scripts", "hermes.exe"),
					Version:    "0.20.1",
				}, nil
			}})
			if _, _, err := svc.Status(context.Background(), root); err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
				t.Fatalf("Status forged selector error = %v, want OPERATION_CONTRACT_MISMATCH", err)
			}
			if runtimeCalls != 0 {
				t.Fatalf("forged selector launched %d runtime probes", runtimeCalls)
			}
		})
	}
}

func TestService_StatusRejectsWhitespaceAlteredRC2KitOwner(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
	desired := rc2Desired(t, root, hermesHome, domain.ProjectAPA, domain.ToolchainCC1CSkills)
	writeRC2Operation(t, desired, rc2FailedToolchainPlan(), exactRC2FailedToolchainReceipt)
	owner := filepath.Join(root, ".teamkit", "owner")
	if err := workspace.WriteFileAtomic(owner, []byte(string(desired.Project())+"\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeCalls := 0
	svc := New(Options{ResolveHermesRuntime: func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
		runtimeCalls++
		return hermes.DiscoveryResult{
			Installed: true, Home: hermesHome,
			Executable: filepath.Join(hermesHome, "hermes-agent", "venv", "Scripts", "hermes.exe"),
			Version:    "0.20.1",
		}, nil
	}})
	if _, _, err := svc.Status(context.Background(), root); err == nil || err.Error() != "OPERATION_CONTRACT_MISMATCH" {
		t.Fatalf("Status altered owner error = %v, want OPERATION_CONTRACT_MISMATCH", err)
	}
	if runtimeCalls != 0 {
		t.Fatalf("altered owner launched %d runtime probes", runtimeCalls)
	}
}

func TestLegacyRC2FailedToolchainShapeRejectsCheckpointAndDiagnosticTampering(t *testing.T) {
	desired := rc2Desired(t, filepath.Join(testutil.TempDir(t), "workspace"), filepath.Join(testutil.TempDir(t), "hermes"), domain.ProjectAPA, domain.ToolchainCC1CSkills)
	plan := rc2FailedToolchainPlan()
	for _, test := range []struct {
		name    string
		receipt func() *reconcile.Receipt
	}{
		{name: "action_40_left_pending", receipt: func() *reconcile.Receipt {
			receipt := reconcile.NewReceipt(desired, plan)
			for _, actionID := range []string{"10-prepare-workspace", "20-sync-content", "30-sync-database"} {
				if err := receipt.Record(actionID, reconcile.EffectSucceeded, ""); err != nil {
					t.Fatal(err)
				}
			}
			return receipt
		}},
		{name: "action_40_succeeded", receipt: func() *reconcile.Receipt {
			receipt := reconcile.NewReceipt(desired, plan)
			for _, actionID := range []string{"10-prepare-workspace", "20-sync-content", "30-sync-database", "40-install-toolchain"} {
				if err := receipt.Record(actionID, reconcile.EffectSucceeded, ""); err != nil {
					t.Fatal(err)
				}
			}
			return receipt
		}},
		{name: "failure_moved_to_action_30", receipt: func() *reconcile.Receipt {
			receipt := reconcile.NewReceipt(desired, plan)
			for _, actionID := range []string{"10-prepare-workspace", "20-sync-content"} {
				if err := receipt.Record(actionID, reconcile.EffectSucceeded, ""); err != nil {
					t.Fatal(err)
				}
			}
			if err := receipt.Record("30-sync-database", reconcile.EffectFailed, "failure"); err != nil {
				t.Fatal(err)
			}
			return receipt
		}},
		{name: "diagnostic_changed", receipt: func() *reconcile.Receipt {
			receipt := reconcile.NewReceipt(desired, plan)
			for _, actionID := range []string{"10-prepare-workspace", "20-sync-content", "30-sync-database"} {
				if err := receipt.Record(actionID, reconcile.EffectSucceeded, ""); err != nil {
					t.Fatal(err)
				}
			}
			if err := receipt.Record("40-install-toolchain", reconcile.EffectFailed, "tampered"); err != nil {
				t.Fatal(err)
			}
			return receipt
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if legacyRC2FailedToolchainShape(plan, test.receipt()) {
				t.Fatal("tampered checkpoint shape accepted")
			}
		})
	}
}

func rc2FailedToolchainPlan() reconcile.OperationPlan {
	return reconcile.OperationPlan{
		ContractHash: "4756276de196b4f674e4719fdc934c9aff6f511634b35b7b53a8da54217a27a8",
		Actions: []reconcile.Action{
			{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true},
			{ID: "20-sync-content", Kind: reconcile.ActionSyncContent, Idempotent: true},
			{ID: "30-sync-database", Kind: reconcile.ActionSyncDatabase, Idempotent: true},
			{ID: "40-install-toolchain", Kind: reconcile.ActionInstallToolchain, Idempotent: true},
			{ID: "50-configure-application", Kind: reconcile.ActionConfigureApplication, Idempotent: true},
			{ID: "90-verify-state", Kind: reconcile.ActionVerifyState, Idempotent: true},
		},
	}
}

func interruptedHermesStates(t *testing.T, root, hermesHome, version string) (domain.DesiredState, domain.DesiredState) {
	t.Helper()
	public := testDesired(t, root, domain.AppHermes, true, hermesHome)
	input := desiredStateInput(public)
	input.HermesVersion = version
	return public, mustDesiredState(t, input)
}

func currentHermesOperationPlan(t *testing.T, desired domain.DesiredState) reconcile.OperationPlan {
	t.Helper()
	contract, err := defaultOperationContract(desired)
	if err != nil {
		t.Fatal(err)
	}
	return reconcile.OperationPlan{
		ContractHash: contract,
		Actions: []reconcile.Action{{
			ID: "50-configure-application", Kind: reconcile.ActionConfigureApplication, Idempotent: true,
		}},
	}
}

func writeInterruptedCurrentOperation(t *testing.T, public, bound domain.DesiredState, plan reconcile.OperationPlan) {
	t.Helper()
	if err := os.MkdirAll(public.KitHome(), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, public)
	if err := workspace.EnsureOwner(public.KitHome(), string(public.Project())); err != nil {
		t.Fatal(err)
	}
	persisted, err := state.New(public.KitHome())
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, reconcile.NewReceipt(bound, plan)); err != nil {
		t.Fatal(err)
	}
}

func stableHermesRuntime(home, version string, calls *int) func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
	return func(context.Context, domain.DesiredState) (hermes.DiscoveryResult, error) {
		*calls++
		return hermes.DiscoveryResult{
			Installed: true, Home: home,
			Executable: filepath.Join(home, "hermes-agent", "venv", "bin", "hermes"),
			Version:    version,
		}, nil
	}
}

func rc2Desired(t *testing.T, root, hermesHome string, project domain.ProjectID, toolchain domain.Toolchain) domain.DesiredState {
	t.Helper()
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSWindows, Application: domain.AppHermes, AppInstalled: true,
		KitHome: root, HermesHome: hermesHome, Project: project,
		Role: domain.RoleDeveloper, Toolchain: toolchain,
	})
	if err != nil {
		t.Fatal(err)
	}
	return desired
}

func writeRC2Operation(t *testing.T, desired domain.DesiredState, plan reconcile.OperationPlan, receiptFactory func(domain.DesiredState, reconcile.OperationPlan) *reconcile.Receipt) {
	t.Helper()
	if err := os.MkdirAll(desired.KitHome(), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	if err := workspace.EnsureOwner(desired.KitHome(), string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeRC2ProfileOwnership(t, desired)
	persisted, err := state.New(desired.KitHome())
	if err != nil {
		t.Fatal(err)
	}
	if err := persisted.SaveOperation(plan, receiptFactory(desired, plan)); err != nil {
		t.Fatal(err)
	}
}

func writeRC2ProfileOwnership(t *testing.T, desired domain.DesiredState) {
	t.Helper()
	identity := hermesProfileIdentity(desired)
	profile := filepath.Join(desired.HermesHome(), "profiles", identity)
	for _, directory := range []string{profile, filepath.Join(profile, ".teamkit", "toolchain-source")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	owner := filepath.Join(desired.HermesHome(), ".teamkit", "profiles", identity+".owner")
	if err := workspace.WriteFileAtomic(owner, []byte(identity+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exactRC2FailedToolchainReceipt(desired domain.DesiredState, plan reconcile.OperationPlan) *reconcile.Receipt {
	receipt := reconcile.NewReceipt(desired, plan)
	for _, actionID := range []string{plan.Actions[0].ID, plan.Actions[1].ID, plan.Actions[2].ID} {
		_ = receipt.Record(actionID, reconcile.EffectSucceeded, "")
	}
	_ = receipt.Record(plan.Actions[3].ID, reconcile.EffectFailed, "toolchain skill layout is invalid")
	return receipt
}

type recordingActionEffects struct{ actions *[]reconcile.Action }

func (recordingActionEffects) Observe(context.Context, domain.DesiredState, reconcile.UpdateChoice) (reconcile.ObservedState, error) {
	return reconcile.ObservedState{}, nil
}

func (effects recordingActionEffects) Apply(_ context.Context, _ domain.DesiredState, action reconcile.Action) error {
	*effects.actions = append(*effects.actions, action)
	return nil
}

type sequenceOperationStore struct {
	plans    []reconcile.OperationPlan
	receipts []*reconcile.Receipt
	loads    int
}

func (*sequenceOperationStore) SavePlan(reconcile.OperationPlan) error { return nil }
func (*sequenceOperationStore) LoadPlan() (reconcile.OperationPlan, error) {
	return reconcile.OperationPlan{}, errors.New("unexpected LoadPlan")
}
func (*sequenceOperationStore) SaveReceipt(*reconcile.Receipt) error { return nil }
func (*sequenceOperationStore) LoadReceipt(...string) (*reconcile.Receipt, error) {
	return nil, errors.New("unexpected LoadReceipt")
}
func (*sequenceOperationStore) SaveOperation(reconcile.OperationPlan, *reconcile.Receipt) error {
	return nil
}
func (store *sequenceOperationStore) LoadOperation(...string) (reconcile.OperationPlan, *reconcile.Receipt, error) {
	if store.loads >= len(store.plans) || store.loads >= len(store.receipts) {
		return reconcile.OperationPlan{}, nil, errors.New("unexpected LoadOperation")
	}
	plan, receipt := store.plans[store.loads], store.receipts[store.loads]
	store.loads++
	return plan, receipt, nil
}
