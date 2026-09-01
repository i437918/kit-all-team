package hermes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func nativeOS() domain.OSFamily {
	if runtime.GOOS == "windows" {
		return domain.OSWindows
	}
	if runtime.GOOS == "darwin" {
		return domain.OSMacOS
	}
	return domain.OSLinux
}

func discoveryExecutable(t *testing.T, home string) string {
	return discoveryExecutableWithSchema(t, home, 34)
}

func discoveryExecutableWithSchema(t *testing.T, home string, schema int) string {
	t.Helper()
	path := filepath.Join(home, "hermes-agent", "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(home, "hermes-agent", "venv", "Scripts", "hermes.exe")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("exe"), 0o700); err != nil {
		t.Fatal(err)
	}
	install := filepath.Join(home, "hermes-agent")
	if err := os.MkdirAll(filepath.Join(install, "hermes_cli"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "hermes_cli", "config_defaults.py"), []byte(fmt.Sprintf("DEFAULT_CONFIG = {\n    \"_config_version\": %d,\n}\n", schema)), 0o600); err != nil {
		t.Fatal(err)
	}
	writeBundledSkill(t, install, "github", "github")
	return path
}

func TestDiscover_ExactStandardExecutableAcceptsSchema39WithoutLaunchingHermes(t *testing.T) {
	home := testutil.TempDir(t)
	path := discoveryExecutableWithSchema(t, home, 39)
	result, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home}, DiscoveryDependencies{
		LookPath: func(string) (string, error) { t.Fatal("PATH lookup used"); return "", nil },
		Capture:  func(context.Context, string, []string) ([]byte, error) { t.Fatal("Hermes launched"); return nil, nil },
	})
	if err != nil || !result.Installed || result.Executable != path || result.Contract.ConfigSchema != 39 || result.Version != "" {
		t.Fatalf("Discover()=%#v,%v", result, err)
	}
}

func TestDiscover_WindowsRequestUsesWindowsRuntimeLayout(t *testing.T) {
	home := testutil.TempDir(t)
	path := filepath.Join(home, "hermes-agent", "venv", "Scripts", "hermes.exe")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("exe"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(home, "hermes-agent", "hermes_cli", "config_defaults.py")
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("DEFAULT_CONFIG = {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Discover(context.Background(), DiscoveryRequest{OS: domain.OSWindows, ExplicitHome: home}, DiscoveryDependencies{})
	if !errors.Is(err, ErrConfigSchemaUnsupported) || errors.Is(err, ErrExecutableUnverified) {
		t.Fatalf("err=%v, want HERMES_CONFIG_SCHEMA_UNSUPPORTED without HERMES_EXECUTABLE_UNVERIFIED", err)
	}
}

func TestDiscover_DoesNotUsePathExecutableOutsideSelectedHermesHome(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "selected")
	other := testutil.TempDir(t)
	path := discoveryExecutableWithSchema(t, other, 39)
	yes := true
	_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home, InstalledOverride: &yes}, DiscoveryDependencies{
		LookPath: func(string) (string, error) { t.Fatal("PATH lookup used"); return path, nil },
		Capture:  func(context.Context, string, []string) ([]byte, error) { t.Fatal("Hermes launched"); return nil, nil },
	})
	if !errors.Is(err, ErrExecutableNotFound) {
		t.Fatalf("err=%v, want HERMES_EXECUTABLE_NOT_FOUND", err)
	}
}

func discoveryCapture(path, version string) executableCapture {
	return func(_ context.Context, _ string, args []string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			return runtimeOutput(path, version), nil
		}
		switch strings.Join(args, " ") {
		case "profile create --help":
			return []byte("--no-alias"), nil
		case "skills opt-in --help":
			return []byte("--sync"), nil
		}
		return nil, errors.New("unexpected probe")
	}
}

func TestDiscover_ExplicitHomeWinsAndFindsExactExecutable(t *testing.T) {
	home := testutil.TempDir(t)
	path := discoveryExecutable(t, home)
	result, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home}, DiscoveryDependencies{
		Getenv: func(string) string { return "ignored" }, UserHomeDir: func() (string, error) { return testutil.TempDir(t), nil },
		LookPath: func(string) (string, error) { t.Fatal("PATH lookup after exact candidate"); return "", nil }, Capture: discoveryCapture(path, "0.20.2"),
	})
	if err != nil || !result.Installed || result.Home != home || result.Executable != path || result.Version != "" || result.Contract.ConfigSchema != 34 || len(result.Contract.BundledSkills) != 0 {
		t.Fatalf("Discover()=%#v,%v", result, err)
	}
}

func TestDiscover_EnvironmentHomeFindsExactExecutable(t *testing.T) {
	home := testutil.TempDir(t)
	path := discoveryExecutable(t, home)
	result, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS()}, DiscoveryDependencies{Getenv: func(key string) string {
		if key == "HERMES_HOME" {
			return home
		}
		return ""
	}, LookPath: func(string) (string, error) { t.Fatal("PATH used"); return "", nil }, Capture: discoveryCapture(path, "0.20.2")})
	if err != nil || result.Home != home || result.Version != "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDiscover_NoExecutableReturnsSafeInstallTarget(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "new-home")
	result, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home}, DiscoveryDependencies{
		LookPath: func(string) (string, error) { return "", exec.ErrNotFound },
	})
	if err != nil || result.Installed || result.Home != home {
		t.Fatalf("Discover()=%#v,%v", result, err)
	}
}

