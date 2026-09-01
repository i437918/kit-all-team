//go:build !windows

package hermes

import "context"

import "testing"

func simulateInstallRootPathSwap(t *testing.T, root openedInstallRoot, replacement string) {
	t.Helper()
	opened, ok := root.(*otherInstallRoot)
	if !ok {
		t.Fatalf("opened root type = %T", root)
	}
	opened.path = replacement
}

func TestOpenVerifiedInstallRoot_POSIXReadsBoundedRelativeFile(t *testing.T) {
	install, executable := writeRuntimeFixture(t, runtimeConfigSchema37, []string{"github"})
	root, err := openVerifiedInstallRoot(RuntimeInfo{InstallDir: install, Executable: executable, Version: "0.20.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	data, err := root.ReadRegular(context.Background(), "hermes_cli/config_defaults.py", 512<<10)
	if err != nil || string(data) != "DEFAULT_CONFIG = {\n    \"_config_version\": 37,\n}\n" {
		t.Fatalf("ReadRegular()=%q,%v", data, err)
	}
}
