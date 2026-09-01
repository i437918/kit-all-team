//go:build windows

package hermes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unsafe"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"golang.org/x/sys/windows"
)

var bundledSkillName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// Test seam: production keeps it a no-op; tests deterministically exercise
// replacement between no-follow metadata lookup and handle acquisition.
var afterRuntimeLeafLstat = func(string) {}
var afterVerifiedInstallRootOpen = func() {}
var afterBundledSkillsReadBatch = func(string) {}
var afterBundledDirectoryHandleChange = func(int) {}

type windowsInstallRoot struct {
	rootHandle       windows.Handle
	executableHandle windows.Handle
	path             string
	executablePath   string
	identity         RuntimeIdentity
}

type windowsExecutablePin struct {
	path   string
	handle windows.Handle
	key    string
}

func pinRuntimeExecutable(path string) (runtimeExecutablePin, error) {
	handle, err := openWindowsNoFollowWithShare(path, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		_ = windows.CloseHandle(handle)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("executable is not regular")
	}
	return &windowsExecutablePin{path: path, handle: handle, key: windowsFileInfoKey(info)}, nil
}

func (p *windowsExecutablePin) VerifyPath() error {
	handle, err := openWindowsNoFollow(p.path)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	if windowsHandleKey(p.handle) != p.key || windowsHandleKey(handle) != p.key {
		return fmt.Errorf("executable identity changed")
	}
	return nil
}

func (p *windowsExecutablePin) Key() string  { return p.key }
func (p *windowsExecutablePin) Close() error { return windows.CloseHandle(p.handle) }

func openVerifiedInstallRoot(info RuntimeInfo) (openedInstallRoot, error) {
	if err := pathsafe.ValidateDirectory(info.InstallDir); err != nil {
		return nil, fmt.Errorf("%w: install root is unsafe", ErrConfigSchemaUnsupported)
	}
	installInfo, err := os.Lstat(info.InstallDir)
	if err != nil || !installInfo.IsDir() || installInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: install root is unsafe", ErrConfigSchemaUnsupported)
	}
	executableInfo, err := os.Lstat(info.Executable)
	if err != nil || !executableInfo.Mode().IsRegular() || executableInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: executable is unsafe", ErrConfigSchemaUnsupported)
	}
	rootHandle, err := openWindowsNoFollow(info.InstallDir)
	if err != nil {
		return nil, fmt.Errorf("%w: install root cannot be opened", ErrConfigSchemaUnsupported)
	}
	if !windowsHandleIsDirectory(rootHandle) {
		_ = windows.CloseHandle(rootHandle)
		return nil, fmt.Errorf("%w: install root is not a directory", ErrConfigSchemaUnsupported)
	}
	executableHandle, err := openWindowsNoFollow(info.Executable)
	if err != nil {
		_ = windows.CloseHandle(rootHandle)
		return nil, fmt.Errorf("%w: executable cannot be opened", ErrConfigSchemaUnsupported)
	}
	if !windowsHandleIsRegular(executableHandle) {
		_ = windows.CloseHandle(rootHandle)
		_ = windows.CloseHandle(executableHandle)
		return nil, fmt.Errorf("%w: executable is not regular", ErrConfigSchemaUnsupported)
	}
	afterVerifiedInstallRootOpen()
	identity := RuntimeIdentity{InstallRootKey: windowsHandleKey(rootHandle), ExecutableKey: windowsHandleKey(executableHandle)}
	return &windowsInstallRoot{rootHandle: rootHandle, executableHandle: executableHandle, path: info.InstallDir, executablePath: info.Executable, identity: identity}, nil
}

func (r *windowsInstallRoot) ReadRegular(ctx context.Context, relative string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !safeRuntimeRelative(relative) {
		return nil, fmt.Errorf("unsafe relative path")
	}
	if err := r.VerifyIdentity(r.identity); err != nil {
		return nil, err
	}
	baseline, _, err := r.openWindowsRelative(relative, false)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) || errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) || errors.Is(err, windows.STATUS_OBJECT_PATH_NOT_FOUND) || errors.Is(err, windows.STATUS_NO_SUCH_FILE) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	baselineKey := windowsHandleKey(baseline)
	_ = windows.CloseHandle(baseline)
	afterRuntimeLeafLstat(relative)
	handle, _, err := r.openWindowsRelative(relative, false)
	if err != nil || windowsHandleKey(handle) != baselineKey {
		if err == nil {
			_ = windows.CloseHandle(handle)
		}
		return nil, fmt.Errorf("opened regular file changed")
	}
	file := os.NewFile(uintptr(handle), relative)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("regular file handle is invalid")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil || int64(len(data)) > limit {
		return nil, fmt.Errorf("regular file exceeds limit")
	}
	if err := r.VerifyIdentity(r.identity); err != nil {
		return nil, err
	}
	return data, nil
}

