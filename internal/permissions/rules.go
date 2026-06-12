package permissions

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"ccgo/internal/contracts"
)

type Request struct {
	ToolUseID                 contracts.ID
	ToolName                  string
	Input                     json.RawMessage
	Command                   string
	Path                      string
	WorkingDirectory          string
	ReadOnly                  bool
	WritesFiles               bool
	Destructive               bool
	DangerouslyDisableSandbox bool
	InternalPaths             InternalPathContext
	Metadata                  map[string]string
}

type Rule struct {
	Source   contracts.PermissionRuleSource
	Behavior contracts.PermissionBehavior
	ToolName string
	Pattern  string
	Raw      string
}

func ParseRule(source contracts.PermissionRuleSource, behavior contracts.PermissionBehavior, raw string) (Rule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Rule{}, fmt.Errorf("empty permission rule")
	}
	if behavior == "" {
		return Rule{}, fmt.Errorf("empty permission behavior for %q", raw)
	}
	if raw == "*" {
		return Rule{Source: source, Behavior: behavior, ToolName: "*", Pattern: "*", Raw: raw}, nil
	}
	value := PermissionRuleValueFromString(raw)
	toolName := strings.TrimSpace(value.ToolName)
	pattern := strings.TrimSpace(value.RuleContent)
	if toolName == "" {
		return Rule{}, fmt.Errorf("missing tool name in permission rule %q", raw)
	}
	if pattern == "" {
		pattern = "*"
	}
	return Rule{Source: source, Behavior: behavior, ToolName: toolName, Pattern: pattern, Raw: raw}, nil
}

func MustParseRule(source contracts.PermissionRuleSource, behavior contracts.PermissionBehavior, raw string) Rule {
	rule, err := ParseRule(source, behavior, raw)
	if err != nil {
		panic(err)
	}
	return rule
}

