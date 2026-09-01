package hermes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
	"gopkg.in/yaml.v3"
)

const maxManagedConfigBytes = 512 << 10

// ErrManagedInvariant reports drift in Team Kit-owned profile state.
var ErrManagedInvariant = errors.New("HERMES_MANAGED_INVARIANT_FAILED")

// ManagedProfileExpectation is the complete release-critical state checked
// before and after Hermes Doctor.
type ManagedProfileExpectation struct {
	Runtime      RuntimeContract
	RuntimeProbe func(context.Context, string) (RuntimeContract, error)
	Owner        ProfileOwnerExpectation
	Config       []byte
	Environment  string
	ToolchainPin catalog.Toolchain
}

// ProfileOwnerExpectation binds one managed identity to its exact profile root
// and published owner record.
type ProfileOwnerExpectation struct {
	Identity    string
	ProfileRoot string
	OwnerPath   string
	OwnerRecord []byte
}

// VerifyManagedProfile validates the owned profile state without changing it.
func VerifyManagedProfile(ctx context.Context, expected ManagedProfileExpectation) error {
	fail := func(reason string) error { return fmt.Errorf("%w: %s", ErrManagedInvariant, reason) }
	if err := ctx.Err(); err != nil {
		return fail("cancelled")
	}
	if expected.Runtime.Info.Executable == "" || expected.Runtime.ConfigSchema <= 0 {
		return fail("runtime expectation")
	}
	owner := expected.Owner
	if !profileNamePattern.MatchString(owner.Identity) || !filepath.IsAbs(owner.ProfileRoot) || !filepath.IsAbs(owner.OwnerPath) || filepath.Base(owner.ProfileRoot) != owner.Identity || len(owner.OwnerRecord) == 0 {
		return fail("owner expectation")
	}
	root, err := openProfileRoot(owner.ProfileRoot)
	if err != nil {
		return fail("profile root")
	}
	defer root.Close()
	ownerRecord, err := pathsafe.ReadRegular(owner.OwnerPath, int64(len(owner.OwnerRecord)))
	if err != nil || !bytes.Equal(ownerRecord, owner.OwnerRecord) {
		return fail("owner record")
	}
	if expected.Environment != filepath.Join(owner.ProfileRoot, ".env") {
		return fail("environment path")
	}
	info, err := os.Lstat(expected.Environment)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || privatefile.Validate(expected.Environment) != nil {
		return fail("secret permissions")
	}
	if len(expected.Config) == 0 || len(expected.Config) > maxManagedConfigBytes || validateManagedConfig(expected.Config, expected.Runtime.ConfigSchema) != nil {
		return fail("expected config")
	}
	actualConfig, err := pathsafe.ReadRegular(filepath.Join(owner.ProfileRoot, "config.yaml"), maxManagedConfigBytes)
	if err != nil || !bytes.Equal(actualConfig, expected.Config) || validateManagedConfig(actualConfig, expected.Runtime.ConfigSchema) != nil {
		return fail("config drift")
	}
	installed, err := ToolchainInstalled(owner.ProfileRoot, expected.ToolchainPin)
	if err != nil || !installed {
		return fail("toolchain drift")
	}
	if err := root.VerifyPath(); err != nil {
		return fail("profile root drift")
	}
	return nil
}

func sameRuntimeContract(left, right RuntimeContract) bool {
	if left.Info != right.Info || left.Identity != right.Identity || left.ConfigSchema != right.ConfigSchema || left.BundledInventorySHA256 != right.BundledInventorySHA256 || len(left.BundledSkills) != len(right.BundledSkills) {
		return false
	}
	for index := range left.BundledSkills {
		if left.BundledSkills[index] != right.BundledSkills[index] {
			return false
		}
	}
	return true
}

