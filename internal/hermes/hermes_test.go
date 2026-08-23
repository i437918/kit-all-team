package hermes

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"gopkg.in/yaml.v3"
)

func TestProvider_PublicProviderContract(t *testing.T) {
	provider := PublicProviderProvider()

	if provider.Endpoint != "https://llm.example.invalid/v1" {
		t.Fatalf("endpoint = %q", provider.Endpoint)
	}
	if provider.Model != "public-development" {
		t.Fatalf("model = %q", provider.Model)
	}
	if provider.APIKeyEnvironment != "TEAMKIT_PUBLIC_PROVIDER_API_KEY" {
		t.Fatalf("API key environment = %q", provider.APIKeyEnvironment)
	}
	if provider.ID != "public-provider" || provider.Name != "PublicProvider" || provider.APIMode != "chat_completions" {
		t.Fatalf("provider identity = %#v", provider)
	}
}

func TestProfile_Render_ContainsPinnedToolchainAndMCPs(t *testing.T) {
	profile := Profile{
		Project: "billing",
		Role:    "developer",
		KitHome: "/teamkit/billing",
		Toolchain: Toolchain{
			Name:    "bsl-language-server",
			Origin:  "toolchain-secret-value",
			Version: "0.29.0",
		},
		V8StdEndpoint: "https://mcp.example.test/v8std",
	}

	officeCLICommand := filepath.Join(t.TempDir(), officeCLIManagedNameForTest())
	profile, err := profile.WithOfficeCLI(officeCLICommand)
	if err != nil {
		t.Fatalf("WithOfficeCLI() error = %v", err)
	}
	rendered, err := profile.Render(PublicProviderProvider())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(rendered, &config); err != nil {
		t.Fatalf("rendered config is not YAML: %v\n%s", err, rendered)
	}
	if config["_config_version"] != HermesConfigVersion {
		t.Fatalf("config version = %#v", config["_config_version"])
	}
	model := config["model"].(map[string]any)
	if model["default"] != "public-development" || model["provider"] != "custom:public-provider" ||
		model["base_url"] != "https://llm.example.invalid/v1" || model["api_mode"] != "chat_completions" ||
		model["api_key"] != "${TEAMKIT_PUBLIC_PROVIDER_API_KEY}" {
		t.Fatalf("model config = %#v", model)
	}
	providers := config["providers"].(map[string]any)
	provider := providers["public-provider"].(map[string]any)
	if len(providers) != 1 || provider["name"] != "PublicProvider" || provider["api"] != "https://llm.example.invalid/v1" ||
		provider["key_env"] != "TEAMKIT_PUBLIC_PROVIDER_API_KEY" || provider["default_model"] != "public-development" ||
		provider["transport"] != "chat_completions" {
		t.Fatalf("providers config = %#v", providers)
	}
	mcpServers := config["mcp_servers"].(map[string]any)
	v8std := mcpServers["v8std"].(map[string]any)
	if len(mcpServers) != 4 || !equalAnyMap(v8std, map[string]any{"url": "https://mcp.example.test/v8std", "enabled": true}) {
		t.Fatalf("MCP config = %#v", mcpServers)
	}
	for _, expected := range catalog.AtlassianMCPs() {
		actual := mcpServers[expected.ID].(map[string]any)
		if actual["url"] != expected.Endpoint || actual["enabled"] != true || actual["sampling"].(map[string]any)["enabled"] != false || actual["supports_parallel_tool_calls"] != false || actual["connect_timeout"] != expected.ConnectTimeout || actual["timeout"] != expected.Timeout || !equalAnyMap(actual["headers"].(map[string]any), stringMapToAny(expected.Headers)) {
			t.Fatalf("%s MCP = %#v", expected.ID, actual)
		}
		for _, forbidden := range []string{"command", "args", "env"} {
			if _, ok := actual[forbidden]; ok {
				t.Fatalf("%s contains stdio field %q: %#v", expected.ID, forbidden, actual)
			}
		}
	}
	officeCLI := mcpServers["officecli"].(map[string]any)
	if !equalAnyMap(officeCLI, map[string]any{"command": officeCLICommand, "args": []any{"mcp"}, "enabled": true}) {
		t.Fatalf("OfficeCLI MCP = %#v", officeCLI)
	}
	for _, forbidden := range []string{"url", "headers", "sampling", "supports_parallel_tool_calls", "connect_timeout", "timeout", "env"} {
		if _, ok := officeCLI[forbidden]; ok {
			t.Fatalf("OfficeCLI contains remote field %q: %#v", forbidden, officeCLI)
		}
	}
	for _, shell := range []string{"cmd", "powershell", "pwsh", "sh", "bash"} {
		if strings.HasPrefix(strings.ToLower(filepath.Base(officeCLI["command"].(string))), shell) {
			t.Fatalf("OfficeCLI command uses shell %q", officeCLI["command"])
		}
	}
	terminal := config["terminal"].(map[string]any)
	if terminal["backend"] != "local" || terminal["cwd"] != "/teamkit/billing" {
		t.Fatalf("terminal config = %#v", terminal)
	}
	text := string(rendered)
	if strings.Contains(text, "secret-value") || strings.Contains(text, "toolchain:") {
		t.Fatalf("config contains forbidden secret/toolchain metadata: %s", text)
	}
}

func TestProfile_RenderRejectsRelativeOfficeCLICommand(t *testing.T) {
	profile := Profile{}
	updated, err := profile.WithOfficeCLI(filepath.Join("relative", officeCLIManagedNameForTest()))
	if err == nil || updated != (Profile{}) {
		t.Fatalf("WithOfficeCLI(relative) = %#v, %v; want unchanged profile and error", updated, err)
	}
	direct := Profile{
		Project: "billing", Role: "developer", KitHome: "/teamkit/billing",
		Toolchain:        Toolchain{Name: "bsl-language-server", Version: "0.29.0"},
		V8StdEndpoint:    "https://mcp.example.test/v8std",
		OfficeCLICommand: filepath.Join("relative", officeCLIManagedNameForTest()),
	}
	if rendered, err := direct.Render(PublicProviderProvider()); !errors.Is(err, errInvalidProfile) || rendered != nil {
		t.Fatalf("Render() = %q, %v; want nil errInvalidProfile", rendered, err)
	}
}

func TestProfile_RenderForSchema_RenderspublicAtlassianMCPs(t *testing.T) {
	profile := Profile{
		Project: "billing",
		Role:    "developer",
		KitHome: "/teamkit/billing",
		Toolchain: Toolchain{
			Name:    "bsl-language-server",
			Origin:  "provider-secret-value-jira-secret-value-confluence-secret-value",
			Version: "0.29.0",
		},
		V8StdEndpoint: "https://mcp.example.test/v8std",
	}
	officeCLICommand := filepath.Join(t.TempDir(), officeCLIManagedNameForTest())
	profile, err := profile.WithOfficeCLI(officeCLICommand)
	if err != nil {
		t.Fatalf("WithOfficeCLI() error = %v", err)
	}
	type samplingConfig struct {
		Enabled bool `yaml:"enabled"`
	}
	type mcpConfig struct {
		URL                       string            `yaml:"url"`
		Command                   string            `yaml:"command"`
		Args                      []string          `yaml:"args"`
		Env                       map[string]string `yaml:"env"`
		Enabled                   bool              `yaml:"enabled"`
		Headers                   map[string]string `yaml:"headers"`
		Sampling                  *samplingConfig   `yaml:"sampling"`
		SupportsParallelToolCalls *bool             `yaml:"supports_parallel_tool_calls"`
		ConnectTimeout            int               `yaml:"connect_timeout"`
		Timeout                   int               `yaml:"timeout"`
	}
	for _, schema := range []int{34, 37} {
		t.Run(fmt.Sprintf("schema-%d", schema), func(t *testing.T) {
			rendered, err := profile.RenderForSchema(PublicProviderProvider(), schema)
			if err != nil {
				t.Fatalf("RenderForSchema() error = %v", err)
			}
			var config struct {
				MCPServers map[string]mcpConfig `yaml:"mcp_servers"`
			}
			if err := yaml.Unmarshal(rendered, &config); err != nil {
				t.Fatalf("rendered config is not YAML: %v\n%s", err, rendered)
			}
			if len(config.MCPServers) != 4 {
				t.Fatalf("MCP server count = %d, want 4", len(config.MCPServers))
			}
			jira := config.MCPServers["public-provider-issues"]
			if !jira.Enabled {
				t.Fatal("Jira MCP must be enabled")
			}
			if jira.URL != "https://mcp.example.invalid/issues" || jira.Headers["x-mcp-jira-authorization"] != "Token ${TEAMKIT_PUBLIC_ISSUES_KEY}" || jira.Sampling == nil || jira.Sampling.Enabled || jira.SupportsParallelToolCalls == nil || *jira.SupportsParallelToolCalls || jira.ConnectTimeout != 60 || jira.Timeout != 120 {
				t.Fatalf("jira = %#v", jira)
			}
			conf := config.MCPServers["public-provider-wiki"]
			if !conf.Enabled {
				t.Fatal("Confluence MCP must be enabled")
			}
			if conf.URL != "https://mcp.example.invalid/wiki" || conf.Headers["x-mcp-confluence-authorization"] != "Token ${TEAMKIT_PUBLIC_WIKI_KEY}" {
				t.Fatalf("confluence = %#v", conf)
			}
			officeCLI := config.MCPServers["officecli"]
			if officeCLI.Command != officeCLICommand || !reflect.DeepEqual(officeCLI.Args, []string{"mcp"}) || !officeCLI.Enabled || officeCLI.URL != "" || len(officeCLI.Headers) != 0 || officeCLI.Sampling != nil || officeCLI.SupportsParallelToolCalls != nil || officeCLI.ConnectTimeout != 0 || officeCLI.Timeout != 0 || len(officeCLI.Env) != 0 {
				t.Fatalf("officecli = %#v", officeCLI)
			}
			for _, secret := range []string{"provider-secret-value", "jira-secret-value", "confluence-secret-value"} {
				if strings.Contains(string(rendered), secret) {
					t.Fatalf("rendered config contains sentinel secret %q", secret)
				}
			}
		})
	}
}

