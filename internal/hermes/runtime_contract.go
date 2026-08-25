package hermes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
)

const (
	maxConfigDefaultsBytes = 512 << 10
	skillSupportDirCount   = 4
)

var (
	// ErrConfigSchemaUnsupported reports a Hermes runtime whose configuration
	// schema cannot be proven safe for Team Kit to render.
	ErrConfigSchemaUnsupported = errors.New("HERMES_CONFIG_SCHEMA_UNSUPPORTED")
	// ErrBundledSkillsCatalogUnverified reports an unsafe or unverifiable
	// bundled-skills inventory. Callers must not write a profile after it.
	ErrBundledSkillsCatalogUnverified = errors.New("HERMES_BUNDLED_SKILLS_CATALOG_UNVERIFIED")
)

// RuntimeContract is the verified, same-install-root Hermes capability and
// configuration contract used by profile operations.
type RuntimeContract struct {
	Info                   RuntimeInfo
	Identity               RuntimeIdentity
	ConfigSchema           int
	BundledSkills          []string
	BundledInventorySHA256 string
}

// RuntimeIdentity binds a contract to the verified installation root and
// executable file identities without exposing their contents.
type RuntimeIdentity struct {
	InstallRootKey string
	ExecutableKey  string
}

type openedInstallRoot interface {
	ReadRegular(context.Context, string, int64) ([]byte, error)
	WalkBundledSkills(context.Context, bundledInventoryLimits) ([]bundledSkill, error)
	Identity() RuntimeIdentity
	VerifyIdentity(expected RuntimeIdentity) error
	Close() error
}

// runtimeExecutablePin keeps a native no-follow executable handle alive while
// the runtime contract is verified. Every root check is bounded by a fresh
// path-to-handle identity comparison against this retained handle.
type runtimeExecutablePin interface {
	Key() string
	VerifyPath() error
	Close() error
}

// Test seam for a replacement before the installation root is acquired.
var beforeRuntimeRootOpen = func() {}
var afterRuntimeSchemaProbe = func(openedInstallRoot) {}

type bundledInventoryLimits struct {
	MaxDirectories      int
	MaxDepth            int
	MaxFiles            int
	MaxBytes            int64
	MaxFrontmatterBytes int64
}

type bundledSkill struct {
	Name         string
	RelativePath string
}

// isExcludedSkillPath mirrors EXCLUDED_SKILL_DIRS from the pinned Hermes skill
// scanner. Only this literal set is excluded; other hidden directories may be
// legitimate categories or skill roots.
func isExcludedSkillPath(relative string) bool {
	for _, component := range strings.Split(filepath.ToSlash(relative), "/") {
		if isExcludedSkillDirectory(component) {
			return true
		}
	}
	return false
}

func isExcludedSkillDirectory(component string) bool {
	switch component {
	case ".git", ".github", ".hub", ".archive", ".venv", "venv",
		"node_modules", "site-packages", "__pycache__", ".tox", ".nox",
		".pytest_cache", ".mypy_cache", ".ruff_cache":
		return true
	default:
		return false
	}
}

func isSkillSupportDirectory(component string) bool {
	switch component {
	case "references", "templates", "assets", "scripts":
		return true
	default:
		return false
	}
}

var defaultBundledInventoryLimits = bundledInventoryLimits{
	MaxDirectories: 256, MaxDepth: 8, MaxFiles: 4096,
	MaxBytes: 16 << 20, MaxFrontmatterBytes: 64 << 10,
}

