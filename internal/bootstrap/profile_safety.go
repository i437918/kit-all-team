package bootstrap

import (
	"fmt"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

func validateHermesProfilePath(desired domain.DesiredState) error {
	home := desired.HermesHome()
	components := []string{
		home,
		filepath.Join(home, "certs"),
		filepath.Join(home, "profiles"),
		profileDirectory(desired),
		filepath.Join(profileDirectory(desired), ".teamkit"),
		toolchainPath(desired),
		filepath.Join(home, ".teamkit"),
		filepath.Join(home, ".teamkit", "profiles"),
		filepath.Join(home, ".teamkit", "cache"),
		filepath.Join(home, ".teamkit", "hermes-agent-source"),
	}
	for _, component := range components {
		if err := pathsafe.ValidateDirectory(component); err != nil {
			return fmt.Errorf("%w: %v", ErrForeignProfile, err)
		}
	}
	for _, file := range []string{
		filepath.Join(home, "certs", "ca-bundle.pem"),
		profilePath(desired),
		profileOwnerPath(desired),
		profileCreatingPath(desired),
		installedMarker(desired),
	} {
		if err := pathsafe.ValidateRegular(file); err != nil {
			return fmt.Errorf("%w: %v", ErrForeignProfile, err)
		}
	}
	return nil
}
