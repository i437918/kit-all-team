// Package secrets stores credentials only in the selected application's private directory.
package secrets

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
)

// Store is an application-local secret file store.
type Store struct {
	applicationHome string
	values          []string
}

// Status is intentionally path-only metadata about a secret store.
type Status struct {
	Path       string
	Configured bool
}

// String returns metadata that is safe to put in status output.
func (s Status) String() string {
	return fmt.Sprintf("configured=%t path=%s", s.Configured, s.Path)
}

// NewStore creates a store whose secret file is applicationHome/.env.
// It accepts only an absolute clean application directory to avoid writing a
// secret in the current directory or through redirected path components.
func NewStore(applicationHome string) (*Store, error) {
	if strings.TrimSpace(applicationHome) == "" || !filepath.IsAbs(applicationHome) || filepath.Clean(applicationHome) != applicationHome {
		return nil, fmt.Errorf("application home must be an absolute clean path")
	}
	if _, err := validateStoreLocation(applicationHome); err != nil {
		return nil, err
	}
	return &Store{applicationHome: applicationHome}, nil
}

// Save atomically writes private, sorted dotenv entries and returns their path.
func (s *Store) Save(values map[string]string) (string, error) {
	path, err := validateStoreLocation(s.applicationHome)
	if err != nil {
		return "", err
	}
	if err := privatefile.Validate(path); err != nil {
		return "", err
	}
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if !validKey(key) || strings.ContainsAny(value, "\r\n") {
			return "", fmt.Errorf("secret environment entry is invalid")
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines, err := readSecretDotenv(path)
	if err != nil {
		return "", err
	}
	pending := make(map[string]string, len(values))
	for key, value := range values {
		pending[key] = value
	}
	output := make([]string, 0, len(lines)+len(pending))
	for _, line := range lines {
		if line.key != "" {
			if value, replace := pending[line.key]; replace {
				output = append(output, line.key+"="+value)
				delete(pending, line.key)
				continue
			}
		}
		output = append(output, line.raw)
	}
	s.values = s.values[:0]
	for _, key := range keys {
		if value, appendEntry := pending[key]; appendEntry {
			output = append(output, key+"="+value)
		}
		s.values = append(s.values, values[key])
	}
	content := strings.Join(output, "\n")
	if content != "" {
		content += "\n"
	}
	if err := pathsafe.EnsureDirectory(s.applicationHome, 0o700); err != nil {
		return "", err
	}
	if _, err := validateStoreLocation(s.applicationHome); err != nil {
		return "", err
	}
	if err := privatefile.Validate(path); err != nil {
		return "", err
	}
	if err := writePrivateAtomic(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	if _, err := validateStoreLocation(s.applicationHome); err != nil {
		return "", err
	}
	if err := privatefile.Validate(path); err != nil {
		return "", err
	}
	return path, nil
}

// Load validates the application-local dotenv file and returns only requested keys.
func (s *Store) Load(keys ...string) (map[string]string, error) {
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if !validKey(key) {
			return nil, fmt.Errorf("requested secret key is invalid")
		}
		allowed[key] = struct{}{}
	}
	path, err := validateStoreLocation(s.applicationHome)
	if err != nil {
		return nil, err
	}
	if err := privatefile.Validate(path); err != nil {
		return nil, err
	}
	lines, err := readSecretDotenv(path)
	if err != nil {
		return nil, err
	}
	loaded := make(map[string]string, len(allowed))
	for _, line := range lines {
		if _, requested := allowed[line.key]; requested {
			loaded[line.key] = line.value
			s.values = append(s.values, line.value)
		}
	}
	return loaded, nil
}

func validateStoreLocation(applicationHome string) (string, error) {
	if strings.TrimSpace(applicationHome) == "" || !filepath.IsAbs(applicationHome) || filepath.Clean(applicationHome) != applicationHome {
		return "", fmt.Errorf("application home must be an absolute clean path")
	}
	if err := pathsafe.ValidateDirectory(applicationHome); err != nil {
		return "", err
	}
	path := filepath.Join(applicationHome, ".env")
	if err := pathsafe.ValidateRegular(path); err != nil {
		return "", err
	}
	return path, nil
}

// Status reports whether the local secret file exists without reading it.
func (s *Store) Status() Status {
	path := filepath.Join(s.applicationHome, ".env")
	_, err := os.Stat(path)
	return Status{Path: path, Configured: err == nil}
}

// Redact removes values that this store has received from a diagnostic string.
func (s *Store) Redact(text string) string {
	for _, value := range s.values {
		if value != "" {
			text = strings.ReplaceAll(text, value, "[REDACTED]")
		}
	}
	return text
}

type dotenvLine struct {
	raw   string
	key   string
	value string
}

func readSecretDotenv(path string) ([]dotenvLine, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseSecretDotenv(string(data))
}

func parseSecretDotenv(input string) ([]dotenvLine, error) {
	if strings.Contains(strings.ReplaceAll(input, "\r\n", ""), "\r") {
		return nil, fmt.Errorf("secret dotenv line is invalid")
	}
	input = strings.ReplaceAll(input, "\r\n", "\n")
	rawLines := strings.Split(input, "\n")
	lines := make([]dotenvLine, 0, len(rawLines))
	seen := make(map[string]struct{})
	for index, raw := range rawLines {
		if raw == "" && index == len(rawLines)-1 {
			continue
		}
		trimmed := strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(trimmed, "#") {
			lines = append(lines, dotenvLine{raw: raw})
			continue
		}
		key, value, found := strings.Cut(raw, "=")
		if !found || !validKey(key) {
			return nil, fmt.Errorf("secret dotenv line is invalid")
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("secret dotenv contains duplicate key")
		}
		seen[key] = struct{}{}
		lines = append(lines, dotenvLine{raw: raw, key: key, value: value})
	}
	return lines, nil
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for index, character := range key {
		letter := (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z')
		digit := character >= '0' && character <= '9'
		if character == '_' || letter || (index > 0 && digit) {
			continue
		}
		return false
	}
	return true
}

func writePrivateAtomic(path string, data []byte, perm fs.FileMode) error {
	return privatefile.WriteAtomic(path, data, perm)
}
