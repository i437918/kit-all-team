package workspace

import (
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitignore_AddsRequiredEntriesWithoutDroppingExisting(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), ".gitignore")
	if err := os.WriteFile(path, []byte("dist/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGitignore(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"dist/", ".env", "/db/", "/.teamkit/"} {
		if !containsLine(string(got), entry) {
			t.Fatalf("gitignore missing %q: %q", entry, got)
		}
	}
}

func TestEnsureGitignore_AddsLiteralEntriesWhenWhitespaceVariantsExist(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), ".gitignore")
	if err := os.WriteFile(path, []byte("  .env  \n/db/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGitignore(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{".env", "/db/", "/.teamkit/"} {
		if !containsLine(string(got), entry) {
			t.Fatalf("gitignore missing literal %q: %q", entry, got)
		}
	}
}

func TestEnsureLocalExclude_PreservesSafeExistingRules(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "exclude")
	const existing = "# local developer rules\n*.scratch\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLocalExclude(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), existing) {
		t.Fatalf("local exclude lost safe existing rules: %q", got)
	}
	for _, entry := range []string{".env", "/db/", "/.teamkit/", "/.gitignore"} {
		if !containsLine(string(got), entry) {
			t.Fatalf("local exclude missing %q: %q", entry, got)
		}
	}
}

func containsLine(text, want string) bool {
	for _, line := range splitLines(text) {
		if line == want {
			return true
		}
	}
	return false
}
