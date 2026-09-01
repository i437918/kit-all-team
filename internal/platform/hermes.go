package platform

import (
	"errors"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

// ErrHermesHomeUnavailable reports missing data for a platform default home.
var ErrHermesHomeUnavailable = errors.New("HERMES_HOME_UNAVAILABLE")

// DefaultHermesHome returns the conventional private Hermes configuration home.
func DefaultHermesHome(family domain.OSFamily, home, appData string) (string, error) {
	switch family {
	case domain.OSWindows:
		if appData == "" {
			return "", ErrHermesHomeUnavailable
		}
		return filepath.Join(appData, "hermes"), nil
	case domain.OSMacOS:
		if home == "" {
			return "", ErrHermesHomeUnavailable
		}
		return filepath.Join(home, ".hermes"), nil
	case domain.OSLinux, domain.OSALTLinux:
		if home == "" {
			return "", ErrHermesHomeUnavailable
		}
		return filepath.Join(home, ".hermes"), nil
	default:
		return "", ErrUnsupportedOS
	}
}