func officeCLIManagedNameForTest() string {
	if runtime.GOOS == "windows" {
		return "officecli.exe"
	}
	return "officecli"
}

func equalAnyMap(got, want map[string]any) bool {
	return reflect.DeepEqual(got, want)
}

func stringMapToAny(values map[string]string) map[string]any {
	converted := make(map[string]any, len(values))
	for key, value := range values {
		converted[key] = value
	}
	return converted
}

func TestProfileFromDesired_BindsCatalogPinAndSeparateV8std(t *testing.T) {
	state, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
		KitHome: "/teamkit", HermesHome: "/hermes", Project: domain.ProjectWMS,
		Role: domain.RoleArchitect, Toolchain: domain.ToolchainAIRules1C,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := ProfileFromDesired(state)
	if err != nil {
		t.Fatalf("ProfileFromDesired() error = %v", err)
	}
	if profile.Toolchain != (Toolchain{Name: string(domain.ToolchainAIRules1C), Origin: catalog.Toolchains()[1].Origin, Version: catalog.Toolchains()[1].Commit}) {
		t.Fatalf("profile toolchain = %#v", profile.Toolchain)
	}
	if profile.V8StdEndpoint != catalog.V8StdMCP().Endpoint {
		t.Fatalf("v8std endpoint = %q", profile.V8StdEndpoint)
	}
	if profile.KitHome != "/teamkit" {
		t.Fatalf("KIT home = %q", profile.KitHome)
	}
	if profile.OfficeCLICommand != "" {
		t.Fatalf("OfficeCLI command = %q, want empty before service wiring", profile.OfficeCLICommand)
	}
}

