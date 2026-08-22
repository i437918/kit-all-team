package platform

import (
	"errors"
	"os/exec"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

var executableCandidates = map[domain.AIApplication][]string{
	domain.AppHermes:      {"hermes"},
	domain.AppCursor:      {"cursor"},
	domain.AppClaudeCode:  {"claude"},
	domain.AppCodex:       {"codex"},
	domain.AppOpenCode:    {"opencode"},
	domain.AppKiloCode:    {"kilo"},
	domain.AppKimi:        {"kimi"},
	domain.AppQwen:        {"qwen"},
	domain.AppCommandCode: {"command-code"},
	domain.AppCline:       {"cline"},
	domain.AppPi:          {"pi"},
}

// LookPath resolves an executable without exposing process execution to callers.
type LookPath func(file string) (string, error)

// ExecutableCandidates returns a defensive copy of deterministic executable
// names for a closed catalog application.
func ExecutableCandidates(application domain.AIApplication) ([]string, error) {
	if _, err := catalog.LookupAIApplication(application); err != nil {
		return nil, err
	}
	candidates, ok := executableCandidates[application]
	if !ok {
		return nil, domain.NewValidationError(domain.ApplicationUnknown, "application", string(application))
	}
	return append([]string(nil), candidates...), nil
}

// DetectInstalled reports whether LookPath finds one of the application's known
// executable candidates. It performs no application launch.
func DetectInstalled(application domain.AIApplication, lookPath LookPath) (bool, error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	candidates, err := ExecutableCandidates(application)
	if err != nil {
		return false, err
	}
	for _, candidate := range candidates {
		if _, err := lookPath(candidate); err == nil {
			return true, nil
		} else if !errors.Is(err, exec.ErrNotFound) {
			return false, err
		}
	}
	return false, nil
}
