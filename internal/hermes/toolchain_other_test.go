//go:build !windows

package hermes

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestMaterializeToolchain_POSIXRejectsBackslashSkillBeforeProfileMutation(t *testing.T) {
	pin, err := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	source, profile := testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{
		`invalid\name/SKILL.md`: "# invalid\n",
	})
	if err := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); !errors.Is(err, ErrToolchainLayout) {
		t.Fatalf("error = %v, want ErrToolchainLayout", err)
	}
	for _, relative := range []string{".teamkit", "external", "skills"} {
		if _, err := os.Lstat(filepath.Join(profile, relative)); !os.IsNotExist(err) {
			t.Fatalf("profile mutated at %s: %v", relative, err)
		}
	}
}