func TestMaterializeToolchain_CopiesClosedSkillLayoutAndIsIdempotent(t *testing.T) {
	for _, fixture := range []struct {
		id      domain.Toolchain
		subpath string
	}{
		{domain.ToolchainAIRules1C, filepath.Join("content", "skills")},
		{domain.ToolchainCC1CSkills, filepath.Join(".claude", "skills")},
	} {
		t.Run(string(fixture.id), func(t *testing.T) {
			pin, err := catalog.LookupToolchain(fixture.id)
			if err != nil {
				t.Fatal(err)
			}
			source, profile := testutil.TempDir(t), testutil.TempDir(t)
			if err := os.MkdirAll(filepath.Join(source, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, ".git", "HEAD"), []byte(pin.Commit+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, skill := range []string{"alpha", "beta"} {
				directory := filepath.Join(source, fixture.subpath, skill)
				if err := os.MkdirAll(directory, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("# "+skill+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Join(profile, "skills"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := MaterializeToolchain(source, profile, pin); err != nil {
				t.Fatalf("MaterializeToolchain: %v", err)
			}
			if err := MaterializeToolchain(source, profile, pin); err != nil {
				t.Fatalf("idempotent MaterializeToolchain: %v", err)
			}
			for _, skill := range []string{"alpha", "beta"} {
				if _, err := os.Stat(filepath.Join(profile, "skills", skill, "SKILL.md")); err != nil {
					t.Fatalf("installed %s: %v", skill, err)
				}
			}
			data, err := os.ReadFile(filepath.Join(profile, "external", string(fixture.id)+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var lock ToolchainLock
			if err := json.Unmarshal(data, &lock); err != nil {
				t.Fatal(err)
			}
			if lock.Toolchain != fixture.id || lock.Commit != pin.Commit || len(lock.InstalledSkills) != 2 || len(lock.Files) != 4 || len(lock.TreeSHA256) != sha256.Size*2 {
				t.Fatalf("lock = %#v", lock)
			}
		})
	}
}

func TestToolchainInstalled_RejectsOversizedExternalDirectory(t *testing.T) {
	pin, err := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{
		"alpha/SKILL.md": "# alpha\n",
	})
	if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(profile, "external")
	for index := 0; index < maxToolchainRootEntries; index++ {
		name := filepath.Join(external, fmt.Sprintf("unrelated-%03d", index))
		if err := os.WriteFile(name, []byte("unchanged\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ready, err := ToolchainInstalled(profile, pin)
	if ready || !errors.Is(err, ErrToolchainLayout) {
		t.Fatalf("ToolchainInstalled() = %v, %v; want false, ErrToolchainLayout", ready, err)
	}
}

func TestMaterializeToolchain_MergesWithoutTouchingUnrelatedSkills(t *testing.T) {
	for _, id := range []domain.Toolchain{domain.ToolchainAIRules1C, domain.ToolchainCC1CSkills} {
		t.Run(string(id), func(t *testing.T) {
			pin, err := catalog.LookupToolchain(id)
			if err != nil {
				t.Fatal(err)
			}
			source, profile := testutil.TempDir(t), testutil.TempDir(t)
			writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(id), pin.Commit, map[string]string{
				"alpha/SKILL.md":      "# alpha\n",
				"alpha/reference.txt": "trusted\n",
				"beta/SKILL.md":       "# beta\n",
			})
			unrelated := map[string]string{
				"github/SKILL.md":       "# bundled\n",
				"user-learned/SKILL.md": "# learned\n",
				"user-learned/note.txt": "user data\n",
			}
			for relative, contents := range unrelated {
				path := filepath.Join(profile, "skills", filepath.FromSlash(relative))
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before := hashSelectedFiles(t, profile, unrelated)

			if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); err != nil {
				t.Fatalf("materializeToolchain() error = %v", err)
			}
			assertSelectedFileHashes(t, profile, before)
			ready, err := ToolchainInstalled(profile, pin)
			if err != nil || !ready {
				t.Fatalf("ToolchainInstalled() = %v, %v", ready, err)
			}
			if err := os.WriteFile(filepath.Join(profile, "skills", "alpha", "reference.txt"), []byte("tampered\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ready, err = ToolchainInstalled(profile, pin)
			if err != nil {
				t.Fatal(err)
			}
			if ready {
				t.Fatal("tampered lock-owned file was reported ready")
			}
			assertSelectedFileHashes(t, profile, before)
		})
	}
}

func TestCheckBundledSkillCollisions_RejectsOnlySelectedNameOverlap(t *testing.T) {
	if ErrToolchainCollision.Error() != "HERMES_TOOLCHAIN_NAME_COLLISION" {
		t.Fatalf("collision code = %q", ErrToolchainCollision)
	}
	if err := CheckBundledSkillCollisions([]string{"github", "software-development"}, []string{"alpha", "beta"}); err != nil {
		t.Fatalf("unrelated names: %v", err)
	}
	if err := CheckBundledSkillCollisions([]string{"alpha", "github"}, []string{"alpha", "beta"}); !errors.Is(err, ErrToolchainCollision) {
		t.Fatalf("collision error = %v, want ErrToolchainCollision", err)
	}
}

func TestMaterializeToolchain_RejectsExistingSelectedNameWithoutMutation(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# source\n"})
	path := filepath.Join(profile, "skills", "alpha", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := fileSHA256(t, path)
	if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainCollision) {
		t.Fatalf("error = %v, want ErrToolchainCollision", err)
	}
	if got := fileSHA256(t, path); got != before {
		t.Fatalf("existing selected skill changed: got %s want %s", got, before)
	}
	if _, err := os.Lstat(filepath.Join(profile, ".teamkit", "toolchain.pending.json")); !os.IsNotExist(err) {
		t.Fatalf("pending created after collision: %v", err)
	}
}

func TestMaterializeToolchain_ResumesEveryInterruptionWithSameNonce(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	for failureAfter := 0; failureAfter <= 2; failureAfter++ {
		t.Run(fmt.Sprintf("after-%d-publishes", failureAfter), func(t *testing.T) {
			source, profile := testutil.TempDir(t), testutil.TempDir(t)
			writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{
				"alpha/SKILL.md": "# alpha\n",
				"beta/SKILL.md":  "# beta\n",
			})
			unrelatedBefore := addUnrelatedToolchainFixtures(t, profile)
			receiptPath := filepath.Join(profile, ".teamkit", "action-receipt.json")
			if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(receiptPath, []byte("receipt-sentinel\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			receiptBefore := fileSHA256(t, receiptPath)
			interrupt := errors.New("interrupt")
			published := 0
			options := MaterializeOptions{NonceSource: fixedToolchainNonce}
			if failureAfter == 0 {
				options.AfterPending = func() error { return interrupt }
			} else {
				options.AfterPublish = func(string) error {
					published++
					if published == failureAfter {
						return interrupt
					}
					return nil
				}
			}
			if err := materializeToolchain(source, profile, pin, options); !errors.Is(err, interrupt) {
				t.Fatalf("interrupted error = %v", err)
			}
			if got := fileSHA256(t, receiptPath); got != receiptBefore {
				t.Fatalf("action receipt changed on interruption: got %s want %s", got, receiptBefore)
			}
			assertSelectedFileHashes(t, profile, unrelatedBefore)
			pending := readToolchainPendingForTest(t, profile)
			if err := privatefile.Validate(filepath.Join(profile, ".teamkit", "toolchain.pending.json")); err != nil {
				t.Fatalf("pending is not owner-only: %v", err)
			}
			if pending.Nonce != strings.Repeat("a", 32) {
				t.Fatalf("pending nonce = %q", pending.Nonce)
			}
			nonceCalls := 0
			if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: func() (string, error) {
				nonceCalls++
				return strings.Repeat("b", 32), nil
			}}); err != nil {
				t.Fatalf("retry error = %v", err)
			}
			if nonceCalls != 0 {
				t.Fatalf("retry generated %d new nonces", nonceCalls)
			}
			ready, err := ToolchainInstalled(profile, pin)
			if err != nil || !ready {
				t.Fatalf("ToolchainInstalled() = %v, %v", ready, err)
			}
			if _, err := os.Lstat(filepath.Join(profile, ".teamkit", "toolchain.pending.json")); !os.IsNotExist(err) {
				t.Fatalf("pending remains after success: %v", err)
			}
			if err := privatefile.Validate(filepath.Join(profile, "external", string(pin.ID)+".json")); err != nil {
				t.Fatalf("final lock is not owner-only: %v", err)
			}
			if got := fileSHA256(t, receiptPath); got != receiptBefore {
				t.Fatalf("action receipt changed on retry: got %s want %s", got, receiptBefore)
			}
			assertSelectedFileHashes(t, profile, unrelatedBefore)
		})
	}
}

func TestMaterializeToolchain_RejectsChangedPendingNonceOrManifest(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	for _, mutation := range []string{"nonce", "manifest"} {
		t.Run(mutation, func(t *testing.T) {
			source, profile := interruptedToolchainFixture(t, pin, 0)
			unrelatedBefore := unrelatedToolchainHashes(t, profile)
			pending := readToolchainPendingForTest(t, profile)
			originalStaging := filepath.Join(profile, ".teamkit", "toolchain-staging-"+pending.Nonce)
			if mutation == "nonce" {
				pending.Nonce = strings.Repeat("b", 32)
			} else {
				pending.Lock.Files[1].SHA256 = strings.Repeat("0", sha256.Size*2)
			}
			writeToolchainPendingForTest(t, profile, pending)
			if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainLayout) {
				t.Fatalf("error = %v, want ErrToolchainLayout", err)
			}
			if info, err := os.Lstat(originalStaging); err != nil || !info.IsDir() {
				t.Fatalf("foreign/original staging was cleaned: %v, %v", info, err)
			}
			if _, err := os.Lstat(filepath.Join(profile, "skills", "alpha")); !os.IsNotExist(err) {
				t.Fatalf("skill published from malformed pending: %v", err)
			}
			assertSelectedFileHashes(t, profile, unrelatedBefore)
		})
	}
}

func TestMaterializeToolchain_CompletedEntriesFailClosedBeforePublishing(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	for _, mutation := range []string{"tampered", "deleted"} {
		t.Run(mutation, func(t *testing.T) {
			source, profile := interruptedToolchainFixture(t, pin, 1)
			unrelatedBefore := unrelatedToolchainHashes(t, profile)
			pending := readToolchainPendingForTest(t, profile)
			pendingPath := filepath.Join(profile, ".teamkit", "toolchain.pending.json")
			pendingBefore := fileSHA256(t, pendingPath)
			if len(pending.Completed) != 1 {
				t.Fatalf("completed = %#v", pending.Completed)
			}
			completedPath := filepath.Join(profile, "skills", pending.Completed[0])
			if mutation == "tampered" {
				if err := os.WriteFile(filepath.Join(completedPath, "SKILL.md"), []byte("tampered\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.RemoveAll(completedPath); err != nil {
				t.Fatal(err)
			}
			if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); err == nil ||
				(mutation == "tampered" && !errors.Is(err, ErrToolchainCollision)) ||
				(mutation == "deleted" && !errors.Is(err, ErrToolchainLayout)) {
				t.Fatalf("error = %v", err)
			}
			if _, err := os.Lstat(filepath.Join(profile, "skills", "beta")); !os.IsNotExist(err) {
				t.Fatalf("new skill published after completed mutation: %v", err)
			}
			if got := fileSHA256(t, pendingPath); got != pendingBefore {
				t.Fatalf("pending progress changed on failed retry: got %s want %s", got, pendingBefore)
			}
			assertSelectedFileHashes(t, profile, unrelatedBefore)
		})
	}
}

func TestMaterializeToolchain_RecoversRenameBeforePendingRewrite(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := interruptedToolchainFixture(t, pin, 0)
	unrelatedBefore := unrelatedToolchainHashes(t, profile)
	pending := readToolchainPendingForTest(t, profile)
	skill := pending.Lock.InstalledSkills[0]
	staged := filepath.Join(profile, ".teamkit", "toolchain-staging-"+pending.Nonce, skill)
	if err := os.MkdirAll(filepath.Join(profile, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stageToolchainSkill(filepath.Join(source, toolchainSkillsSubpath(pin.ID), skill), staged, skill, pending.Lock.Files); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staged, filepath.Join(profile, "skills", skill)); err != nil {
		t.Fatal(err)
	}
	if got := readToolchainPendingForTest(t, profile).Completed; len(got) != 0 {
		t.Fatalf("fixture progress = %#v, want empty", got)
	}
	if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: func() (string, error) {
		t.Fatal("retry generated a nonce")
		return "", nil
	}}); err != nil {
		t.Fatalf("retry error = %v", err)
	}
	ready, err := ToolchainInstalled(profile, pin)
	if err != nil || !ready {
		t.Fatalf("ToolchainInstalled() = %v, %v", ready, err)
	}
	assertSelectedFileHashes(t, profile, unrelatedBefore)
}

func TestMaterializeToolchain_RecoversFinalLockBeforePendingDeletion(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := interruptedToolchainFixture(t, pin, 2)
	unrelatedBefore := unrelatedToolchainHashes(t, profile)
	pending := readToolchainPendingForTest(t, profile)
	if len(pending.Completed) != len(pending.Lock.InstalledSkills) {
		t.Fatalf("completed = %#v", pending.Completed)
	}
	if err := archiveEmptyStaging(filepath.Join(profile, ".teamkit"), filepath.Join(profile, ".teamkit", "toolchain-staging-"+pending.Nonce), pending.Nonce, pending.Lock.TreeSHA256, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := writeToolchainLock(filepath.Join(profile, "external", string(pin.ID)+".json"), pending.Lock); err != nil {
		t.Fatal(err)
	}
	if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: func() (string, error) {
		t.Fatal("retry generated a nonce")
		return "", nil
	}}); err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(profile, ".teamkit", "toolchain.pending.json")); !os.IsNotExist(err) {
		t.Fatalf("pending remains after retry: %v", err)
	}
	ready, err := ToolchainInstalled(profile, pin)
	if err != nil || !ready {
		t.Fatalf("ToolchainInstalled() = %v, %v", ready, err)
	}
	assertSelectedFileHashes(t, profile, unrelatedBefore)
}

func TestMaterializeToolchain_RejectsRedirectedSelectedNameWithoutTouchingTarget(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile, outside := testutil.TempDir(t), testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
	outsideFile := filepath.Join(outside, "SKILL.md")
	if err := os.WriteFile(outsideFile, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(profile, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(profile, "skills", "alpha")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	before := fileSHA256(t, outsideFile)
	if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainCollision) {
		t.Fatalf("error = %v, want ErrToolchainCollision", err)
	}
	if got := fileSHA256(t, outsideFile); got != before {
		t.Fatal("redirect target changed")
	}
}

func TestMaterializeToolchain_RejectsSecondFinalLock(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
	if err := os.MkdirAll(filepath.Join(profile, "external"), 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(profile, "external", "ai_rules_1c.json")
	if err := os.WriteFile(foreign, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := fileSHA256(t, foreign)
	if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainCollision) {
		t.Fatalf("error = %v, want ErrToolchainCollision", err)
	}
	if got := fileSHA256(t, foreign); got != before {
		t.Fatal("foreign final lock changed")
	}
}

func TestToolchainInstalled_RejectsUnsafeFinalAndMaterializeNormalizesExactLegacyLock(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(profile, "external", string(pin.ID)+".json")
	makeToolchainFileBroad(t, lockPath)
	if ready, err := ToolchainInstalled(profile, pin); err == nil || ready {
		t.Fatalf("unsafe final readiness = %v, %v", ready, err)
	}
	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatalf("normalize exact legacy final: %v", err)
	}
	if err := privatefile.Validate(lockPath); err != nil {
		t.Fatalf("normalized final permissions: %v", err)
	}
	if ready, err := ToolchainInstalled(profile, pin); err != nil || !ready {
		t.Fatalf("normalized final readiness = %v, %v", ready, err)
	}
}

func TestMaterializeToolchain_DoesNotNormalizeUnsafeNonExactLegacyLock(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(profile, "external", string(pin.ID)+".json")
	lock, err := readToolchainLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock.Commit = strings.Repeat("0", 40)
	lock.TreeSHA256 = toolchainTreeSHA256(lock.Toolchain, lock.Origin, lock.Commit, lock.InstalledSkills, lock.Files)
	data, _ := json.Marshal(lock)
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockPath, 0o644); err != nil {
		t.Fatal(err)
	}
	before := fileSHA256(t, lockPath)
	if err := MaterializeToolchain(source, profile, pin); !errors.Is(err, ErrToolchainLayout) {
		t.Fatalf("unsafe non-exact final error = %v", err)
	}
	if got := fileSHA256(t, lockPath); got != before {
		t.Fatal("unsafe non-exact final was normalized or replaced")
	}
}

func TestMaterializeToolchain_RejectsUnsafeExistingPendingWithoutPublishing(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := interruptedToolchainFixture(t, pin, 0)
	pendingPath := filepath.Join(profile, ".teamkit", "toolchain.pending.json")
	makeToolchainFileBroad(t, pendingPath)
	before := unrelatedToolchainHashes(t, profile)
	if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainLayout) {
		t.Fatalf("unsafe pending error = %v, want ErrToolchainLayout", err)
	}
	assertSelectedFileHashes(t, profile, before)
	if _, err := os.Lstat(filepath.Join(profile, "skills", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("published from unsafe pending: %v", err)
	}
}

func TestMaterializeToolchain_RejectsDuplicateJSONKeysAtEveryLevel(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	mutations := map[string]func(string) string{
		"pending": func(text string) string {
			return strings.Replace(text, `"nonce":`, `"nonce":"`+strings.Repeat("a", 32)+`","nonce":`, 1)
		},
		"nested-lock": func(text string) string {
			return strings.Replace(text, `"origin":`, `"origin":"duplicate","origin":`, 1)
		},
		"file-object": func(text string) string { return strings.Replace(text, `"path":`, `"path":"duplicate","path":`, 1) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			source, profile := interruptedToolchainFixture(t, pin, 0)
			path := filepath.Join(profile, ".teamkit", "toolchain.pending.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := privatefile.WriteAtomic(path, []byte(mutate(string(data))), 0o600); err != nil {
				t.Fatal(err)
			}
			before := unrelatedToolchainHashes(t, profile)
			if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainLayout) {
				t.Fatalf("duplicate error = %v", err)
			}
			assertSelectedFileHashes(t, profile, before)
		})
	}

	t.Run("final", func(t *testing.T) {
		source, profile := testutil.TempDir(t), testutil.TempDir(t)
		writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
		if err := MaterializeToolchain(source, profile, pin); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(profile, "external", string(pin.ID)+".json")
		data, _ := os.ReadFile(path)
		duplicate := strings.Replace(string(data), `"commit":`, `"commit":"duplicate","commit":`, 1)
		if err := privatefile.WriteAtomic(path, []byte(duplicate), 0o600); err != nil {
			t.Fatal(err)
		}
		if ready, err := ToolchainInstalled(profile, pin); !errors.Is(err, ErrToolchainLayout) || ready {
			t.Fatalf("duplicate final readiness = %v, %v", ready, err)
		}
	})
}

func TestMaterializeToolchain_SerializesTwoCallersAndPreservesFirstNonce(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
	entered, release := make(chan struct{}), make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- materializeToolchain(source, profile, pin, MaterializeOptions{
			NonceSource: fixedToolchainNonce,
			AfterPending: func() error {
				close(entered)
				<-release
				return errors.New("first interrupted")
			},
		})
	}()
	<-entered
	secondErr := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: func() (string, error) { return strings.Repeat("b", 32), nil }})
	close(release)
	if secondErr == nil {
		t.Fatal("second caller was not serialized")
	}
	if err := <-firstDone; err == nil {
		t.Fatal("first caller did not interrupt")
	}
	if got := readToolchainPendingForTest(t, profile).Nonce; got != strings.Repeat("a", 32) {
		t.Fatalf("pending nonce = %q", got)
	}
}

func TestMaterializeToolchain_FailsClosedOnDestinationPendingAndFinalRaces(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	for _, race := range []string{"destination", "pending-progress", "final", "pending-delete"} {
		t.Run(race, func(t *testing.T) {
			source, profile := testutil.TempDir(t), testutil.TempDir(t)
			writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
			var sentinel string
			options := MaterializeOptions{NonceSource: fixedToolchainNonce}
			if race == "destination" {
				options.BeforePublish = func(string) error {
					sentinel = filepath.Join(profile, "skills", "alpha", "user.txt")
					if err := os.MkdirAll(filepath.Dir(sentinel), 0o700); err != nil {
						return err
					}
					return os.WriteFile(sentinel, []byte("user\n"), 0o600)
				}
			}
			if race == "pending-progress" {
				options.BeforePendingReplace = func() error {
					path := filepath.Join(profile, ".teamkit", "toolchain.pending.json")
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					if err := os.Remove(path); err != nil {
						return err
					}
					return privatefile.WriteAtomic(path, data, 0o600)
				}
			}
			if race == "final" {
				options.BeforeFinal = func() error {
					sentinel = filepath.Join(profile, "external", string(pin.ID)+".json")
					return privatefile.WriteAtomic(sentinel, []byte(`{}`), 0o600)
				}
			}
			if race == "pending-delete" {
				options.BeforePendingDelete = func() error {
					path := filepath.Join(profile, ".teamkit", "toolchain.pending.json")
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					if err := os.Remove(path); err != nil {
						return err
					}
					return privatefile.WriteAtomic(path, data, 0o600)
				}
			}
			before := addUnrelatedToolchainFixtures(t, profile)
			if err := materializeToolchain(source, profile, pin, options); err == nil {
				t.Fatal("race was accepted")
			}
			assertSelectedFileHashes(t, profile, before)
			if sentinel != "" {
				if data, err := os.ReadFile(sentinel); err != nil || (race == "destination" && string(data) != "user\n") || (race == "final" && string(data) != `{}`) {
					t.Fatalf("race sentinel = %q, %v", data, err)
				}
			}
		})
	}
}

func TestMaterializeToolchain_PreservesForeignPendingSwappedAfterIdentityVerification(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	for _, phase := range []string{"progress", "delete"} {
		t.Run(phase, func(t *testing.T) {
			source, profile := testutil.TempDir(t), testutil.TempDir(t)
			writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
			foreign := []byte("foreign-pending-sentinel\n")
			swap := func() error {
				path := filepath.Join(profile, ".teamkit", "toolchain.pending.json")
				if err := os.Remove(path); err != nil {
					return err
				}
				return privatefile.WriteAtomic(path, foreign, 0o600)
			}
			options := MaterializeOptions{NonceSource: fixedToolchainNonce}
			if phase == "progress" {
				options.AfterPendingVerifyBeforeProgress = swap
			} else {
				options.AfterPendingVerifyBeforeDelete = swap
			}
			if err := materializeToolchain(source, profile, pin, options); err == nil {
				t.Fatal("post-verification pending swap was accepted")
			}
			path := filepath.Join(profile, ".teamkit", "toolchain.pending.json")
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, foreign) {
				t.Fatalf("foreign pending = %q, %v", data, err)
			}
		})
	}
}

func TestMaterializeToolchain_PreservesForeignFinalSwappedAfterLegacyVerification(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(profile, "external", string(pin.ID)+".json")
	makeToolchainFileBroad(t, path)
	foreign := []byte("foreign-final-sentinel\n")
	err := materializeToolchain(source, profile, pin, MaterializeOptions{
		NonceSource: fixedToolchainNonce,
		AfterLegacyFinalVerifyBeforeNormalize: func() error {
			if err := os.Rename(path, path+".review-swapped"); err != nil {
				return err
			}
			return privatefile.WriteAtomic(path, foreign, 0o600)
		},
	})
	if err == nil {
		t.Fatal("post-verification legacy final swap was accepted")
	}
	if data, readErr := os.ReadFile(path); readErr != nil || !bytes.Equal(data, foreign) {
		t.Fatalf("materialize error = %v; foreign final = %q, %v", err, data, readErr)
	}
}

func TestMaterializeToolchain_DoesNotNormalizeExactFinalInSwappedExternalDirectory(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(profile, "external")
	path := filepath.Join(external, string(pin.ID)+".json")
	makeToolchainFileBroad(t, path)
	exact, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterRenameCalled := false
	err = materializeToolchain(source, profile, pin, MaterializeOptions{
		NonceSource: fixedToolchainNonce,
		AfterLegacyFinalVerifyBeforeNormalize: func() error {
			if err := os.Rename(external, external+".original"); err != nil {
				return err
			}
			if err := os.Mkdir(external, 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(path, exact, 0o644); err != nil {
				return err
			}
			return os.Chmod(path, 0o644)
		},
		AfterLegacyPreviousRename: func() error {
			afterRenameCalled = true
			return errors.New("must not rename inside swapped external")
		},
	})
	if err == nil {
		t.Fatal("swapped external directory was accepted")
	}
	if afterRenameCalled {
		t.Fatal("foreign exact final was renamed before external binding revalidation")
	}
	if validateErr := privatefile.Validate(path); validateErr == nil {
		t.Fatal("foreign exact final was normalized")
	}
}

func TestMaterializeToolchain_PreservesForeignStagingSwapAfterVerification(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
	var sentinel string
	err := materializeToolchain(source, profile, pin, MaterializeOptions{
		NonceSource: fixedToolchainNonce,
		AfterStagingVerifyBeforeArchive: func(path string) error {
			if err := os.Rename(path, path+".original"); err != nil {
				return err
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				return err
			}
			sentinel = filepath.Join(path, "foreign-sentinel")
			return os.WriteFile(sentinel, []byte("foreign\n"), 0o600)
		},
	})
	if err == nil {
		t.Fatal("post-verification staging swap was accepted")
	}
	if data, readErr := os.ReadFile(sentinel); readErr != nil || string(data) != "foreign\n" {
		t.Fatalf("foreign sentinel = %q, %v", data, readErr)
	}
}

func TestMaterializeToolchain_RecoversEveryTransactionRenameCrash(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	for _, crashPoint := range []string{"pending-previous", "pending-next", "staging", "staging-token", "staging-archive", "pending-delete", "legacy-previous", "legacy-next"} {
		t.Run(crashPoint, func(t *testing.T) {
			source, profile := testutil.TempDir(t), testutil.TempDir(t)
			writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
			if strings.HasPrefix(crashPoint, "legacy-") {
				if err := MaterializeToolchain(source, profile, pin); err != nil {
					t.Fatal(err)
				}
				makeToolchainFileBroad(t, filepath.Join(profile, "external", string(pin.ID)+".json"))
			}
			crash := errors.New("simulated crash after rename")
			options := MaterializeOptions{NonceSource: fixedToolchainNonce}
			switch crashPoint {
			case "pending-previous":
				options.AfterPendingPreviousRename = func() error { return crash }
			case "pending-next":
				options.AfterPendingNextRename = func() error { return crash }
			case "staging":
				options.AfterStagingRename = func() error { return crash }
			case "staging-token":
				options.AfterStagingTokenRemoved = func() error { return crash }
			case "staging-archive":
				options.AfterStagingArchiveRemoved = func() error { return crash }
			case "pending-delete":
				options.AfterPendingDeleteRename = func() error { return crash }
			case "legacy-previous":
				options.AfterLegacyPreviousRename = func() error { return crash }
			case "legacy-next":
				options.AfterLegacyNextRename = func() error { return crash }
			}
			if err := materializeToolchain(source, profile, pin, options); !errors.Is(err, crash) {
				t.Fatalf("first error = %v, want crash", err)
			}
			if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: func() (string, error) {
				t.Fatal("recovery generated a new nonce")
				return "", nil
			}}); err != nil {
				t.Fatalf("recovery error = %v", err)
			}
			ready, err := ToolchainInstalled(profile, pin)
			if err != nil || !ready {
				t.Fatalf("ToolchainInstalled() = %v, %v", ready, err)
			}
			assertNoToolchainTransactionArtifacts(t, profile)
		})
	}
}

