package workspace

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strings"
)

var requiredIgnoreEntries = []string{".env", "/db/", "/.teamkit/"}
var localExcludeEntries = []string{".env", "/db/", "/.teamkit/", "/.gitignore"}

// EnsureGitignore preserves existing rules and adds the required private entries.
func EnsureGitignore(path string) error {
	return ensureIgnoreEntries(path, requiredIgnoreEntries)
}

// IsTeamKitGitignore reports whether current is exactly the append-only root
// .gitignore content Team Kit derives from original.
func IsTeamKitGitignore(original, current []byte) bool {
	return bytes.Equal(current, []byte(withIgnoreEntries(string(original), requiredIgnoreEntries)))
}

// EnsureLocalExclude adds Team Kit runtime paths plus an untracked generated
// root .gitignore to Git's per-worktree exclude file.
func EnsureLocalExclude(path string) error {
	return ensureIgnoreEntries(path, localExcludeEntries)
}

func ensureIgnoreEntries(path string, entries []string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text := withIgnoreEntries(string(data), entries)
	return WriteFileAtomic(path, []byte(text), 0o600)
}

func withIgnoreEntries(text string, entries []string) string {
	for _, entry := range entries {
		if !hasExactLine(text, entry) {
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += entry + "\n"
		}
	}
	return text
}

func splitLines(text string) []string {
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

func hasExactLine(text, wanted string) bool {
	for _, line := range splitLines(text) {
		if line == wanted {
			return true
		}
	}
	return false
}