func RulesFromSettings(source contracts.PermissionRuleSource, settings *contracts.PermissionsSetting) ([]Rule, error) {
	if settings == nil {
		return nil, nil
	}
	var rules []Rule
	for _, raw := range settings.Deny {
		rule, err := ParseRule(source, contracts.PermissionDeny, raw)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	for _, raw := range settings.Allow {
		rule, err := ParseRule(source, contracts.PermissionAllow, raw)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	for _, raw := range settings.Ask {
		rule, err := ParseRule(source, contracts.PermissionAsk, raw)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func RuleValuesFromStrings(values []string) []contracts.PermissionRuleValue {
	out := make([]contracts.PermissionRuleValue, 0, len(values))
	for _, raw := range values {
		out = append(out, PermissionRuleValueFromString(raw))
	}
	return out
}

func PermissionRuleValueFromString(raw string) contracts.PermissionRuleValue {
	open := findFirstUnescaped(raw, '(')
	if open < 0 {
		return contracts.PermissionRuleValue{ToolName: normalizeLegacyToolName(strings.TrimSpace(raw))}
	}
	close := findLastUnescaped(raw, ')')
	if close < 0 || close <= open || close != len(raw)-1 {
		return contracts.PermissionRuleValue{ToolName: normalizeLegacyToolName(strings.TrimSpace(raw))}
	}
	toolName := strings.TrimSpace(raw[:open])
	if toolName == "" {
		return contracts.PermissionRuleValue{ToolName: normalizeLegacyToolName(strings.TrimSpace(raw))}
	}
	content := raw[open+1 : close]
	if content == "" || content == "*" {
		return contracts.PermissionRuleValue{ToolName: normalizeLegacyToolName(toolName)}
	}
	return contracts.PermissionRuleValue{
		ToolName:    normalizeLegacyToolName(toolName),
		RuleContent: UnescapeRuleContent(content),
	}
}

func PermissionRuleValueToString(value contracts.PermissionRuleValue) string {
	if value.RuleContent == "" {
		return value.ToolName
	}
	return value.ToolName + "(" + EscapeRuleContent(value.RuleContent) + ")"
}

func EscapeRuleContent(content string) string {
	content = strings.ReplaceAll(content, `\`, `\\`)
	content = strings.ReplaceAll(content, "(", `\(`)
	content = strings.ReplaceAll(content, ")", `\)`)
	return content
}

func UnescapeRuleContent(content string) string {
	content = strings.ReplaceAll(content, `\(`, "(")
	content = strings.ReplaceAll(content, `\)`, ")")
	content = strings.ReplaceAll(content, `\\`, `\`)
	return content
}

func (r Rule) Matches(req Request) bool {
	if !matchTool(r.ToolName, req.ToolName) {
		return false
	}
	if r.Pattern == "" || r.Pattern == "*" {
		return true
	}
	target := req.MatchTarget()
	if target == "" {
		return false
	}
	return matchPattern(r.Pattern, target)
}

func (r Rule) String() string {
	if r.Raw != "" {
		return r.Raw
	}
	if r.Pattern == "" || r.Pattern == "*" {
		return r.ToolName
	}
	return PermissionRuleValueToString(contracts.PermissionRuleValue{ToolName: r.ToolName, RuleContent: r.Pattern})
}

func (r Request) MatchTarget() string {
	if r.Command != "" {
		return r.Command
	}
	if r.Path != "" {
		return r.Path
	}
	if value := firstStringFromJSON(r.Input, "command", "cmd", "path", "file_path", "url", "pattern", "query", "server_name"); value != "" {
		return value
	}
	return string(r.Input)
}

func matchTool(pattern string, toolName string) bool {
	pattern = strings.TrimSpace(pattern)
	toolName = strings.TrimSpace(toolName)
	if pattern == "*" {
		return true
	}
	if strings.EqualFold(pattern, toolName) {
		return true
	}
	ok, err := filepath.Match(pattern, toolName)
	return err == nil && ok
}

func matchPattern(pattern string, target string) bool {
	pattern = strings.TrimSpace(pattern)
	target = strings.TrimSpace(target)
	if pattern == "*" {
		return true
	}
	if pattern == target {
		return true
	}
	if prefix := permissionRuleExtractPrefix(pattern); prefix != "" {
		return target == prefix || strings.HasPrefix(target, prefix+" ")
	}
	ok, err := filepath.Match(pattern, target)
	if err == nil && ok {
		return true
	}
	if hasUnescapedWildcard(pattern) && matchWildcardPattern(pattern, target, false) {
		return true
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return strings.Contains(target, pattern)
	}
	return false
}

func permissionRuleExtractPrefix(pattern string) string {
	if strings.HasSuffix(pattern, ":*") && len(pattern) > 2 {
		return strings.TrimSuffix(pattern, ":*")
	}
	return ""
}

func hasUnescapedWildcard(pattern string) bool {
	if strings.HasSuffix(pattern, ":*") {
		return false
	}
	return findFirstUnescaped(pattern, '*') >= 0
}

func matchWildcardPattern(pattern string, target string, caseInsensitive bool) bool {
	pattern = strings.TrimSpace(pattern)
	var regex strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' && i+1 < len(pattern) {
			switch pattern[i+1] {
			case '*', '\\':
				regex.WriteString(regexp.QuoteMeta(string(pattern[i+1])))
				i++
				continue
			}
		}
		if pattern[i] == '*' {
			regex.WriteString(".*")
			continue
		}
		regex.WriteString(regexp.QuoteMeta(string(pattern[i])))
	}
	regexPattern := regex.String()
	unescapedStars := countUnescaped(pattern, '*')
	if strings.HasSuffix(regexPattern, " .*") && unescapedStars == 1 {
		regexPattern = strings.TrimSuffix(regexPattern, " .*") + `( .*)?`
	}
	flags := "(?s)"
	if caseInsensitive {
		flags = "(?is)"
	}
	re, err := regexp.Compile(flags + "^" + regexPattern + "$")
	return err == nil && re.MatchString(target)
}

func firstStringFromJSON(raw json.RawMessage, keys ...string) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := obj[key].(string); ok {
			return value
		}
	}
	return ""
}

func normalizeLegacyToolName(name string) string {
	switch name {
	case "Task":
		return "Agent"
	case "KillShell":
		return "TaskStop"
	case "AgentOutputTool", "BashOutputTool":
		return "TaskOutput"
	default:
		return name
	}
}

func findFirstUnescaped(s string, char byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == char && !isEscaped(s, i) {
			return i
		}
	}
	return -1
}

func findLastUnescaped(s string, char byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == char && !isEscaped(s, i) {
			return i
		}
	}
	return -1
}

func countUnescaped(s string, char byte) int {
	count := 0
	for i := 0; i < len(s); i++ {
		if s[i] == char && !isEscaped(s, i) {
			count++
		}
	}
	return count
}

func isEscaped(s string, index int) bool {
	backslashes := 0
	for i := index - 1; i >= 0 && s[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 != 0
}
