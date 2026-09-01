//go:build windows

package privatefile

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"golang.org/x/sys/windows"
)

var afterNormalizeOpen = func(string) error { return nil }

type windowsFileIdentity struct {
	volume uint32
	high   uint32
	low    uint32
}

// NormalizeOwnerOnly protects an existing regular file with a current-user-only
// DACL without reading or rewriting its contents through the path.
func NormalizeOwnerOnly(path string) error {
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return normalizeWindowsError(err)
	}
	handle, err := windows.CreateFile(
		pathUTF16,
		windows.GENERIC_READ|windows.READ_CONTROL|windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return normalizeWindowsError(err)
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return normalizeWindowsError(fmt.Errorf("secret file handle is invalid"))
	}
	defer file.Close()

	identity, err := windowsRegularIdentity(handle)
	if err != nil {
		return normalizeWindowsError(err)
	}
	before, err := hashOpenFile(file)
	if err != nil {
		return normalizeWindowsError(err)
	}
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return normalizeWindowsError(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return normalizeWindowsError(err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return normalizeWindowsError(fmt.Errorf("secret file owner is unsafe"))
	}
	if err := afterNormalizeOpen(path); err != nil {
		return normalizeWindowsError(err)
	}

	ownerOnly, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return normalizeWindowsError(err)
	}
	dacl, _, err := ownerOnly.DACL()
	if err != nil || dacl == nil {
		return normalizeWindowsError(fmt.Errorf("owner-only DACL is unavailable"))
	}
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return normalizeWindowsError(err)
	}
	runtime.KeepAlive(ownerOnly)

	postDescriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return normalizeWindowsError(err)
	}
	if err := validateWindowsSecurityDescriptor(postDescriptor, user.User.Sid); err != nil {
		return normalizeWindowsError(err)
	}
	afterIdentity, err := windowsRegularIdentity(handle)
	if err != nil || afterIdentity != identity {
		return normalizeWindowsError(fmt.Errorf("secret file identity changed"))
	}
	after, err := hashOpenFile(file)
	if err != nil || after != before {
		return normalizeWindowsError(fmt.Errorf("secret file contents changed"))
	}
	if err := windowsPathStillNamesIdentity(path, identity); err != nil {
		return normalizeWindowsError(err)
	}
	return nil
}

func windowsRegularIdentity(handle windows.Handle) (windowsFileIdentity, error) {
	fileType, err := windows.GetFileType(handle)
	if err != nil {
		return windowsFileIdentity{}, err
	}
	if fileType != windows.FILE_TYPE_DISK {
		return windowsFileIdentity{}, fmt.Errorf("secret file handle is not a disk file")
	}
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		return windowsFileIdentity{}, err
	}
	if information.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return windowsFileIdentity{}, fmt.Errorf("secret file is redirected or not regular")
	}
	return windowsFileIdentity{volume: information.VolumeSerialNumber, high: information.FileIndexHigh, low: information.FileIndexLow}, nil
}

func windowsPathStillNamesIdentity(path string, want windowsFileIdentity) error {
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(pathUTF16, windows.GENERIC_READ|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	got, err := windowsRegularIdentity(handle)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("secret path was replaced")
	}
	return nil
}

func hashOpenFile(file *os.File) ([sha256.Size]byte, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return [sha256.Size]byte{}, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [sha256.Size]byte{}, err
	}
	var sum [sha256.Size]byte
	copy(sum[:], hash.Sum(nil))
	return sum, nil
}

func normalizeWindowsError(err error) error {
	if errors.Is(err, ErrUnsafePermissions) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUnsafePermissions, err)
}
