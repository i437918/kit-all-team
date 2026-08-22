package hermes

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/platform"
)

var (
	ErrHomeAutoDetect = errors.New("HERMES_HOME_AUTO_DETECT_FAILED")
	ErrHomeOverlap    = errors.New("HOME_PATH_OVERLAP")
)

type DiscoveryRequest struct {
	OS                domain.OSFamily
	ExplicitHome      string
	InstalledOverride *bool
	KitHome           string
}

type DiscoveryResult struct {
	Installed  bool
	Home       string
	Executable string
	Version    string
	Contract   RuntimeContract
}

type DiscoveryDependencies struct {
	Getenv      func(string) string
	UserHomeDir func() (string, error)
	LookPath    func(string) (string, error)
	Lstat       func(string) (os.FileInfo, error)
	Abs         func(string) (string, error)
	Capture     executableCapture
}

func Discover(ctx context.Context, request DiscoveryRequest, dependencies DiscoveryDependencies) (DiscoveryResult, error) {
	deps := discoveryDependencies(dependencies)
	home, authoritative, err := selectHermesHome(request, deps)
	if err != nil {
		return DiscoveryResult{}, err
	}
	if authoritative && request.KitHome != "" {
		kitHome, absErr := deps.Abs(request.KitHome)
		if absErr != nil {
			return DiscoveryResult{}, fmt.Errorf("%w: KIT_ALL_TEAM_HOME is invalid", ErrHomeAutoDetect)
		}
		overlap, overlapErr := pathsafe.Overlaps(filepath.Clean(kitHome), home)
		if overlapErr != nil {
			return DiscoveryResult{}, fmt.Errorf("%w: %v", ErrHomeAutoDetect, overlapErr)
		}
		if overlap {
			return DiscoveryResult{}, fmt.Errorf("%w: KIT_ALL_TEAM_HOME and HERMES_HOME must be separate trees", ErrHomeOverlap)
		}
	}
	if request.InstalledOverride != nil && !*request.InstalledOverride {
		if err := requireSafeInstallTarget(home, deps.Lstat); err != nil {
			return DiscoveryResult{}, err
		}
		if err := validateHomeAndOverlap(home, request.KitHome, deps); err != nil {
			return DiscoveryResult{}, err
		}
		return DiscoveryResult{Home: home}, nil
	}

	for _, candidate := range exactCandidates(request.OS, home) {
		_, statErr := deps.Lstat(candidate)
		if statErr == nil {
			contract, verifyErr := verifyDiscoveryExecutable(ctx, candidate, deps.Capture)
			if verifyErr != nil {
				return DiscoveryResult{}, verifyErr
			}
			info := contract.Info
			derived, standard := standardHomeFromRuntime(request.OS, info)
			if !standard || !samePath(home, derived, request.OS) {
				return DiscoveryResult{}, homeError("exact runtime does not belong to the selected HERMES_HOME")
			}
			if err := requireExistingHome(home, deps.Lstat); err != nil {
				return DiscoveryResult{}, err
			}
			if err := validateHomeAndOverlap(home, request.KitHome, deps); err != nil {
				return DiscoveryResult{}, err
			}
			return DiscoveryResult{Installed: true, Home: home, Executable: info.Executable, Version: info.Version, Contract: contract}, nil
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return DiscoveryResult{}, fmt.Errorf("%w: exact executable cannot be inspected", ErrExecutableUnverified)
		}
	}

	path, lookupErr := deps.LookPath("hermes")
	if lookupErr == nil {
		abs, absErr := deps.Abs(path)
		if absErr != nil {
			return DiscoveryResult{}, fmt.Errorf("%w: PATH executable is invalid", ErrExecutableUnverified)
		}
		contract, verifyErr := verifyDiscoveryExecutable(ctx, filepath.Clean(abs), deps.Capture)
		if verifyErr != nil {
			return DiscoveryResult{}, verifyErr
		}
		info := contract.Info
		derived, standard := standardHomeFromRuntime(request.OS, info)
		if authoritative {
			if !standard || !samePath(home, derived, request.OS) {
				return DiscoveryResult{}, homeError("PATH runtime does not belong to the selected HERMES_HOME")
			}
		} else if standard {
			home = derived
			if err := validateHomeAndOverlap(home, request.KitHome, deps); err != nil {
				return DiscoveryResult{}, err
			}
		} else {
			return DiscoveryResult{}, homeError("PATH runtime does not use a supported Hermes layout")
		}
		if err := requireExistingHome(home, deps.Lstat); err != nil {
			return DiscoveryResult{}, err
		}
		return DiscoveryResult{Installed: true, Home: home, Executable: info.Executable, Version: info.Version, Contract: contract}, nil
	}
	if !errors.Is(lookupErr, exec.ErrNotFound) && lookupErr != nil {
		return DiscoveryResult{}, fmt.Errorf("%w: PATH lookup failed", ErrExecutableUnverified)
	}
	if request.InstalledOverride != nil && *request.InstalledOverride {
		return DiscoveryResult{}, fmt.Errorf("%w: Hermes executable was not found", ErrExecutableUnverified)
	}
	if err := validateHomeAndOverlap(home, request.KitHome, deps); err != nil {
		return DiscoveryResult{}, err
	}
	if err := requireSafeInstallTarget(home, deps.Lstat); err != nil {
		return DiscoveryResult{}, err
	}
	return DiscoveryResult{Home: home}, nil
}

