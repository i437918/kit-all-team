package hermes

import (
	"bytes"
	"context"
	"errors"
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
	if err := os.WriteFile(filepath.Join(install, "hermes_cli", "config_defaults.py"), []byte("DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeBundledSkill(t, install, "github", "github")
	return path
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
	if err != nil || !result.Installed || result.Home != home || result.Executable != path || result.Version != "0.20.2" || result.Contract.ConfigSchema != 34 || !result.Contract.HasBundledSkill("github") {
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
	if err != nil || result.Home != home || result.Version != "0.20.2" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDiscover_UsesOnlyFirstPATHCandidate(t *testing.T) {
	home := testutil.TempDir(t)
	path := discoveryExecutable(t, home)
	lookups := 0
	result, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home}, DiscoveryDependencies{
		LookPath: func(name string) (string, error) { lookups++; return path, nil }, Lstat: func(candidate string) (os.FileInfo, error) {
			if candidate == path {
				return nil, os.ErrNotExist
			}
			return os.Lstat(candidate)
		}, Capture: discoveryCapture(path, "0.20.1"),
	})
	if err != nil || !result.Installed || lookups != 1 {
		t.Fatalf("result=%#v err=%v lookups=%d", result, err, lookups)
	}
}

func TestDiscover_RejectsPATHRuntimeOutsideAuthoritativeHome(t *testing.T) {
	home := testutil.TempDir(t)
	other := testutil.TempDir(t)
	path := discoveryExecutable(t, other)
	_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home}, DiscoveryDependencies{LookPath: func(string) (string, error) { return path, nil }, Capture: discoveryCapture(path, "0.20.1")})
	if !errors.Is(err, ErrHomeAutoDetect) {
		t.Fatalf("err=%v", err)
	}
}

func TestDiscover_DerivesOfficialHomeFromPATHWhenNoAuthoritativeSource(t *testing.T) {
	customHome := testutil.TempDir(t)
	path := discoveryExecutable(t, customHome)
	defaultBase := testutil.TempDir(t)
	result, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS()}, DiscoveryDependencies{
		Getenv: func(key string) string {
			if key == "LOCALAPPDATA" {
				return defaultBase
			}
			return ""
		},
		UserHomeDir: func() (string, error) { return testutil.TempDir(t), nil }, LookPath: func(string) (string, error) { return path, nil }, Capture: discoveryCapture(path, "0.20.2"),
	})
	if err != nil || result.Home != customHome || !result.Installed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDiscover_RejectsNonstandardPATHLayoutWithoutAuthoritativeSource(t *testing.T) {
	install := filepath.Join(testutil.TempDir(t), "arbitrary-install")
	path := filepath.Join(install, "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(install, "venv", "Scripts", "hermes.exe")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("exe"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(install, "hermes_cli"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "hermes_cli", "config_defaults.py"), []byte("DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeBundledSkill(t, install, "github", "github")
	_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS()}, DiscoveryDependencies{Getenv: func(string) string { return "" }, UserHomeDir: func() (string, error) { return testutil.TempDir(t), nil }, LookPath: func(string) (string, error) { return path, nil }, Capture: discoveryCapture(path, "0.20.1")})
	if !errors.Is(err, ErrHomeAutoDetect) {
		t.Fatalf("err=%v", err)
	}
}

func TestDiscover_ExactCandidateRejectsUnverifiableReportedInstallRoot(t *testing.T) {
	home := testutil.TempDir(t)
	path := discoveryExecutable(t, home)
	base := discoveryCapture(path, "0.20.1")
	bad := func(ctx context.Context, executable string, args []string) ([]byte, error) {
		data, err := base(ctx, executable, args)
		if err != nil || strings.Join(args, " ") != "--version" {
			return data, err
		}
		install := filepath.Join(home, "hermes-agent")
		return bytes.Replace(data, []byte("Install directory: "+install), []byte("Install directory: "+home), 1), nil
	}
	_, err := Discover(context.Background(), DiscoveryRequest{OS: nativeOS(), ExplicitHome: home}, DiscoveryDependencies{Capture: bad})
	if !errors.Is(err, ErrConfigSchemaUnsupported) {
		t.Fatalf("err=%v", err)
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
	if !errors.Is(err, ErrExecutableUnverified) {
		t.Fatalf("err=%v", err)
	}
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
