package hermes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

const (
	runtimeConfigSchema34 = "DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n}\n"
	runtimeConfigSchema37 = "DEFAULT_CONFIG = {\n    \"_config_version\": 37,\n}\n"
	runtimeConfigSchema38 = "DEFAULT_CONFIG = {\n    \"_config_version\": 38,\n}\n"
)

func TestVerifyRuntimeContract_DoesNotCallCaptureVersionHelpOrInventory(t *testing.T) {
	_, executable := writeRuntimeFixture(t, "DEFAULT_CONFIG = {\n    \"_config_version\": 39,\n}\n", nil)
	contract, err := VerifyRuntimeContract(context.Background(), executable, func(context.Context, string, []string) ([]byte, error) {
		t.Fatal("Hermes launched")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if contract.Info.Executable != filepath.Clean(executable) || contract.ConfigSchema != 39 || contract.Info.Version != "" || contract.Identity != (RuntimeIdentity{}) || len(contract.BundledSkills) != 0 || contract.BundledInventorySHA256 != "" {
		t.Fatalf("contract=%#v", contract)
	}
}

func TestVerifyRuntimeContract_RejectsMissingNonPositiveAndAmbiguousSchema(t *testing.T) {
	for name, config := range map[string]string{
		"missing":   "DEFAULT_CONFIG = {\n    \"default_model\": \"x\",\n}\n",
		"zero":      "DEFAULT_CONFIG = {\n    \"_config_version\": 0,\n}\n",
		"negative":  "DEFAULT_CONFIG = {\n    \"_config_version\": -1,\n}\n",
		"ambiguous": "DEFAULT_CONFIG = {\n    \"_config_version\": 39,\n    \"_config_version\": 40,\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, executable := writeRuntimeFixture(t, config, nil)
			_, err := VerifyRuntimeContract(context.Background(), executable, nil)
			if !errors.Is(err, ErrConfigSchemaUnsupported) {
				t.Fatalf("err=%v, want HERMES_CONFIG_SCHEMA_UNSUPPORTED", err)
			}
		})
	}
}

func writeRuntimeFixture(t *testing.T, config string, skills []string) (string, string) {
	t.Helper()
	root := filepath.Join(testutil.TempDir(t), "install")
	executable := filepath.Join(root, "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		executable = filepath.Join(root, "venv", "Scripts", "hermes.exe")
	}
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("exe"), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "hermes_cli", "config_defaults.py")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, skill := range skills {
		writeBundledSkill(t, root, skill, skill)
	}
	return root, executable
}

func writeBundledSkill(t *testing.T, root, directory, name string) {
	t.Helper()
	path := filepath.Join(root, "skills", directory, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: "+name+"\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
