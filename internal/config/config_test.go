package config_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/config"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

func TestEncodeDecode_HermesRoundTripUsesExactPublicKeys(t *testing.T) {
	want, err := domain.NewDesiredState(domain.DesiredStateInput{OS: domain.OSALTLinux, Application: domain.AppHermes, AppInstalled: true, KitHome: "/srv/team kit", HermesHome: "/srv/hermes profile", HermesVersion: "0.20.2", Project: domain.ProjectWMS, Role: domain.RoleArchitect, Toolchain: domain.ToolchainAIRules1C})
	if err != nil {
		t.Fatal(err)
	}
	wantValues := map[string]string{
		"KIT_ALL_TEAM_HOME": "/srv/team kit",
		"OPERATING_SYSTEM":  "altlinux",
		"AI_APPLICATION":    "hermes",
		"AI_APP_INSTALLED":  "true",
		"HERMES_HOME":       "/srv/hermes profile",
		"HERMES_VERSION":    "0.20.2",
		"PROJECT":           "wms",
		"ROLE":              "architect",
		"TOOLCHAIN":         "ai_rules_1c",
	}
	values := config.Encode(want)
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("Encode() = %#v, want %#v", values, wantValues)
	}
	got, err := config.Decode(values)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Decode(Encode(state)) = %#v, want %#v", got, want)
	}
}

func TestDecode_LegacyHermesWithoutVersionStillWorks(t *testing.T) {
	values := map[string]string{"KIT_ALL_TEAM_HOME": "/kit", "OPERATING_SYSTEM": "linux", "AI_APPLICATION": "hermes", "AI_APP_INSTALLED": "true", "HERMES_HOME": "/home/dev/.hermes", "PROJECT": "apa", "ROLE": "developer", "TOOLCHAIN": "cc_1c_skills"}
	got, err := config.Decode(values)
	if err != nil || got.HermesVersion() != "" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestEncodeDecode_NonHermesOmitsHermesHome(t *testing.T) {
	want := desiredState(t, domain.AppCodex, false, "")
	values := config.Encode(want)
	if _, exists := values["HERMES_HOME"]; exists {
		t.Fatalf("non-Hermes values contain HERMES_HOME: %#v", values)
	}
	got, err := config.Decode(values)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Decode(Encode(state)) = %#v, want %#v", got, want)
	}
}

func TestDecode_RejectsInvalidPublicConfiguration(t *testing.T) {
	base := map[string]string{
		"KIT_ALL_TEAM_HOME": "/srv/teamkit",
		"OPERATING_SYSTEM":  "linux",
		"AI_APPLICATION":    "codex",
		"AI_APP_INSTALLED":  "true",
		"PROJECT":           "aisuz",
		"ROLE":              "developer",
		"TOOLCHAIN":         "cc_1c_skills",
	}
	tests := []struct {
		name string
		edit func(map[string]string)
		code config.ErrorCode
	}{
		{"unknown key", func(v map[string]string) { v["EXTRA"] = "value" }, config.KeyUnknown},
		{"missing key", func(v map[string]string) { delete(v, "ROLE") }, config.KeyMissing},
		{"secret-like key", func(v map[string]string) { v["LLM_API_KEY"] = "TEAMKIT_SECRET_CANARY_c3ef" }, config.SecretKeyForbidden},
		{"invalid boolean", func(v map[string]string) { v["AI_APP_INSTALLED"] = "1" }, config.BooleanInvalid},
		{"Hermes home on other app", func(v map[string]string) { v["HERMES_HOME"] = "/srv/hermes" }, config.HermesHomeForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := clone(base)
			tt.edit(values)
			_, err := config.Decode(values)
			assertCode(t, err, tt.code)
			if strings.Contains(errorText(err), "TEAMKIT_SECRET_CANARY_c3ef") {
				t.Fatalf("error leaked secret canary: %v", err)
			}
		})
	}
}

func TestDecode_HermesRequiresConditionalHome(t *testing.T) {
	values := map[string]string{
		"KIT_ALL_TEAM_HOME": "/srv/teamkit",
		"OPERATING_SYSTEM":  "linux",
		"AI_APPLICATION":    "hermes",
		"AI_APP_INSTALLED":  "false",
		"PROJECT":           "aisuz",
		"ROLE":              "analyst",
		"TOOLCHAIN":         "cc_1c_skills",
	}
	_, err := config.Decode(values)
	assertCode(t, err, config.KeyMissing)
}

func TestDecode_InvalidSelectorDoesNotLeakValue(t *testing.T) {
	canary := "TEAMKIT_SECRET_CANARY_SELECTOR_4d92"
	values := map[string]string{
		"KIT_ALL_TEAM_HOME": "/srv/teamkit",
		"OPERATING_SYSTEM":  "linux",
		"AI_APPLICATION":    "codex",
		"AI_APP_INSTALLED":  "true",
		"PROJECT":           canary,
		"ROLE":              "developer",
		"TOOLCHAIN":         "cc_1c_skills",
	}
	_, err := config.Decode(values)
	assertCode(t, err, config.ValueInvalid)
	if strings.Contains(errorText(err), canary) {
		t.Fatalf("error leaked invalid selector canary: %v", err)
	}
}

func TestParseDotenv_RejectsDuplicateKeysBeforeMapConversion(t *testing.T) {
	input := "KIT_ALL_TEAM_HOME=/srv/one\n" +
		"OPERATING_SYSTEM=linux\n" +
		"AI_APPLICATION=codex\n" +
		"AI_APP_INSTALLED=true\n" +
		"PROJECT=aisuz\n" +
		"ROLE=developer\n" +
		"TOOLCHAIN=cc_1c_skills\n" +
		"KIT_ALL_TEAM_HOME=/srv/two\n"
	_, err := config.ParseDotenv(input)
	assertCode(t, err, config.KeyDuplicate)
}

func TestParseDotenv_AcceptsCompleteCRLFDocument(t *testing.T) {
	input := "KIT_ALL_TEAM_HOME=C:\\Team Kit\r\n" +
		"OPERATING_SYSTEM=windows\r\n" +
		"AI_APPLICATION=cline\r\n" +
		"AI_APP_INSTALLED=false\r\n" +
		"PROJECT=esed\r\n" +
		"ROLE=analyst\r\n" +
		"TOOLCHAIN=cc_1c_skills\r\n"
	got, err := config.ParseDotenv(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.KitHome() != `C:\Team Kit` || got.OS() != domain.OSWindows || got.Application() != domain.AppCline || got.AppInstalled() {
		t.Fatalf("parsed state = %#v", got)
	}
}

func desiredState(t *testing.T, app domain.AIApplication, installed bool, hermesHome string) domain.DesiredState {
	t.Helper()
	state, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS:           domain.OSALTLinux,
		Application:  app,
		AppInstalled: installed,
		KitHome:      "/srv/team kit",
		HermesHome:   hermesHome,
		Project:      domain.ProjectWMS,
		Role:         domain.RoleArchitect,
		Toolchain:    domain.ToolchainAIRules1C,
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func clone(values map[string]string) map[string]string {
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func assertCode(t *testing.T, err error, want config.ErrorCode) {
	t.Helper()
	var configErr *config.Error
	if !errors.As(err, &configErr) || configErr.Code != want {
		t.Fatalf("error = %v, want code %q", err, want)
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
