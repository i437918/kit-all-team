//go:build !windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigureHermesHome sets HERMES_HOME for child processes. POSIX installers
// also receive the home as a fixed, explicit argument.
func ConfigureHermesHome(home string) error {
	if !filepath.IsAbs(home) {
		return fmt.Errorf("HERMES_HOME_INVALID")
	}
	return os.Setenv("HERMES_HOME", filepath.Clean(home))
}