// VerifyRuntimeContract proves the installed Hermes executable, configuration
// schema, and bundled skills from one stable no-follow installation root.
func VerifyRuntimeContract(ctx context.Context, executable string, capture executableCapture) (RuntimeContract, error) {
	info, err := runtimeInfoFromExecutableLayout(executable)
	if err != nil {
		return RuntimeContract{}, err
	}
	pin, err := pinRuntimeExecutable(info.Executable)
	if err != nil {
		return RuntimeContract{}, ErrExecutableUnverified
	}
	defer pin.Close()
	if err := pathsafe.ValidateDirectory(info.InstallDir); err != nil {
		return RuntimeContract{}, fmt.Errorf("%w: install root is unsafe", ErrExecutableUnverified)
	}
	if err := pathsafe.ValidateDirectory(filepath.Dir(info.Executable)); err != nil {
		return RuntimeContract{}, fmt.Errorf("%w: executable parent is unsafe", ErrExecutableUnverified)
	}
	if err := pathsafe.ValidateRegular(info.Executable); err != nil {
		return RuntimeContract{}, fmt.Errorf("%w: executable is unsafe", ErrExecutableUnverified)
	}
	beforeRuntimeRootOpen()
	root, err := openVerifiedInstallRoot(info)
	if err != nil {
		return RuntimeContract{}, err
	}
	defer root.Close()
	if root.Identity().ExecutableKey != pin.Key() {
		return RuntimeContract{}, ErrExecutableUnverified
	}
	schema, err := probeConfigSchema(ctx, root)
	if err != nil {
		return RuntimeContract{}, err
	}
	afterRuntimeSchemaProbe(root)
	skills, digest, err := inventoryBundledSkillsAndDigest(ctx, root)
	if err != nil {
		return RuntimeContract{}, err
	}
	identity := root.Identity()
	if err := root.VerifyIdentity(identity); err != nil || identity.ExecutableKey != pin.Key() {
		return RuntimeContract{}, fmt.Errorf("%w: runtime identity changed", ErrBundledSkillsCatalogUnverified)
	}
	return RuntimeContract{
		Info: info, Identity: identity, ConfigSchema: schema, BundledSkills: skills,
		BundledInventorySHA256: digest,
	}, nil
}

func runtimeInfoFromExecutableLayout(executable string) (RuntimeInfo, error) {
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	absolute = filepath.Clean(absolute)
	installDir := filepath.Dir(filepath.Dir(filepath.Dir(absolute)))
	relative, err := filepath.Rel(installDir, absolute)
	if err != nil {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	expected := filepath.Join("venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		expected = filepath.Join("venv", "Scripts", "hermes.exe")
		if !strings.EqualFold(relative, expected) {
			return RuntimeInfo{}, ErrExecutableUnverified
		}
	} else if relative != expected {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	return RuntimeInfo{Executable: absolute, InstallDir: installDir}, nil
}

func probeConfigSchema(ctx context.Context, root openedInstallRoot) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("%w: config probe cancelled", ErrConfigSchemaUnsupported)
	}
	data, err := root.ReadRegular(ctx, filepath.Join("hermes_cli", "config_defaults.py"), maxConfigDefaultsBytes)
	if err != nil {
		return 0, fmt.Errorf("%w: config defaults cannot be read", ErrConfigSchemaUnsupported)
	}
	if !utf8.Valid(data) {
		return 0, ErrConfigSchemaUnsupported
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.Contains(text, "\r") {
		return 0, ErrConfigSchemaUnsupported
	}
	mappings, closed := pythonDefaultConfigMappings(text)
	if !closed || len(mappings) != 1 {
		return 0, ErrConfigSchemaUnsupported
	}
	value, occurrences, valid := pythonConfigVersion(mappings[0])
	if !valid || occurrences != 1 || (value != 34 && value != 37) {
		return 0, ErrConfigSchemaUnsupported
	}
	return value, nil
}

// pythonDefaultConfigMappings lexes only enough Python to locate real
// module-level DEFAULT_CONFIG mapping assignments. It deliberately ignores
// braces and identifiers inside comments and quoted (including triple-quoted)
// strings, and rejects an unterminated string anywhere in the module.
func pythonDefaultConfigMappings(text string) ([]string, bool) {
	var mappings []string
	for pos := 0; pos < len(text); {
		if text[pos] == '#' {
			for pos < len(text) && text[pos] != '\n' {
				pos++
			}
			continue
		}
		if next, isString, closed := pythonSkipString(text, pos); isString {
			if !closed {
				return nil, false
			}
			pos = next
			continue
		}
		if (pos != 0 && text[pos-1] != '\n') || !pythonIdentifierAt(text, pos, "DEFAULT_CONFIG") {
			pos++
			continue
		}
		cursor := pos + len("DEFAULT_CONFIG")
		for cursor < len(text) && (text[cursor] == ' ' || text[cursor] == '\t') {
			cursor++
		}
		if cursor >= len(text) || text[cursor] != '=' {
			return nil, false
		}
		cursor++
		for cursor < len(text) && (text[cursor] == ' ' || text[cursor] == '\t') {
			cursor++
		}
		if cursor >= len(text) || text[cursor] != '{' {
			return nil, false
		}
		end, ok := pythonClosingBrace(text, cursor)
		if !ok {
			return nil, false
		}
		mappings = append(mappings, text[cursor:end])
		pos = end
	}
	return mappings, true
}

func pythonIdentifierAt(text string, pos int, word string) bool {
	if !strings.HasPrefix(text[pos:], word) {
		return false
	}
	before := pos == 0 || !pythonIdentByte(text[pos-1])
	after := pos+len(word) == len(text) || !pythonIdentByte(text[pos+len(word)])
	return before && after
}

func pythonIdentByte(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func pythonSkipString(text string, pos int) (next int, isString bool, closed bool) {
	if pos >= len(text) || (text[pos] != '\'' && text[pos] != '"') {
		return pos, false, true
	}
	quote := text[pos]
	triple := pos+2 < len(text) && text[pos+1] == quote && text[pos+2] == quote
	start := pos + 1
	if triple {
		start = pos + 3
	}
	for index := start; index < len(text); index++ {
		if text[index] == '\\' {
			index++
			continue
		}
		if triple && index+2 < len(text) && text[index] == quote && text[index+1] == quote && text[index+2] == quote {
			return index + 3, true, true
		}
		if !triple && text[index] == quote {
			return index + 1, true, true
		}
		if !triple && text[index] == '\n' {
			return index, true, false
		}
	}
	return len(text), true, false
}

func pythonClosingBrace(text string, start int) (int, bool) {
	depth := 0
	for pos := start; pos < len(text); pos++ {
		if text[pos] == '#' {
			for pos < len(text) && text[pos] != '\n' {
				pos++
			}
			continue
		}
		if next, isString, closed := pythonSkipString(text, pos); isString {
			if !closed {
				return 0, false
			}
			pos = next - 1
			continue
		}
		switch text[pos] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return pos + 1, true
			}
			if depth < 0 {
				return 0, false
			}
		}
	}
	return 0, false
}

