//go:build windows

package privatefile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const privateTempAttempts = 32

// CreateTemp creates a Windows file with a protected DACL containing exactly
// one full-control ACE for the current process owner. The DACL is supplied to
// CreateFile, so inherited readers never gain access between creation and ACL
// hardening.
func CreateTemp(directory, prefix, suffix string, _ fs.FileMode) (*os.File, error) {
	if strings.ContainsAny(prefix+suffix, `/\\`) {
		return nil, fmt.Errorf("private temporary name is invalid")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := user.User.Sid.String()
	descriptor, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;;FA;;;" + sid + ")")
	if err != nil {
		return nil, err
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	for attempt := 0; attempt < privateTempAttempts; attempt++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, err
		}
		path := filepath.Join(directory, prefix+hex.EncodeToString(random[:])+suffix)
		pathUTF16, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return nil, err
		}
		handle, err := windows.CreateFile(
			pathUTF16,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0,
			&attributes,
			windows.CREATE_NEW,
			windows.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		runtime.KeepAlive(descriptor)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(handle), path)
		if file == nil {
			_ = windows.CloseHandle(handle)
			return nil, fmt.Errorf("private temporary file handle is invalid")
		}
		return file, nil
	}
	return nil, fmt.Errorf("private temporary filename collision")
}

func validatePermissions(path string, _ os.FileInfo) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	return validateWindowsSecurityDescriptor(descriptor, user.User.Sid)
}

func validateWindowsSecurityDescriptor(descriptor *windows.SECURITY_DESCRIPTOR, currentUser *windows.SID) error {
	if descriptor == nil || currentUser == nil {
		return fmt.Errorf("security owner cannot be determined")
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !owner.Equals(currentUser) {
		return fmt.Errorf("file is not owned by the current user")
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("DACL inheritance is enabled")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if dacl == nil || dacl.AceCount != 1 {
		return fmt.Errorf("DACL is not owner-only")
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		return err
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		return fmt.Errorf("DACL entry is not an allow entry")
	}
	entrySID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if !entrySID.Equals(currentUser) {
		return fmt.Errorf("DACL entry does not belong to the current user")
	}
	return nil
}
