package hermes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"gopkg.in/yaml.v3"
)

func TestVerifyManagedProfile_AcceptsExactManagedState(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	if err := VerifyManagedProfile(context.Background(), fixture.expectation); err != nil {
		t.Fatalf("VerifyManagedProfile() error = %v", err)
	}
}

func TestVerifyManagedProfile_AcceptsSchema38(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	fixture.config = []byte(strings.Replace(string(fixture.config), "_config_version: 34", "_config_version: 38", 1))
	writeManagedFile(t, filepath.Join(fixture.root, "config.yaml"), string(fixture.config))
	fixture.runtime.ConfigSchema = 38
	fixture.expectation.Runtime = fixture.runtime
	fixture.expectation.Config = fixture.config
	fixture.expectation.RuntimeProbe = func(_ context.Context, executable string) (RuntimeContract, error) {
		if executable != fixture.runtime.Info.Executable {
			return RuntimeContract{}, errors.New("wrong executable")
		}
		return fixture.runtime, nil
	}
	if err := VerifyManagedProfile(context.Background(), fixture.expectation); err != nil {
		t.Fatalf("VerifyManagedProfile() error = %v", err)
	}
}

func TestVerifyManagedProfile_RejectsManagedInvariantDrift(t *testing.T) {
	mutations := map[string]func(*testing.T, *managedProfileFixture){
		"config-schema": func(t *testing.T, f *managedProfileFixture) {
			writeManagedFile(t, filepath.Join(f.root, "config.yaml"), strings.Replace(string(f.config), "_config_version: 34", "_config_version: 37", 1))
		},
		"provider-model": func(t *testing.T, f *managedProfileFixture) {
			writeManagedFile(t, filepath.Join(f.root, "config.yaml"), strings.ReplaceAll(string(f.config), "public-development", "wrong-model"))
		},
		"v8std": func(t *testing.T, f *managedProfileFixture) {
			writeManagedFile(t, filepath.Join(f.root, "config.yaml"), strings.Replace(string(f.config), "https://ai.v8std.ru/mcp", "https://wrong.invalid/mcp", 1))
		},
		"jira": func(t *testing.T, f *managedProfileFixture) {
			writeManagedFile(t, filepath.Join(f.root, "config.yaml"), strings.Replace(string(f.config), "Token ${TEAMKIT_PUBLIC_ISSUES_KEY}", "Token wrong", 1))
		},
		"workspace": func(t *testing.T, f *managedProfileFixture) {
			writeManagedFile(t, filepath.Join(f.root, "config.yaml"), strings.Replace(string(f.config), f.workspace, filepath.Join(filepath.Dir(f.workspace), "other"), 1))
		},
		"secret-permissions": func(t *testing.T, f *managedProfileFixture) {
			if err := os.Remove(f.environment); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(f.environment, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"owner-record": func(t *testing.T, f *managedProfileFixture) {
			writeManagedFile(t, f.ownerPath, "foreign\n")
		},
		"profile-root": func(t *testing.T, f *managedProfileFixture) {
			moved := f.root + ".moved"
			if err := os.Rename(f.root, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(f.root, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"final-lock": func(t *testing.T, f *managedProfileFixture) {
			lock := filepath.Join(f.root, "external", string(f.pin.ID)+".json")
			writeManagedFile(t, lock, "{}")
		},
		"owned-file": func(t *testing.T, f *managedProfileFixture) {
			writeManagedFile(t, filepath.Join(f.root, "skills", "alpha", "SKILL.md"), "tampered")
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			fixture := newManagedProfileFixture(t)
			mutate(t, fixture)
			if err := VerifyManagedProfile(context.Background(), fixture.expectation); !errors.Is(err, ErrManagedInvariant) {
				t.Fatalf("VerifyManagedProfile() error = %v, want ErrManagedInvariant", err)
			}
		})
	}
}

func TestValidateManagedConfig_AcceptsEnabledAtlassianMCP(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	if err := validateManagedConfig(fixture.config, HermesConfigVersion); err != nil {
		t.Fatalf("validateManagedConfig() error = %v", err)
	}
}

func TestManagedConfig_RequiresOfficeCLI(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	legacyProfile := Profile{
		Project: "apa", Role: "developer", KitHome: fixture.workspace,
		Toolchain:     Toolchain{Name: "cc_1c_skills", Origin: "pin", Version: "pin"},
		V8StdEndpoint: catalog.V8StdMCP().Endpoint,
	}
	legacy, err := legacyProfile.RenderForSchema(PublicProviderProvider(), HermesConfigVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateManagedConfig(legacy, HermesConfigVersion); err == nil {
		t.Fatal("three-MCP profile was accepted")
	}
	if err := validateManagedConfig(fixture.config, HermesConfigVersion); err != nil {
		t.Fatalf("exact four-MCP profile rejected: %v", err)
	}
	withExtra := managedConfig(t, fixture.config)
	withExtra.MCPServers["unexpected"] = mcpYAML{URL: "https://example.invalid/mcp", Enabled: true}
	requireManagedConfigRejected(t, withExtra)
	for _, id := range []string{"officecli", "v8std", "public-provider-issues", "public-provider-wiki"} {
		t.Run("missing/"+id, func(t *testing.T) {
			missing := managedConfig(t, fixture.config)
			delete(missing.MCPServers, id)
			requireManagedConfigRejected(t, missing)
		})
	}
}

func TestManagedConfig_RejectsExplicitForbiddenMCPFields(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	for _, test := range []struct {
		name  string
		mcpID string
		field string
		value yaml.Node
	}{
		{name: "officecli/url-empty", mcpID: "officecli", field: "url", value: yamlScalar("!!str", "")},
		{name: "officecli/url-null", mcpID: "officecli", field: "url", value: yamlScalar("!!null", "null")},
		{name: "officecli/headers-empty", mcpID: "officecli", field: "headers", value: yamlEmptyMapping()},
		{name: "officecli/headers-null", mcpID: "officecli", field: "headers", value: yamlScalar("!!null", "null")},
		{name: "officecli/sampling-null", mcpID: "officecli", field: "sampling", value: yamlScalar("!!null", "null")},
		{name: "officecli/parallel-null", mcpID: "officecli", field: "supports_parallel_tool_calls", value: yamlScalar("!!null", "null")},
		{name: "officecli/connect-timeout-zero", mcpID: "officecli", field: "connect_timeout", value: yamlScalar("!!int", "0")},
		{name: "officecli/connect-timeout-null", mcpID: "officecli", field: "connect_timeout", value: yamlScalar("!!null", "null")},
		{name: "officecli/timeout-zero", mcpID: "officecli", field: "timeout", value: yamlScalar("!!int", "0")},
		{name: "officecli/timeout-null", mcpID: "officecli", field: "timeout", value: yamlScalar("!!null", "null")},
		{name: "officecli/env-empty", mcpID: "officecli", field: "env", value: yamlEmptyMapping()},
		{name: "officecli/env-null", mcpID: "officecli", field: "env", value: yamlScalar("!!null", "null")},
		{name: "v8std/command-empty", mcpID: "v8std", field: "command", value: yamlScalar("!!str", "")},
		{name: "v8std/command-null", mcpID: "v8std", field: "command", value: yamlScalar("!!null", "null")},
		{name: "v8std/args-empty", mcpID: "v8std", field: "args", value: yamlEmptySequence()},
		{name: "v8std/args-null", mcpID: "v8std", field: "args", value: yamlScalar("!!null", "null")},
		{name: "v8std/env-empty", mcpID: "v8std", field: "env", value: yamlEmptyMapping()},
		{name: "v8std/env-null", mcpID: "v8std", field: "env", value: yamlScalar("!!null", "null")},
		{name: "jira/command-empty", mcpID: "public-provider-issues", field: "command", value: yamlScalar("!!str", "")},
		{name: "jira/args-empty", mcpID: "public-provider-issues", field: "args", value: yamlEmptySequence()},
		{name: "jira/env-empty", mcpID: "public-provider-issues", field: "env", value: yamlEmptyMapping()},
		{name: "confluence/command-null", mcpID: "public-provider-wiki", field: "command", value: yamlScalar("!!null", "null")},
		{name: "confluence/args-null", mcpID: "public-provider-wiki", field: "args", value: yamlScalar("!!null", "null")},
		{name: "confluence/env-null", mcpID: "public-provider-wiki", field: "env", value: yamlScalar("!!null", "null")},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := withRawMCPField(t, fixture.config, test.mcpID, test.field, test.value)
			if err := validateManagedConfig(data, HermesConfigVersion); err == nil {
				t.Fatalf("explicit forbidden %s on %s was accepted:\n%s", test.field, test.mcpID, data)
			}
		})
	}
}

func TestManagedConfig_RejectsMergedForbiddenMCPFields(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	for _, test := range []struct {
		name  string
		mcpID string
		field string
		value yaml.Node
	}{
		{name: "officecli/http-url", mcpID: "officecli", field: "url", value: yamlScalar("!!str", "")},
		{name: "v8std/stdio-command", mcpID: "v8std", field: "command", value: yamlScalar("!!str", "")},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := withRawMCPMerge(t, fixture.config, test.mcpID, test.field, test.value)
			if err := validateManagedConfig(data, HermesConfigVersion); err == nil {
				t.Fatalf("merged forbidden %s on %s was accepted:\n%s", test.field, test.mcpID, data)
			}
		})
	}
}

func TestManagedConfig_PreservesV8stdRemoteFieldCompatibility(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	config := managedConfig(t, fixture.config)
	v8std := config.MCPServers["v8std"]
	enabled := true
	v8std.Headers = map[string]string{"x-compatible": "value"}
	v8std.Sampling = &samplingYAML{Enabled: true}
	v8std.SupportsParallelToolCalls = &enabled
	v8std.ConnectTimeout = 1
	v8std.Timeout = 1
	config.MCPServers["v8std"] = v8std
	rendered, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateManagedConfig(rendered, HermesConfigVersion); err != nil {
		t.Fatalf("existing v8std HTTP fields were rejected: %v", err)
	}
}

func TestManagedConfig_RejectsOfficeCLITransportDrift(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	mutations := map[string]func(*mcpYAML){
		"relative command": func(mcp *mcpYAML) { mcp.Command = filepath.Base(mcp.Command) },
		"changed args":     func(mcp *mcpYAML) { mcp.Args = []string{"serve"} },
		"additional args":  func(mcp *mcpYAML) { mcp.Args = []string{"mcp", "extra"} },
		"environment":      func(mcp *mcpYAML) { mcp.Env = map[string]string{"TOKEN": "value"} },
		"url":              func(mcp *mcpYAML) { mcp.URL = "https://example.invalid/mcp" },
		"headers":          func(mcp *mcpYAML) { mcp.Headers = map[string]string{"x-test": "value"} },
		"sampling":         func(mcp *mcpYAML) { mcp.Sampling = &samplingYAML{Enabled: false} },
		"parallel":         func(mcp *mcpYAML) { disabled := false; mcp.SupportsParallelToolCalls = &disabled },
		"connect timeout":  func(mcp *mcpYAML) { mcp.ConnectTimeout = 60 },
		"timeout":          func(mcp *mcpYAML) { mcp.Timeout = 120 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			config := managedConfig(t, fixture.config)
			mcp := config.MCPServers["officecli"]
			mutate(&mcp)
			config.MCPServers["officecli"] = mcp
			requireManagedConfigRejected(t, config)
		})
	}
}

