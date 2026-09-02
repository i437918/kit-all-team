//go:build windows

package hermes

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"golang.org/x/sys/windows"
)

func captureProfileRootIdentity(profileRoot string) (profileRootIdentity, error) {
	root, err := openProfileRoot(profileRoot)
	if err != nil {
		return profileRootIdentity{}, err
	}
	defer root.Close()
	return root.Identity(), nil
}

type windowsProfileRoot struct {
	path     string
	handle   windows.Handle
	identity profileRootIdentity
}

func openProfileRoot(profileRoot string) (openedProfileRoot, error) {
	if !filepath.IsAbs(profileRoot) {
		return nil, fmt.Errorf("profile root is not absolute")
	}
	if err := pathsafe.ValidateDirectory(profileRoot); err != nil {
		return nil, err
	}
	handle, err := openWindowsNoFollowWithShare(profileRoot, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE)
	if err != nil {
		return nil, err
	}
	if !windowsHandleIsDirectory(handle) {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("profile root is not a directory")
	}
	key := windowsHandleKey(handle)
	if key == "" {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("profile root identity is unavailable")
	}
	return &windowsProfileRoot{path: profileRoot, handle: handle, identity: profileRootIdentity{Key: key}}, nil
}

func sameProfileRootIdentity(left, right profileRootIdentity) bool {
	return left.Key != "" && left.Key == right.Key
}

func (r *windowsProfileRoot) Identity() profileRootIdentity { return r.identity }

func (r *windowsProfileRoot) ReadLegacyOptOutMarker() ([]byte, error) {
	handle, _, err := openWindowsChild(r.handle, ".no-bundled-skills", false)
	if err != nil {
		if err == windows.ERROR_FILE_NOT_FOUND || err == windows.ERROR_PATH_NOT_FOUND || err == windows.STATUS_OBJECT_NAME_NOT_FOUND || err == windows.STATUS_OBJECT_PATH_NOT_FOUND || err == windows.STATUS_NO_SUCH_FILE {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	file := os.NewFile(uintptr(handle), ".no-bundled-skills")
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("marker handle is invalid")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxLegacyOptOutMarkerBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxLegacyOptOutMarkerBytes {
		return nil, fmt.Errorf("marker exceeds limit")
	}
	return data, nil
}

func (r *windowsProfileRoot) VerifyPath() error {
	handle, err := openWindowsNoFollowWithShare(r.path, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	if !windowsHandleIsDirectory(handle) || !sameProfileRootIdentity(r.identity, profileRootIdentity{Key: windowsHandleKey(handle)}) {
		return fmt.Errorf("profile root identity changed")
	}
	return nil
}

func (r *windowsProfileRoot) Close() error { return windows.CloseHandle(r.handle) }
