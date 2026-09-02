package apps

import (
	"fmt"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

// Toolchain is the pinned toolchain that an alternative app must configure.
type Toolchain struct {
	Name    string
	Origin  string
	Version string
}

// HandoffRequest is the non-secret configuration needed by an alternative app.
// SecretValues are accepted only so the renderer can reject accidental inclusion.
type HandoffRequest struct {
	Toolchain     Toolchain
	V8StdEndpoint string
	SecretValues  []string
}

// Handoff is the one-command, paste-ready alternative application setup.
type Handoff struct {
	Command string
}

// PrepareHandoff emits a single safe command for an installed application.
func PrepareHandoff(application Application, request HandoffRequest) (Handoff, error) {
	if !application.Installed || application.ID == "" || !isSupportedApplication(application.ID) {
		return Handoff{}, ErrApplicationRequired
	}
	pinned, err := PinnedToolchain(domain.Toolchain(request.Toolchain.Name))
	if err != nil {
		return Handoff{}, err
	}
	if pinned.Version != request.Toolchain.Version {
		return Handoff{}, fmt.Errorf("toolchain pin does not match catalog")
	}
	request.Toolchain = pinned
	request.V8StdEndpoint = catalog.V8StdMCP().Endpoint
	for _, value := range []string{application.ID, request.Toolchain.Name, request.Toolchain.Origin, request.Toolchain.Version, request.V8StdEndpoint} {
		if !safeArgument(value) {
			return Handoff{}, fmt.Errorf("unsafe handoff value")
		}
	}
	command := installationInstructions(request.Toolchain, request.V8StdEndpoint)
	if request.Toolchain.Name == string(domain.ToolchainAIRules1C) {
		command += " Follow AGENT-INSTALL.md from the pinned ai_rules_1c tree at the exact catalog commit."
	}
	for _, secret := range request.SecretValues {
		if secret != "" && strings.Contains(command, secret) {
			return Handoff{}, fmt.Errorf("handoff would reveal a secret")
		}
	}
	return Handoff{Command: command}, nil
}

func installationInstructions(toolchain Toolchain, v8stdEndpoint string) string {
	return fmt.Sprintf(
		"Для установки выбранного набора %s используй источник %s с точным закреплённым commit %s.\n\nДля настройки MCP-сервера v8std.ru используй endpoint %s.",
		toolchain.Name,
		toolchain.Origin,
		toolchain.Version,
		v8stdEndpoint,
	)
}
func isSupportedApplication(id string) bool {
	for _, supported := range SupportedApplications() {
		if string(supported) == id {
			return true
		}
	}
	return false
}

func safeArgument(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n;&|`$'\"")
}