func TestMaterializeToolchain_RecoveryRestoresForeignMovedBeforeCrashVerification(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	for _, phase := range []string{"pending-progress", "pending-delete", "legacy-final", "staging"} {
		t.Run(phase, func(t *testing.T) {
			source, profile := testutil.TempDir(t), testutil.TempDir(t)
			writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
			foreign := []byte("foreign-transaction-sentinel\n")
			crash := errors.New("crash before moved-record verification")
			options := MaterializeOptions{NonceSource: fixedToolchainNonce}
			var survivor string
			swapFile := func(path string) func() error {
				return func() error {
					if err := os.Remove(path); err != nil {
						return err
					}
					return privatefile.WriteAtomic(path, foreign, 0o600)
				}
			}
			switch phase {
			case "pending-progress":
				survivor = filepath.Join(profile, ".teamkit", "toolchain.pending.json")
				options.AfterPendingVerifyBeforeProgress = swapFile(survivor)
				options.AfterPendingPreviousRename = func() error { return crash }
			case "pending-delete":
				survivor = filepath.Join(profile, ".teamkit", "toolchain.pending.json")
				options.AfterPendingVerifyBeforeDelete = swapFile(survivor)
				options.AfterPendingDeleteRename = func() error { return crash }
			case "legacy-final":
				if err := MaterializeToolchain(source, profile, pin); err != nil {
					t.Fatal(err)
				}
				survivor = filepath.Join(profile, "external", string(pin.ID)+".json")
				makeToolchainFileBroad(t, survivor)
				options.AfterLegacyFinalVerifyBeforeNormalize = func() error {
					if err := os.Rename(survivor, survivor+".original"); err != nil {
						return err
					}
					return privatefile.WriteAtomic(survivor, foreign, 0o600)
				}
				options.AfterLegacyPreviousRename = func() error { return crash }
			case "staging":
				options.AfterStagingVerifyBeforeArchive = func(path string) error {
					if err := os.Rename(path, path+".original"); err != nil {
						return err
					}
					if err := os.Mkdir(path, 0o700); err != nil {
						return err
					}
					survivor = filepath.Join(path, "foreign-sentinel")
					return os.WriteFile(survivor, foreign, 0o600)
				}
				options.AfterStagingRename = func() error { return crash }
			}
			if err := materializeToolchain(source, profile, pin, options); !errors.Is(err, crash) {
				t.Fatalf("first error = %v, want crash", err)
			}
			retryErr := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce})
			if retryErr == nil {
				t.Fatal("foreign moved record was accepted on recovery")
			}
			if data, err := os.ReadFile(survivor); err != nil || !bytes.Equal(data, foreign) {
				t.Fatalf("survivor = %q, %v", data, err)
			}
		})
	}
}