func validateManagedConfig(data []byte, schema int) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	mcpFields, err := managedMCPFieldSets(&document)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var config renderedProfile
	if err := decoder.Decode(&config); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("multiple YAML documents")
	}
	provider := PublicProviderProvider()
	pinnedProvider, ok := config.Providers[provider.ID]
	v8std, v8stdOK := config.MCPServers[catalog.V8StdMCP().ID]
	officeCLI, officeCLIOK := config.MCPServers["officecli"]
	atlassianMCPs := catalog.AtlassianMCPs()
	if config.ConfigVersion != schema || len(config.Providers) != 1 || !ok || len(config.MCPServers) != 2+len(atlassianMCPs) || !v8stdOK || !officeCLIOK ||
		config.Model.Default != provider.Model || config.Model.Provider != "custom:"+provider.ID || config.Model.BaseURL != provider.Endpoint || config.Model.APIKey != "${"+provider.APIKeyEnvironment+"}" || config.Model.APIMode != provider.APIMode ||
		pinnedProvider.Name != provider.Name || pinnedProvider.API != provider.Endpoint || pinnedProvider.KeyEnv != provider.APIKeyEnvironment || pinnedProvider.DefaultModel != provider.Model || pinnedProvider.Transport != provider.APIMode ||
		v8std.URL != catalog.V8StdMCP().Endpoint || !v8std.Enabled || v8std.Command != "" || len(v8std.Args) != 0 || len(v8std.Env) != 0 || mcpFieldsContain(mcpFields["v8std"], "command", "args", "env") ||
		officeCLI.URL != "" || !filepath.IsAbs(officeCLI.Command) || len(officeCLI.Args) != 1 || officeCLI.Args[0] != "mcp" || !officeCLI.Enabled || len(officeCLI.Env) != 0 || len(officeCLI.Headers) != 0 || officeCLI.Sampling != nil || officeCLI.SupportsParallelToolCalls != nil || officeCLI.ConnectTimeout != 0 || officeCLI.Timeout != 0 || mcpFieldsContain(mcpFields["officecli"], "url", "env", "headers", "sampling", "supports_parallel_tool_calls", "connect_timeout", "timeout") ||
		config.Terminal.Backend != "local" || !filepath.IsAbs(config.Terminal.CWD) {
		return fmt.Errorf("managed config mismatch")
	}
	for _, expected := range atlassianMCPs {
		actual, ok := config.MCPServers[expected.ID]
		if !ok || actual.URL != expected.Endpoint || !actual.Enabled || actual.Command != "" || len(actual.Args) != 0 || len(actual.Env) != 0 || mcpFieldsContain(mcpFields[expected.ID], "command", "args", "env") || actual.Sampling == nil || actual.Sampling.Enabled || actual.SupportsParallelToolCalls == nil || *actual.SupportsParallelToolCalls || actual.ConnectTimeout != expected.ConnectTimeout || actual.Timeout != expected.Timeout || len(actual.Headers) != len(expected.Headers) {
			return fmt.Errorf("managed config mismatch")
		}
		for key, value := range expected.Headers {
			if actual.Headers[key] != value {
				return fmt.Errorf("managed config mismatch")
			}
		}
	}
	return nil
}

func managedMCPFieldSets(document *yaml.Node) (map[string]map[string]struct{}, error) {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("managed config must be a mapping")
	}
	root := document.Content[0]
	var servers *yaml.Node
	for index := 0; index < len(root.Content); index += 2 {
		if root.Content[index].Value == "mcp_servers" {
			servers = root.Content[index+1]
			break
		}
	}
	if servers == nil || servers.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("managed MCP servers must be a mapping")
	}
	fields := make(map[string]map[string]struct{}, len(servers.Content)/2)
	for index := 0; index < len(servers.Content); index += 2 {
		name, mcp := servers.Content[index].Value, servers.Content[index+1]
		if mcp.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("managed MCP %q must be a mapping", name)
		}
		mcpFields := make(map[string]struct{}, len(mcp.Content)/2)
		for fieldIndex := 0; fieldIndex < len(mcp.Content); fieldIndex += 2 {
			field := mcp.Content[fieldIndex].Value
			if field == "<<" {
				return nil, fmt.Errorf("managed MCP %q contains YAML merge key", name)
			}
			mcpFields[field] = struct{}{}
		}
		fields[name] = mcpFields
	}
	return fields, nil
}

func mcpFieldsContain(fields map[string]struct{}, names ...string) bool {
	for _, name := range names {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}