func pythonConfigVersion(mapping string) (value int, occurrences int, valid bool) {
	braceDepth, bracketDepth, parenthesisDepth := 0, 0, 0
	valid = true
	for pos := 0; pos < len(mapping); {
		if mapping[pos] == '#' {
			for pos < len(mapping) && mapping[pos] != '\n' {
				pos++
			}
			continue
		}
		if mapping[pos] == '{' {
			braceDepth++
			pos++
			continue
		}
		if mapping[pos] == '}' {
			braceDepth--
			pos++
			continue
		}
		switch mapping[pos] {
		case '[':
			bracketDepth++
			pos++
			continue
		case ']':
			bracketDepth--
			pos++
			continue
		case '(':
			parenthesisDepth++
			pos++
			continue
		case ')':
			parenthesisDepth--
			pos++
			continue
		}
		keyEnd, exactKey := pythonExactConfigVersionKey(mapping, pos)
		if braceDepth == 1 && bracketDepth == 0 && parenthesisDepth == 0 && exactKey {
			cursor := keyEnd
			for cursor < len(mapping) && pythonWhitespace(mapping[cursor]) {
				cursor++
			}
			if cursor < len(mapping) && mapping[cursor] == ':' {
				occurrences++
				cursor++
				for cursor < len(mapping) && pythonWhitespace(mapping[cursor]) {
					cursor++
				}
				end := cursor
				for end < len(mapping) && mapping[end] >= '0' && mapping[end] <= '9' {
					end++
				}
				candidate, conversionErr := strconv.Atoi(mapping[cursor:end])
				candidateValid := end > cursor && !(end-cursor > 1 && mapping[cursor] == '0') && conversionErr == nil && pythonBareDecimalTerminated(mapping, end)
				if !candidateValid {
					valid = false
				} else if occurrences == 1 {
					value = candidate
				}
				pos = end
				continue
			}
		}
		if next, isString, closed := pythonSkipString(mapping, pos); isString {
			if !closed {
				valid = false
				break
			}
			pos = next
			continue
		}
		pos++
	}
	return value, occurrences, valid
}

