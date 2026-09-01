package hermes

import (
	"bytes"
	"context"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func runtimeOutput(path, version string) []byte {
	return []byte("Hermes Agent v" + version + " (2026.8.13)\n" +
		"Install directory: " + filepath.Dir(filepath.Dir(filepath.Dir(path))) + "\n" +
		"Python: 3.11.15\n" +
		"OpenAI SDK: 2.24.0\n" +
		"Run 'hermes version' for update status.\n")
}

func TestParseRuntimeInfo_AcceptsSupportedRange(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "install", "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(testutil.TempDir(t), "install", "venv", "Scripts", "hermes.exe")
	}
	for _, version := range []string{"0.20.1", "0.20.2", "0.20.99"} {
		t.Run(version, func(t *testing.T) {
			got, err := ParseRuntimeInfo(path, runtimeOutput(path, version))
			if err != nil || got.Version != version || got.Executable != path {
				t.Fatalf("ParseRuntimeInfo() = %#v, %v", got, err)
			}
		})
	}
}

func TestParseRuntimeInfo_AcceptsGitBuildMetadataAndUpdateAvailability(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "install", "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(testutil.TempDir(t), "install", "venv", "Scripts", "hermes.exe")
	}
	installRoot := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	output := []byte("Hermes Agent v0.20.5 (2026.8.19) · upstream 6ed8bcee · local 706f33d4 (+1 carried commit)\n" +
		"Install directory: " + installRoot + "\n" +
		"Install method: git\n" +
		"Python: 3.11.15\n" +
		"OpenAI SDK: 2.24.0\n" +
		"Update available: 233 commits behind — run 'hermes update'\n")

	got, err := ParseRuntimeInfo(path, output)
	if err != nil || got.Version != "0.20.5" || got.Executable != path || got.InstallDir != installRoot {
		t.Fatalf("ParseRuntimeInfo() = %#v, %v", got, err)
	}
}

func TestParseRuntimeInfo_RejectsUnsupportedVersions(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "install", "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(testutil.TempDir(t), "install", "venv", "Scripts", "hermes.exe")
	}
	for _, version := range []string{"0.20.0", "0.21.0", "1.0.0"} {
		t.Run(version, func(t *testing.T) {
			_, err := ParseRuntimeInfo(path, runtimeOutput(path, version))
			if !errors.Is(err, ErrVersionUnsupported) {
				t.Fatalf("error=%v, want HERMES_VERSION_UNSUPPORTED", err)
			}
		})
	}
}

func TestVerifyExecutable_DefersProfileCapabilitiesToRuntimeContract(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "install", "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(testutil.TempDir(t), "install", "venv", "Scripts", "hermes.exe")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("exe"), 0o700); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	info, err := VerifyExecutable(context.Background(), path, func(_ context.Context, _ string, args []string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		switch strings.Join(args, " ") {
		case "--version":
			return runtimeOutput(path, "0.20.2"), nil
		default:
			return nil, errors.New("runtime capabilities belong to VerifyRuntimeContract")
		}
	})
	if err != nil || info.Version != "0.20.2" {
		t.Fatalf("VerifyExecutable() = %#v, %v", info, err)
	}
	want := [][]string{{"--version"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%#v, want %#v", calls, want)
	}
}

