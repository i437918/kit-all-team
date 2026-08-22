package hermes

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"gopkg.in/yaml.v3"
)

var errInvalidProfile = errors.New("invalid Hermes profile")

// HermesConfigVersion is the configuration schema used by the pinned Hermes
// source revision.
const HermesConfigVersion = 34

// HermesSourceCommit is the exact Hermes revision the installer must select.
const HermesSourceCommit = "f80f453ae0679347e38abc917c7f94f717bf96c5"

// HermesSourceOrigin is the exact upstream repository for managed checkouts.
const HermesSourceOrigin = "https://github.com/NousResearch/hermes-agent.git"

// Toolchain identifies the sole pinned toolchain for a Hermes profile.
type Toolchain struct {
	Name    string `json:"name"`
	Origin  string `json:"origin"`
	Version string `json:"version"`
}

// Profile contains the non-secret inputs to one Hermes identity.
type Profile struct {
	Project          string
	Role             string
	KitHome          string
	Toolchain        Toolchain
	V8StdEndpoint    string
	OfficeCLICommand string
}

type renderedProfile struct {
	ConfigVersion int                     `yaml:"_config_version"`
	Model         renderedModel           `yaml:"model"`
	Providers     map[string]providerYAML `yaml:"providers"`
	MCPServers    map[string]mcpYAML      `yaml:"mcp_servers"`
	Terminal      terminalYAML            `yaml:"terminal"`
}

type renderedModel struct {
	Default  string `yaml:"default"`
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	APIMode  string `yaml:"api_mode"`
}

type providerYAML struct {
	Name         string `yaml:"name"`
	API          string `yaml:"api"`
	KeyEnv       string `yaml:"key_env"`
	DefaultModel string `yaml:"default_model"`
	Transport    string `yaml:"transport"`
}

type mcpYAML struct {
	URL                       string            `yaml:"url,omitempty"`
	Command                   string            `yaml:"command,omitempty"`
	Args                      []string          `yaml:"args,omitempty"`
	Env                       map[string]string `yaml:"env,omitempty"`
	Enabled                   bool              `yaml:"enabled"`
	Headers                   map[string]string `yaml:"headers,omitempty"`
	Sampling                  *samplingYAML     `yaml:"sampling,omitempty"`
	SupportsParallelToolCalls *bool             `yaml:"supports_parallel_tool_calls,omitempty"`
	ConnectTimeout            int               `yaml:"connect_timeout,omitempty"`
	Timeout                   int               `yaml:"timeout,omitempty"`
}

type samplingYAML struct {
	Enabled bool `yaml:"enabled"`
}

type terminalYAML struct {
	Backend string `yaml:"backend"`
	CWD     string `yaml:"cwd"`
}

// ProfileFromDesired resolves the selected, catalog-pinned toolchain while
// keeping v8std as an independent MCP configuration.
func ProfileFromDesired(desired domain.DesiredState) (Profile, error) {
	toolchain, err := catalog.LookupToolchain(desired.Toolchain())
	if err != nil {
		return Profile{}, err
	}
	return Profile{
		Project:       string(desired.Project()),
		Role:          string(desired.Role()),
		KitHome:       desired.KitHome(),
		Toolchain:     Toolchain{Name: string(toolchain.ID), Origin: toolchain.Origin, Version: toolchain.Commit},
		V8StdEndpoint: catalog.V8StdMCP().Endpoint,
	}, nil
}

// Identity returns the stable Hermes profile identity.
func (p Profile) Identity() string {
	return fmt.Sprintf("1c-%s-%s-%s", p.Project, p.Role, p.Toolchain.Name)
}

// WithOfficeCLI returns a copy of p configured to launch OfficeCLI as a
// local stdio MCP. The command must be an absolute, already-selected path.
func (p Profile) WithOfficeCLI(command string) (Profile, error) {
	if !filepath.IsAbs(command) {
		return p, errInvalidProfile
	}
	p.OfficeCLICommand = command
	return p, nil
}

// Render produces versionable, non-secret configuration for this profile using
// the pinned managed-runtime schema. New callers must use RenderForSchema with
// the schema proven from the exact runtime executable.
func (p Profile) Render(provider Provider) ([]byte, error) {
	return p.RenderForSchema(provider, HermesConfigVersion)
}

// RenderForSchema produces configuration for an audited schema proven from the
// exact Hermes runtime. Version strings are deliberately not used as a proxy.
func (p Profile) RenderForSchema(provider Provider, schema int) ([]byte, error) {
	if schema != 34 && schema != 37 {
		return nil, ErrConfigSchemaUnsupported
	}
	if p.Project == "" || p.Role == "" || p.KitHome == "" || p.Toolchain.Name == "" || p.Toolchain.Version == "" || p.V8StdEndpoint == "" || p.OfficeCLICommand != "" && !filepath.IsAbs(p.OfficeCLICommand) {
		return nil, errInvalidProfile
	}
	if provider.ID == "" || provider.Name == "" || provider.Endpoint == "" || provider.Model == "" || provider.APIMode == "" || provider.APIKeyEnvironment == "" {
		return nil, errInvalidProfile
	}
	mcpServers := map[string]mcpYAML{"v8std": {URL: p.V8StdEndpoint, Enabled: true}}
	for _, mcp := range catalog.AtlassianMCPs() {
		headers := make(map[string]string, len(mcp.Headers))
		for key, value := range mcp.Headers {
			headers[key] = value
		}
		disabled := false
		mcpServers[mcp.ID] = mcpYAML{
			URL:                       mcp.Endpoint,
			Enabled:                   true,
			Headers:                   headers,
			Sampling:                  &samplingYAML{Enabled: false},
			SupportsParallelToolCalls: &disabled,
			ConnectTimeout:            mcp.ConnectTimeout,
			Timeout:                   mcp.Timeout,
		}
	}
	if p.OfficeCLICommand != "" {
		mcpServers["officecli"] = mcpYAML{Command: p.OfficeCLICommand, Args: []string{"mcp"}, Enabled: true}
	}
	return yaml.Marshal(renderedProfile{
		ConfigVersion: schema,
		Model: renderedModel{
			Default: provider.Model, Provider: "custom:" + provider.ID,
			BaseURL: provider.Endpoint, APIKey: "${" + provider.APIKeyEnvironment + "}", APIMode: provider.APIMode,
		},
		Providers: map[string]providerYAML{provider.ID: {
			Name: provider.Name, API: provider.Endpoint, KeyEnv: provider.APIKeyEnvironment,
			DefaultModel: provider.Model, Transport: provider.APIMode,
		}},
		MCPServers: mcpServers,
		Terminal:   terminalYAML{Backend: "local", CWD: p.KitHome},
	})
}