func pythonExactConfigVersionKey(text string, pos int) (int, bool) {
	const key = "_config_version"
	if pos >= len(text) || (text[pos] != '\'' && text[pos] != '"') {
		return pos, false
	}
	end := pos + len(key) + 2
	if end > len(text) || text[pos+1:end-1] != key || text[end-1] != text[pos] {
		return pos, false
	}
	return end, true
}

func pythonBareDecimalTerminated(mapping string, pos int) bool {
	for pos < len(mapping) {
		switch mapping[pos] {
		case ' ', '\t', '\n':
			pos++
		case '#':
			for pos < len(mapping) && mapping[pos] != '\n' {
				pos++
			}
		case ',', '}':
			return true
		default:
			return false
		}
	}
	return false
}

func pythonWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n'
}

func inventoryBundledSkills(root openedInstallRoot) ([]string, error) {
	skills, _, err := inventoryBundledSkillsAndDigest(context.Background(), root)
	return skills, err
}

func inventoryBundledSkillsAndDigest(ctx context.Context, root openedInstallRoot) ([]string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", fmt.Errorf("%w: bundled inventory cancelled", ErrBundledSkillsCatalogUnverified)
	}
	found, err := root.WalkBundledSkills(ctx, defaultBundledInventoryLimits)
	if err != nil {
		return nil, "", fmt.Errorf("%w: bundled skills cannot be inventoried", ErrBundledSkillsCatalogUnverified)
	}
	if len(found) == 0 {
		return nil, "", ErrBundledSkillsCatalogUnverified
	}
	if err := verifyBundledManifest(ctx, root, found); err != nil {
		return nil, "", fmt.Errorf("%w: bundled manifest cannot be verified", ErrBundledSkillsCatalogUnverified)
	}
	seen := make(map[string]struct{}, len(found))
	skills := make([]string, 0, len(found))
	pairs := make([]string, 0, len(found))
	for _, skill := range found {
		if skill.Name == "" || skill.RelativePath == "" {
			return nil, "", ErrBundledSkillsCatalogUnverified
		}
		if _, duplicate := seen[skill.Name]; duplicate {
			return nil, "", ErrBundledSkillsCatalogUnverified
		}
		seen[skill.Name] = struct{}{}
		skills = append(skills, skill.Name)
		pairs = append(pairs, skill.Name+"\x00"+filepath.ToSlash(skill.RelativePath))
	}
	sort.Strings(skills)
	sort.Strings(pairs)
	return skills, bundledInventorySHA256(pairs), nil
}

func verifyBundledManifest(ctx context.Context, root openedInstallRoot, found []bundledSkill) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := root.ReadRegular(ctx, filepath.Join("skills", "manifest.json"), defaultBundledInventoryLimits.MaxBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var manifest struct {
		Skills []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if len(manifest.Skills) != len(found) {
		return fmt.Errorf("manifest length mismatch")
	}
	pairs := make(map[string]struct{}, len(found))
	for _, skill := range found {
		pair := skill.Name + "\x00" + filepath.ToSlash(skill.RelativePath)
		if _, duplicate := pairs[pair]; duplicate {
			return fmt.Errorf("duplicate inventory entry")
		}
		pairs[pair] = struct{}{}
	}
	seen := make(map[string]struct{}, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		pair := skill.Name + "\x00" + filepath.ToSlash(skill.Path)
		if _, duplicate := seen[pair]; duplicate {
			return fmt.Errorf("duplicate manifest entry")
		}
		seen[pair] = struct{}{}
		if _, ok := pairs[pair]; !ok {
			return fmt.Errorf("manifest entry mismatch")
		}
	}
	return nil
}

// HasBundledSkill reports whether name was present in the verified bundled
// inventory. The inventory is sorted before a contract is returned.
func (c RuntimeContract) HasBundledSkill(name string) bool {
	index := sort.SearchStrings(c.BundledSkills, name)
	return index < len(c.BundledSkills) && c.BundledSkills[index] == name
}

func bundledInventorySHA256(skills []string) string {
	canonical := append([]string(nil), skills...)
	sort.Strings(canonical)
	hash := sha256.New()
	for _, skill := range canonical {
		_, _ = hash.Write([]byte(skill))
		_, _ = hash.Write([]byte{'\n'})
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}