func TestManagedConfig_RejectsRemoteStdioTransport(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	for _, id := range []string{"v8std", "public-provider-issues", "public-provider-wiki"} {
		for _, mutation := range []struct {
			name  string
			apply func(*mcpYAML)
		}{
			{name: "command", apply: func(mcp *mcpYAML) { mcp.Command = filepath.Join(t.TempDir(), officeCLIManagedNameForTest()) }},
			{name: "args", apply: func(mcp *mcpYAML) { mcp.Args = []string{"mcp"} }},
			{name: "env", apply: func(mcp *mcpYAML) { mcp.Env = map[string]string{"TOKEN": "value"} }},
		} {
			t.Run(id+"/"+mutation.name, func(t *testing.T) {
				config := managedConfig(t, fixture.config)
				mcp := config.MCPServers[id]
				mutation.apply(&mcp)
				config.MCPServers[id] = mcp
				requireManagedConfigRejected(t, config)
			})
		}
	}
}

func TestManagedConfig_RejectsRemoteMCPContractDrift(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	for _, id := range []string{"public-provider-issues", "public-provider-wiki"} {
		for _, mutation := range []struct {
			name  string
			apply func(*mcpYAML)
		}{
			{name: "url", apply: func(mcp *mcpYAML) { mcp.URL = "https://example.invalid/mcp" }},
			{name: "header", apply: func(mcp *mcpYAML) { mcp.Headers = map[string]string{"authorization": "wrong"} }},
			{name: "sampling", apply: func(mcp *mcpYAML) { mcp.Sampling = &samplingYAML{Enabled: true} }},
			{name: "parallel", apply: func(mcp *mcpYAML) { enabled := true; mcp.SupportsParallelToolCalls = &enabled }},
			{name: "connect timeout", apply: func(mcp *mcpYAML) { mcp.ConnectTimeout++ }},
			{name: "timeout", apply: func(mcp *mcpYAML) { mcp.Timeout++ }},
		} {
			t.Run(id+"/"+mutation.name, func(t *testing.T) {
				config := managedConfig(t, fixture.config)
				mcp := config.MCPServers[id]
				mutation.apply(&mcp)
				config.MCPServers[id] = mcp
				requireManagedConfigRejected(t, config)
			})
		}
	}
}

