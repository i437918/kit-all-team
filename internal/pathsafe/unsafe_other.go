//go:build !windows

package pathsafe

import "os"

func unsafeComponent(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}
