//go:build windows

package service

import "golang.org/x/sys/windows"

func officeCLIUserHome() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Profile, 0)
}
