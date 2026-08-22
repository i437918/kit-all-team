// Package catalog exposes the closed, offline Team Kit catalog.
package catalog

import (
	"errors"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

// ErrOfficeCLIPlatformUnsupported reports a requested OfficeCLI platform with no qualified asset.
var ErrOfficeCLIPlatformUnsupported = errors.New("OFFICECLI_PLATFORM_UNSUPPORTED")

// Project binds a project identifier to its central content and database sources.
type Project struct {
	ID                 domain.ProjectID
	ContentRepository  string
	ContentBranch      string
	DatabaseRepository string
	DatabaseBranch     string
}

// Role is a selectable role and its display label.
type Role struct {
	ID    domain.Role
	Label string
}

// Toolchain is a selectable toolchain pinned to one source commit.
type Toolchain struct {
	ID     domain.Toolchain
	Origin string
	Commit string
}

// OfficeCLIAsset identifies one qualified, immutable OfficeCLI release asset.
type OfficeCLIAsset struct {
	Version      string
	Commit       string
	OS           domain.OSFamily
	Architecture string
	FileName     string
	URL          string
	Size         int64
	SHA256       string
}

// OperatingSystem is a selectable OS family and its display label.
type OperatingSystem struct {
	ID    domain.OSFamily
	Label string
}

// AIApplication is a selectable AI application and its display label.
type AIApplication struct {
	ID    domain.AIApplication
	Label string
}

// Provider describes the default OpenAI-compatible LLM provider.
type Provider struct {
	ID                string
	Name              string
	BaseURL           string
	Model             string
	APIMode           string
	APIKeyEnvironment string
}

// MCP describes a configured MCP endpoint.
type MCP struct {
	ID             string
	Endpoint       string
	Headers        map[string]string
	ConnectTimeout int
	Timeout        int
}

var projects = [...]Project{
	{domain.ProjectAISUZ, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-aisuz", "https://gitlab.example.invalid/1c/aisuz/main.git", "develop"},
	{domain.ProjectAPA, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-apa", "https://gitlab.example.invalid/1c/apa/main.git", "develop"},
	{domain.ProjectASBNU, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-asbnu", "https://gitlab.example.invalid/1c/asbnu/asbnu3.git", "develop"},
	{domain.ProjectASKU, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-asku", "https://gitlab.example.invalid/1c/asku/main.git", "develop"},
	{domain.ProjectEASR, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-easr", "https://gitlab.example.invalid/1c/easr/main.git", "develop"},
	{domain.ProjectEISKO, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-eisko", "https://gitlab.example.invalid/1c/eisko/eisko1.git", "develop"},
	{domain.ProjectESED, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-esed", "https://gitlab.example.invalid/1c/esed/main.git", "develop"},
	{domain.ProjectUAT, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-uat", "https://gitlab.example.invalid/1c/uat/main.git", "develop"},
	{domain.ProjectUNIP, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-unip", "https://gitlab.example.invalid/1c/unip/main.git", "develop"},
	{domain.ProjectZUP, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-zup", "https://gitlab.example.invalid/1c/zup/zup3.git", "develop"},
	{domain.ProjectWMS, "https://gitlab.example.invalid/1c/aisuz/ai.git", "content-wms", "https://gitlab.example.invalid/1c/fulfillment/wms.git", "develop"},
}

var roles = [...]Role{
	{domain.RoleAnalyst, "1C Analyst"},
	{domain.RoleDeveloper, "1C Developer"},
	{domain.RoleArchitect, "1C Architect"},
}

var toolchains = [...]Toolchain{
	{domain.ToolchainCC1CSkills, "https://github.com/Nikolay-Shirokov/cc-1c-skills.git", "e01688e764a3cf1c1b4a0ad5069ea885837cfb2e"},
	{domain.ToolchainAIRules1C, "https://github.com/comol/ai_rules_1c.git", "f33d2405207cf325f893dc8ca2789157d887db81"},
}

var officeCLIAssets = [...]OfficeCLIAsset{
	{Version: "1.0.144", Commit: "1ced45e900782c5083ed550ddf328ee974e425e7", OS: domain.OSWindows, Architecture: "amd64", FileName: "officecli-win-x64.exe", URL: "https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-win-x64.exe", Size: 33382312, SHA256: "e780cc6a5385f84b4d54d71b0c179904ed534125ec33fe39b1a8711fa80e387e"},
	{Version: "1.0.144", Commit: "1ced45e900782c5083ed550ddf328ee974e425e7", OS: domain.OSLinux, Architecture: "amd64", FileName: "officecli-linux-x64", URL: "https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-linux-x64", Size: 35316133, SHA256: "32ef7a21a54a4ca6c9806bf5e9f3d32bfb1291017329c55044cb2aac71822eb8"},
	{Version: "1.0.144", Commit: "1ced45e900782c5083ed550ddf328ee974e425e7", OS: domain.OSMacOS, Architecture: "amd64", FileName: "officecli-mac-x64", URL: "https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-mac-x64", Size: 34705536, SHA256: "366100643d757b0da24829422897ca74768a894b5ecd1a471a1336f8e2a0787d"},
	{Version: "1.0.144", Commit: "1ced45e900782c5083ed550ddf328ee974e425e7", OS: domain.OSMacOS, Architecture: "arm64", FileName: "officecli-mac-arm64", URL: "https://github.com/iOfficeAI/OfficeCLI/releases/download/v1.0.144/officecli-mac-arm64", Size: 33760816, SHA256: "04757163428c5bde8d91e8f838517818e74722157722ca5f3877b6716b77bd45"},
}

var operatingSystems = [...]OperatingSystem{
	{domain.OSWindows, "Windows"},
	{domain.OSMacOS, "macOS"},
	{domain.OSLinux, "Linux"},
	{domain.OSALTLinux, "ALT Linux"},
}

var applications = [...]AIApplication{
	{domain.AppHermes, "Hermes"},
	{domain.AppCursor, "Cursor"},
	{domain.AppClaudeCode, "Claude Code"},
	{domain.AppCodex, "Codex"},
	{domain.AppOpenCode, "OpenCode"},
	{domain.AppKiloCode, "Kilo Code"},
	{domain.AppKimi, "Kimi"},
	{domain.AppQwen, "Qwen"},
	{domain.AppCommandCode, "Command Code"},
	{domain.AppCline, "Cline"},
	{domain.AppPi, "Pi"},
}

var atlassianMCPs = [...]MCP{
	{ID: "customllm-jira", Endpoint: "https://llm.example.invalid/jira/mcp", Headers: map[string]string{"x-litellm-api-key": "Bearer ${HERMES_CUSTOM_LLM_API_KEY}", "x-mcp-jira-authorization": "Token ${HERMES_CUSTOM_ISSUE_TRACKER_TOKEN}"}, ConnectTimeout: 60, Timeout: 120},
	{ID: "customllm-confluence", Endpoint: "https://llm.example.invalid/confluence/mcp", Headers: map[string]string{"x-litellm-api-key": "Bearer ${HERMES_CUSTOM_LLM_API_KEY}", "x-mcp-confluence-authorization": "Token ${HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN}"}, ConnectTimeout: 60, Timeout: 120},
}

// Projects returns a defensive copy in stable display and planning order.
func Projects() []Project { return append([]Project(nil), projects[:]...) }

// LookupProject returns the binding for id without performing network access.
func LookupProject(id domain.ProjectID) (Project, error) {
	for _, project := range projects {
		if project.ID == id {
			return project, nil
		}
	}
	return Project{}, domain.NewValidationError(domain.ProjectUnknown, "project", string(id))
}

// Roles returns a defensive copy in stable questionnaire order.
func Roles() []Role { return append([]Role(nil), roles[:]...) }

// LookupRole returns the closed role entry for id.
func LookupRole(id domain.Role) (Role, error) {
	for _, role := range roles {
		if role.ID == id {
			return role, nil
		}
	}
	return Role{}, domain.NewValidationError(domain.RoleUnknown, "role", string(id))
}

// Toolchains returns a defensive copy in stable questionnaire order.
func Toolchains() []Toolchain { return append([]Toolchain(nil), toolchains[:]...) }

// LookupToolchain returns the closed pinned toolchain entry for id.
func LookupToolchain(id domain.Toolchain) (Toolchain, error) {
	for _, toolchain := range toolchains {
		if toolchain.ID == id {
			return toolchain, nil
		}
	}
	return Toolchain{}, domain.NewValidationError(domain.ToolchainUnknown, "toolchain", string(id))
}

// LookupOfficeCLIAsset returns the qualified OfficeCLI asset for a supported platform.
func LookupOfficeCLIAsset(family domain.OSFamily, architecture string) (OfficeCLIAsset, error) {
	if family == domain.OSALTLinux {
		family = domain.OSLinux
	}
	for _, asset := range officeCLIAssets {
		if asset.OS == family && asset.Architecture == architecture {
			return asset, nil
		}
	}
	return OfficeCLIAsset{}, ErrOfficeCLIPlatformUnsupported
}

// OperatingSystems returns a defensive copy in stable questionnaire order.
func OperatingSystems() []OperatingSystem {
	return append([]OperatingSystem(nil), operatingSystems[:]...)
}

// LookupOperatingSystem returns the closed operating-system entry for id.
func LookupOperatingSystem(id domain.OSFamily) (OperatingSystem, error) {
	for _, operatingSystem := range operatingSystems {
		if operatingSystem.ID == id {
			return operatingSystem, nil
		}
	}
	return OperatingSystem{}, domain.NewValidationError(domain.OSUnknown, "os", string(id))
}

// AIApplications returns a defensive copy in stable questionnaire order.
func AIApplications() []AIApplication {
	return append([]AIApplication(nil), applications[:]...)
}

// LookupAIApplication returns the closed AI application entry for id.
func LookupAIApplication(id domain.AIApplication) (AIApplication, error) {
	for _, application := range applications {
		if application.ID == id {
			return application, nil
		}
	}
	return AIApplication{}, domain.NewValidationError(domain.ApplicationUnknown, "application", string(id))
}

// DefaultProvider returns the pinned CustomLLM provider configuration.
func DefaultProvider() Provider {
	return Provider{
		ID:                "customllm",
		Name:              "CustomLLM",
		BaseURL:           "https://llm.example.invalid/v1",
		Model:             "generic-development",
		APIMode:           "chat_completions",
		APIKeyEnvironment: "HERMES_CUSTOM_LLM_API_KEY",
	}
}

// V8StdMCP returns the pinned independent v8std MCP declaration.
func V8StdMCP() MCP {
	return MCP{
		ID:       "v8std",
		Endpoint: "https://ai.v8std.ru/mcp",
	}
}

// AtlassianMCPs returns the closed corporate Atlassian MCP declarations.
func AtlassianMCPs() []MCP {
	mcpServers := make([]MCP, len(atlassianMCPs))
	for i, server := range atlassianMCPs {
		mcpServers[i] = server
		mcpServers[i].Headers = make(map[string]string, len(server.Headers))
		for key, value := range server.Headers {
			mcpServers[i].Headers[key] = value
		}
	}
	return mcpServers
}