func (r *windowsInstallRoot) WalkBundledSkills(ctx context.Context, limits bundledInventoryLimits) ([]bundledSkill, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.VerifyIdentity(r.identity); err != nil {
		return nil, err
	}
	root, _, err := r.openWindowsRelative("skills", true)
	if err != nil {
		return nil, err
	}
	afterBundledDirectoryHandleChange(1)
	directories := 0
	result, err := r.walkWindowsDirectory(ctx, root, "skills", 0, limits, &directories, new(int), new(int64))
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RelativePath < result[j].RelativePath })
	if err := r.VerifyIdentity(r.identity); err != nil {
		return nil, err
	}
	return result, nil
}

type windowsEntry struct {
	key       string
	size      int64
	directory bool
}

type windowsBundledDirectory struct {
	name     string
	relative string
	key      string
}

func (r *windowsInstallRoot) walkWindowsDirectory(ctx context.Context, handle windows.Handle, relative string, depth int, limits bundledInventoryLimits, directories, files *int, bytesRead *int64) ([]bundledSkill, error) {
	directory := os.NewFile(uintptr(handle), relative)
	if directory == nil {
		_ = windows.CloseHandle(handle)
		afterBundledDirectoryHandleChange(-1)
		return nil, fmt.Errorf("directory handle is invalid")
	}
	defer func() {
		_ = directory.Close()
		afterBundledDirectoryHandleChange(-1)
	}()
	var result []bundledSkill
	var pendingDirectories []windowsBundledDirectory
	maxPendingDirectories := limits.MaxDirectories - *directories
	if depth > 0 {
		maxPendingDirectories += skillSupportDirCount
	}
	if maxPendingDirectories < 0 {
		return nil, fmt.Errorf("bundled inventory exceeds directory limit")
	}
	hasSkill := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, readErr := directory.ReadDir(64)
		afterBundledSkillsReadBatch(relative)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			childRelative := filepath.Join(relative, entry.Name())
			if isExcludedSkillPath(childRelative) {
				continue
			}
			child, metadata, err := openWindowsChild(windows.Handle(directory.Fd()), entry.Name(), entry.IsDir())
			if err != nil {
				return nil, fmt.Errorf("bundled entry is redirected: %w", err)
			}
			if metadata.directory {
				afterBundledDirectoryHandleChange(1)
				pendingDirectories = append(pendingDirectories, windowsBundledDirectory{name: entry.Name(), relative: childRelative, key: metadata.key})
				_ = windows.CloseHandle(child)
				afterBundledDirectoryHandleChange(-1)
				if len(pendingDirectories) > maxPendingDirectories {
					return nil, fmt.Errorf("bundled inventory exceeds directory limit")
				}
				continue
			}
			*files++
			if *files > limits.MaxFiles {
				_ = windows.CloseHandle(child)
				return nil, fmt.Errorf("bundled inventory file limit exceeded")
			}
			if entry.Name() != "SKILL.md" {
				_ = windows.CloseHandle(child)
				continue
			}
			*bytesRead += metadata.size
			if *bytesRead > limits.MaxBytes {
				_ = windows.CloseHandle(child)
				return nil, fmt.Errorf("bundled inventory exceeds byte limit")
			}
			baseline := metadata.key
			_ = windows.CloseHandle(child)
			afterRuntimeLeafLstat(childRelative)
			child, metadata, err = openWindowsChild(windows.Handle(directory.Fd()), entry.Name(), false)
			if err != nil || metadata.key != baseline {
				if err == nil {
					_ = windows.CloseHandle(child)
				}
				return nil, fmt.Errorf("bundled skill changed")
			}
			data, readErr := readWindowsFrontmatter(ctx, child, limits.MaxFrontmatterBytes)
			if readErr != nil {
				return nil, readErr
			}
			skill, parseErr := parseBundledSkill(childRelative, data)
			if parseErr != nil {
				return nil, parseErr
			}
			hasSkill = true
			result = append(result, skill)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	sort.Slice(pendingDirectories, func(i, j int) bool { return pendingDirectories[i].relative < pendingDirectories[j].relative })
	for _, pending := range pendingDirectories {
		if depth > 0 && hasSkill && isSkillSupportDirectory(pending.name) {
			continue
		}
		if depth+1 > limits.MaxDepth {
			return nil, fmt.Errorf("bundled inventory exceeds depth limit")
		}
		if *directories >= limits.MaxDirectories {
			return nil, fmt.Errorf("bundled inventory exceeds directory limit")
		}
		child, metadata, err := openWindowsChild(windows.Handle(directory.Fd()), pending.name, true)
		if err != nil {
			return nil, fmt.Errorf("bundled entry is redirected: %w", err)
		}
		afterBundledDirectoryHandleChange(1)
		if metadata.key != pending.key {
			_ = windows.CloseHandle(child)
			afterBundledDirectoryHandleChange(-1)
			return nil, fmt.Errorf("bundled directory changed")
		}
		*directories++
		children, walkErr := r.walkWindowsDirectory(ctx, child, pending.relative, depth+1, limits, directories, files, bytesRead)
		if walkErr != nil {
			return nil, walkErr
		}
		result = append(result, children...)
	}
	return result, nil
}

