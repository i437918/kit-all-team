//go:build windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// ConfigureHermesHome sets the current process value and persists the selected
// home in the current user's Windows environment without requiring elevation.
func ConfigureHermesHome(home string) error {
	if !filepath.IsAbs(home) {
		return fmt.Errorf("HERMES_HOME_INVALID")
	}
	clean := filepath.Clean(home)
	if err := os.Setenv("HERMES_HOME", clean); err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("HERMES_HOME_PERSIST_FAILED: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue("HERMES_HOME", clean); err != nil {
		return fmt.Errorf("HERMES_HOME_PERSIST_FAILED: %w", err)
	}
	return nil
}
