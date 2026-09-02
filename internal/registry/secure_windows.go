//go:build windows

package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
	"golang.org/x/sys/windows"
)

const registryFileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff

func ensureRegistryDirectory(path string) error {
	if err := pathsafe.ValidateDirectory(path); err != nil {
		return err
	}
	clean := filepath.Clean(path)
	var missing []string
	current := clean
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: registry directory is redirected", pathsafe.ErrUnsafe)
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("%w: filesystem root does not exist", pathsafe.ErrUnsafe)
		}
		missing = append(missing, current)
		current = parent
	}

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sid := user.User.Sid.String()
	descriptor, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;;FA;;;" + sid + ")")
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	for index := len(missing) - 1; index >= 0; index-- {
		pathUTF16, err := windows.UTF16PtrFromString(missing[index])
		if err != nil {
			return err
		}
		err = windows.CreateDirectory(pathUTF16, &attributes)
		runtime.KeepAlive(descriptor)
		if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return err
		}
		if err := validateRegistryDirectory(missing[index]); err != nil {
			return err
		}
	}
	return validateRegistryDirectory(clean)
}

func validateRegistryDirectory(path string) error {
	if err := pathsafe.ValidateDirectory(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: registry directory is redirected", privatefile.ErrUnsafePermissions)
	}
	if attributes, ok := info.Sys().(*syscall.Win32FileAttributeData); ok && attributes.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("%w: registry directory is redirected", pathsafe.ErrUnsafe)
	}
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	current, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(current.User.Sid) {
		return fmt.Errorf("%w: registry directory owner is not current user", privatefile.ErrUnsafePermissions)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("%w: registry directory DACL is not protected", privatefile.ErrUnsafePermissions)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 1 {
		return fmt.Errorf("%w: registry directory DACL is not owner-only", privatefile.ErrUnsafePermissions)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		return err
	}
	entrySID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !entrySID.Equals(current.User.Sid) || ace.Mask != registryFileAllAccess {
		return fmt.Errorf("%w: registry directory DACL does not grant current-user full access", privatefile.ErrUnsafePermissions)
	}
	return nil
}

func replaceRegistryFile(source, target string) error {
	sourceUTF16, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetUTF16, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(sourceUTF16, targetUTF16, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
