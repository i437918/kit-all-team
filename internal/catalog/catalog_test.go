package catalog_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

func TestProjects_ReturnsClosedRepositoryBindings(t *testing.T) {
	want := []catalog.Project{
		{ID: domain.ProjectAISUZ, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-aisuz", DatabaseRepository: "https://gitlab.example.invalid/1c/aisuz/main.git", DatabaseBranch: "develop"},
		{ID: domain.ProjectAPA, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-apa", DatabaseRepository: "https://gitlab.example.invalid/1c/apa/main.git", DatabaseBranch: "develop"},
		{ID: domain.ProjectASBNU, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-asbnu", DatabaseRepository: "https://gitlab.example.invalid/1c/asbnu/asbnu3.git", DatabaseBranch: "develop"},
		{ID: domain.ProjectASKU, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-asku", DatabaseRepository: "https://gitlab.example.invalid/1c/asku/main.git", DatabaseBranch: "develop"},
		{ID: domain.ProjectEASR, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-easr", DatabaseRepository: "https://gitlab.example.invalid/1c/easr/main.git", DatabaseBranch: "develop"},
		{ID: domain.ProjectEISKO, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-eisko", DatabaseRepository: "https://gitlab.example.invalid/1c/eisko/eisko1.git", DatabaseBranch: "develop"},
		{ID: domain.ProjectESED, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-esed", DatabaseRepository: "https://gitlab.example.invalid/1c/esed/main.git", DatabaseBranch: "develop"},
		{ID: domain.ProjectUAT, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-uat", DatabaseRepository: "https://gitlab.example.invalid/1c/uat/main.git", DatabaseBranch: "develop"},
		{ID: domain.ProjectUNIP, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-unip", DatabaseRepository: "https://gitlab.example.invalid/1c/unip/main.git", DatabaseBranch: "develop"},
		{ID: domain.ProjectZUP, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-zup", DatabaseRepository: "https://gitlab.example.invalid/1c/zup/zup3.git", DatabaseBranch: "develop"},
		{ID: domain.ProjectWMS, ContentRepository: "https://gitlab.example.invalid/1c/aisuz/ai.git", ContentBranch: "content-wms", DatabaseRepository: "https://gitlab.example.invalid/1c/fulfillment/wms.git", DatabaseBranch: "develop"},
	}
	if got := catalog.Projects(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Projects() = %#v, want %#v", got, want)
	}
	for _, project := range want {
		got, err := catalog.LookupProject(project.ID)
		if err != nil || got != project {
			t.Fatalf("LookupProject(%q) = %#v, %v; want %#v", project.ID, got, err, project)
		}
	}
}

func TestCatalog_ReturnsClosedSelectionsAndPins(t *testing.T) {
	if got, want := catalog.Roles(), []catalog.Role{
		{ID: domain.RoleAnalyst, Label: "1C Analyst"},
		{ID: domain.RoleDeveloper, Label: "1C Developer"},
		{ID: domain.RoleArchitect, Label: "1C Architect"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Roles() = %#v, want %#v", got, want)
	}
	if got, want := catalog.Toolchains(), []catalog.Toolchain{
		{ID: domain.ToolchainCC1CSkills, Origin: "https://github.com/Nikolay-Shirokov/cc-1c-skills.git", Commit: "e01688e764a3cf1c1b4a0ad5069ea885837cfb2e"},
		{ID: domain.ToolchainAIRules1C, Origin: "https://github.com/comol/ai_rules_1c.git", Commit: "f33d2405207cf325f893dc8ca2789157d887db81"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Toolchains() = %#v, want %#v", got, want)
	}
	if got, want := catalog.OperatingSystems(), []catalog.OperatingSystem{
		{ID: domain.OSFamily("windows"), Label: "Windows"},
		{ID: domain.OSFamily("macos"), Label: "macOS"},
		{ID: domain.OSFamily("linux"), Label: "Linux"},
		{ID: domain.OSFamily("altlinux"), Label: "ALT Linux"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("OperatingSystems() = %#v, want %#v", got, want)
	}
	if got, want := catalog.AIApplications(), []catalog.AIApplication{
		{ID: domain.AIApplication("hermes"), Label: "Hermes"},
		{ID: domain.AIApplication("cursor"), Label: "Cursor"},
		{ID: domain.AIApplication("claude-code"), Label: "Claude Code"},
		{ID: domain.AIApplication("codex"), Label: "Codex"},
		{ID: domain.AIApplication("opencode"), Label: "OpenCode"},
		{ID: domain.AIApplication("kilo-code"), Label: "Kilo Code"},
		{ID: domain.AIApplication("kimi"), Label: "Kimi"},
		{ID: domain.AIApplication("qwen"), Label: "Qwen"},
		{ID: domain.AIApplication("command-code"), Label: "Command Code"},
		{ID: domain.AIApplication("cline"), Label: "Cline"},
		{ID: domain.AIApplication("pi"), Label: "Pi"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AIApplications() = %#v, want %#v", got, want)
	}
}

func TestCatalog_ReturnsExactProviderAndMCPValues(t *testing.T) {
	if got, want := catalog.DefaultProvider(), (catalog.Provider{
		ID: "customllm", Name: "CustomLLM", BaseURL: "https://llm.example.invalid/v1",
		Model: "generic-development", APIMode: "chat_completions", APIKeyEnvironment: "HERMES_CUSTOM_LLM_API_KEY",
	}); got != want {
		t.Fatalf("DefaultProvider() = %#v, want %#v", got, want)
	}
	if got, want := catalog.V8StdMCP(), (catalog.MCP{
		ID: "v8std", Endpoint: "https://ai.v8std.ru/mcp",
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("V8StdMCP() = %#v, want %#v", got, want)
	}
}

func TestAtlassianMCPs_ExactCorporateContractAndDefensiveHeaders(t *testing.T) {
	got := catalog.AtlassianMCPs()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	want := []catalog.MCP{
		{ID: "customllm-jira", Endpoint: "https://llm.example.invalid/jira/mcp", Headers: map[string]string{"x-litellm-api-key": "Bearer ${HERMES_CUSTOM_LLM_API_KEY}", "x-mcp-jira-authorization": "Token ${HERMES_CUSTOM_ISSUE_TRACKER_TOKEN}"}, ConnectTimeout: 60, Timeout: 120},
		{ID: "customllm-confluence", Endpoint: "https://llm.example.invalid/confluence/mcp", Headers: map[string]string{"x-litellm-api-key": "Bearer ${HERMES_CUSTOM_LLM_API_KEY}", "x-mcp-confluence-authorization": "Token ${HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN}"}, ConnectTimeout: 60, Timeout: 120},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MCPs = %#v, want %#v", got, want)
	}
	got[0].Headers["x-litellm-api-key"] = "mutated"
	if catalog.AtlassianMCPs()[0].Headers["x-litellm-api-key"] != want[0].Headers["x-litellm-api-key"] {
		t.Fatal("catalog headers share caller state")
	}
}

func TestLookupProject_RejectsUnknownIdentifierWithStableCode(t *testing.T) {
	_, err := catalog.LookupProject("missing")
	var validationErr *domain.ValidationError
	if !errors.As(err, &validationErr) || validationErr.Code != domain.ProjectUnknown {
		t.Fatalf("error = %v, want code %q", err, domain.ProjectUnknown)
	}
}

func TestCatalogLookups_ResolveEveryClosedSelector(t *testing.T) {
	for _, want := range catalog.Roles() {
		if got, err := catalog.LookupRole(want.ID); err != nil || got != want {
			t.Fatalf("LookupRole(%q) = %#v, %v; want %#v", want.ID, got, err, want)
		}
	}
	for _, want := range catalog.Toolchains() {
		if got, err := catalog.LookupToolchain(want.ID); err != nil || got != want {
			t.Fatalf("LookupToolchain(%q) = %#v, %v; want %#v", want.ID, got, err, want)
		}
	}
	for _, want := range catalog.OperatingSystems() {
		if got, err := catalog.LookupOperatingSystem(want.ID); err != nil || got != want {
			t.Fatalf("LookupOperatingSystem(%q) = %#v, %v; want %#v", want.ID, got, err, want)
		}
	}
	for _, want := range catalog.AIApplications() {
		if got, err := catalog.LookupAIApplication(want.ID); err != nil || got != want {
			t.Fatalf("LookupAIApplication(%q) = %#v, %v; want %#v", want.ID, got, err, want)
		}
	}
}

func TestCatalogLookups_RejectUnknownSelectorsWithStableCodes(t *testing.T) {
	tests := []struct {
		name   string
		lookup func() error
		code   domain.ErrorCode
	}{
		{"role", func() error { _, err := catalog.LookupRole("missing"); return err }, domain.RoleUnknown},
		{"toolchain", func() error { _, err := catalog.LookupToolchain("missing"); return err }, domain.ToolchainUnknown},
		{"os", func() error { _, err := catalog.LookupOperatingSystem("missing"); return err }, domain.OSUnknown},
		{"application", func() error { _, err := catalog.LookupAIApplication("missing"); return err }, domain.ApplicationUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var validationErr *domain.ValidationError
			if err := tt.lookup(); !errors.As(err, &validationErr) || validationErr.Code != tt.code {
				t.Fatalf("error = %v, want code %q", err, tt.code)
			}
		})
	}
}

func TestProjects_ReturnsDefensiveCopy(t *testing.T) {
	first := catalog.Projects()
	first[0].ContentBranch = "tampered"
	if got := catalog.Projects()[0].ContentBranch; got != "content-aisuz" {
		t.Fatalf("catalog mutated through returned slice: %q", got)
	}
}

func TestLookupOfficeCLIAsset_PlatformMatrix(t *testing.T) {
	tests := []struct {
		name         string
		family       domain.OSFamily
		architecture string
		want         catalog.OfficeCLIAsset
	}{
		{
			name:         "windows amd64",
			family:       domain.OSWindows,
			architecture: "amd64",
			want: catalog.OfficeCLIAsset{
				Version: "1.0.144", Commit: "1ced45e900782c5083ed550ddf328ee974e425e7", OS: domain.OSWindows, Architecture: "amd64",
				FileName: "officecli-win-x64.exe", URL: "https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-win-x64.exe", Size: 33382312,
				SHA256: "e780cc6a5385f84b4d54d71b0c179904ed534125ec33fe39b1a8711fa80e387e",
			},
		},
		{
			name:         "linux amd64",
			family:       domain.OSLinux,
			architecture: "amd64",
			want: catalog.OfficeCLIAsset{
				Version: "1.0.144", Commit: "1ced45e900782c5083ed550ddf328ee974e425e7", OS: domain.OSLinux, Architecture: "amd64",
				FileName: "officecli-linux-x64", URL: "https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-linux-x64", Size: 35316133,
				SHA256: "32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8",
			},
		},
		{
			name:         "alt linux amd64",
			family:       domain.OSALTLinux,
			architecture: "amd64",
			want: catalog.OfficeCLIAsset{
				Version: "1.0.144", Commit: "1ced45e900782c5083ed550ddf328ee974e425e7", OS: domain.OSLinux, Architecture: "amd64",
				FileName: "officecli-linux-x64", URL: "https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-linux-x64", Size: 35316133,
				SHA256: "32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8",
			},
		},
		{
			name:         "macos amd64",
			family:       domain.OSMacOS,
			architecture: "amd64",
			want: catalog.OfficeCLIAsset{
				Version: "1.0.144", Commit: "1ced45e900782c5083ed550ddf328ee974e425e7", OS: domain.OSMacOS, Architecture: "amd64",
				FileName: "officecli-mac-x64", URL: "https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-mac-x64", Size: 34705536,
				SHA256: "366100643d757b0da24829422897ca74768a894b5ecd1a471a1336f8e2a0787d",
			},
		},
		{
			name:         "macos arm64",
			family:       domain.OSMacOS,
			architecture: "arm64",
			want: catalog.OfficeCLIAsset{
				Version: "1.0.144", Commit: "1ced45e900782c5083ed550ddf328ee974e425e7", OS: domain.OSMacOS, Architecture: "arm64",
				FileName: "officecli-mac-arm64", URL: "https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-mac-arm64", Size: 33760816,
				SHA256: "04757163428c5bde8d91e8f838517818e74722157722ca5f3877b6716b77bd45",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := catalog.LookupOfficeCLIAsset(tt.family, tt.architecture)
			if err != nil || got != tt.want {
				t.Fatalf("LookupOfficeCLIAsset(%q, %q) = %#v, %v; want %#v", tt.family, tt.architecture, got, err, tt.want)
			}
		})
	}
}

func TestLookupOfficeCLIAsset_RejectsUnsupportedPlatform(t *testing.T) {
	tests := []struct {
		name         string
		family       domain.OSFamily
		architecture string
	}{
		{name: "windows arm64", family: domain.OSWindows, architecture: "arm64"},
		{name: "linux arm64", family: domain.OSLinux, architecture: "arm64"},
		{name: "alt linux arm64", family: domain.OSALTLinux, architecture: "arm64"},
		{name: "macos 386", family: domain.OSMacOS, architecture: "386"},
		{name: "unknown os", family: domain.OSFamily("unknown"), architecture: "amd64"},
		{name: "empty architecture", family: domain.OSWindows, architecture: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := catalog.LookupOfficeCLIAsset(tt.family, tt.architecture)
			if !errors.Is(err, catalog.ErrOfficeCLIPlatformUnsupported) {
				t.Fatalf("LookupOfficeCLIAsset(%q, %q) error = %v, want ErrOfficeCLIPlatformUnsupported", tt.family, tt.architecture, err)
			}
		})
	}
}