func TestVerifyExecutable_AllSupportedVersionsSkipCapabilityProbes(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "install", "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(testutil.TempDir(t), "install", "venv", "Scripts", "hermes.exe")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("exe"), 0o700); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err := VerifyExecutable(context.Background(), path, func(_ context.Context, _ string, args []string) ([]byte, error) {
		calls++
		return runtimeOutput(path, "0.20.2"), nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestVerifyExecutable_AcceptsRuntimeBeforeCapabilityContract(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "install", "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(testutil.TempDir(t), "install", "venv", "Scripts", "hermes.exe")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("exe"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := VerifyExecutable(context.Background(), path, func(_ context.Context, _ string, args []string) ([]byte, error) {
		if len(args) == 1 {
			return runtimeOutput(path, "0.20.2"), nil
		}
		return nil, errors.New("runtime capabilities belong to VerifyRuntimeContract")
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
}

func TestParseRuntimeInfo_RejectsControlBytes(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "install", "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(testutil.TempDir(t), "install", "venv", "Scripts", "hermes.exe")
	}
	for name, control := range map[string]string{"C0": "\x01", "DEL": "\x7f", "unicode": "\u0085"} {
		t.Run(name, func(t *testing.T) {
			output := runtimeOutput(path, "0.20.1")
			marker := bytes.Index(output, []byte("2026"))
			output = append(append(append([]byte(nil), output[:marker]...), []byte(control)...), output[marker+1:]...)
			if _, err := ParseRuntimeInfo(path, output); !errors.Is(err, ErrExecutableUnverified) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestBoundedBuffer_StopsRetainingAfterLimit(t *testing.T) {
	buffer := &boundedBuffer{limit: 8}
	payload := bytes.Repeat([]byte("x"), 1024)
	n, err := buffer.Write(payload)
	if err != nil || n != len(payload) || !buffer.overflow {
		t.Fatalf("n=%d err=%v overflow=%v", n, err, buffer.overflow)
	}
	if got := len(buffer.Bytes()); got != 9 {
		t.Fatalf("retained=%d want 9", got)
	}
}

func TestResolveCompatibleExecutable_ReturnsVerifiedAbsolutePath(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "hermes")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, []byte("executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveCompatibleExecutable(
		context.Background(),
		func(string) (string, error) { return path, nil },
		func(_ context.Context, name string, args []string) ([]byte, error) {
			if name != path || len(args) != 1 || args[0] != "--version" {
				t.Fatalf("version command = %q %#v", name, args)
			}
			return []byte("Hermes Agent v0.20.1 (2026.8.13)\n" +
				"Install directory: " + filepath.Dir(filepath.Dir(filepath.Dir(path))) + "\n" +
				"Install method: git\n" +
				"Python: 3.11.15\n" +
				"OpenAI SDK: 2.24.0\n" +
				"Run 'hermes version' for update status.\n"), nil
		},
	)
	if err != nil || got != path || !filepath.IsAbs(got) {
		t.Fatalf("ResolveCompatibleExecutable() = %q, %v", got, err)
	}
}

func TestResolveCompatibleExecutable_RejectsIncompleteOrMisleadingVersionOutput(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "install", "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(testutil.TempDir(t), "install", "venv", "Scripts", "hermes.exe")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	installRoot := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	tests := map[string]string{
		"single version line":  "Hermes Agent v0.20.1 (2026.8.13)\n",
		"duplicate version":    "Hermes Agent v0.20.1 (2026.8.13)\nInstall directory: " + installRoot + "\nPython: 3.11.15\nOpenAI SDK: 2.24.0\nHermes Agent v0.20.1 (2026.8.13)\nRun 'hermes version' for update status.\n",
		"foreign install root": "Hermes Agent v0.20.1 (2026.8.13)\nInstall directory: " + testutil.TempDir(t) + "\nPython: 3.11.15\nOpenAI SDK: 2.24.0\nRun 'hermes version' for update status.\n",
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveCompatibleExecutable(context.Background(), func(string) (string, error) { return path, nil }, func(context.Context, string, []string) ([]byte, error) {
				return []byte(output), nil
			})
			if !errors.Is(err, ErrExecutableUnverified) {
				t.Fatalf("ResolveCompatibleExecutable() error = %v, want ErrExecutableUnverified", err)
			}
		})
	}
}

func TestCompatibleVersionOutput_AcceptsOutputWithoutInstallMethodStamp(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "install", "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		path = filepath.Join(testutil.TempDir(t), "install", "venv", "Scripts", "hermes.exe")
	}
	installRoot := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	output := []byte("Hermes Agent v0.20.1 (2026.8.13)\r\n" +
		"Install directory: " + installRoot + "\r\n" +
		"Python: 3.11.15\r\n" +
		"OpenAI SDK: Not installed\r\n" +
		"Run 'hermes version' for update status.\r\n")
	if !compatibleVersionOutput(path, output) {
		t.Fatal("valid pinned multiline output without an install-method stamp was rejected")
	}
}

func TestResolveCompatibleExecutable_FailsClosedForIncompatiblePATHShadow(t *testing.T) {
	shadow := filepath.Join(testutil.TempDir(t), "hermes")
	if runtime.GOOS == "windows" {
		shadow += ".exe"
	}
	if err := os.WriteFile(shadow, []byte("shadow"), 0o700); err != nil {
		t.Fatal(err)
	}
	captures := 0
	_, err := ResolveCompatibleExecutable(
		context.Background(),
		func(string) (string, error) { return shadow, nil },
		func(context.Context, string, []string) ([]byte, error) {
			captures++
			return []byte("Hermes Agent v99.0.0 (2099.1.1)\n"), nil
		},
	)
	if !errors.Is(err, ErrExecutableUnverified) {
		t.Fatalf("ResolveCompatibleExecutable() error = %v, want ErrExecutableUnverified", err)
	}
	if captures != 1 {
		t.Fatalf("version captures = %d, want only first PATH candidate", captures)
	}
}

func TestResolveCompatibleExecutable_RejectsRedirectedExecutable(t *testing.T) {
	external := filepath.Join(testutil.TempDir(t), "external-hermes")
	if err := os.WriteFile(external, []byte("external"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(testutil.TempDir(t), "hermes")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := ResolveCompatibleExecutable(
		context.Background(),
		func(string) (string, error) { return link, nil },
		func(context.Context, string, []string) ([]byte, error) {
			t.Fatal("unsafe executable was started")
			return nil, nil
		},
	)
	if !errors.Is(err, ErrExecutableUnverified) {
		t.Fatalf("ResolveCompatibleExecutable() error = %v, want ErrExecutableUnverified", err)
	}
}
