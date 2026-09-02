//go:build !windows

package bootstrap

import "os"

func unsafeProfileComponent(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}
