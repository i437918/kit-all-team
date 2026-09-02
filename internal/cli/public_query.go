package cli

import (
	"context"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/apps"
	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/platform"
	"github.com/mi1man-cmd/kit-all-team/internal/registry"
)

const publicQuerySchemaVersion = 1

type publicApplicationRequiredError struct{}

func (publicApplicationRequiredError) Error() string {
	return "selected AI application is not installed"
}

func (publicApplicationRequiredError) Unwrap() error {
	return apps.ErrApplicationRequired
}

// PublicChoice is one stable selector exposed to the Windows wizard.
type PublicChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PublicCatalog is the complete, non-sensitive selector catalog for the wizard.
type PublicCatalog struct {
	SchemaVersion int            `json:"schema_version"`
	Applications  []PublicChoice `json:"applications"`
	Projects      []PublicChoice `json:"projects"`
	Roles         []PublicChoice `json:"roles"`
	Toolchains    []PublicChoice `json:"toolchains"`
}

// PublicDetection is the installed state of one selected AI application.
type PublicDetection struct {
	SchemaVersion int    `json:"schema_version"`
	ApplicationID string `json:"application_id"`
	Installed     bool   `json:"installed"`
	Version       string `json:"version,omitempty"`
	Home          string `json:"home,omitempty"`
}

// PublicEnvironment is a verified, updateable environment exposed to the wizard.
type PublicEnvironment struct {
	Home          string `json:"home"`
	ApplicationID string `json:"application_id"`
	ProjectID     string `json:"project_id"`
	RoleID        string `json:"role_id"`
	ToolchainID   string `json:"toolchain_id"`
	Status        string `json:"status"`
}

func publicCatalog() PublicCatalog {
	return PublicCatalog{
		SchemaVersion: publicQuerySchemaVersion,
		Applications:  publicApplicationChoices(),
		Projects:      publicProjectChoices(),
		Roles:         publicRoleChoices(),
		Toolchains:    publicToolchainChoices(),
	}
}

func choiceIDs(choices []PublicChoice) []string {
	result := make([]string, len(choices))
	for index, choice := range choices {
		result[index] = choice.ID
	}
	return result
}

func publicApplicationChoices() []PublicChoice {
	items := catalog.AIApplications()
	result := make([]PublicChoice, len(items))
	for index, item := range items {
		result[index] = PublicChoice{ID: string(item.ID), Label: item.Label}
	}
	return result
}

func publicProjectChoices() []PublicChoice {
	items := catalog.Projects()
	result := make([]PublicChoice, len(items))
	for index, item := range items {
		result[index] = PublicChoice{ID: string(item.ID), Label: string(item.ID)}
	}
	return result
}

func publicRoleChoices() []PublicChoice {
	items := catalog.Roles()
	result := make([]PublicChoice, len(items))
	for index, item := range items {
		result[index] = PublicChoice{ID: string(item.ID), Label: item.Label}
	}
	return result
}

func publicToolchainChoices() []PublicChoice {
	items := catalog.Toolchains()
	result := make([]PublicChoice, len(items))
	for index, item := range items {
		result[index] = PublicChoice{ID: string(item.ID), Label: string(item.ID)}
	}
	return result
}

func (r Runner) publicDetection(ctx context.Context, application, installedConfirmation string) (PublicDetection, error) {
	id, err := publicApplicationID(application)
	if err != nil {
		return PublicDetection{}, err
	}
	if id == domain.AppHermes {
		if r.HermesDiscovery == nil {
			return PublicDetection{}, errors.New("HERMES_DISCOVERY_REQUIRED")
		}
		result, err := r.HermesDiscovery(ctx, hermesDiscoveryRequest(r.GOOS))
		if err != nil {
			return PublicDetection{}, err
		}
		if !result.Installed {
			return PublicDetection{}, publicApplicationRequiredError{}
		}
		return PublicDetection{SchemaVersion: publicQuerySchemaVersion, ApplicationID: application, Installed: true, Version: result.Version, Home: result.Home}, nil
	}
	if r.GOOS == "windows" && installedConfirmation != "" {
		installed, err := strconv.ParseBool(installedConfirmation)
		if err != nil {
			return PublicDetection{}, newOperationalError(codeInputRequired, "--app-installed must be true or false", err)
		}
		if !installed {
			return PublicDetection{}, publicApplicationRequiredError{}
		}
		// This is the user's installed-state confirmation, not executable
		// verification. Keep Home and Version empty: many Windows GUI clients do
		// not publish a CLI command through PATH.
		return PublicDetection{SchemaVersion: publicQuerySchemaVersion, ApplicationID: application, Installed: true}, nil
	}
	installed, err := platform.DetectInstalled(id, r.ApplicationLookPath)
	if err != nil {
		return PublicDetection{}, err
	}
	if !installed {
		return PublicDetection{}, publicApplicationRequiredError{}
	}
	return PublicDetection{SchemaVersion: publicQuerySchemaVersion, ApplicationID: application, Installed: true}, nil
}

func publicApplicationID(application string) (domain.AIApplication, error) {
	id := domain.AIApplication(application)
	if _, err := catalog.LookupAIApplication(id); err != nil {
		return "", publicApplicationRequiredError{}
	}
	return id, nil
}

func hermesDiscoveryRequest(goos string) hermes.DiscoveryRequest {
	return hermes.DiscoveryRequest{OS: osFamily(goos)}
}

func osFamily(goos string) domain.OSFamily {
	switch goos {
	case "windows":
		return domain.OSWindows
	case "darwin":
		return domain.OSMacOS
	case "altlinux":
		return domain.OSALTLinux
	default:
		return domain.OSLinux
	}
}

func (r *Runner) publicEnvironments(ctx context.Context, application string) ([]PublicEnvironment, error) {
	id, err := publicApplicationID(application)
	if err != nil {
		return nil, err
	}
	request := environment.DiscoveryRequest{EnvironmentHome: os.Getenv("KIT_ALL_TEAM_HOME")}
	if r.Registry != nil {
		snapshot, state, _ := r.Registry.Load(ctx)
		if state == registry.LoadValid {
			request.RegistryHomes = append([]string(nil), snapshot.Homes...)
		}
	}
	discovered, err := environment.Discover(ctx, request, r.environmentInspector())
	if err != nil {
		return nil, err
	}
	result := make([]PublicEnvironment, 0, len(discovered.Environments))
	for _, verified := range discovered.Environments {
		if verified.Desired.Application() != id {
			continue
		}
		status := environment.Ready.String()
		if verified.Pending {
			status = environment.RetryRequired.String()
		}
		result = append(result, PublicEnvironment{
			Home: verified.Home, ApplicationID: string(verified.Desired.Application()), ProjectID: string(verified.Desired.Project()),
			RoleID: string(verified.Desired.Role()), ToolchainID: string(verified.Desired.Toolchain()), Status: status,
		})
	}
	sort.Slice(result, func(left, right int) bool {
		if r.GOOS == "windows" {
			leftFolded, rightFolded := strings.ToLower(result[left].Home), strings.ToLower(result[right].Home)
			if leftFolded != rightFolded {
				return leftFolded < rightFolded
			}
		}
		return result[left].Home < result[right].Home
	})
	return result, nil
}
