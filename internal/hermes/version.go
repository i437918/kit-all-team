package hermes

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

const (
	HermesMinimumVersion          = "0.20.1"
	HermesMaximumExclusiveVersion = "0.21.0"
)

var (
	ErrVersionUnsupported = errors.New("HERMES_VERSION_UNSUPPORTED")
	versionLinePattern    = regexp.MustCompile(`^Hermes Agent v([0-9]+)\.([0-9]+)\.([0-9]+) \([^\r\n()]+\)$`)
)

type RuntimeInfo struct {
	Executable string
	InstallDir string
	Version    string
}

type semanticVersion struct{ major, minor, patch int }

func ParseRuntimeInfo(executable string, output []byte) (RuntimeInfo, error) {
	if len(output) == 0 || len(output) > maxVersionOutputBytes || !utf8.Valid(output) {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	for _, value := range string(output) {
		if unicode.IsControl(value) && value != '\n' && value != '\r' {
			return RuntimeInfo{}, ErrExecutableUnverified
		}
	}
	text := strings.ReplaceAll(string(output), "\r\n", "\n")
	if strings.Contains(text, "\r") {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	if len(lines) != 5 && len(lines) != 6 {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	match := versionLinePattern.FindStringSubmatch(lines[0])
	if match == nil {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	version, err := parseVersion(match[1], match[2], match[3])
	if err != nil {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	if version.major != 0 || version.minor != 20 || version.patch < 1 {
		return RuntimeInfo{}, fmt.Errorf("%w: supported range >=%s,<%s", ErrVersionUnsupported, HermesMinimumVersion, HermesMaximumExclusiveVersion)
	}
	if !strings.HasPrefix(lines[1], "Install directory: ") {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	installDirectory := strings.TrimSpace(strings.TrimPrefix(lines[1], "Install directory: "))
	if !filepath.IsAbs(installDirectory) || pathsafe.ValidateDirectory(installDirectory) != nil {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	cleanExecutable := filepath.Clean(executable)
	cleanInstall := filepath.Clean(installDirectory)
	relative, err := filepath.Rel(cleanInstall, cleanExecutable)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	index := 2
	if len(lines) == 6 {
		if !strings.HasPrefix(lines[index], "Install method: ") || strings.TrimSpace(strings.TrimPrefix(lines[index], "Install method: ")) == "" {
			return RuntimeInfo{}, ErrExecutableUnverified
		}
		index++
	}
	if !pythonVersionLine.MatchString(lines[index]) {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	index++
	if !strings.HasPrefix(lines[index], "OpenAI SDK: ") || strings.TrimSpace(strings.TrimPrefix(lines[index], "OpenAI SDK: ")) == "" {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	index++
	if lines[index] != "Run 'hermes version' for update status." {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	return RuntimeInfo{Executable: cleanExecutable, InstallDir: cleanInstall, Version: fmt.Sprintf("%d.%d.%d", version.major, version.minor, version.patch)}, nil
}

func parseVersion(major, minor, patch string) (semanticVersion, error) {
	parse := func(value string) (int, error) {
		if len(value) > 1 && value[0] == '0' {
			return 0, errors.New("non-canonical version")
		}
		return strconv.Atoi(value)
	}
	ma, err := parse(major)
	if err != nil {
		return semanticVersion{}, err
	}
	mi, err := parse(minor)
	if err != nil {
		return semanticVersion{}, err
	}
	pa, err := parse(patch)
	if err != nil {
		return semanticVersion{}, err
	}
	return semanticVersion{ma, mi, pa}, nil
}
