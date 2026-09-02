//go:build !windows

package hermes

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"golang.org/x/sys/unix"
)

func captureProfileRootIdentity(profileRoot string) (profileRootIdentity, error) {
	root, err := openProfileRoot(profileRoot)
	if err != nil {
		return profileRootIdentity{}, err
	}
	defer root.Close()
	return root.Identity(), nil
}

type otherProfileRoot struct {
	path     string
	fd       int
	identity profileRootIdentity
}

func openProfileRoot(profileRoot string) (openedProfileRoot, error) {
	if !filepath.IsAbs(profileRoot) {
		return nil, fmt.Errorf("profile root is not absolute")
	}
	if err := pathsafe.ValidateDirectory(profileRoot); err != nil {
		return nil, err
	}
	fd, err := openUnixNoFollow(profileRoot, true)
	if err != nil {
		return nil, err
	}
	key := unixHandleKey(fd)
	if key == "" {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("profile root identity is unavailable")
	}
	return &otherProfileRoot{path: profileRoot, fd: fd, identity: profileRootIdentity{Key: key}}, nil
}

func sameProfileRootIdentity(left, right profileRootIdentity) bool {
	return left.Key != "" && left.Key == right.Key
}

func (r *otherProfileRoot) Identity() profileRootIdentity { return r.identity }

func (r *otherProfileRoot) ReadLegacyOptOutMarker() ([]byte, error) {
	fd, _, err := openUnixChild(r.fd, ".no-bundled-skills", false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), ".no-bundled-skills")
	if file == nil {
		_ = unix.Close(fd)
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

func (r *otherProfileRoot) VerifyPath() error {
	fd, err := openUnixNoFollow(r.path, true)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if !sameProfileRootIdentity(r.identity, profileRootIdentity{Key: unixHandleKey(fd)}) {
		return fmt.Errorf("profile root identity changed")
	}
	return nil
}

func (r *otherProfileRoot) Close() error { return unix.Close(r.fd) }
