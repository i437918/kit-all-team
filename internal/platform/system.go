// Package platform provides small, injectable ports for platform-specific effects.
package platform

import (
	"errors"
	"runtime"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

// ErrUnsupportedOS reports a runtime family outside the supported contract.
var ErrUnsupportedOS = errors.New("OS_UNSUPPORTED")

// OSReleaseReader reads the OS release document from the supplied path.
type OSReleaseReader func(path string) ([]byte, error)

// CurrentOSFamily maps the current runtime to a supported OS family.
func CurrentOSFamily(readOSRelease OSReleaseReader) (domain.OSFamily, error) {
	return DetectOSFamily(runtime.GOOS, readOSRelease)
}

// DetectOSFamily maps a Go runtime family, inspecting /etc/os-release only for
// Linux so ALT Linux can be distinguished without executing a command.
func DetectOSFamily(goos string, readOSRelease OSReleaseReader) (domain.OSFamily, error) {
	switch goos {
	case "windows":
		return domain.OSWindows, nil
	case "darwin":
		return domain.OSMacOS, nil
	case "linux":
		if readOSRelease == nil {
			return domain.OSLinux, nil
		}
		contents, err := readOSRelease("/etc/os-release")
		if err == nil && isALTRelease(string(contents)) {
			return domain.OSALTLinux, nil
		}
		return domain.OSLinux, nil
	default:
		return "", ErrUnsupportedOS
	}
}

func isALTRelease(contents string) bool {
	for _, line := range strings.Split(contents, "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"")
		if (key == "ID" || key == "ID_LIKE") && strings.Contains(strings.ToLower(value), "altlinux") {
			return true
		}
	}
	return false
}