func TestDiscover_InstalledOverrideRequiresExecutable(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "new-home")
	yes := true
	_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home, InstalledOverride: &yes}, DiscoveryDependencies{LookPath: func(string) (string, error) { return "", exec.ErrNotFound }})
	if !errors.Is(err, ErrExecutableNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestDiscover_InstalledOverrideClassifiesExactExecutableFailures(t *testing.T) {
	yes := true
	t.Run("missing exact executable", func(t *testing.T) {
		home := testutil.TempDir(t)
		_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home, InstalledOverride: &yes}, DiscoveryDependencies{
			LookPath: func(string) (string, error) { t.Fatal("PATH lookup used"); return "", nil },
		})
		if !errors.Is(err, ErrExecutableNotFound) || errors.Is(err, ErrExecutableUnverified) {
			t.Fatalf("err=%v, want only ErrExecutableNotFound", err)
		}
	})

	t.Run("exact executable inspection failure", func(t *testing.T) {
		home := testutil.TempDir(t)
		candidate := exactCandidates(nativeOS(), home)[0]
		_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home, InstalledOverride: &yes}, DiscoveryDependencies{
			Lstat: func(path string) (os.FileInfo, error) {
				if filepath.Clean(path) == filepath.Clean(candidate) {
					return nil, os.ErrPermission
				}
				return os.Lstat(path)
			},
		})
		if !errors.Is(err, ErrExecutableUnverified) || errors.Is(err, ErrExecutableNotFound) {
			t.Fatalf("err=%v, want only ErrExecutableUnverified", err)
		}
	})

	t.Run("unsupported schema", func(t *testing.T) {
		home := testutil.TempDir(t)
		discoveryExecutableWithSchema(t, home, 39)
		config := filepath.Join(home, "hermes-agent", "hermes_cli", "config_defaults.py")
		if err := os.WriteFile(config, []byte("DEFAULT_CONFIG = {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home, InstalledOverride: &yes}, DiscoveryDependencies{})
		if !errors.Is(err, ErrConfigSchemaUnsupported) || errors.Is(err, ErrExecutableNotFound) || errors.Is(err, ErrExecutableUnverified) {
			t.Fatalf("err=%v, want only ErrConfigSchemaUnsupported", err)
		}
	})
}

func TestDiscover_NotInstalledOverrideSkipsRuntime(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "new-home")
	no := false
	result, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home, InstalledOverride: &no}, DiscoveryDependencies{LookPath: func(string) (string, error) { t.Fatal("LookPath called"); return "", nil }, Capture: func(context.Context, string, []string) ([]byte, error) { t.Fatal("capture called"); return nil, nil }})
	if err != nil || result.Installed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDiscover_NotInstalledOverrideStillValidatesExistingParent(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "new-home")
	no := false
	_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home, InstalledOverride: &no}, DiscoveryDependencies{Lstat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }})
	if !errors.Is(err, ErrHomeAutoDetect) {
		t.Fatalf("err=%v", err)
	}
}

func TestDiscover_NotInstalledDefaultStillRejectsOverlap(t *testing.T) {
	user := testutil.TempDir(t)
	local := testutil.TempDir(t)
	home := filepath.Join(user, ".hermes")
	if nativeOS() == domain.OSWindows {
		home = filepath.Join(local, "hermes")
	}
	no := false
	_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), KitHome: filepath.Join(home, "kit"), InstalledOverride: &no}, DiscoveryDependencies{Getenv: func(key string) string {
		if key == "LOCALAPPDATA" {
			return local
		}
		return ""
	}, UserHomeDir: func() (string, error) { return user, nil }})
	if !errors.Is(err, ErrHomeOverlap) {
		t.Fatalf("err=%v", err)
	}
}

func TestDiscover_DefaultExactRuntimeStillRejectsOverlap(t *testing.T) {
	user := testutil.TempDir(t)
	local := testutil.TempDir(t)
	home := filepath.Join(user, ".hermes")
	if nativeOS() == domain.OSWindows {
		home = filepath.Join(local, "hermes")
	}
	path := discoveryExecutable(t, home)
	_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), KitHome: filepath.Join(home, "kit")}, DiscoveryDependencies{Getenv: func(key string) string {
		if key == "LOCALAPPDATA" {
			return local
		}
		return ""
	}, UserHomeDir: func() (string, error) { return user, nil }, Capture: discoveryCapture(path, "0.20.1")})
	if !errors.Is(err, ErrHomeOverlap) {
		t.Fatalf("err=%v", err)
	}
}

func TestDiscover_RejectsRelativeOrOverlappingHome(t *testing.T) {
	_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: "relative"}, DiscoveryDependencies{})
	if !errors.Is(err, ErrHomeAutoDetect) {
		t.Fatalf("relative err=%v", err)
	}
	root := testutil.TempDir(t)
	_, err = Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: filepath.Join(root, "hermes"), KitHome: root}, DiscoveryDependencies{})
	if !errors.Is(err, ErrHomeOverlap) {
		t.Fatalf("overlap err=%v", err)
	}
	_, err = Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: root, KitHome: filepath.Join(root, "kit")}, DiscoveryDependencies{})
	if !errors.Is(err, ErrHomeOverlap) {
		t.Fatalf("nested kit err=%v", err)
	}
}
