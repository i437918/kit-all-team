//go:build !windows

package privatefile

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

var afterNormalizeOpen = func(string) error { return nil }

type posixFileIdentity struct {
	device uint64
	inode  uint64
}

// NormalizeOwnerOnly changes an existing regular file to mode 0600 through a
// no-follow descriptor while preserving its exact contents.
func NormalizeOwnerOnly(path string) error {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return normalizePOSIXError(err)
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return normalizePOSIXError(fmt.Errorf("secret file descriptor is invalid"))
	}
	defer file.Close()

	identity, err := posixRegularIdentity(file)
	if err != nil {
		return normalizePOSIXError(err)
	}
	info, err := file.Stat()
	if err != nil {
		return normalizePOSIXError(err)
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata == nil || metadata.Uid != uint32(os.Geteuid()) {
		return normalizePOSIXError(fmt.Errorf("secret file owner is unsafe"))
	}
	before, err := hashOpenPOSIXFile(file)
	if err != nil {
		return normalizePOSIXError(err)
	}
	if err := afterNormalizeOpen(path); err != nil {
		return normalizePOSIXError(err)
	}
	if err := file.Chmod(0o600); err != nil {
		return normalizePOSIXError(err)
	}
	postInfo, err := file.Stat()
	if err != nil || !postInfo.Mode().IsRegular() || postInfo.Mode().Perm() != 0o600 {
		return normalizePOSIXError(fmt.Errorf("secret file postcondition failed"))
	}
	afterIdentity, err := posixRegularIdentity(file)
	if err != nil || afterIdentity != identity {
		return normalizePOSIXError(fmt.Errorf("secret file identity changed"))
	}
	after, err := hashOpenPOSIXFile(file)
	if err != nil || after != before {
		return normalizePOSIXError(fmt.Errorf("secret file contents changed"))
	}
	if err := posixPathStillNamesIdentity(path, identity); err != nil {
		return normalizePOSIXError(err)
	}
	return nil
}

func posixRegularIdentity(file *os.File) (posixFileIdentity, error) {
	info, err := file.Stat()
	if err != nil {
		return posixFileIdentity{}, err
	}
	if !info.Mode().IsRegular() {
		return posixFileIdentity{}, fmt.Errorf("secret file handle is not regular")
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata == nil {
		return posixFileIdentity{}, fmt.Errorf("secret file identity is unavailable")
	}
	return posixFileIdentity{device: uint64(metadata.Dev), inode: metadata.Ino}, nil
}

func posixPathStillNamesIdentity(path string, want posixFileIdentity) error {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return fmt.Errorf("secret path descriptor is invalid")
	}
	defer file.Close()
	got, err := posixRegularIdentity(file)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("secret path was replaced")
	}
	return nil
}

func hashOpenPOSIXFile(file *os.File) ([sha256.Size]byte, error) {
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

func normalizePOSIXError(err error) error {
	if errors.Is(err, ErrUnsafePermissions) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUnsafePermissions, err)
}
