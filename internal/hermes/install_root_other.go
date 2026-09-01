//go:build !windows

package hermes

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"golang.org/x/sys/unix"
)

var bundledSkillName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

var afterRuntimeLeafLstat = func(string) {}
var afterVerifiedInstallRootOpen = func() {}
var afterBundledSkillsReadBatch = func(string) {}
var afterBundledDirectoryHandleChange = func(int) {}

type otherInstallRoot struct {
	rootFD, executableFD int
	path, executablePath string
	identity             RuntimeIdentity
}

type otherExecutablePin struct {
	path string
	fd   int
	key  string
}

func pinRuntimeExecutable(path string) (runtimeExecutablePin, error) {
	fd, err := openUnixNoFollow(path, false)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("executable is not regular")
	}
	return &otherExecutablePin{path: path, fd: fd, key: unixHandleKey(fd)}, nil
}

func (p *otherExecutablePin) VerifyPath() error {
	fd, err := openUnixNoFollow(p.path, false)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if unixHandleKey(p.fd) != p.key || unixHandleKey(fd) != p.key {
		return fmt.Errorf("executable identity changed")
	}
	return nil
}

func (p *otherExecutablePin) Key() string  { return p.key }
func (p *otherExecutablePin) Close() error { return unix.Close(p.fd) }

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
	rootFD, err := openUnixNoFollow(info.InstallDir, true)
	if err != nil {
		return nil, fmt.Errorf("%w: install root cannot be opened", ErrConfigSchemaUnsupported)
	}
	executableFD, err := openUnixNoFollow(info.Executable, false)
	if err != nil {
		_ = unix.Close(rootFD)
		return nil, fmt.Errorf("%w: executable cannot be opened", ErrConfigSchemaUnsupported)
	}
	afterVerifiedInstallRootOpen()
	identity := RuntimeIdentity{InstallRootKey: unixHandleKey(rootFD), ExecutableKey: unixHandleKey(executableFD)}
	return &otherInstallRoot{rootFD: rootFD, executableFD: executableFD, path: info.InstallDir, executablePath: info.Executable, identity: identity}, nil
}

func (r *otherInstallRoot) ReadRegular(ctx context.Context, relative string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !safeRuntimeRelative(relative) || r.VerifyIdentity(r.identity) != nil {
		return nil, ErrConfigSchemaUnsupported
	}
	fd, err := r.openRegular(relative)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), relative)
	if file == nil {
		_ = unix.Close(fd)
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

func (r *otherInstallRoot) openRegular(relative string) (int, error) {
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	parent, err := unix.Dup(r.rootFD)
	if err != nil {
		return -1, err
	}
	for index, part := range parts {
		last := index == len(parts)-1
		var before unix.Stat_t
		if err := unix.Fstatat(parent, part, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			_ = unix.Close(parent)
			return -1, err
		}
		if last {
			afterRuntimeLeafLstat(relative)
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if !last {
			flags |= unix.O_DIRECTORY
		} else {
			flags |= unix.O_NONBLOCK
		}
		child, openErr := unix.Openat(parent, part, flags, 0)
		_ = unix.Close(parent)
		if openErr != nil {
			return -1, openErr
		}
		var after unix.Stat_t
		if statErr := unix.Fstat(child, &after); statErr != nil || before.Dev != after.Dev || before.Ino != after.Ino {
			_ = unix.Close(child)
			return -1, fmt.Errorf("opened regular file changed")
		}
		if last {
			if after.Mode&unix.S_IFMT != unix.S_IFREG {
				_ = unix.Close(child)
				return -1, fmt.Errorf("opened regular file is not regular")
			}
			return child, nil
		}
		parent = child
	}
	return -1, fmt.Errorf("empty regular path")
}

func (r *otherInstallRoot) WalkBundledSkills(ctx context.Context, limits bundledInventoryLimits) ([]bundledSkill, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.VerifyIdentity(r.identity); err != nil {
		return nil, err
	}
	root, _, err := r.openUnixRelative("skills", true)
	if err != nil {
		return nil, err
	}
	afterBundledDirectoryHandleChange(1)
	directories, files := 0, 0
	var bytesRead int64
	skills, err := r.walkUnixDirectory(ctx, root, "skills", 0, limits, &directories, &files, &bytesRead)
	if err != nil {
		return nil, err
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].RelativePath < skills[j].RelativePath })
	if err := r.VerifyIdentity(r.identity); err != nil {
		return nil, err
	}
	return skills, nil
}

type unixBundledDirectory struct {
	name     string
	relative string
	key      string
}