func discoveryDependencies(deps DiscoveryDependencies) DiscoveryDependencies {
	if deps.Getenv == nil {
		deps.Getenv = os.Getenv
	}
	if deps.UserHomeDir == nil {
		deps.UserHomeDir = os.UserHomeDir
	}
	if deps.LookPath == nil {
		deps.LookPath = exec.LookPath
	}
	if deps.Lstat == nil {
		deps.Lstat = os.Lstat
	}
	if deps.Abs == nil {
		deps.Abs = filepath.Abs
	}
	if deps.Capture == nil {
		deps.Capture = func(ctx context.Context, name string, args []string) ([]byte, error) {
			return captureCommandBounded(ctx, name, args, maxVersionOutputBytes)
		}
	}
	return deps
}

func selectHermesHome(request DiscoveryRequest, deps DiscoveryDependencies) (string, bool, error) {
	source := strings.TrimSpace(request.ExplicitHome)
	authoritative := source != ""
	if source == "" {
		source = strings.TrimSpace(deps.Getenv("HERMES_HOME"))
		authoritative = source != ""
	}
	if source == "" {
		userHome, err := deps.UserHomeDir()
		if err != nil {
			return "", false, homeError("user home is unavailable")
		}
		local := ""
		if request.OS == domain.OSWindows {
			local = strings.TrimSpace(deps.Getenv("LOCALAPPDATA"))
		}
		defaultHome, err := platform.DefaultHermesHome(request.OS, userHome, local)
		if err != nil {
			return "", false, homeError("platform default is unavailable")
		}
		source = defaultHome
	}
	if !filepath.IsAbs(source) {
		return "", false, homeError("HERMES_HOME must be absolute")
	}
	abs, err := deps.Abs(source)
	if err != nil {
		return "", false, homeError("HERMES_HOME is invalid")
	}
	home := filepath.Clean(abs)
	if err := pathsafe.ValidateDirectory(home); err != nil {
		return "", false, homeError("HERMES_HOME is unsafe")
	}
	return home, authoritative, nil
}

func standardHomeFromRuntime(family domain.OSFamily, info RuntimeInfo) (string, bool) {
	install := filepath.Clean(info.InstallDir)
	executable := filepath.Clean(info.Executable)
	expected := filepath.Join(install, "venv", "bin", "hermes")
	if family == domain.OSWindows {
		expected = filepath.Join(install, "venv", "Scripts", "hermes.exe")
	}
	if !samePath(executable, expected, family) {
		return "", false
	}
	if baseEqual(filepath.Base(install), "hermes-agent", family) {
		return filepath.Dir(install), true
	}
	if baseEqual(filepath.Base(install), "hermes-agent-source", family) && baseEqual(filepath.Base(filepath.Dir(install)), ".teamkit", family) {
		return filepath.Dir(filepath.Dir(install)), true
	}
	return "", false
}

func baseEqual(left, right string, family domain.OSFamily) bool {
	if family == domain.OSWindows {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func samePath(left, right string, family domain.OSFamily) bool {
	if family == domain.OSWindows {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func validateHomeAndOverlap(home, kit string, deps DiscoveryDependencies) error {
	if !filepath.IsAbs(home) || pathsafe.ValidateDirectory(home) != nil {
		return homeError("derived HERMES_HOME is unsafe")
	}
	if kit == "" {
		return nil
	}
	abs, err := deps.Abs(kit)
	if err != nil {
		return homeError("KIT_ALL_TEAM_HOME is invalid")
	}
	overlap, err := pathsafe.Overlaps(filepath.Clean(abs), home)
	if err != nil {
		return homeError("home overlap cannot be checked")
	}
	if overlap {
		return fmt.Errorf("%w: KIT_ALL_TEAM_HOME and HERMES_HOME must be separate trees", ErrHomeOverlap)
	}
	return nil
}

func exactCandidates(family domain.OSFamily, home string) []string {
	if family == domain.OSWindows {
		return []string{filepath.Join(home, "hermes-agent", "venv", "Scripts", "hermes.exe")}
	}
	return []string{
		filepath.Join(home, "hermes-agent", "venv", "bin", "hermes"),
		filepath.Join(home, ".teamkit", "hermes-agent-source", "venv", "bin", "hermes"),
	}
}

func verifyDiscoveryExecutable(ctx context.Context, path string, capture executableCapture) (RuntimeContract, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return VerifyRuntimeContract(probeCtx, path, func(ctx context.Context, name string, args []string) ([]byte, error) {
		data, err := capture(ctx, name, args)
		if len(data) > maxVersionOutputBytes {
			return nil, ErrExecutableUnverified
		}
		return data, err
	})
}

func requireExistingHome(home string, lstat func(string) (os.FileInfo, error)) error {
	info, err := lstat(home)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return homeError("installed Hermes home does not exist or is unsafe")
	}
	return nil
}

func requireSafeInstallTarget(home string, lstat func(string) (os.FileInfo, error)) error {
	for current := home; ; current = filepath.Dir(current) {
		info, err := lstat(current)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return homeError("install target parent is unsafe")
			}
			return nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return homeError("install target cannot be inspected")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return homeError("install target has no existing safe parent")
		}
	}
}

func homeError(reason string) error {
	return fmt.Errorf("%w: %s; use --hermes-home with an absolute verified path", ErrHomeAutoDetect, reason)
}
