package workspace

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// WritePublicEnv writes sorted, non-secret workspace selections to path.
func WritePublicEnv(path string, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if isSecretKey(key) {
			return &Error{Code: "SECRET_KEY_FORBIDDEN", Err: fmt.Errorf("%s is not allowed in workspace .env", key)}
		}
		if key == "" || strings.ContainsAny(key, "=\r\n") || strings.ContainsAny(value, "\r\n") {
			return &Error{Code: "ENV_VALUE_INVALID", Err: fmt.Errorf("invalid workspace environment entry")}
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var content strings.Builder
	for _, key := range keys {
		content.WriteString(key)
		content.WriteByte('=')
		content.WriteString(values[key])
		content.WriteByte('\n')
	}
	return WriteFileAtomic(filepath.Clean(path), []byte(content.String()), 0o600)
}

func isSecretKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"SECRET", "TOKEN", "PASSWORD", "API_KEY", "PRIVATE_KEY"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}
