//go:build windows

package pathsafe

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

const volumeNameDOS = 0

func canonicalPath(path string) (string, error) {
	return canonicalPathWithResolver(path, finalPath)
}

func canonicalPathWithResolver(path string, resolve func(string) (string, error)) (string, error) {
	existing, suffix, err := existingPathPrefix(path)
	if err != nil {
		return "", err
	}
	canonical, err := resolve(existing)
	if err != nil {
		return "", fmt.Errorf("cannot canonicalize existing path prefix: %w", err)
	}
	for index := len(suffix) - 1; index >= 0; index-- {
		canonical = filepath.Join(canonical, suffix[index])
	}
	return filepath.Clean(canonical), nil
}

func comparisonKey(path string) string { return strings.ToLower(filepath.Clean(path)) }

func existingPathPrefix(path string) (string, []string, error) {
	current := path
	var suffix []string
	for {
		_, err := os.Lstat(current)
		if err == nil {
			return current, suffix, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", nil, err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil, unsafeError(path, "no existing path prefix")
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func finalPath(path string) (string, error) {
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		pathUTF16,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 512)
	for {
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), volumeNameDOS)
		if err != nil {
			return "", err
		}
		if int(length) < len(buffer) {
			resolved := windows.UTF16ToString(buffer[:length])
			if strings.HasPrefix(resolved, `\\?\UNC\`) {
				return `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`), nil
			}
			return strings.TrimPrefix(resolved, `\\?\`), nil
		}
		buffer = make([]uint16, int(length)+1)
	}
}
