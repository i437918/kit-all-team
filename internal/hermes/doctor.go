package hermes

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxHermesCommandOutput = 64 << 10

const (
	doctorSuccessLine = "  All checks passed! 🎉"
	doctorTipLine     = "  Tip: run 'hermes doctor --fix' to auto-fix what's possible."
)

var (
	doctorANSISequence    = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	doctorProgressRefresh = regexp.MustCompile(`\r[ \t]*\r`)
	doctorIssueHeader     = regexp.MustCompile(`^  Found ([1-9][0-9]*) issue\(s\) to address:$`)
)

// ParseDoctorTerminal accepts only the audited terminal success footer or a
// complete numbered diagnostic block. Diagnostic text is intentionally not
// classified by meaning.
func ParseDoctorTerminal(output []byte) error {
	if !utf8.Valid(output) {
		return ErrProfileDoctor
	}
	text := doctorANSISequence.ReplaceAllString(string(output), "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = doctorProgressRefresh.ReplaceAllString(text, "\n")
	if strings.Contains(text, "\r") {
		return ErrProfileDoctor
	}
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ErrProfileDoctor
	}

	terminalMarkers := 0
	headerIndex := -1
	issueCount := 0
	for index, line := range lines {
		if line == doctorSuccessLine || strings.HasPrefix(line, "  Found ") && strings.HasSuffix(line, " issue(s) to address:") {
			terminalMarkers++
		}
		if match := doctorIssueHeader.FindStringSubmatch(line); len(match) == 2 {
			count, err := strconv.Atoi(match[1])
			if err != nil {
				return ErrProfileDoctor
			}
			headerIndex, issueCount = index, count
		}
	}
	if terminalMarkers != 1 {
		return ErrProfileDoctor
	}
	if lines[len(lines)-1] == doctorSuccessLine {
		return nil
	}
	if headerIndex < 0 || issueCount < 1 {
		return ErrProfileDoctor
	}

	position := headerIndex + 1
	for position < len(lines) && lines[position] == "" {
		position++
	}
	for number := 1; number <= issueCount; number++ {
		prefix := fmt.Sprintf("  %d. ", number)
		if position >= len(lines) || !strings.HasPrefix(lines[position], prefix) || strings.TrimSpace(strings.TrimPrefix(lines[position], prefix)) == "" {
			return ErrProfileDoctor
		}
		position++
	}
	for position < len(lines) && lines[position] == "" {
		position++
	}
	if position < len(lines) && lines[position] == doctorTipLine {
		position++
	}
	for position < len(lines) && lines[position] == "" {
		position++
	}
	if position != len(lines) {
		return ErrProfileDoctor
	}
	return nil
}
