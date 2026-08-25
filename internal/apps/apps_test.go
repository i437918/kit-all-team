package apps

import (
	"errors"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

func TestPrepareHandoff_RequiresInstalledApplication(t *testing.T) {
	_, err := PrepareHandoff(Application{ID: "codex", Installed: false}, HandoffRequest{
		Toolchain:     Toolchain{Name: "cc_1c_skills", Version: catalog.Toolchains()[0].Commit},
		V8StdEndpoint: catalog.V8StdMCP().Endpoint,
	})
	if !errors.Is(err, ErrApplicationRequired) {
		t.Fatalf("PrepareHandoff() error = %v, want ErrApplicationRequired", err)
	}
	if Code(err) != "AI_APP_REQUIRED" {
		t.Fatalf("Code(err) = %q", Code(err))
	}
}

func TestPinnedToolchainAndSupportedApplications_BindClosedCatalog(t *testing.T) {
	toolchain, err := PinnedToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	if toolchain.Name != string(domain.ToolchainCC1CSkills) || toolchain.Origin != catalog.Toolchains()[0].Origin || toolchain.Version != catalog.Toolchains()[0].Commit {
		t.Fatalf("PinnedToolchain() = %#v", toolchain)
	}
	if got := SupportedApplications(); len(got) != len(catalog.AIApplications())-1 {
		t.Fatalf("SupportedApplications() = %#v", got)
	}
}

func TestPrepareHandoff_RejectsEmptyApplicationID(t *testing.T) {
	_, err := PrepareHandoff(Application{Installed: true}, HandoffRequest{Toolchain: Toolchain{Name: "cc_1c_skills", Version: catalog.Toolchains()[0].Commit}})
	if !errors.Is(err, ErrApplicationRequired) {
		t.Fatalf("PrepareHandoff() error = %v, want ErrApplicationRequired", err)
	}
}

func TestPrepareHandoff_EmitsSelectedImmutableToolchainAndV8StdEndpoint(t *testing.T) {
	const canary = "do-not-leak-api-key"
	toolchain, err := PinnedToolchain(domain.ToolchainAIRules1C)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := PrepareHandoff(Application{ID: "codex", Installed: true}, HandoffRequest{Toolchain: toolchain, SecretValues: []string{canary}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Для установки выбранного набора ai_rules_1c",
		"https://github.com/comol/ai_rules_1c.git",
		"f33d2405207cf325f893dc8ca2789157d887db81",
		"https://ai.v8std.ru/mcp",
	} {
		if !strings.Contains(handoff.Command, want) {
			t.Fatalf("handoff does not contain %q: %s", want, handoff.Command)
		}
	}
	for _, forbidden := range []string{canary, "https://v8std.ru/mcp/", "default branch", "ветк"} {
		if strings.Contains(handoff.Command, forbidden) {
			t.Fatalf("handoff contains forbidden value %q: %s", forbidden, handoff.Command)
		}
	}
}
func TestPrepareHandoff_AllAlternativeApplicationsAndToolchainsAreExclusive(t *testing.T) {
	const canary = "TEAMKIT_SECRET_CANARY"
	for _, application := range SupportedApplications() {
		for _, selected := range catalog.Toolchains() {
			t.Run(string(application)+"/"+string(selected.ID), func(t *testing.T) {
				got, err := PrepareHandoff(Application{ID: string(application), Installed: true}, HandoffRequest{Toolchain: Toolchain{Name: string(selected.ID), Origin: selected.Origin, Version: selected.Commit}, SecretValues: []string{canary}})
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{selected.Origin, selected.Commit, "https://ai.v8std.ru/mcp"} {
					if !strings.Contains(got.Command, want) {
						t.Fatalf("handoff does not contain selected value %q: %q", want, got.Command)
					}
				}
				for _, forbidden := range []string{canary, "https://v8std.ru/mcp/", "default branch", "ветк"} {
					if strings.Contains(got.Command, forbidden) {
						t.Fatalf("handoff contains forbidden value %q: %q", forbidden, got.Command)
					}
				}
				for _, other := range catalog.Toolchains() {
					if other.ID != selected.ID && (strings.Contains(got.Command, other.Origin) || strings.Contains(got.Command, other.Commit)) {
						t.Fatalf("unselected toolchain leaked: selected=%s other=%s handoff=%q", selected.ID, other.ID, got.Command)
					}
				}
			})
		}
	}
}
