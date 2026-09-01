package workspace

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
)

const wizardFile = ".teamkit/wizard.env"

var addWizardKeys = map[string]bool{
	"TEAMKIT_APP_ID":         true,
	"TEAMKIT_MODE":           true,
	"TEAMKIT_PROJECT_ID":     true,
	"TEAMKIT_ROLE_ID":        true,
	"TEAMKIT_TOOLCHAIN_ID":   true,
	"TEAMKIT_WORKSPACE_ROOT": true,
}

var updateWizardKeys = map[string]bool{
	"TEAMKIT_APP_ID":         true,
	"TEAMKIT_MODE":           true,
	"TEAMKIT_UPDATE_SCOPE":   true,
	"TEAMKIT_WORKSPACE_ROOT": true,
}

// WizardEnvPath returns the private pre-plan checkpoint location for root.
func WizardEnvPath(root string) string { return filepath.Join(root, wizardFile) }

// WriteWizardEnv stores one validated, public-only wizard checkpoint. The
// checkpoint is not an environment identity and cannot make a workspace ready.
func WriteWizardEnv(root string, values map[string]string) error {
	canonical, err := canonicalWizardRoot(root)
	if err != nil {
		return checkpointError(err)
	}
	if err := validateWizardValues(canonical, values); err != nil {
		return err
	}
	if err := pathsafe.EnsureDirectory(canonical, 0o700); err != nil {
		return checkpointError(err)
	}
	metadata := filepath.Join(canonical, ".teamkit")
	if err := pathsafe.EnsureDirectory(metadata, 0o700); err != nil {
		return checkpointError(err)
	}
	path := WizardEnvPath(canonical)
	if err := privatefile.Validate(path); err != nil {
		return checkpointError(err)
	}
	content, err := encodePublicEnv(values)
	if err != nil {
		return checkpointError(err)
	}
	if err := privatefile.WriteAtomic(path, content, 0o600); err != nil {
		return checkpointError(err)
	}
	if err := privatefile.Validate(path); err != nil {
		return checkpointError(err)
	}
	return nil
}

func isWizardCheckpoint(root string) (bool, error) {
	canonical, err := canonicalWizardRoot(root)
	if err != nil {
		return false, err
	}
	metadata := filepath.Join(canonical, ".teamkit")
	entries, err := os.ReadDir(metadata)
	if err != nil {
		return false, err
	}
	if len(entries) != 1 || entries[0].Name() != "wizard.env" || entries[0].IsDir() {
		return false, nil
	}
	path := WizardEnvPath(canonical)
	file, err := privatefile.OpenValidated(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 64<<10))
	if err != nil {
		return false, err
	}
	values, err := parseWizardEnv(content)
	if err != nil {
		return false, nil
	}
	return validateWizardValues(canonical, values) == nil, nil
}

func canonicalWizardRoot(root string) (string, error) {
	if strings.HasPrefix(root, `\\`) || strings.HasPrefix(root, "//") {
		return "", fmt.Errorf("UNC workspace paths are not allowed")
	}
	return pathsafe.CanonicalPath(root)
}

func validateWizardValues(root string, values map[string]string) error {
	mode := values["TEAMKIT_MODE"]
	allowed := addWizardKeys
	switch mode {
	case "add":
		allowed = addWizardKeys
	case "update":
		allowed = updateWizardKeys
	default:
		return checkpointError(fmt.Errorf("mode is invalid"))
	}
	if len(values) != len(allowed) {
		return checkpointError(fmt.Errorf("unexpected checkpoint keys"))
	}
	for key, value := range values {
		if !allowed[key] || strings.TrimSpace(value) == "" || isSecretKey(key) || strings.ContainsAny(value, "\r\n") {
			return checkpointError(fmt.Errorf("checkpoint entry is invalid"))
		}
	}
	workspace, err := canonicalWizardRoot(values["TEAMKIT_WORKSPACE_ROOT"])
	if err != nil || workspace != root {
		return checkpointError(fmt.Errorf("workspace root does not match checkpoint location"))
	}
	return nil
}

func parseWizardEnv(content []byte) (map[string]string, error) {
	values := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(content), "\n"), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" || strings.ContainsAny(key, "\r\n") || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("invalid checkpoint encoding")
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("duplicate checkpoint key")
		}
		values[key] = value
	}
	return values, nil
}

func checkpointError(err error) error {
	if err == nil {
		return nil
	}
	var coded *Error
	if errors.As(err, &coded) && coded.Code == "WIZARD_CHECKPOINT_INVALID" {
		return err
	}
	return &Error{Code: "WIZARD_CHECKPOINT_INVALID", Err: err}
}