func TestMaterializeToolchain_RecoveryRestoresEmptyForeignStagingMovedBeforeCrashVerification(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
	crash := errors.New("crash before moved staging verification")
	var staging string
	var foreignInfo os.FileInfo
	err := materializeToolchain(source, profile, pin, MaterializeOptions{
		NonceSource: fixedToolchainNonce,
		AfterStagingVerifyBeforeArchive: func(path string) error {
			staging = path
			if err := os.Rename(path, path+".original"); err != nil {
				return err
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				return err
			}
			var err error
			foreignInfo, err = os.Lstat(path)
			return err
		},
		AfterStagingRename: func() error { return crash },
	})
	if !errors.Is(err, crash) {
		t.Fatalf("first error = %v, want crash", err)
	}
	if retryErr := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); retryErr == nil {
		t.Fatal("empty foreign staging directory was accepted on recovery")
	}
	restored, statErr := os.Lstat(staging)
	if statErr != nil || !os.SameFile(foreignInfo, restored) {
		t.Fatalf("foreign staging identity was not restored: %v", statErr)
	}
}

func TestMaterializeToolchain_RejectsOversizedRootAndGrowingSourceAfterPending(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	t.Run("root-entry-limit", func(t *testing.T) {
		source, profile := testutil.TempDir(t), testutil.TempDir(t)
		files := map[string]string{"alpha/SKILL.md": "# alpha\n"}
		for i := 0; i <= maxToolchainRootEntries; i++ {
			files[fmt.Sprintf("service-%04d.txt", i)] = "x"
		}
		writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, files)
		if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainLayout) {
			t.Fatalf("root limit error = %v", err)
		}
		if _, err := os.Lstat(filepath.Join(profile, ".teamkit")); !os.IsNotExist(err) {
			t.Fatalf("profile mutated before bounded preflight: %v", err)
		}
	})
	t.Run("selected-skill-limit", func(t *testing.T) {
		source, profile := testutil.TempDir(t), testutil.TempDir(t)
		files := make(map[string]string, maxToolchainSkills+1)
		for i := 0; i <= maxToolchainSkills; i++ {
			files[fmt.Sprintf("skill-%03d/SKILL.md", i)] = "# skill\n"
		}
		writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, files)
		if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainLayout) {
			t.Fatalf("skill limit error = %v", err)
		}
		if _, err := os.Lstat(filepath.Join(profile, ".teamkit")); !os.IsNotExist(err) {
			t.Fatalf("profile mutated before skill limit rejection: %v", err)
		}
	})
	t.Run("sparse-byte-limit", func(t *testing.T) {
		source, profile := testutil.TempDir(t), testutil.TempDir(t)
		writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
		oversized := filepath.Join(source, toolchainSkillsSubpath(pin.ID), "alpha", "oversized.bin")
		if err := os.WriteFile(oversized, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(oversized, maxToolchainBytes+1); err != nil {
			t.Fatal(err)
		}
		if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainLayout) {
			t.Fatalf("byte limit error = %v", err)
		}
		if _, err := os.Lstat(filepath.Join(profile, ".teamkit")); !os.IsNotExist(err) {
			t.Fatalf("profile mutated before byte limit rejection: %v", err)
		}
	})
	t.Run("directory-entry-limit", func(t *testing.T) {
		source, profile := testutil.TempDir(t), testutil.TempDir(t)
		writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
		root := filepath.Join(source, toolchainSkillsSubpath(pin.ID), "alpha", "directories")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < maxToolchainFiles; i++ {
			if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("d-%05d", i)), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainLayout) {
			t.Fatalf("directory limit error = %v", err)
		}
		if _, err := os.Lstat(filepath.Join(profile, ".teamkit")); !os.IsNotExist(err) {
			t.Fatalf("profile mutated before directory limit rejection: %v", err)
		}
	})
	t.Run("grow-after-pending", func(t *testing.T) {
		source, profile := testutil.TempDir(t), testutil.TempDir(t)
		writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
		before := addUnrelatedToolchainFixtures(t, profile)
		err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce, AfterPending: func() error {
			return os.WriteFile(filepath.Join(source, toolchainSkillsSubpath(pin.ID), "alpha", "SKILL.md"), []byte(strings.Repeat("x", 1<<20)), 0o600)
		}})
		if !errors.Is(err, ErrToolchainLayout) {
			t.Fatalf("growing source error = %v", err)
		}
		assertSelectedFileHashes(t, profile, before)
	})
}

