// Package apps detects supported non-Hermes applications and creates safe
// copy-and-paste handoffs for their selected development tooling.
package apps

import (
	"errors"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

// ErrApplicationRequired reports that the chosen non-Hermes app is unavailable.
var ErrApplicationRequired = errors.New("AI_APP_REQUIRED: selected AI application is not installed")

// Application identifies a selected alternative AI application.
type Application struct {
	ID        string
	Installed bool
}

// Detector discovers whether an application is installed without launching it.
type Detector interface {
	Installed(applicationID string) (bool, error)
}

// DetectApplication uses detector to produce an application selection.
func DetectApplication(detector Detector, applicationID string) (Application, error) {
	installed, err := detector.Installed(applicationID)
	if err != nil {
		return Application{}, err
	}
	return Application{ID: applicationID, Installed: installed}, nil
}

// SupportedApplications returns the closed non-Hermes application set.
func SupportedApplications() []domain.AIApplication {
	all := catalog.AIApplications()
	applications := make([]domain.AIApplication, 0, len(all)-1)
	for _, application := range all {
		if application.ID != domain.AppHermes {
			applications = append(applications, application.ID)
		}
	}
	return applications
}

// PinnedToolchain resolves a selected toolchain to its immutable catalog pin.
func PinnedToolchain(id domain.Toolchain) (Toolchain, error) {
	pinned, err := catalog.LookupToolchain(id)
	if err != nil {
		return Toolchain{}, err
	}
	return Toolchain{Name: string(pinned.ID), Origin: pinned.Origin, Version: pinned.Commit}, nil
}

// Code returns the stable error code associated with an adapter error.
func Code(err error) string {
	if errors.Is(err, ErrApplicationRequired) {
		return "AI_APP_REQUIRED"
	}
	return ""
}
