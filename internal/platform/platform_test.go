package platform

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

func TestDetectOSFamily_MapsRuntimeFamiliesAndALTRelease(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		release string
		want    domain.OSFamily
	}{
		{name: "windows", goos: "windows", want: domain.OSWindows},
		{name: "macos", goos: "darwin", want: domain.OSMacOS},
		{name: "linux", goos: "linux", release: "ID=ubuntu\n", want: domain.OSLinux},
		{name: "alt", goos: "linux", release: "NAME=ALT Linux\nID=altlinux\n", want: domain.OSALTLinux},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectOSFamily(tt.goos, func(path string) ([]byte, error) {
				if path != "/etc/os-release" {
					t.Fatalf("os-release path = %q", path)
				}
				return []byte(tt.release), nil
			})
			if err != nil || got != tt.want {
				t.Fatalf("DetectOSFamily() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestExecutableCandidates_CoversEachCatalogApplicationDeterministically(t *testing.T) {
	seen := make(map[string]bool)
	for _, application := range catalog.AIApplications() {
		candidates, err := ExecutableCandidates(application.ID)
		if err != nil || len(candidates) == 0 {
			t.Fatalf("ExecutableCandidates(%q) = %#v, %v", application.ID, candidates, err)
		}
		for _, candidate := range candidates {
			if candidate == "" || seen[string(application.ID)+":"+candidate] {
				t.Fatalf("invalid or duplicate candidate %q for %q", candidate, application.ID)
			}
			seen[string(application.ID)+":"+candidate] = true
		}
	}
}

func TestDetectInstalled_UsesCandidatesAndDoesNotTreatLookupFailureAsInstalled(t *testing.T) {
	candidates, err := ExecutableCandidates(domain.AppCodex)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := DetectInstalled(domain.AppCodex, func(name string) (string, error) {
		if name == candidates[0] {
			return filepath.Join("tools", name), nil
		}
		return "", exec.ErrNotFound
	})
	if err != nil || !installed {
		t.Fatalf("DetectInstalled() = %v, %v; want true, nil", installed, err)
	}
	installed, err = DetectInstalled(domain.AppCodex, func(string) (string, error) { return "", exec.ErrNotFound })
	if err != nil || installed {
		t.Fatalf("DetectInstalled() = %v, %v; want false, nil", installed, err)
	}
}

func TestDefaultHermesHome_UsesPlatformConventions(t *testing.T) {
	tests := []struct {
		family  domain.OSFamily
		home    string
		appData string
		want    string
	}{
		{domain.OSWindows, "C:\\Users\\dev", "C:\\Users\\dev\\AppData\\Local", filepath.Join("C:\\Users\\dev\\AppData\\Local", "hermes")},
		{domain.OSMacOS, "/Users/dev", "", filepath.Join("/Users/dev", ".hermes")},
		{domain.OSLinux, "/home/dev", "", filepath.Join("/home/dev", ".hermes")},
		{domain.OSALTLinux, "/home/dev", "", filepath.Join("/home/dev", ".hermes")},
	}
	for _, tt := range tests {
		got, err := DefaultHermesHome(tt.family, tt.home, tt.appData)
		if err != nil || got != tt.want {
			t.Fatalf("DefaultHermesHome(%q) = %q, %v; want %q", tt.family, got, err, tt.want)
		}
	}
}

func TestDefaultHermesHome_RejectsMissingRequiredEnvironment(t *testing.T) {
	for _, tt := range []struct {
		family      domain.OSFamily
		home, local string
	}{
		{domain.OSWindows, "", ""}, {domain.OSMacOS, "", ""}, {domain.OSLinux, "", ""}, {domain.OSALTLinux, "", ""},
	} {
		if _, err := DefaultHermesHome(tt.family, tt.home, tt.local); !errors.Is(err, ErrHermesHomeUnavailable) {
			t.Fatalf("DefaultHermesHome(%s) err=%v", tt.family, err)
		}
	}
}

func TestFixedInstallerRunner_ForwardsOnlyConfiguredArgv(t *testing.T) {
	var gotName string
	var gotArgs []string
	runner := FixedInstallerRunner{
		Executable: "Hermes-Setup.exe",
		Arguments:  []string{"/S", "/norestart"},
		RunProcess: ProcessRunnerFunc(func(ctx context.Context, name string, args []string) error {
			gotName = name
			gotArgs = append([]string(nil), args...)
			return nil
		}),
	}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gotName != "Hermes-Setup.exe" || !reflect.DeepEqual(gotArgs, []string{"/S", "/norestart"}) {
		t.Fatalf("installer argv = %q %#v", gotName, gotArgs)
	}
}

func TestDetectInstalled_RejectsUnknownApplication(t *testing.T) {
	_, err := DetectInstalled("unknown", func(string) (string, error) { return "", errors.New("must not run") })
	if err == nil {
		t.Fatal("DetectInstalled() accepted unknown application")
	}
}