func TestMaterializeToolchain_RejectsOversizedGrowingAndSwappedHEADWithoutProfileMutation(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	for _, mutation := range []string{"oversized", "growing", "swapped", "redirect-before-open"} {
		t.Run(mutation, func(t *testing.T) {
			source, profile := testutil.TempDir(t), testutil.TempDir(t)
			writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
			headPath := filepath.Join(source, ".git", "HEAD")
			options := MaterializeOptions{NonceSource: fixedToolchainNonce}
			if mutation == "oversized" {
				if err := os.WriteFile(headPath, []byte(strings.Repeat("a", 4097)), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if mutation == "redirect-before-open" {
				outside := filepath.Join(testutil.TempDir(t), "outside-head")
				if err := os.WriteFile(outside, []byte(pin.Commit+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				options.AfterHEADValidateBeforeOpen = func() error {
					if err := os.Rename(headPath, headPath+".swapped"); err != nil {
						return err
					}
					return os.Symlink(outside, headPath)
				}
			} else {
				options.AfterHEADVerifyBeforeRead = func() error {
					if mutation == "growing" {
						file, err := os.OpenFile(headPath, os.O_APPEND|os.O_WRONLY, 0)
						if err != nil {
							return err
						}
						_, writeErr := file.WriteString("x")
						closeErr := file.Close()
						if writeErr != nil {
							return writeErr
						}
						return closeErr
					}
					if err := os.Rename(headPath, headPath+".swapped"); err != nil {
						return err
					}
					return os.WriteFile(headPath, []byte(pin.Commit+"\n"), 0o600)
				}
			}
			if err := materializeToolchain(source, profile, pin, options); !errors.Is(err, ErrToolchainLayout) {
				t.Fatalf("error = %v, want ErrToolchainLayout", err)
			}
			for _, relative := range []string{".teamkit", "external", "skills"} {
				if _, err := os.Lstat(filepath.Join(profile, relative)); !os.IsNotExist(err) {
					t.Fatalf("profile mutated at %s: %v", relative, err)
				}
			}
		})
	}
}

func TestMaterializeToolchain_RejectsDeterministicRootAndSourceSwaps(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	for _, swap := range []string{"skills", "external", "source-skill", "source-file"} {
		t.Run(swap, func(t *testing.T) {
			source, profile, outside := testutil.TempDir(t), testutil.TempDir(t), testutil.TempDir(t)
			writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
			before := addUnrelatedToolchainFixtures(t, profile)
			if swap == "external" {
				if err := os.MkdirAll(filepath.Join(profile, "external"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			outsideSentinel := filepath.Join(outside, "sentinel")
			if err := os.WriteFile(outsideSentinel, []byte("outside\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce, AfterPending: func() error {
				switch swap {
				case "skills":
					return replacePathWithSymlink(filepath.Join(profile, "skills"), outside)
				case "external":
					return replacePathWithSymlink(filepath.Join(profile, "external"), outside)
				case "source-skill":
					return replacePathWithSymlink(filepath.Join(source, toolchainSkillsSubpath(pin.ID), "alpha"), outside)
				case "source-file":
					path := filepath.Join(source, toolchainSkillsSubpath(pin.ID), "alpha", "SKILL.md")
					if err := os.Remove(path); err != nil {
						return err
					}
					return os.Symlink(outsideSentinel, path)
				}
				return nil
			}})
			if err == nil {
				t.Fatal("swap was accepted")
			}
			if swap == "skills" {
				assertSelectedFileHashesAfterSkillsSwap(t, profile, before)
			} else {
				assertSelectedFileHashes(t, profile, before)
			}
			if data, readErr := os.ReadFile(outsideSentinel); readErr != nil || string(data) != "outside\n" {
				t.Fatalf("outside sentinel = %q, %v", data, readErr)
			}
		})
	}
}

func TestMaterializeToolchain_RejectsDeterministicAncestorAndMetadataSwaps(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	for _, swap := range []string{"source-root", "profile-root", "teamkit"} {
		t.Run(swap, func(t *testing.T) {
			source, profile, outside := testutil.TempDir(t), testutil.TempDir(t), testutil.TempDir(t)
			writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
			before := addUnrelatedToolchainFixtures(t, profile)
			outsideSentinel := filepath.Join(outside, "sentinel")
			if err := os.WriteFile(outsideSentinel, []byte("outside\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			swapped := false
			options := MaterializeOptions{NonceSource: func() (string, error) {
				if swap == "source-root" {
					if err := replacePathWithSymlink(source, outside); err != nil {
						return "", err
					}
					swapped = true
				}
				if swap == "profile-root" {
					if err := replacePathWithSymlink(profile, outside); err != nil {
						return "", err
					}
					swapped = true
				}
				return strings.Repeat("a", 32), nil
			}}
			if swap == "teamkit" {
				options.AfterPending = func() error {
					if err := replacePathWithSymlink(filepath.Join(profile, ".teamkit"), outside); err != nil {
						return err
					}
					swapped = true
					return nil
				}
			}
			err := materializeToolchain(source, profile, pin, options)
			if err == nil {
				t.Fatalf("ancestor swap was accepted (swap completed=%v)", swapped)
			}
			if swap != "profile-root" {
				assertSelectedFileHashes(t, profile, before)
			}
			if data, readErr := os.ReadFile(outsideSentinel); readErr != nil || string(data) != "outside\n" {
				t.Fatalf("outside sentinel = %q, %v", data, readErr)
			}
		})
	}
}

func replacePathWithSymlink(path, target string) error {
	backup := path + ".swapped"
	if err := os.Rename(path, backup); err != nil {
		return err
	}
	return os.Symlink(target, path)
}

func assertNoToolchainTransactionArtifacts(t *testing.T, profile string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(profile, ".teamkit"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "toolchain-txn") || strings.Contains(entry.Name(), "toolchain-staging-trash") || strings.Contains(entry.Name(), "toolchain-staging-marker") || strings.Contains(entry.Name(), "toolchain-progress-") || strings.Contains(entry.Name(), "toolchain-completed-") {
			t.Fatalf("transaction artifact remains: %s", entry.Name())
		}
	}
}

func makeToolchainFileBroad(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

func addUnrelatedToolchainFixtures(t *testing.T, profile string) map[string]string {
	t.Helper()
	files := map[string]string{
		"github/SKILL.md":  "# bundled\n",
		"learned/SKILL.md": "# learned\n",
		"user/note.txt":    "user\n",
	}
	for relative, contents := range files {
		path := filepath.Join(profile, "skills", filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return hashSelectedFiles(t, profile, files)
}

func unrelatedToolchainHashes(t *testing.T, profile string) map[string]string {
	t.Helper()
	return hashSelectedFiles(t, profile, map[string]string{
		"github/SKILL.md": "", "learned/SKILL.md": "", "user/note.txt": "",
	})
}

func fixedToolchainNonce() (string, error) { return strings.Repeat("a", 32), nil }

func interruptedToolchainFixture(t *testing.T, pin catalog.Toolchain, publishCount int) (string, string) {
	t.Helper()
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{
		"alpha/SKILL.md": "# alpha\n",
		"beta/SKILL.md":  "# beta\n",
	})
	addUnrelatedToolchainFixtures(t, profile)
	interrupt := errors.New("interrupt")
	options := MaterializeOptions{NonceSource: fixedToolchainNonce}
	if publishCount == 0 {
		options.AfterPending = func() error { return interrupt }
	} else {
		seen := 0
		options.AfterPublish = func(string) error {
			seen++
			if seen == publishCount {
				return interrupt
			}
			return nil
		}
	}
	if err := materializeToolchain(source, profile, pin, options); !errors.Is(err, interrupt) {
		t.Fatalf("interrupted error = %v", err)
	}
	return source, profile
}

func readToolchainPendingForTest(t *testing.T, profile string) ToolchainPending {
	t.Helper()
	pending, exists, identity, err := loadToolchainPending(filepath.Join(profile, ".teamkit", "toolchain.pending.json"))
	if err != nil || !exists {
		t.Fatalf("loadToolchainPending() = %#v, %v, want existing", pending, err)
	}
	identity.Close()
	return pending
}

func writeToolchainPendingForTest(t *testing.T, profile string, pending ToolchainPending) {
	t.Helper()
	data, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	if err := privatefile.WriteAtomic(filepath.Join(profile, ".teamkit", "toolchain.pending.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func hashSelectedFiles(t *testing.T, root string, files map[string]string) map[string]string {
	t.Helper()
	hashes := make(map[string]string, len(files))
	for relative := range files {
		hashes[filepath.ToSlash(filepath.Join("skills", relative))] = fileSHA256(t, filepath.Join(root, "skills", filepath.FromSlash(relative)))
	}
	return hashes
}

func assertSelectedFileHashes(t *testing.T, root string, want map[string]string) {
	t.Helper()
	for relative, expected := range want {
		if got := fileSHA256(t, filepath.Join(root, filepath.FromSlash(relative))); got != expected {
			t.Fatalf("hash %s = %s want %s", relative, got, expected)
		}
	}
}

func assertSelectedFileHashesAfterSkillsSwap(t *testing.T, profile string, want map[string]string) {
	t.Helper()
	skillsRoot := filepath.Join(profile, "skills.swapped")
	if _, err := os.Lstat(skillsRoot); os.IsNotExist(err) {
		skillsRoot = filepath.Join(profile, "skills")
	} else if err != nil {
		t.Fatal(err)
	}
	for relative, expected := range want {
		relocated := strings.TrimPrefix(filepath.ToSlash(relative), "skills/")
		if got := fileSHA256(t, filepath.Join(skillsRoot, filepath.FromSlash(relocated))); got != expected {
			t.Fatalf("hash %s = %s want %s", relative, got, expected)
		}
	}
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func TestMaterializeToolchain_CC1CSkillsIgnoresUnrelatedRootServiceFile(t *testing.T) {
	pin, err := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, filepath.Join(".claude", "skills"), pin.Commit, map[string]string{
		".gitignore":            "*.local\n",
		"cf-edit/SKILL.md":      "# cf-edit\n",
		"cf-edit/reference.txt": "trusted reference\n",
		"db-run/SKILL.md":       "# db-run\n",
	})
	for _, directory := range []string{filepath.Join(profile, "external"), filepath.Join(profile, "skills")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatalf("MaterializeToolchain() error = %v", err)
	}
	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatalf("idempotent MaterializeToolchain() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(profile, "skills", ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("root service file was installed: %v", err)
	}
	lock, err := readToolchainLock(filepath.Join(profile, "external", string(pin.ID)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range lock.Files {
		if file.Path == ".gitignore" {
			t.Fatalf("root service file is present in lock: %#v", lock.Files)
		}
	}
	if err := os.WriteFile(filepath.Join(profile, "skills", ".gitignore"), []byte("*.local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready, err := ToolchainInstalled(profile, pin)
	if err != nil {
		t.Fatalf("ToolchainInstalled() = %v, %v", ready, err)
	}
	if !ready {
		t.Fatal("unrelated root service file made owned toolchain unready")
	}
	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatalf("idempotent materialization with unrelated root file: %v", err)
	}
}

func TestMaterializeToolchain_RejectsSymlinkedRootServiceFile(t *testing.T) {
	pin, err := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	source, profile, external := testutil.TempDir(t), testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, filepath.Join(".claude", "skills"), pin.Commit, map[string]string{
		"cf-edit/SKILL.md": "# cf-edit\n",
	})
	if err := os.WriteFile(filepath.Join(external, "service-file"), []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(external, "service-file"), filepath.Join(source, ".claude", "skills", ".gitignore")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := MaterializeToolchain(source, profile, pin); !errors.Is(err, ErrToolchainLayout) {
		t.Fatalf("MaterializeToolchain() error = %v, want ErrToolchainLayout", err)
	}
}

func TestToolchainInstalled_DetectsInstalledContentTampering(t *testing.T) {
	pin, err := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, filepath.Join(".claude", "skills"), pin.Commit, map[string]string{
		"alpha/SKILL.md":      "# alpha\n",
		"alpha/reference.txt": "trusted reference\n",
	})
	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatal(err)
	}
	ready, err := ToolchainInstalled(profile, pin)
	if err != nil || !ready {
		t.Fatalf("initial ToolchainInstalled() = %v, %v", ready, err)
	}
	if err := os.WriteFile(filepath.Join(profile, "skills", "alpha", "reference.txt"), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready, err = ToolchainInstalled(profile, pin)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("tampered installed skill content was reported ready")
	}
}

func TestToolchainInstalled_DetectsUnmanifestedInstalledFile(t *testing.T) {
	pin, err := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, filepath.Join(".claude", "skills"), pin.Commit, map[string]string{
		"alpha/SKILL.md": "# alpha\n",
	})
	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "skills", "alpha", "injected.txt"), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready, err := ToolchainInstalled(profile, pin)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("unmanifested installed skill file was reported ready")
	}
}

func TestToolchainInstalled_DetectsCryptographicLockTampering(t *testing.T) {
	pin, err := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, filepath.Join(".claude", "skills"), pin.Commit, map[string]string{
		"alpha/SKILL.md": "# alpha\n",
	})
	if err := MaterializeToolchain(source, profile, pin); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(profile, "external", string(pin.ID)+".json")
	lock, err := readToolchainLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock.TreeSHA256 = strings.Repeat("0", sha256.Size*2)
	if err := writeToolchainLock(lockPath, lock); err != nil {
		t.Fatal(err)
	}
	ready, err := ToolchainInstalled(profile, pin)
	if !errors.Is(err, ErrToolchainLayout) || ready {
		t.Fatalf("tampered cryptographic tree binding = %v, %v; want false, ErrToolchainLayout", ready, err)
	}
}

func writeToolchainSourceFixture(t *testing.T, source, subpath, commit string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(source, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".git", "HEAD"), []byte(commit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for relative, contents := range files {
		path := filepath.Join(source, subpath, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMaterializeToolchain_RejectsNonemptyOrWrongCommit(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	if err := os.MkdirAll(filepath.Join(source, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".git", "HEAD"), []byte(strings.Repeat("0", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MaterializeToolchain(source, profile, pin); !errors.Is(err, ErrToolchainPin) {
		t.Fatalf("wrong commit error = %v", err)
	}
}

func TestMaterializeToolchain_RejectsSymlinkedSkillLayout(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainAIRules1C)
	source, profile, external := testutil.TempDir(t), testutil.TempDir(t), testutil.TempDir(t)
	if err := os.MkdirAll(filepath.Join(source, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".git", "HEAD"), []byte(pin.Commit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "content"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(source, "content", "skills")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := MaterializeToolchain(source, profile, pin); !errors.Is(err, ErrToolchainLayout) {
		t.Fatalf("MaterializeToolchain error = %v", err)
	}
}

func TestMaterializeToolchain_RejectsSymlinkedSourceAncestor(t *testing.T) {
	pin, _ := catalog.LookupToolchain(domain.ToolchainAIRules1C)
	external := testutil.TempDir(t)
	repository := filepath.Join(external, "repository")
	for _, directory := range []string{filepath.Join(repository, ".git"), filepath.Join(repository, "content", "skills", "fixture")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repository, ".git", "HEAD"), []byte(pin.Commit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "content", "skills", "fixture", "SKILL.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := testutil.TempDir(t)
	link := filepath.Join(root, "source")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := MaterializeToolchain(filepath.Join(link, "repository"), testutil.TempDir(t), pin); !errors.Is(err, ErrToolchainLayout) {
		t.Fatalf("MaterializeToolchain() error = %v, want ErrToolchainLayout", err)
	}
}

type recordingHermesRunner struct {
	calls  [][]string
	output []byte
}

func (r *recordingHermesRunner) Run(_ context.Context, name string, args []string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil
}

func (r *recordingHermesRunner) Capture(_ context.Context, name string, args []string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return append([]byte(nil), r.output...), nil
}

func TestProfileCLI_UsesPinnedNoninteractiveCreateAndDoctorArguments(t *testing.T) {
	runner := &recordingHermesRunner{output: []byte("\x1b[32m  All checks passed! 🎉\x1b[0m\n")}
	client := ProfileCLI{Executable: "hermes", Runner: runner}
	if err := client.Create(context.Background(), "1c-aisuz-developer-cc_1c_skills"); err != nil {
		t.Fatal(err)
	}
	if err := client.OptInBundledSkills(context.Background(), "1c-aisuz-developer-cc_1c_skills"); err != nil {
		t.Fatal(err)
	}
	if err := client.Doctor(context.Background(), "1c-aisuz-developer-cc_1c_skills"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"hermes", "profile", "create", "1c-aisuz-developer-cc_1c_skills", "--no-alias"},
		{"hermes", "-p", "1c-aisuz-developer-cc_1c_skills", "skills", "opt-in", "--sync"},
		{"hermes", "-p", "1c-aisuz-developer-cc_1c_skills", "doctor"},
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("calls = %#v", runner.calls)
	}
	for i := range want {
		if strings.Join(runner.calls[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Fatalf("call %d = %#v, want %#v", i, runner.calls[i], want[i])
		}
	}
}

func TestProfileCLI_DoctorRejectsPinnedIssueSummaryDespiteExitZero(t *testing.T) {
	runner := &recordingHermesRunner{output: []byte("\x1b[33m  Found 2 issue(s) to address:\x1b[0m\n")}
	client := ProfileCLI{Executable: "hermes", Runner: runner}

	err := client.Doctor(context.Background(), "1c-aisuz-developer-cc_1c_skills")
	if !errors.Is(err, ErrProfileDoctor) {
		t.Fatalf("Doctor() error = %v, want ErrProfileDoctor", err)
	}
}

func TestProfileCLI_DoctorRejectsIssueSummaryEvenWithSuccessFooter(t *testing.T) {
	runner := &recordingHermesRunner{output: []byte("  Found 1 issue(s) to address:\n  All checks passed! 🎉\n")}
	client := ProfileCLI{Executable: "hermes", Runner: runner}

	err := client.Doctor(context.Background(), "1c-aisuz-developer-cc_1c_skills")
	if !errors.Is(err, ErrProfileDoctor) {
		t.Fatalf("Doctor() error = %v, want ErrProfileDoctor", err)
	}
}

func TestProfileCLI_DoctorFailsClosedForUnrecognizedExitZeroOutput(t *testing.T) {
	runner := &recordingHermesRunner{output: []byte("Hermes doctor output contract changed\n")}
	client := ProfileCLI{Executable: "hermes", Runner: runner}

	err := client.Doctor(context.Background(), "1c-aisuz-developer-cc_1c_skills")
	if !errors.Is(err, ErrProfileDoctor) {
		t.Fatalf("Doctor() error = %v, want ErrProfileDoctor", err)
	}
}

func TestCertificates_ExtractsOnlyBelowHermesHomeAndSetsApplicationLocalCA(t *testing.T) {
	home := testutil.TempDir(t)
	archive := zipFixtures(t, map[string]string{"company-ca.pem": "certificate-data", "ca-bundle.pem": "bundle-data"})

	certificate, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home)
	if err != nil {
		t.Fatalf("ExtractCertificates() error = %v", err)
	}
	wantPath := filepath.Join(home, "certs", "ca-bundle.pem")
	if certificate != wantPath {
		t.Fatalf("certificate path = %q, want %q", certificate, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("certificate was not extracted: %v", err)
	}
	if got := ApplicationCAEnvironment(certificate); !equalStringMap(got, map[string]string{
		"HERMES_CA_BUNDLE":    wantPath,
		"SSL_CERT_FILE":       wantPath,
		"REQUESTS_CA_BUNDLE":  wantPath,
		"CURL_CA_BUNDLE":      wantPath,
		"GIT_SSL_CAINFO":      wantPath,
		"NODE_EXTRA_CA_CERTS": wantPath,
	}) {
		t.Fatalf("ApplicationCAEnvironment() = %#v", got)
	}
}

func TestCertificates_RequiresCABundleEntry(t *testing.T) {
	archive := zipFixture(t, "company-ca.pem", "certificate-data")
	_, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), testutil.TempDir(t))
	if !errors.Is(err, ErrCABundleMissing) {
		t.Fatalf("ExtractCertificates() error = %v, want ErrCABundleMissing", err)
	}
}

func TestCertificates_RejectsTraversalArchiveEntry(t *testing.T) {
	home := testutil.TempDir(t)
	archive := zipFixture(t, "../outside.pem", "certificate-data")

	_, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home)
	if !errors.Is(err, ErrArchivePath) {
		t.Fatalf("ExtractCertificates() error = %v, want ErrArchivePath", err)
	}
}

func TestCertificates_RejectsSymlinkedDestinationWithoutTouchingTarget(t *testing.T) {
	home := testutil.TempDir(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(home, "certs")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	archive := zipFixture(t, "ca-bundle.pem", "replacement")

	_, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home)
	if !errors.Is(err, ErrArchivePath) {
		t.Fatalf("ExtractCertificates() error = %v, want ErrArchivePath", err)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(external, "ca-bundle.pem")); !os.IsNotExist(statErr) {
		t.Fatalf("archive escaped through symlink: %v", statErr)
	}
}

func TestCertificates_RejectsSymlinkedLeafWithoutChangingSentinel(t *testing.T) {
	home := testutil.TempDir(t)
	destination := filepath.Join(home, "certs")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(testutil.TempDir(t), "sentinel")
	if err := os.WriteFile(external, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(destination, "ca-bundle.pem")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	archive := zipFixture(t, "ca-bundle.pem", "replacement")

	_, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home)
	if !errors.Is(err, ErrArchivePath) {
		t.Fatalf("ExtractCertificates() error = %v, want ErrArchivePath", err)
	}
	contents, readErr := os.ReadFile(external)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
}

func TestCertificates_InvalidArchiveLeavesNoPublishedDirectory(t *testing.T) {
	home := testutil.TempDir(t)
	archive := zipFixture(t, "company-ca.pem", "certificate-data")

	_, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home)
	if !errors.Is(err, ErrCABundleMissing) {
		t.Fatalf("ExtractCertificates() error = %v, want ErrCABundleMissing", err)
	}
	if _, statErr := os.Lstat(filepath.Join(home, "certs")); !os.IsNotExist(statErr) {
		t.Fatalf("invalid archive published destination: %v", statErr)
	}
}

func TestCertificates_IdenticalArchiveIsIdempotent(t *testing.T) {
	home := testutil.TempDir(t)
	archive := zipFixtures(t, map[string]string{"company-ca.pem": "certificate-data", "ca-bundle.pem": "bundle-data"})
	first, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home)
	if err != nil {
		t.Fatalf("second ExtractCertificates() error = %v", err)
	}
	if first != second {
		t.Fatalf("bundle paths differ: first=%q second=%q", first, second)
	}
}

func TestManagedCertificateBundleReady_BindsBundleToPinnedArchive(t *testing.T) {
	home := testutil.TempDir(t)
	archive := zipFixtures(t, map[string]string{"company-ca.pem": "certificate-data", "ca-bundle.pem": "bundle-data"})
	digest := sha256.Sum256(archive)
	expected := hex.EncodeToString(digest[:])
	if _, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home); err != nil {
		t.Fatal(err)
	}

	ready, err := ManagedCertificateBundleReady(home, expected)
	if err != nil || !ready {
		t.Fatalf("ManagedCertificateBundleReady() = %v, %v", ready, err)
	}
	if err := os.WriteFile(filepath.Join(home, "certs", "ca-bundle.pem"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready, err = ManagedCertificateBundleReady(home, expected)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("tampered certificate bundle was reported ready")
	}
}

func TestManagedCertificateBundleReady_RejectsTamperedManagedArchive(t *testing.T) {
	home := testutil.TempDir(t)
	archive := zipFixture(t, "ca-bundle.pem", "bundle-data")
	digest := sha256.Sum256(archive)
	if _, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home); err != nil {
		t.Fatal(err)
	}
	managedArchive := filepath.Join(home, ".teamkit", "certificates.zip")
	if err := os.WriteFile(managedArchive, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}

	ready, err := ManagedCertificateBundleReady(home, hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("tampered managed certificate archive was reported ready")
	}
}

func TestCertificateEnvironmentReady_RequiresAllSixExactBundleBindings(t *testing.T) {
	home := testutil.TempDir(t)
	bundle := filepath.Join(home, "certs", "ca-bundle.pem")
	if err := os.MkdirAll(filepath.Dir(bundle), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle, []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := ApplicationCAEnvironment(bundle)
	environment[PublicProviderProvider().APIKeyEnvironment] = "provider-test-value"
	var lines []string
	for key, value := range environment {
		lines = append(lines, key+"="+value)
	}
	sort.Strings(lines)
	envPath := filepath.Join(home, ".env")
	if err := privatefile.WriteAtomic(envPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready, err := CertificateEnvironmentReady(envPath, bundle)
	if err != nil || !ready {
		t.Fatalf("CertificateEnvironmentReady() = %v, %v", ready, err)
	}
	if err := os.WriteFile(envPath, []byte(strings.Join(lines[:len(lines)-1], "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready, err = CertificateEnvironmentReady(envPath, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("environment missing a mandatory CA variable was reported ready")
	}
}

func TestCertificateEnvironmentReady_RequiresNonBlankPublicProviderKey(t *testing.T) {
	home := testutil.TempDir(t)
	bundle := filepath.Join(home, "certs", "ca-bundle.pem")
	if err := os.MkdirAll(filepath.Dir(bundle), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle, []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		include bool
		value   string
	}{
		{name: "missing"},
		{name: "empty", include: true},
		{name: "whitespace", include: true, value: "   "},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := ApplicationCAEnvironment(bundle)
			if test.include {
				values[PublicProviderProvider().APIKeyEnvironment] = test.value
			}
			var lines []string
			for key, value := range values {
				lines = append(lines, key+"="+value)
			}
			sort.Strings(lines)
			path := filepath.Join(home, test.name+".env")
			if err := privatefile.WriteAtomic(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			ready, err := CertificateEnvironmentReady(path, bundle)
			if err != nil {
				t.Fatal(err)
			}
			if ready {
				t.Fatal("environment without a nonblank provider key was reported ready")
			}
		})
	}
}

func TestWindowsInstaller_RequiresPinnedHashAndSignerBeforeInstall(t *testing.T) {
	executable := []byte("Hermes setup bytes")
	digest := sha256.Sum256(executable)
	called := false
	installer := WindowsInstaller{
		ExpectedSHA256: hex.EncodeToString(digest[:]),
		ExpectedSigner: "Nous Research Inc.",
		Verifier: SignerVerifierFunc(func(path string) (SignerMetadata, error) {
			return SignerMetadata{Subject: "CN=Nous Research Inc.", Trusted: true}, nil
		}),
		Install: InstallerFunc(func(ctx context.Context, path string) error {
			called = true
			return nil
		}),
	}
	path := filepath.Join(testutil.TempDir(t), "Hermes-Setup.exe")
	if err := os.WriteFile(path, executable, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := installer.Apply(context.Background(), path); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !called {
		t.Fatal("installer was not called after verification")
	}

	installer.ExpectedSHA256 = strings.Repeat("0", 64)
	if err := installer.Apply(context.Background(), path); !errors.Is(err, ErrInstallerChecksum) {
		t.Fatalf("Apply() error = %v, want ErrInstallerChecksum", err)
	}

	installer.ExpectedSHA256 = hex.EncodeToString(digest[:])
	installer.Verifier = SignerVerifierFunc(func(path string) (SignerMetadata, error) {
		return SignerMetadata{Subject: "NousResearch", Trusted: false}, nil
	})
	if err := installer.Apply(context.Background(), path); !errors.Is(err, ErrInstallerSigner) {
		t.Fatalf("Apply() error = %v, want ErrInstallerSigner", err)
	}
}

func TestWindowsInstaller_RejectsUnexpectedTrustedSigner(t *testing.T) {
	contents := []byte("Hermes setup bytes")
	digest := sha256.Sum256(contents)
	path := filepath.Join(testutil.TempDir(t), "Hermes-Setup.exe")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	installer := WindowsInstaller{
		ExpectedSHA256: hex.EncodeToString(digest[:]), ExpectedSigner: "Nous Research Inc.",
		Verifier: SignerVerifierFunc(func(string) (SignerMetadata, error) {
			return SignerMetadata{Subject: "CN=Different Company", Trusted: true}, nil
		}),
		Install: InstallerFunc(func(context.Context, string) error { called = true; return nil }),
	}
	if err := installer.Apply(context.Background(), path); !errors.Is(err, ErrInstallerSigner) {
		t.Fatalf("Apply() error = %v, want ErrInstallerSigner", err)
	}
	if called {
		t.Fatal("installer ran for an unexpected signer")
	}
}

func TestPlatformInstallStatus_ExplainsNonWindowsInstallWithoutClaimingParity(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "altlinux"} {
		t.Run(platform, func(t *testing.T) {
			status := PlatformInstallStatus(platform)
			if status.Code != "HERMES_INSTALL_MANUAL" || !strings.Contains(status.Guidance, "NousResearch") {
				t.Fatalf("PlatformInstallStatus(%q) = %#v", platform, status)
			}
		})
	}
}

func zipFixture(t *testing.T, name, body string) []byte {
	t.Helper()
	return zipFixtures(t, map[string]string{name: body})
}

func zipFixtures(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func equalStringMap(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}
