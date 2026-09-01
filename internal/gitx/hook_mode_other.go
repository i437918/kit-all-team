//go:build !windows

package gitx

import (
	"io"
	"io/fs"
	"os"
)

func hookModeReady(info fs.FileInfo) bool {
	return info.Mode().Perm()&0o111 == 0o111
}

func repairManagedHookMode(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	opened, err := file.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return ErrHooksPath
	}
	contents, err := io.ReadAll(io.LimitReader(file, int64(len(expected)+1)))
	if err != nil {
		return err
	}
	if string(contents) != expected {
		return ErrHookCollision
	}
	return file.Chmod(0o755)
}
