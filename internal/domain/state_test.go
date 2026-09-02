package domain_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

func TestNewDesiredState_AcceptsExactlyCoreContractMatrix(t *testing.T) {
	projects := []domain.ProjectID{
		domain.ProjectAISUZ, domain.ProjectAPA, domain.ProjectASBNU,
		domain.ProjectASKU, domain.ProjectEASR, domain.ProjectEISKO,
		domain.ProjectESED, domain.ProjectUAT, domain.ProjectUNIP,
		domain.ProjectWMS, domain.ProjectZUP,
	}
	roles := []domain.Role{domain.RoleAnalyst, domain.RoleDeveloper, domain.RoleArchitect}
	toolchains := []domain.Toolchain{domain.ToolchainCC1CSkills, domain.ToolchainAIRules1C}
	operatingSystems := []domain.OSFamily{domain.OSWindows, domain.OSMacOS, domain.OSLinux, domain.OSALTLinux}

	count := 0
	for _, project := range projects {
		for _, role := range roles {
			for _, toolchain := range toolchains {
				for _, operatingSystem := range operatingSystems {
					state, err := domain.NewDesiredState(domain.DesiredStateInput{
						OS:           operatingSystem,
						Application:  domain.AppHermes,
						AppInstalled: true,
						KitHome:      "/work/teamkit",
						HermesHome:   "/work/hermes",
						Project:      project,
						Role:         role,
						Toolchain:    toolchain,
					})
					if err != nil {
						t.Fatalf("valid combination %q/%q/%q/%q: %v", project, role, toolchain, operatingSystem, err)
					}
					if state.Project() != project || state.Role() != role || state.Toolchain() != toolchain || state.OS() != operatingSystem {
						t.Fatalf("state getters changed selected combination")
					}
					count++
				}
			}
		}
	}

	if count != 264 {
		t.Fatalf("core contract matrix count = %d, want 264", count)
	}
}

func TestNewDesiredState_RejectsUnknownIdentifiersWithStableCodes(t *testing.T) {
	valid := domain.DesiredStateInput{
		OS:           domain.OSWindows,
		Application:  domain.AppHermes,
		AppInstalled: true,
		KitHome:      `C:\teamkit`,
		HermesHome:   `C:\hermes`,
		Project:      domain.ProjectAISUZ,
		Role:         domain.RoleDeveloper,
		Toolchain:    domain.ToolchainCC1CSkills,
	}
	tests := []struct {
		name string
		edit func(*domain.DesiredStateInput)
		code domain.ErrorCode
	}{
		{"project", func(v *domain.DesiredStateInput) { v.Project = "unknown" }, domain.ProjectUnknown},
		{"role", func(v *domain.DesiredStateInput) { v.Role = "unknown" }, domain.RoleUnknown},
		{"toolchain", func(v *domain.DesiredStateInput) { v.Toolchain = "unknown" }, domain.ToolchainUnknown},
		{"os", func(v *domain.DesiredStateInput) { v.OS = "unknown" }, domain.OSUnknown},
		{"application", func(v *domain.DesiredStateInput) { v.Application = "unknown" }, domain.ApplicationUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := valid
			tt.edit(&input)
			_, err := domain.NewDesiredState(input)
			var validationErr *domain.ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %v, want ValidationError", err)
			}
			if validationErr.Code != tt.code {
				t.Fatalf("code = %q, want %q", validationErr.Code, tt.code)
			}
		})
	}
}

func TestNewDesiredState_RequiresHomesAtTheirBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		input domain.DesiredStateInput
		code  domain.ErrorCode
	}{
		{
			name:  "kit home always required",
			input: domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppCodex, AppInstalled: true, Project: domain.ProjectAISUZ, Role: domain.RoleAnalyst, Toolchain: domain.ToolchainAIRules1C},
			code:  domain.KitHomeRequired,
		},
		{
			name:  "Hermes home required for Hermes",
			input: domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true, KitHome: "/kit", Project: domain.ProjectAISUZ, Role: domain.RoleAnalyst, Toolchain: domain.ToolchainAIRules1C},
			code:  domain.HermesHomeRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := domain.NewDesiredState(tt.input)
			var validationErr *domain.ValidationError
			if !errors.As(err, &validationErr) || validationErr.Code != tt.code {
				t.Fatalf("error = %v, want code %q", err, tt.code)
			}
		})
	}
}

func TestClosedIdentifiers_HaveExactExternalValues(t *testing.T) {
	if got, want := []domain.OSFamily{domain.OSWindows, domain.OSMacOS, domain.OSLinux, domain.OSALTLinux}, []domain.OSFamily{"windows", "macos", "linux", "altlinux"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("OS identifiers = %#v, want %#v", got, want)
	}
	if got, want := []domain.AIApplication{
		domain.AppHermes, domain.AppCursor, domain.AppClaudeCode, domain.AppCodex,
		domain.AppOpenCode, domain.AppKiloCode, domain.AppKimi, domain.AppQwen,
		domain.AppCommandCode, domain.AppCline, domain.AppPi,
	}, []domain.AIApplication{
		"hermes", "cursor", "claude-code", "codex", "opencode", "kilo-code",
		"kimi", "qwen", "command-code", "cline", "pi",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("application identifiers = %#v, want %#v", got, want)
	}
}

func TestNewDesiredState_HermesVersionRoundTrips(t *testing.T) {
	state, err := domain.NewDesiredState(domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true, KitHome: "/kit", HermesHome: "/home/dev/.hermes", HermesVersion: "0.20.2", Project: domain.ProjectAPA, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills})
	if err != nil || state.HermesVersion() != "0.20.2" {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	_, err = domain.NewDesiredState(domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppCodex, AppInstalled: true, KitHome: "/kit", HermesVersion: "0.20.2", Project: domain.ProjectAPA, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills})
	if err == nil {
		t.Fatal("non-Hermes state accepted HermesVersion")
	}
}

func TestNewDesiredState_RejectsVersionForNotInstalledHermes(t *testing.T) {
	_, err := domain.NewDesiredState(domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: false, KitHome: "/kit", HermesHome: "/home/dev/.hermes", HermesVersion: "0.20.2", Project: domain.ProjectAPA, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills})
	if err == nil {
		t.Fatal("not-installed Hermes accepted observed version")
	}
}