func (r *otherInstallRoot) walkUnixDirectory(ctx context.Context, fd int, relative string, depth int, limits bundledInventoryLimits, directories, files *int, bytesRead *int64) ([]bundledSkill, error) {
	file := os.NewFile(uintptr(fd), relative)
	if file == nil {
		_ = unix.Close(fd)
		afterBundledDirectoryHandleChange(-1)
		return nil, fmt.Errorf("directory handle is invalid")
	}
	defer func() {
		_ = file.Close()
		afterBundledDirectoryHandleChange(-1)
	}()
	var result []bundledSkill
	var pendingDirectories []unixBundledDirectory
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
		entries, readErr := file.ReadDir(64)
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
			childFD, stat, err := openUnixChild(int(file.Fd()), entry.Name(), entry.IsDir())
			if err != nil {
				return nil, fmt.Errorf("bundled entry is redirected: %w", err)
			}
			if entry.IsDir() {
				afterBundledDirectoryHandleChange(1)
				pendingDirectories = append(pendingDirectories, unixBundledDirectory{name: entry.Name(), relative: childRelative, key: unixStatKey(stat)})
				_ = unix.Close(childFD)
				afterBundledDirectoryHandleChange(-1)
				if len(pendingDirectories) > maxPendingDirectories {
					return nil, fmt.Errorf("bundled inventory exceeds directory limit")
				}
				continue
			}
			*files++
			if *files > limits.MaxFiles {
				_ = unix.Close(childFD)
				return nil, fmt.Errorf("bundled inventory file limit exceeded")
			}
			if entry.Name() != "SKILL.md" {
				_ = unix.Close(childFD)
				continue
			}
			baseline := unixStatKey(stat)
			*bytesRead += stat.Size
			if *bytesRead > limits.MaxBytes {
				_ = unix.Close(childFD)
				return nil, fmt.Errorf("bundled inventory exceeds byte limit")
			}
			_ = unix.Close(childFD)
			afterRuntimeLeafLstat(childRelative)
			childFD, stat, err = openUnixChild(int(file.Fd()), entry.Name(), false)
			if err != nil || unixStatKey(stat) != baseline {
				if err == nil {
					_ = unix.Close(childFD)
				}
				return nil, fmt.Errorf("bundled skill changed")
			}
			data, readErr := readUnixFrontmatter(ctx, childFD, limits.MaxFrontmatterBytes)
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
		childFD, stat, err := openUnixChild(int(file.Fd()), pending.name, true)
		if err != nil {
			return nil, fmt.Errorf("bundled entry is redirected: %w", err)
		}
		afterBundledDirectoryHandleChange(1)
		if unixStatKey(stat) != pending.key {
			_ = unix.Close(childFD)
			afterBundledDirectoryHandleChange(-1)
			return nil, fmt.Errorf("bundled directory changed")
		}
		*directories++
		children, walkErr := r.walkUnixDirectory(ctx, childFD, pending.relative, depth+1, limits, directories, files, bytesRead)
		if walkErr != nil {
			return nil, walkErr
		}
		result = append(result, children...)
	}
	return result, nil
}

func openUnixChild(parent int, name string, directory bool) (int, unix.Stat_t, error) {
	var before unix.Stat_t
	if err := unix.Fstatat(parent, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return -1, before, err
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if directory {
		flags |= unix.O_DIRECTORY
	} else {
		flags |= unix.O_NONBLOCK
	}
	fd, err := unix.Openat(parent, name, flags, 0)
	if err != nil {
		return -1, before, err
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || before.Dev != after.Dev || before.Ino != after.Ino {
		_ = unix.Close(fd)
		return -1, before, fmt.Errorf("entry changed")
	}
	if !directory && after.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		return -1, before, fmt.Errorf("entry is not regular")
	}
	return fd, after, nil
}

func (r *otherInstallRoot) openUnixRelative(relative string, directory bool) (int, unix.Stat_t, error) {
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	parent, err := unix.Dup(r.rootFD)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	for index, part := range parts {
		child, stat, openErr := openUnixChild(parent, part, index < len(parts)-1 || directory)
		_ = unix.Close(parent)
		if openErr != nil {
			return -1, unix.Stat_t{}, openErr
		}
		if index == len(parts)-1 {
			return child, stat, nil
		}
		parent = child
	}
	return -1, unix.Stat_t{}, fmt.Errorf("empty relative path")
}

func readUnixFrontmatter(ctx context.Context, fd int, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "SKILL.md")
	if file == nil {
		_ = unix.Close(fd)
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

func (r *otherInstallRoot) Identity() RuntimeIdentity { return r.identity }
func (r *otherInstallRoot) VerifyIdentity(expected RuntimeIdentity) error {
	rootFD, rootErr := openUnixNoFollow(r.path, true)
	if rootErr == nil {
		defer unix.Close(rootFD)
	}
	executableFD, executableErr := openUnixNoFollow(r.executablePath, false)
	if executableErr == nil {
		defer unix.Close(executableFD)
	}
	if expected != r.identity || rootErr != nil || executableErr != nil || unixHandleKey(rootFD) != r.identity.InstallRootKey || unixHandleKey(executableFD) != r.identity.ExecutableKey {
		return ErrConfigSchemaUnsupported
	}
	return nil
}
func (r *otherInstallRoot) Close() error {
	var err error
	if closeErr := unix.Close(r.rootFD); err == nil {
		err = closeErr
	}
	if closeErr := unix.Close(r.executableFD); err == nil {
		err = closeErr
	}
	return err
}

func safeRuntimeRelative(relative string) bool {
	return relative != "" && !filepath.IsAbs(relative) && filepath.Clean(relative) == relative && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func openUnixNoFollow(path string, directory bool) (int, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return -1, fmt.Errorf("path is not absolute")
	}
	parent, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return -1, err
	}
	parts := strings.Split(strings.TrimPrefix(clean, string(filepath.Separator)), string(filepath.Separator))
	if len(parts) == 1 && parts[0] == "" {
		if !directory {
			_ = unix.Close(parent)
			return -1, fmt.Errorf("root is not a regular file")
		}
		return parent, nil
	}
	for index, part := range parts {
		child, _, openErr := openUnixChild(parent, part, index < len(parts)-1 || directory)
		_ = unix.Close(parent)
		if openErr != nil {
			return -1, openErr
		}
		if index == len(parts)-1 {
			return child, nil
		}
		parent = child
	}
	return -1, fmt.Errorf("empty path")
}

func unixHandleKey(fd int) string {
	var stat unix.Stat_t
	if fd < 0 || unix.Fstat(fd, &stat) != nil {
		return ""
	}
	return unixStatKey(stat)
}

func unixStatKey(stat unix.Stat_t) string {
	return fmt.Sprintf("%x:%x", stat.Dev, stat.Ino)
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
	name := ""
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
