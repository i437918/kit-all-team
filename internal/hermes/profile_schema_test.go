package hermes

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderForSchema_UsesProvenSchemaWithoutChangingManagedState(t *testing.T) {
	profile := Profile{
		Project: "apa", Role: "developer", KitHome: `C:\TeamKit`,
		Toolchain:     Toolchain{Name: "cc_1c_skills", Origin: "schema-secret-value", Version: "pin"},
		V8StdEndpoint: "https://ai.v8std.ru/mcp",
	}
	provider := CustomLLMProvider()
	configs := make(map[int]map[string]any, 2)
	officeCLICommand := filepath.Join(t.TempDir(), officeCLIManagedNameForTest())
	profileWithOfficeCLI, err := profile.WithOfficeCLI(officeCLICommand)
	if err != nil {
		t.Fatalf("WithOfficeCLI() error = %v", err)
	}
	for _, schema := range []int{34, 37} {
		data, err := profile.RenderForSchema(provider, schema)
		if err != nil {
			t.Fatalf("RenderForSchema(schema=%d) error = %v", schema, err)
		}
		if strings.Contains(string(data), "secret-value") {
			t.Fatalf("schema %d config contains a secret value", schema)
		}
		var config map[string]any
		if err := yaml.Unmarshal(data, &config); err != nil {
			t.Fatalf("schema %d config is not YAML: %v", schema, err)
		}
		if config["_config_version"] != schema {
			t.Fatalf("schema field = %#v, want %d", config["_config_version"], schema)
		}
		delete(config, "_config_version")
		configs[schema] = config

		withOfficeCLI, err := profileWithOfficeCLI.RenderForSchema(provider, schema)
		if err != nil {
			t.Fatalf("RenderForSchema(schema=%d) with OfficeCLI error = %v", schema, err)
		}
		var officeCLIConfig map[string]any
		if err := yaml.Unmarshal(withOfficeCLI, &officeCLIConfig); err != nil {
			t.Fatalf("schema %d OfficeCLI config is not YAML: %v", schema, err)
		}
		baseMCPs := config["mcp_servers"].(map[string]any)
		officeCLIMCPs := officeCLIConfig["mcp_servers"].(map[string]any)
		for _, id := range []string{"v8std", "customllm-jira", "customllm-confluence"} {
			if !reflect.DeepEqual(baseMCPs[id], officeCLIMCPs[id]) {
				t.Fatalf("schema %d %s changed after adding OfficeCLI:\nwithout=%#v\nwith=%#v", schema, id, baseMCPs[id], officeCLIMCPs[id])
			}
		}
	}
	if !reflect.DeepEqual(configs[34], configs[37]) {
		t.Fatalf("managed state differs by more than schema:\n34=%#v\n37=%#v", configs[34], configs[37])
	}
}

func TestRenderForSchema_RejectsUnprovenSchema(t *testing.T) {
	profile := Profile{
		Project: "apa", Role: "developer", KitHome: `C:\TeamKit`,
		Toolchain:     Toolchain{Name: "cc_1c_skills", Origin: "https://example.test/skills.git", Version: "pin"},
		V8StdEndpoint: "https://ai.v8std.ru/mcp",
	}
	for _, schema := range []int{33, 38} {
		data, err := profile.RenderForSchema(CustomLLMProvider(), schema)
		if !errors.Is(err, ErrConfigSchemaUnsupported) || data != nil {
			t.Fatalf("RenderForSchema(schema=%d) = %q, %v; want nil ErrConfigSchemaUnsupported", schema, data, err)
		}
	}
}