func (r *windowsInstallRoot) openWindowsRelative(relative string, directory bool) (windows.Handle, windowsEntry, error) {
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	parent := r.rootHandle
	owned := false
	for index, part := range parts {
		child, entry, err := openWindowsChild(parent, part, index < len(parts)-1 || directory)
		if owned {
			_ = windows.CloseHandle(parent)
		}
		if err != nil {
			return windows.InvalidHandle, windowsEntry{}, err
		}
		if index == len(parts)-1 {
			return child, entry, nil
		}
		parent, owned = child, true
	}
	return windows.InvalidHandle, windowsEntry{}, fmt.Errorf("empty relative path")
}

func openWindowsChild(parent windows.Handle, name string, directory bool) (windows.Handle, windowsEntry, error) {
	return openWindowsChildWithShare(parent, name, directory, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE)
}

func openWindowsChildWithShare(parent windows.Handle, name string, directory bool, share uint32) (windows.Handle, windowsEntry, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return windows.InvalidHandle, windowsEntry{}, err
	}
	attributes := &windows.OBJECT_ATTRIBUTES{Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})), RootDirectory: parent, ObjectName: objectName, Attributes: windows.OBJ_CASE_INSENSITIVE}
	options := uint32(windows.FILE_OPEN_REPARSE_POINT | windows.FILE_SYNCHRONOUS_IO_NONALERT)
	if directory {
		options |= windows.FILE_DIRECTORY_FILE
	} else {
		options |= windows.FILE_NON_DIRECTORY_FILE
	}
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, windows.FILE_GENERIC_READ, attributes, &windows.IO_STATUS_BLOCK{}, nil, windows.FILE_ATTRIBUTE_NORMAL, share, windows.FILE_OPEN, options, 0, 0)
	if err != nil {
		return windows.InvalidHandle, windowsEntry{}, err
	}
	var info windows.ByHandleFileInformation
	fileType, typeErr := windows.GetFileType(handle)
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil || typeErr != nil || fileType != windows.FILE_TYPE_DISK || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory {
		_ = windows.CloseHandle(handle)
		if err != nil {
			return windows.InvalidHandle, windowsEntry{}, err
		}
		if typeErr != nil {
			return windows.InvalidHandle, windowsEntry{}, typeErr
		}
		return windows.InvalidHandle, windowsEntry{}, fmt.Errorf("redirected entry")
	}
	return handle, windowsEntry{key: windowsFileInfoKey(info), size: int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow), directory: directory}, nil
}

func readWindowsFrontmatter(ctx context.Context, handle windows.Handle, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	file := os.NewFile(uintptr(handle), "SKILL.md")
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("skill handle is invalid")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		data = data[:limit]
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		if !strings.HasPrefix(text, "---\n") || strings.Index(text[4:], "\n---\n") < 0 {
			return nil, fmt.Errorf("frontmatter exceeds limit")
		}
	}
	return data, nil
}

func (r *windowsInstallRoot) Identity() RuntimeIdentity { return r.identity }

