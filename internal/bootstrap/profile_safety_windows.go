//go:build windows

package bootstrap

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

func unsafeProfileComponent(info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