func TestValidateManagedConfig_RejectsDisabledAtlassianMCP(t *testing.T) {
	fixture := newManagedProfileFixture(t)
	for _, mcpID := range []string{"public-provider-issues", "public-provider-wiki"} {
		t.Run(mcpID, func(t *testing.T) {
			config := managedConfig(t, fixture.config)
			mcp := config.MCPServers[mcpID]
			mcp.Enabled = false
			config.MCPServers[mcpID] = mcp
			rendered, err := yaml.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateManagedConfig(rendered, HermesConfigVersion); err == nil || err.Error() != "managed config mismatch" {
				t.Fatalf("validateManagedConfig() error = %v, want managed config mismatch", err)
			}
		})
	}
}

type managedProfileFixture struct {
	root        string
	workspace   string
	environment string
	ownerPath   string
	config      []byte
	pin         catalog.Toolchain
	runtime     RuntimeContract
	expectation ManagedProfileExpectation
}

func newManagedProfileFixture(t *testing.T) *managedProfileFixture {
	t.Helper()
	base := testutil.TempDir(t)
	root := filepath.Join(base, "profiles", "1c-apa-developer-cc_1c_skills")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(base, "workspace")
	profile := Profile{
		Project: "apa", Role: "developer", KitHome: workspace,
		Toolchain:     Toolchain{Name: "cc_1c_skills", Origin: "pin", Version: "pin"},
		V8StdEndpoint: catalog.V8StdMCP().Endpoint,
	}
	profile, err := profile.WithOfficeCLI(filepath.Join(base, officeCLIManagedNameForTest()))
	if err != nil {
		t.Fatal(err)
	}
	config, err := profile.RenderForSchema(PublicProviderProvider(), 34)
	if err != nil {
		t.Fatal(err)
	}
	managed := managedConfig(t, config)
	for _, mcp := range catalog.AtlassianMCPs() {
		entry := managed.MCPServers[mcp.ID]
		entry.Enabled = true
		managed.MCPServers[mcp.ID] = entry
	}
	config, err = yaml.Marshal(managed)
	if err != nil {
		t.Fatal(err)
	}
	writeManagedFile(t, filepath.Join(root, "config.yaml"), string(config))
	environment := filepath.Join(root, ".env")
	if err := privatefile.WriteAtomic(environment, []byte(PublicProviderProvider().APIKeyEnvironment+"=test-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Base(root)
	ownerRecord := []byte(identity + "\n")
	ownerPath := filepath.Join(base, ".teamkit", "profiles", identity+".owner")
	if err := privatefile.WriteAtomic(ownerPath, ownerRecord, 0o600); err != nil {
		t.Fatal(err)
	}
	pin, err := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(base, "source")
	if err := os.MkdirAll(filepath.Join(source, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeManagedFile(t, filepath.Join(source, ".git", "HEAD"), pin.Commit+"\n")
	skill := filepath.Join(source, ".claude", "skills", "alpha")
	if err := os.MkdirAll(skill, 0o700); err != nil {
		t.Fatal(err)
	}
	writeManagedFile(t, filepath.Join(skill, "SKILL.md"), "---\nname: alpha\n---\n")
	if err := MaterializeToolchain(source, root, pin); err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(base, "runtime")
	runtime := RuntimeContract{
		Info:         RuntimeInfo{Executable: filepath.Join(runtimeRoot, "hermes.exe"), InstallDir: runtimeRoot, Version: "0.20.1"},
		Identity:     RuntimeIdentity{InstallRootKey: "runtime-root", ExecutableKey: "runtime-executable"},
		ConfigSchema: 34, BundledSkills: []string{"github"}, BundledInventorySHA256: "inventory",
	}
	expectation := ManagedProfileExpectation{
		Runtime: runtime,
		RuntimeProbe: func(_ context.Context, executable string) (RuntimeContract, error) {
			if executable != runtime.Info.Executable {
				return RuntimeContract{}, errors.New("wrong executable")
			}
			return runtime, nil
		},
		Owner:  ProfileOwnerExpectation{Identity: identity, ProfileRoot: root, OwnerPath: ownerPath, OwnerRecord: ownerRecord},
		Config: config, Environment: environment, ToolchainPin: pin,
	}
	return &managedProfileFixture{root: root, workspace: workspace, environment: environment, ownerPath: ownerPath, config: config, pin: pin, runtime: runtime, expectation: expectation}
}

func managedConfig(t *testing.T, data []byte) renderedProfile {
	t.Helper()
	var config renderedProfile
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func requireManagedConfigRejected(t *testing.T, config renderedProfile) {
	t.Helper()
	rendered, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateManagedConfig(rendered, HermesConfigVersion); err == nil || err.Error() != "managed config mismatch" {
		t.Fatalf("validateManagedConfig() error = %v, want managed config mismatch", err)
	}
}

func withRawMCPField(t *testing.T, data []byte, mcpID, field string, value yaml.Node) []byte {
	t.Helper()
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	mcpServers := yamlMappingValue(t, document.Content[0], "mcp_servers")
	mcp := yamlMappingValue(t, mcpServers, mcpID)
	mcp.Content = append(mcp.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: field}, &value)
	rendered, err := yaml.Marshal(&document)
	if err != nil {
		t.Fatal(err)
	}
	return rendered
}

func withRawMCPMerge(t *testing.T, data []byte, mcpID, field string, value yaml.Node) []byte {
	t.Helper()
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	mcpServers := yamlMappingValue(t, document.Content[0], "mcp_servers")
	mcp := yamlMappingValue(t, mcpServers, mcpID)
	merge := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: field}, &value,
	}}
	mcp.Content = append(mcp.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!merge", Value: "<<"}, merge)
	rendered, err := yaml.Marshal(&document)
	if err != nil {
		t.Fatal(err)
	}
	return rendered
}

func yamlMappingValue(t *testing.T, mapping *yaml.Node, key string) *yaml.Node {
	t.Helper()
	if mapping.Kind != yaml.MappingNode {
		t.Fatalf("YAML node kind = %d, want mapping", mapping.Kind)
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	t.Fatalf("YAML mapping missing %q", key)
	return nil
}

func yamlScalar(tag, value string) yaml.Node {
	return yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
}

func yamlEmptyMapping() yaml.Node {
	return yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func yamlEmptySequence() yaml.Node {
	return yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
}

func writeManagedFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
