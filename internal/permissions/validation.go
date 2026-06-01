package permissions

import (
	"fmt"
	"strings"
	"unicode"
)

type ValidationResult struct {
	Valid      bool
	Error      string
	Suggestion string
	Examples   []string
}

func ValidatePermissionRule(raw string) ValidationResult {
	if strings.TrimSpace(raw) == "" {
		return ValidationResult{Valid: false, Error: "Permission rule cannot be empty"}
	}
	if countUnescaped(raw, '(') != countUnescaped(raw, ')') {
		return ValidationResult{
			Valid:      false,
			Error:      "Mismatched parentheses",
			Suggestion: "Ensure all opening parentheses have matching closing parentheses",
		}
	}
	if hasUnescapedEmptyParens(raw) {
		toolName := raw
		if idx := findFirstUnescaped(raw, '('); idx >= 0 {
			toolName = raw[:idx]
		}
		if toolName == "" {
			return ValidationResult{Valid: false, Error: "Empty parentheses with no tool name", Suggestion: "Specify a tool name before the parentheses"}
		}
		return ValidationResult{
			Valid:      false,
			Error:      "Empty parentheses",
			Suggestion: fmt.Sprintf("Either specify a pattern or use just %q without parentheses", toolName),
			Examples:   []string{toolName, toolName + "(some-pattern)"},
		}
	}
	value := PermissionRuleValueFromString(raw)
	if value.ToolName == "" {
		return ValidationResult{Valid: false, Error: "Tool name cannot be empty"}
	}
	if isMCPToolName(value.ToolName) {
		if value.RuleContent != "" || countUnescaped(raw, '(') > 0 {
			return ValidationResult{
				Valid:      false,
				Error:      "MCP rules do not support patterns in parentheses",
				Suggestion: fmt.Sprintf("Use %q without parentheses", value.ToolName),
			}
		}
		return ValidationResult{Valid: true}
	}
	if first := []rune(value.ToolName)[0]; !unicode.IsUpper(first) && first != '*' {
		return ValidationResult{
			Valid:      false,
			Error:      "Tool names must start with uppercase",
			Suggestion: "Use " + strings.ToUpper(value.ToolName[:1]) + value.ToolName[1:],
		}
	}
	if isShellTool(value.ToolName) && value.RuleContent != "" {
		if strings.Contains(value.RuleContent, ":*") && !strings.HasSuffix(value.RuleContent, ":*") {
			return ValidationResult{
				Valid:      false,
				Error:      "The :* pattern must be at the end",
				Suggestion: "Move :* to the end for prefix matching, or use * for wildcard matching",
				Examples:   []string{"Bash(npm run:*)", "Bash(npm run *)"},
			}
		}
		if value.RuleContent == ":*" {
			return ValidationResult{
				Valid:      false,
				Error:      "Prefix cannot be empty before :*",
				Suggestion: "Specify a command prefix before :*",
				Examples:   []string{"Bash(npm:*)", "Bash(git:*)"},
			}
		}
	}
	switch value.ToolName {
	case "WebSearch":
		if strings.ContainsAny(value.RuleContent, "*?") {
			return ValidationResult{
				Valid:      false,
				Error:      "WebSearch does not support wildcards",
				Suggestion: "Use exact search terms without * or ?",
				Examples:   []string{"WebSearch(claude ai)", "WebSearch(typescript tutorial)"},
			}
		}
	case "WebFetch":
		if strings.Contains(value.RuleContent, "://") || strings.HasPrefix(value.RuleContent, "http") {
			return ValidationResult{
				Valid:      false,
				Error:      "WebFetch permissions use domain format, not URLs",
				Suggestion: `Use "domain:hostname" format`,
				Examples:   []string{"WebFetch(domain:example.com)", "WebFetch(domain:github.com)"},
			}
		}
		if value.RuleContent != "" && !strings.HasPrefix(value.RuleContent, "domain:") {
			return ValidationResult{
				Valid:      false,
				Error:      `WebFetch permissions must use "domain:" prefix`,
				Suggestion: `Use "domain:hostname" format`,
				Examples:   []string{"WebFetch(domain:example.com)", "WebFetch(domain:*.google.com)"},
			}
		}
	}
	if isFilePatternTool(value.ToolName) && value.RuleContent != "" {
		if strings.Contains(value.RuleContent, ":*") {
			return ValidationResult{
				Valid:      false,
				Error:      `The ":*" syntax is only for Bash prefix rules`,
				Suggestion: `Use glob patterns like "*" or "**" for file matching`,
			}
		}
	}
	return ValidationResult{Valid: true}
}

func hasUnescapedEmptyParens(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '(' && s[i+1] == ')' && !isEscaped(s, i) {
			return true
		}
	}
	return false
}

func isMCPToolName(name string) bool {
	return strings.HasPrefix(name, "mcp__")
}

func isShellTool(name string) bool {
	return name == "Bash" || name == "PowerShell"
}

func isFilePatternTool(name string) bool {
	switch name {
	case "Read", "Edit", "Write", "Glob", "Grep", "NotebookEdit":
		return true
	default:
		return false
	}
}
