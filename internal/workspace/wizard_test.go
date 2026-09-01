package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestWriteWizardEnv_AddCheckpointIsPrivateAndClassified(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	values := map[string]string{
		"TEAMKIT_APP_ID":         "hermes",
		"TEAMKIT_MODE":           "add",
		"TEAMKIT_PROJECT_ID":     "asku",
		"TEAMKIT_ROLE_ID":        "architect",
		"TEAMKIT_TOOLCHAIN_ID":   "cc_1c_skills",
		"TEAMKIT_WORKSPACE_ROOT": root,
	}

	if err := WriteWizardEnv(root, values); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(WizardEnvPath(root))
	if err != nil {
		t.Fatal(err)
	}
	want := "TEAMKIT_APP_ID=hermes\nTEAMKIT_MODE=add\nTEAMKIT_PROJECT_ID=asku\nTEAMKIT_ROLE_ID=architect\nTEAMKIT_TOOLCHAIN_ID=cc_1c_skills\nTEAMKIT_WORKSPACE_ROOT=" + root + "\n"
	if string(contents) != want {
		t.Fatalf("wizard.env = %q; want %q", contents, want)
	}
	state, err := Classify(root)
	if err != nil || state != Checkpoint {
		t.Fatalf("Classify() = %q, %v; want %q, nil", state, err, Checkpoint)
	}
}

func TestWriteWizardEnv_RejectsSecretAndWrongWorkspace(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	for name, values := range map[string]map[string]string{
		"secret": {
			"TEAMKIT_APP_ID":         "codex",
			"TEAMKIT_MODE":           "add",
			"TEAMKIT_PROJECT_ID":     "asku",
			"TEAMKIT_ROLE_ID":        "architect",
			"TEAMKIT_TOOLCHAIN_ID":   "cc_1c_skills",
			"TEAMKIT_WORKSPACE_ROOT": root,
			"TEAMKIT_SOURCE_TOKEN":           "token-canary",
		},
		"wrong workspace": {
			"TEAMKIT_APP_ID":         "codex",
			"TEAMKIT_MODE":           "add",
			"TEAMKIT_PROJECT_ID":     "asku",
			"TEAMKIT_ROLE_ID":        "architect",
			"TEAMKIT_TOOLCHAIN_ID":   "cc_1c_skills",
			"TEAMKIT_WORKSPACE_ROOT": filepath.Join(root, "other"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := WriteWizardEnv(root, values); ErrorCode(err) != "WIZARD_CHECKPOINT_INVALID" {
				t.Fatalf("WriteWizardEnv() error = %v, code %q; want WIZARD_CHECKPOINT_INVALID", err, ErrorCode(err))
			}
		})
	}
}

func TestWriteWizardEnv_UpdateUsesOnlyPersistedIdentityAndScope(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	if err := WriteWizardEnv(root, map[string]string{
		"TEAMKIT_APP_ID":         "codex",
		"TEAMKIT_MODE":           "update",
		"TEAMKIT_UPDATE_SCOPE":   "content",
		"TEAMKIT_WORKSPACE_ROOT": root,
	}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(WizardEnvPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), "TEAMKIT_APP_ID=codex\nTEAMKIT_MODE=update\nTEAMKIT_UPDATE_SCOPE=content\nTEAMKIT_WORKSPACE_ROOT="+root+"\n"; got != want {
		t.Fatalf("wizard.env = %q; want %q", got, want)
	}
	for _, forbidden := range []string{"TEAMKIT_PROJECT_ID", "TEAMKIT_ROLE_ID", "TEAMKIT_TOOLCHAIN_ID"} {
		if strings.Contains(string(contents), forbidden) {
			t.Fatalf("update checkpoint contains editable %s", forbidden)
		}
	}
}

func TestEnsureOwner_ClaimsValidCheckpoint(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "workspace")
	if err := WriteWizardEnv(root, map[string]string{
		"TEAMKIT_APP_ID":         "codex",
		"TEAMKIT_MODE":           "add",
		"TEAMKIT_PROJECT_ID":     "asku",
		"TEAMKIT_ROLE_ID":        "architect",
		"TEAMKIT_TOOLCHAIN_ID":   "cc_1c_skills",
		"TEAMKIT_WORKSPACE_ROOT": root,
	}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOwner(root, "asku"); err != nil {
		t.Fatal(err)
	}
	state, err := Classify(root)
	if err != nil || state != Managed {
		t.Fatalf("Classify() = %q, %v; want %q, nil", state, err, Managed)
	}
}
