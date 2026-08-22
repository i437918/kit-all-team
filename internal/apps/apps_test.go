package apps

import (
	"errors"
	"fmt"
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

func TestPrepareHandoff_EmitsOnePasteReadySecretFreeCommand(t *testing.T) {
	const canary = "do-not-leak-api-key"
	handoff, err := PrepareHandoff(Application{ID: "codex", Installed: true}, HandoffRequest{
		Toolchain:     Toolchain{Name: "cc_1c_skills", Version: catalog.Toolchains()[0].Commit},
		V8StdEndpoint: catalog.V8StdMCP().Endpoint,
		SecretValues:  []string{canary},
	})
	if err != nil {
		t.Fatalf("PrepareHandoff() error = %v", err)
	}
	if strings.Count(handoff.Command, "\n") != 0 {
		t.Fatalf("handoff must be one command, got %q", handoff.Command)
	}
	for _, want := range []string{"codex", "exactly one toolchain", catalog.Toolchains()[0].Origin, catalog.Toolchains()[0].Commit, "v8std", catalog.V8StdMCP().Endpoint} {
		if !strings.Contains(handoff.Command, want) {
			t.Fatalf("handoff command does not contain %q: %s", want, handoff.Command)
		}
	}
	if strings.Contains(handoff.Command, canary) {
		t.Fatalf("handoff leaks secret canary: %s", handoff.Command)
	}
}

func TestPrepareHandoff_AllAlternativeApplicationsAndToolchainsAreExclusive(t *testing.T) {
	const canary = "TEAMKIT_SECRET_CANARY"
	applications := SupportedApplications()
	toolchains := catalog.Toolchains()
	if len(applications) != 10 || len(toolchains) != 2 {
		t.Fatalf("matrix dimensions = %d applications x %d toolchains, want 10 x 2", len(applications), len(toolchains))
	}
	for _, application := range applications {
		for _, selected := range toolchains {
			t.Run(string(application)+"/"+string(selected.ID), func(t *testing.T) {
				got, err := PrepareHandoff(Application{ID: string(application), Installed: true}, HandoffRequest{
					Toolchain:     Toolchain{Name: string(selected.ID), Origin: selected.Origin, Version: selected.Commit},
					V8StdEndpoint: catalog.V8StdMCP().Endpoint,
					SecretValues:  []string{canary},
				})
				if err != nil {
					t.Fatal(err)
				}
				want := fmt.Sprintf("In %s, configure exactly one toolchain from %s pinned to commit %s, then configure the separate v8std MCP endpoint %s.", application, selected.Origin, selected.Commit, catalog.V8StdMCP().Endpoint)
				if got.Command != want {
					t.Fatalf("handoff=%q want=%q", got.Command, want)
				}
				for _, other := range toolchains {
					if other.ID != selected.ID && (strings.Contains(got.Command, other.Origin) || strings.Contains(got.Command, other.Commit)) {
						t.Fatalf("unselected toolchain leaked: %q", got.Command)
					}
				}
				if strings.Contains(got.Command, canary) {
					t.Fatalf("secret leaked: %q", got.Command)
				}
			})
		}
	}
}
