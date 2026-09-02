package environment

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
)

const (
	maxDisplayPathBytes       = 1536
	maxDisplayPathRunes       = 192
	maxDisplayUnsafeTailRunes = 16
)

// ErrTerminalUnsafePath reports a path containing terminal control, format,
// bidirectional, or line-separator characters.
var ErrTerminalUnsafePath = errors.New("terminal-unsafe path")

// ValidateTerminalPath rejects path characters that can alter terminal layout
// or direction. Ordinary spaces, quotes, backslashes, and graphic Unicode are valid.
func ValidateTerminalPath(path string) error {
	for _, r := range path {
		if terminalUnsafeRune(r) {
			return ErrTerminalUnsafePath
		}
	}
	return nil
}

// DisplayPath returns a bounded, quoted path for terminal output. Graphic
// Unicode remains readable while controls and delimiters use explicit escapes.
func DisplayPath(path string) string {
	runes := displayPathRunes(path)
	quoted := normalizeHexEscapes(strconv.QuoteToGraphic(string(runes)))
	if len(quoted) <= maxDisplayPathBytes {
		return quoted
	}
	return `"..."`
}

func displayPathRunes(path string) []rune {
	runes := []rune(path)
	if len(runes) <= maxDisplayPathRunes {
		return runes
	}
	headRunes := maxDisplayPathRunes - maxDisplayUnsafeTailRunes
	excerpt := append([]rune(nil), runes[:headRunes]...)
	for _, r := range runes[headRunes:] {
		if terminalUnsafeRune(r) {
			excerpt = append(excerpt, r)
			if len(excerpt) == maxDisplayPathRunes {
				break
			}
		}
	}
	return append(excerpt, '…')
}

func terminalUnsafeRune(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
}

func normalizeHexEscapes(quoted string) string {
	var result strings.Builder
	result.Grow(len(quoted))
	for i := 0; i < len(quoted); {
		if quoted[i] != '\\' || i+1 >= len(quoted) {
			result.WriteByte(quoted[i])
			i++
			continue
		}
		if quoted[i+1] == '\\' {
			result.WriteString(`\\`)
			i += 2
			continue
		}
		if quoted[i+1] == 'x' && i+3 < len(quoted) {
			result.WriteString(`\u00`)
			result.WriteString(quoted[i+2 : i+4])
			i += 4
			continue
		}
		result.WriteByte(quoted[i])
		result.WriteByte(quoted[i+1])
		i += 2
	}
	return result.String()
}
