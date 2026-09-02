package registry

import (
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"
)

// LocationOptions supplies platform information without performing filesystem
// I/O, which keeps default location selection deterministic and testable.
type LocationOptions struct {
	GOOS        string
	Getenv      func(string) string
	UserHomeDir func() (string, error)
}

func isAbsoluteForOS(goos, value string) bool {
	if value == "" {
		return false
	}
	if goos != "windows" {
		return strings.HasPrefix(value, "/")
	}
	normalized := strings.ReplaceAll(value, "/", `\`)
	if len(normalized) >= 3 && ((normalized[0] >= 'A' && normalized[0] <= 'Z') || (normalized[0] >= 'a' && normalized[0] <= 'z')) && normalized[1] == ':' && normalized[2] == '\\' {
		return true
	}
	if !strings.HasPrefix(normalized, `\\`) {
		return false
	}
	fields := strings.FieldsFunc(strings.TrimPrefix(normalized, `\\`), func(r rune) bool { return r == '\\' })
	return len(fields) >= 2 && fields[0] != "" && fields[1] != ""
}

func joinForOS(goos string, parts ...string) string {
	if goos != "windows" {
		return path.Join(parts...)
	}
	if len(parts) == 0 {
		return ""
	}
	result := strings.TrimRight(strings.ReplaceAll(parts[0], "/", `\`), `\`)
	for _, part := range parts[1:] {
		part = strings.Trim(strings.ReplaceAll(part, "/", `\`), `\`)
		if part != "" {
			result += `\` + part
		}
	}
	return result
}

// DefaultPath returns the registry path for the requested platform.
func DefaultPath(options LocationOptions) (string, error) {
	switch options.GOOS {
	case "windows":
		base := options.Getenv("LOCALAPPDATA")
		if !isAbsoluteForOS("windows", base) {
			return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE")
		}
		return joinForOS("windows", base, "TeamKit", "environments.json"), nil
	case "darwin":
		home, err := options.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE: %w", err)
		}
		if !isAbsoluteForOS("darwin", home) {
			return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE")
		}
		return joinForOS("darwin", home, "Library", "Application Support", "TeamKit", "environments.json"), nil
	case "linux":
		base := options.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := options.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE: %w", err)
			}
			if !isAbsoluteForOS("linux", home) {
				return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE")
			}
			base = joinForOS("linux", home, ".config")
		}
		if !isAbsoluteForOS("linux", base) {
			return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE")
		}
		return joinForOS("linux", base, "teamkit", "environments.json"), nil
	default:
		return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE: unsupported GOOS %q", options.GOOS)
	}
}

func defaultLocationOptions() LocationOptions {
	return LocationOptions{GOOS: runtime.GOOS, Getenv: os.Getenv, UserHomeDir: os.UserHomeDir}
}
