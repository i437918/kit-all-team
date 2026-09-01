package securityaudit

import (
	"regexp"
	"strings"
)

type detector struct {
	name    string
	pattern *regexp.Regexp
	accept  func([][]byte) bool
}

func (d detector) matches(data []byte) bool {
	for _, match := range d.pattern.FindAllSubmatch(data, -1) {
		if d.accept == nil || d.accept(match) {
			return true
		}
	}
	return false
}

var secretDetectors = []detector{
	{name: "private_key", pattern: regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----`)},
	{name: "source_access_token", pattern: regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`)},
	{name: "github_token", pattern: regexp.MustCompile(`(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{20,})`)},
	{name: "aws_access_key", pattern: regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`)},
	{name: "google_api_key", pattern: regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)},
	{name: "slack_token", pattern: regexp.MustCompile(`xox[baprs]-[0-9A-Za-z-]{20,}`)},
	{name: "llm_api_key", pattern: regexp.MustCompile(`sk-(?:proj-|ant-)?[A-Za-z0-9_-]{20,}`)},
	{
		name:    "credential_assignment",
		pattern: regexp.MustCompile(`(?i)(?:api[_-]?key|access[_-]?token|auth[_-]?token|password|client[_-]?secret)\s*[:=]\s*["']?([A-Za-z0-9+/=_-]{24,})`),
		accept: func(match [][]byte) bool {
			if len(match) != 2 {
				return false
			}
			value := string(match[1])
			return !looksLikeEnvironmentName(value)
		},
	},
}

var environmentNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func looksLikeEnvironmentName(value string) bool {
	if value != strings.ToUpper(value) || !environmentNamePattern.MatchString(value) || strings.Count(value, "_") < 2 {
		return false
	}
	for _, suffix := range []string{"_API_KEY", "_TOKEN", "_SECRET", "_PASSWORD"} {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}
