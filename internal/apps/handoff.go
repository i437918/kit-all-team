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
	for _, value := range []string{application.ID, request.Toolchain.Name, request.Toolchain.Version, request.V8StdEndpoint} {
		if !safeArgument(value) {
			return Handoff{}, fmt.Errorf("unsafe handoff value")
		}
	}
	command := fmt.Sprintf("In %s, configure exactly one toolchain from %s pinned to commit %s, then configure the separate v8std MCP endpoint %s.", application.ID, request.Toolchain.Origin, request.Toolchain.Version, request.V8StdEndpoint)
	for _, secret := range request.SecretValues {
		if secret != "" && strings.Contains(command, secret) {
			return Handoff{}, fmt.Errorf("handoff would reveal a secret")
		}
	}
	return Handoff{Command: command}, nil
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
