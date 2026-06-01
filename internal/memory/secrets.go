package memory

import (
	"regexp"
	"strings"
)

type SecretMatch struct {
	Kind string
	Line int
	Text string
}

var secretPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"aws_access_key_id", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"github_token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`)},
	{"openai_api_key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)},
	{"anthropic_api_key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)},
	{"private_key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"generic_secret_assignment", regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password)\s*[:=]\s*['"]?[A-Za-z0-9_./+=-]{16,}`)},
}

func DetectSecrets(content string) []SecretMatch {
	var matches []SecretMatch
	for i, line := range strings.Split(content, "\n") {
		for _, pattern := range secretPatterns {
			if pattern.re.MatchString(line) {
				matches = append(matches, SecretMatch{
					Kind: pattern.kind,
					Line: i + 1,
					Text: strings.TrimSpace(line),
				})
			}
		}
	}
	return matches
}

func GuardTeamMemoryWrite(path string, content string) error {
	if !isTeamMemoryPath(path) {
		return nil
	}
	matches := DetectSecrets(content)
	if len(matches) == 0 {
		return nil
	}
	return SecretError{Path: path, Matches: matches}
}

type SecretError struct {
	Path    string
	Matches []SecretMatch
}

func (e SecretError) Error() string {
	if len(e.Matches) == 0 {
		return "team memory contains secrets"
	}
	return "team memory contains a possible secret on line " + itoa(e.Matches[0].Line)
}

func isTeamMemoryPath(path string) bool {
	path = filepathSlash(path)
	return strings.Contains(path, "/team-memory/") || strings.Contains(path, "/teams/")
}

func filepathSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	return string(buf[i:])
}
