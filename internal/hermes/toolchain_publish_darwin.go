//go:build darwin

package hermes

import "golang.org/x/sys/unix"

func renameToolchainNoReplace(source, target string) error {
	return unix.RenamexNp(source, target, unix.RENAME_EXCL)
}
