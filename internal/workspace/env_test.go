package workspace

import (
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePublicEnv_RejectsSecretKeys(t *testing.T) {
	err := WritePublicEnv(filepath.Join(testutil.TempDir(t), ".env"), map[string]string{
		"KIT_ALL_TEAM_PROJECT": "alpha",
		"API_TOKEN":            "teamkit-secret-canary",
	})
	if ErrorCode(err) != "SECRET_KEY_FORBIDDEN" {
		t.Fatalf("error = %v, code %q; want SECRET_KEY_FORBIDDEN", err, ErrorCode(err))
	}
}

func TestWritePublicEnv_WritesSortedPublicVariables(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), ".env")
	if err := WritePublicEnv(path, map[string]string{
		"KIT_ALL_TEAM_ROLE":    "developer",
		"KIT_ALL_TEAM_PROJECT": "alpha",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "KIT_ALL_TEAM_PROJECT=alpha\nKIT_ALL_TEAM_ROLE=developer\n"
	if string(got) != want {
		t.Fatalf("env = %q; want %q", got, want)
	}
	if strings.Contains(string(got), "secret-canary") {
		t.Fatal("secret canary leaked into public environment")
	}
}
