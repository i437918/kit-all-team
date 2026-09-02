package registry

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
)

var createRegistryTemp = privatefile.CreateTemp

func writeRegistryAtomic(target string, data []byte) error {
	directory := filepath.Dir(target)
	if err := ensureRegistryDirectory(directory); err != nil {
		return err
	}
	if err := validateRegistryDirectory(directory); err != nil {
		return err
	}
	if err := privatefile.Validate(target); err != nil {
		return err
	}
	temporary, err := createRegistryTemp(directory, ".teamkit-registry-", ".tmp", fs.FileMode(0o600))
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := validateRegistryDirectory(directory); err != nil {
		return err
	}
	if err := privatefile.Validate(temporaryPath); err != nil {
		return err
	}
	if err := privatefile.Validate(target); err != nil {
		return err
	}
	if err := replaceRegistryFile(temporaryPath, target); err != nil {
		return err
	}
	return privatefile.Validate(target)
}
