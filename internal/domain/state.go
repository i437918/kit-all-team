package domain

import (
	"regexp"
	"strings"
)

var hermesVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// ProjectID identifies a member of the closed project catalog.
type ProjectID string

const (
	ProjectAISUZ ProjectID = "aisuz"
	ProjectAPA   ProjectID = "apa"
	ProjectASBNU ProjectID = "asbnu"
	ProjectASKU  ProjectID = "asku"
	ProjectEASR  ProjectID = "easr"
	ProjectEISKO ProjectID = "eisko"
	ProjectESED  ProjectID = "esed"
	ProjectUAT   ProjectID = "uat"
	ProjectUNIP  ProjectID = "unip"
	ProjectWMS   ProjectID = "wms"
	ProjectZUP   ProjectID = "zup"
)

// Role identifies a supported 1C team role.
type Role string

const (
	RoleAnalyst   Role = "analyst"
	RoleDeveloper Role = "developer"
	RoleArchitect Role = "architect"
)

// Toolchain identifies exactly one pinned 1C capability set.
type Toolchain string

const (
	ToolchainCC1CSkills Toolchain = "cc_1c_skills"
	ToolchainAIRules1C  Toolchain = "ai_rules_1c"
)

// OSFamily identifies a supported operating-system family.
type OSFamily string

const (
	OSWindows  OSFamily = "windows"
	OSMacOS    OSFamily = "macos"
	OSLinux    OSFamily = "linux"
	OSALTLinux OSFamily = "altlinux"
)

// AIApplication identifies a supported AI application adapter.
type AIApplication string

const (
	AppHermes      AIApplication = "hermes"
	AppCursor      AIApplication = "cursor"
	AppClaudeCode  AIApplication = "claude-code"
	AppCodex       AIApplication = "codex"
	AppOpenCode    AIApplication = "opencode"
	AppKiloCode    AIApplication = "kilo-code"
	AppKimi        AIApplication = "kimi"
	AppQwen        AIApplication = "qwen"
	AppCommandCode AIApplication = "command-code"
	AppCline       AIApplication = "cline"
	AppPi          AIApplication = "pi"
)

// DesiredStateInput contains questionnaire selections for validation.
type DesiredStateInput struct {
	OS            OSFamily
	Application   AIApplication
	AppInstalled  bool
	KitHome       string
	HermesHome    string
	HermesVersion string
	Project       ProjectID
	Role          Role
	Toolchain     Toolchain
}

// DesiredState is an immutable validated copy of questionnaire selections.
// Its fields are intentionally private; consumers use the accessors below.
type DesiredState struct {
	operatingSystem OSFamily
	application     AIApplication
	appInstalled    bool
	kitHome         string
	hermesHome      string
	hermesVersion   string
	project         ProjectID
	role            Role
	toolchain       Toolchain
}

// NewDesiredState validates all closed selectors and required homes.
func NewDesiredState(input DesiredStateInput) (DesiredState, error) {
	if !validProject(input.Project) {
		return DesiredState{}, NewValidationError(ProjectUnknown, "project", string(input.Project))
	}
	if !validRole(input.Role) {
		return DesiredState{}, NewValidationError(RoleUnknown, "role", string(input.Role))
	}
	if !validToolchain(input.Toolchain) {
		return DesiredState{}, NewValidationError(ToolchainUnknown, "toolchain", string(input.Toolchain))
	}
	if !validOS(input.OS) {
		return DesiredState{}, NewValidationError(OSUnknown, "os", string(input.OS))
	}
	if !validApplication(input.Application) {
		return DesiredState{}, NewValidationError(ApplicationUnknown, "application", string(input.Application))
	}
	if strings.TrimSpace(input.KitHome) == "" {
		return DesiredState{}, NewValidationError(KitHomeRequired, "kit_home", "")
	}
	if input.Application == AppHermes && strings.TrimSpace(input.HermesHome) == "" {
		return DesiredState{}, NewValidationError(HermesHomeRequired, "hermes_home", "")
	}
	if input.Application != AppHermes && input.HermesVersion != "" {
		return DesiredState{}, NewValidationError(ApplicationUnknown, "hermes_version", "")
	}
	if input.Application == AppHermes && !input.AppInstalled && input.HermesVersion != "" {
		return DesiredState{}, NewValidationError(ApplicationUnknown, "hermes_version", "")
	}
	if input.HermesVersion != "" && !hermesVersionPattern.MatchString(input.HermesVersion) {
		return DesiredState{}, NewValidationError(ApplicationUnknown, "hermes_version", "")
	}
	return DesiredState{
		operatingSystem: input.OS,
		application:     input.Application,
		appInstalled:    input.AppInstalled,
		kitHome:         input.KitHome,
		hermesHome:      input.HermesHome,
		hermesVersion:   input.HermesVersion,
		project:         input.Project,
		role:            input.Role,
		toolchain:       input.Toolchain,
	}, nil
}

// OS returns the selected operating-system family.
func (s DesiredState) OS() OSFamily { return s.operatingSystem }

// Application returns the selected AI application.
func (s DesiredState) Application() AIApplication { return s.application }

// AppInstalled reports the observed questionnaire answer for application installation.
func (s DesiredState) AppInstalled() bool { return s.appInstalled }

// KitHome returns the selected Team Kit workspace home.
func (s DesiredState) KitHome() string { return s.kitHome }

// HermesHome returns the selected Hermes home, or an empty string for another application.
func (s DesiredState) HermesHome() string { return s.hermesHome }

// HermesVersion returns the verified installed Hermes version, when observed.
func (s DesiredState) HermesVersion() string { return s.hermesVersion }

// Project returns the selected project.
func (s DesiredState) Project() ProjectID { return s.project }

// Role returns the selected role.
func (s DesiredState) Role() Role { return s.role }

// Toolchain returns the selected pinned toolchain.
func (s DesiredState) Toolchain() Toolchain { return s.toolchain }

func validProject(value ProjectID) bool {
	switch value {
	case ProjectAISUZ, ProjectAPA, ProjectASBNU, ProjectASKU, ProjectEASR, ProjectEISKO, ProjectESED, ProjectUAT, ProjectUNIP, ProjectWMS, ProjectZUP:
		return true
	default:
		return false
	}
}

func validRole(value Role) bool {
	switch value {
	case RoleAnalyst, RoleDeveloper, RoleArchitect:
		return true
	default:
		return false
	}
}

func validToolchain(value Toolchain) bool {
	switch value {
	case ToolchainCC1CSkills, ToolchainAIRules1C:
		return true
	default:
		return false
	}
}

func validOS(value OSFamily) bool {
	switch value {
	case OSWindows, OSMacOS, OSLinux, OSALTLinux:
		return true
	default:
		return false
	}
}

func validApplication(value AIApplication) bool {
	switch value {
	case AppHermes, AppCursor, AppClaudeCode, AppCodex, AppOpenCode, AppKiloCode, AppKimi, AppQwen, AppCommandCode, AppCline, AppPi:
		return true
	default:
		return false
	}
}
