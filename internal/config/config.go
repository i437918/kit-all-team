package config

import (
	"strconv"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

const (
	keyKitHome       = "KIT_ALL_TEAM_HOME"
	keyOS            = "OPERATING_SYSTEM"
	keyApplication   = "AI_APPLICATION"
	keyAppInstalled  = "AI_APP_INSTALLED"
	keyHermesHome    = "HERMES_HOME"
	keyHermesVersion = "HERMES_VERSION"
	keyProject       = "PROJECT"
	keyRole          = "ROLE"
	keyToolchain     = "TOOLCHAIN"
)

var requiredKeys = [...]string{
	keyKitHome,
	keyOS,
	keyApplication,
	keyAppInstalled,
	keyProject,
	keyRole,
	keyToolchain,
}

// Encode returns a fresh map containing only the exact public workspace keys.
func Encode(desired domain.DesiredState) map[string]string {
	values := map[string]string{
		keyKitHome:      desired.KitHome(),
		keyOS:           string(desired.OS()),
		keyApplication:  string(desired.Application()),
		keyAppInstalled: strconv.FormatBool(desired.AppInstalled()),
		keyProject:      string(desired.Project()),
		keyRole:         string(desired.Role()),
		keyToolchain:    string(desired.Toolchain()),
	}
	if desired.Application() == domain.AppHermes {
		values[keyHermesHome] = desired.HermesHome()
		if desired.HermesVersion() != "" {
			values[keyHermesVersion] = desired.HermesVersion()
		}
	}
	return values
}

// Decode validates an exact set of public workspace values and rebuilds the
// immutable state through domain.NewDesiredState.
func Decode(values map[string]string) (domain.DesiredState, error) {
	for key := range values {
		if secretLike(key) {
			return domain.DesiredState{}, configError(SecretKeyForbidden)
		}
		if !knownKey(key) {
			return domain.DesiredState{}, configError(KeyUnknown)
		}
	}
	for _, key := range requiredKeys {
		if _, exists := values[key]; !exists {
			return domain.DesiredState{}, configError(KeyMissing)
		}
	}

	installed, err := strictBool(values[keyAppInstalled])
	if err != nil {
		return domain.DesiredState{}, err
	}
	application := domain.AIApplication(values[keyApplication])
	_, hasHermesHome := values[keyHermesHome]
	_, hasHermesVersion := values[keyHermesVersion]
	if application == domain.AppHermes && !hasHermesHome {
		return domain.DesiredState{}, configError(KeyMissing)
	}
	if application != domain.AppHermes && hasHermesHome {
		return domain.DesiredState{}, configError(HermesHomeForbidden)
	}
	if application != domain.AppHermes && hasHermesVersion {
		return domain.DesiredState{}, configError(HermesHomeForbidden)
	}

	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS:            domain.OSFamily(values[keyOS]),
		Application:   application,
		AppInstalled:  installed,
		KitHome:       values[keyKitHome],
		HermesHome:    values[keyHermesHome],
		HermesVersion: values[keyHermesVersion],
		Project:       domain.ProjectID(values[keyProject]),
		Role:          domain.Role(values[keyRole]),
		Toolchain:     domain.Toolchain(values[keyToolchain]),
	})
	if err != nil {
		return domain.DesiredState{}, configError(ValueInvalid)
	}
	return desired, nil
}

// ParseDotenv parses a complete strict dotenv document before Decode so
// duplicate keys cannot be hidden by map conversion. A final newline and CRLF
// line endings are accepted; comments and blank interior lines are not.
func ParseDotenv(input string) (domain.DesiredState, error) {
	if strings.Contains(strings.ReplaceAll(input, "\r\n", ""), "\r") {
		return domain.DesiredState{}, configError(LineInvalid)
	}
	input = strings.ReplaceAll(input, "\r\n", "\n")
	lines := strings.Split(input, "\n")
	values := make(map[string]string, len(lines))
	for index, line := range lines {
		if line == "" && index == len(lines)-1 {
			continue
		}
		if line == "" {
			return domain.DesiredState{}, configError(LineInvalid)
		}
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" {
			return domain.DesiredState{}, configError(LineInvalid)
		}
		if secretLike(key) {
			return domain.DesiredState{}, configError(SecretKeyForbidden)
		}
		if _, duplicate := values[key]; duplicate {
			return domain.DesiredState{}, configError(KeyDuplicate)
		}
		values[key] = value
	}
	return Decode(values)
}

func knownKey(key string) bool {
	switch key {
	case keyKitHome, keyOS, keyApplication, keyAppInstalled, keyHermesHome, keyHermesVersion, keyProject, keyRole, keyToolchain:
		return true
	default:
		return false
	}
}

func secretLike(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"SECRET", "TOKEN", "PASSWORD", "API_KEY", "PRIVATE_KEY"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

func strictBool(value string) (bool, error) {
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, configError(BooleanInvalid)
	}
}