func (r *windowsInstallRoot) VerifyIdentity(expected RuntimeIdentity) error {
	if expected != r.identity {
		return ErrConfigSchemaUnsupported
	}
	currentRoot, rootErr := openWindowsNoFollow(r.path)
	if rootErr == nil {
		defer windows.CloseHandle(currentRoot)
	}
	currentExecutable, executableErr := openWindowsNoFollow(r.executablePath)
	if executableErr == nil {
		defer windows.CloseHandle(currentExecutable)
	}
	if rootErr != nil || executableErr != nil || windowsHandleKey(currentRoot) != r.identity.InstallRootKey || windowsHandleKey(currentExecutable) != r.identity.ExecutableKey {
		return ErrConfigSchemaUnsupported
	}
	return nil
}

func (r *windowsInstallRoot) Close() error {
	var err error
	if closeErr := windows.CloseHandle(r.rootHandle); err == nil {
		err = closeErr
	}
	if closeErr := windows.CloseHandle(r.executableHandle); err == nil {
		err = closeErr
	}
	return err
}

func openWindowsNoFollow(path string) (windows.Handle, error) {
	return openWindowsNoFollowWithShare(path, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE)
}

func openWindowsNoFollowWithShare(path string, share uint32) (windows.Handle, error) {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	if volume == "" || !filepath.IsAbs(clean) {
		return windows.InvalidHandle, fmt.Errorf("path is not absolute")
	}
	volumeRoot := volume + string(filepath.Separator)
	parent, err := openWindowsPathNoFollow(volumeRoot, share)
	if err != nil {
		return windows.InvalidHandle, err
	}
	remainder := strings.TrimPrefix(clean, volumeRoot)
	if remainder == "" {
		return parent, nil
	}
	parts := strings.Split(remainder, string(filepath.Separator))
	for index, part := range parts {
		directory := index < len(parts)-1
		child, _, openErr := openWindowsChildWithShare(parent, part, directory, share)
		if index == len(parts)-1 && openErr != nil {
			child, _, openErr = openWindowsChildWithShare(parent, part, !directory, share)
		}
		_ = windows.CloseHandle(parent)
		if openErr != nil {
			return windows.InvalidHandle, openErr
		}
		if index == len(parts)-1 {
			return child, nil
		}
		parent = child
	}
	return windows.InvalidHandle, fmt.Errorf("empty path")
}

func windowsHandleIsDirectory(handle windows.Handle) bool {
	var info windows.ByHandleFileInformation
	fileType, err := windows.GetFileType(handle)
	return err == nil && fileType == windows.FILE_TYPE_DISK && windows.GetFileInformationByHandle(handle, &info) == nil && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 && info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
}

func windowsHandleIsRegular(handle windows.Handle) bool {
	var info windows.ByHandleFileInformation
	fileType, err := windows.GetFileType(handle)
	return err == nil && fileType == windows.FILE_TYPE_DISK && windows.GetFileInformationByHandle(handle, &info) == nil && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 && info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0
}

func openWindowsPathNoFollow(path string, share uint32) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, share, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return windows.InvalidHandle, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		if err != nil {
			return windows.InvalidHandle, err
		}
		return windows.InvalidHandle, fmt.Errorf("reparse point")
	}
	return handle, nil
}

func windowsHandleKey(handle windows.Handle) string {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return ""
	}
	return windowsFileInfoKey(info)
}

func windowsFileInfoKey(info windows.ByHandleFileInformation) string {
	return fmt.Sprintf("%08x:%08x:%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow)
}

func safeRuntimeRelative(relative string) bool {
	return relative != "" && !filepath.IsAbs(relative) && filepath.Clean(relative) == relative && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != ".."
}

func parseBundledSkill(relative string, data []byte) (bundledSkill, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return bundledSkill{}, fmt.Errorf("missing skill frontmatter")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return bundledSkill{}, fmt.Errorf("unterminated skill frontmatter")
	}
	var name string
	for _, line := range strings.Split(text[4:4+end], "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			if name != "" {
				return bundledSkill{}, fmt.Errorf("duplicate skill name")
			}
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), "\"'")
		}
	}
	if name == "" {
		name = filepath.Base(filepath.Dir(relative))
	}
	if !bundledSkillName.MatchString(name) {
		return bundledSkill{}, fmt.Errorf("skill name is unsafe")
	}
	return bundledSkill{Name: name, RelativePath: filepath.ToSlash(relative)}, nil
}
