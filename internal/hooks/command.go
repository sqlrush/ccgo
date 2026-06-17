package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
	"ccgo/internal/tool"
)

const defaultCommandHookTimeout = 10 * time.Minute
const defaultHTTPHookTimeout = 10 * time.Minute

type CommandHook struct {
	Phase         string
	Matcher       string
	If            string
	Command       string
	Shell         string
	StatusMessage string
	Timeout       time.Duration
}

type HTTPHook struct {
	Phase                string
	Matcher              string
	If                   string
	URL                  string
	Headers              map[string]string
	AllowedEnvVars       []string
	StatusMessage        string
	Timeout              time.Duration
	AllowedURLPatterns   []string
	PolicyAllowedEnvVars []string
}

type Options struct {
	AllowedHTTPHookURLs    []string
	HTTPHookAllowedEnvVars []string
}

func FromSettings(settings contracts.Settings) []tool.Hook {
	if settings.DisableAllHooks != nil && *settings.DisableAllHooks {
		return nil
	}
	if settings.AllowManagedHooksOnly != nil && *settings.AllowManagedHooksOnly {
		return nil
	}
	return FromRaw(settings.Hooks, Options{
		AllowedHTTPHookURLs:    settings.AllowedHTTPHookURLs,
		HTTPHookAllowedEnvVars: settings.HTTPHookAllowedEnvVars,
	})
}

func FromRaw(raw map[string]any, options Options) []tool.Hook {
	if len(raw) == 0 {
		return nil
	}
	var hooks []tool.Hook
	for phase, value := range raw {
		phase = strings.TrimSpace(phase)
		if phase == "" {
			continue
		}
		for _, hook := range hooksForPhase(phase, value, options) {
			hooks = append(hooks, hook)
		}
	}
	return hooks
}

func hooksForPhase(phase string, raw any, options Options) []tool.Hook {
	var out []tool.Hook
	for _, matcher := range hookMatchers(raw) {
		out = append(out, hooksFromMatcher(phase, matcher, options)...)
	}
	return out
}

type hookMatcher struct {
	Matcher string
	Hooks   any
}

func hookMatchers(raw any) []hookMatcher {
	switch value := raw.(type) {
	case []any:
		var out []hookMatcher
		for _, item := range value {
			out = append(out, hookMatchers(item)...)
		}
		return out
	case map[string]any:
		if hooks, ok := value["hooks"]; ok {
			return []hookMatcher{{Matcher: stringField(value, "matcher"), Hooks: hooks}}
		}
		return []hookMatcher{{Hooks: value}}
	case string:
		return []hookMatcher{{Hooks: value}}
	default:
		return nil
	}
}

func hooksFromMatcher(phase string, matcher hookMatcher, options Options) []tool.Hook {
	var out []tool.Hook
	for _, rawHook := range hookSpecs(matcher.Hooks) {
		if hook, ok := commandHookFromRaw(phase, matcher.Matcher, rawHook); ok {
			out = append(out, hook)
			continue
		}
		if hook, ok := httpHookFromRaw(phase, matcher.Matcher, rawHook, options); ok {
			out = append(out, hook)
		}
	}
	return out
}

func hookSpecs(raw any) []any {
	switch value := raw.(type) {
	case []any:
		var out []any
		for _, item := range value {
			out = append(out, hookSpecs(item)...)
		}
		return out
	case nil:
		return nil
	default:
		return []any{value}
	}
}

func httpHookFromRaw(phase string, matcher string, raw any, options Options) (HTTPHook, bool) {
	value, ok := raw.(map[string]any)
	if !ok {
		return HTTPHook{}, false
	}
	hookType := strings.TrimSpace(stringField(value, "type"))
	url := strings.TrimSpace(stringField(value, "url"))
	if url == "" || hookType != "http" {
		return HTTPHook{}, false
	}
	timeout := durationSeconds(value["timeout"])
	if timeout <= 0 {
		timeout = defaultHTTPHookTimeout
	}
	return HTTPHook{
		Phase:                phase,
		Matcher:              matcher,
		If:                   stringField(value, "if"),
		URL:                  url,
		Headers:              stringMapField(value, "headers"),
		AllowedEnvVars:       stringListField(value, "allowedEnvVars", "allowed_env_vars"),
		StatusMessage:        stringField(value, "statusMessage", "status_message"),
		Timeout:              timeout,
		AllowedURLPatterns:   append([]string(nil), options.AllowedHTTPHookURLs...),
		PolicyAllowedEnvVars: append([]string(nil), options.HTTPHookAllowedEnvVars...),
	}, true
}

func commandHookFromRaw(phase string, matcher string, raw any) (CommandHook, bool) {
	switch value := raw.(type) {
	case string:
		command := strings.TrimSpace(value)
		if command == "" {
			return CommandHook{}, false
		}
		return CommandHook{Phase: phase, Matcher: matcher, Command: command, Timeout: defaultCommandHookTimeout}, true
	case map[string]any:
		hookType := strings.TrimSpace(stringField(value, "type"))
		command := strings.TrimSpace(stringField(value, "command"))
		if command == "" || (hookType != "" && hookType != "command") {
			return CommandHook{}, false
		}
		timeout := durationSeconds(value["timeout"])
		if timeout <= 0 {
			timeout = defaultCommandHookTimeout
		}
		return CommandHook{
			Phase:         phase,
			Matcher:       matcher,
			If:            stringField(value, "if"),
			Command:       command,
			Shell:         stringField(value, "shell"),
			StatusMessage: stringField(value, "statusMessage", "status_message"),
			Timeout:       timeout,
		}, true
	default:
		return CommandHook{}, false
	}
}

func stringField(object map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := object[name].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringMapField(object map[string]any, name string) map[string]string {
	raw, ok := object[name].(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for key, value := range raw {
		text, ok := value.(string)
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stringListField(object map[string]any, names ...string) []string {
	for _, name := range names {
		switch value := object[name].(type) {
		case []any:
			var out []string
			for _, item := range value {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					out = append(out, strings.TrimSpace(text))
				}
			}
			return out
		case []string:
			out := make([]string, 0, len(value))
			for _, item := range value {
				if strings.TrimSpace(item) != "" {
					out = append(out, strings.TrimSpace(item))
				}
			}
			return out
		}
	}
	return nil
}

func durationSeconds(raw any) time.Duration {
	switch value := raw.(type) {
	case float64:
		if value > 0 {
			return time.Duration(value * float64(time.Second))
		}
	case int:
		if value > 0 {
			return time.Duration(value) * time.Second
		}
	case json.Number:
		if parsed, err := strconv.ParseFloat(string(value), 64); err == nil && parsed > 0 {
			return time.Duration(parsed * float64(time.Second))
		}
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return 0
		}
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
			return time.Duration(parsed * float64(time.Second))
		}
	}
	return 0
}

func (h CommandHook) HookPhases() []string {
	return []string{h.Phase}
}

func (h HTTPHook) HookPhases() []string {
	return []string{h.Phase}
}

func (h CommandHook) RunToolHook(ctx tool.Context, event tool.HookEvent) (tool.HookResult, error) {
	if event.Phase != h.Phase {
		return tool.HookResult{}, nil
	}
	if !matchesPattern(event.ToolName, h.Matcher) || !h.matchesIf(event, ctx.WorkingDirectory) {
		return tool.HookResult{}, nil
	}
	input, err := hookInput(ctx, event)
	if err != nil {
		return tool.HookResult{}, err
	}
	stdout, stderr, exitCode, err := h.runCommand(ctx, input)
	if err != nil {
		return tool.HookResult{}, err
	}
	return hookResultFromOutput(h, event, stdout, stderr, exitCode), nil
}

func (h HTTPHook) RunToolHook(ctx tool.Context, event tool.HookEvent) (tool.HookResult, error) {
	if event.Phase != h.Phase {
		return tool.HookResult{}, nil
	}
	if !matchesPattern(event.ToolName, h.Matcher) || !h.matchesIf(event, ctx.WorkingDirectory) {
		return tool.HookResult{}, nil
	}
	input, err := hookInput(ctx, event)
	if err != nil {
		return tool.HookResult{}, err
	}
	body, statusCode, err := h.runHTTP(ctx, input)
	if err != nil {
		return tool.HookResult{Metadata: h.metadata("", statusCode, err.Error())}, nil
	}
	return h.resultFromHTTPBody(event, body, statusCode), nil
}

func hookInput(ctx tool.Context, event tool.HookEvent) (string, error) {
	payload := map[string]any{
		"session_id":      string(ctx.SessionID),
		"transcript_path": metadataString(ctx.Metadata, tool.MetadataSessionPathKey),
		"cwd":             ctx.WorkingDirectory,
		"hook_event_name": event.Phase,
		"tool_use_id":     string(event.ToolUse.ID),
		"tool_name":       event.ToolName,
		"tool_input":      json.RawMessage(event.Input),
	}
	if event.Decision != nil {
		payload["permission_decision"] = event.Decision
	}
	if event.Result != nil {
		payload["tool_response"] = event.Result
	}
	if event.Error != "" {
		payload["error"] = event.Error
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}

func (h CommandHook) runCommand(ctx tool.Context, input string) (string, string, int, error) {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = defaultCommandHookTimeout
	}
	base := ctx.Context
	if base == nil {
		base = context.Background()
	}
	cmdCtx, cancel := context.WithTimeout(base, timeout)
	defer cancel()
	cmd := shellCommand(cmdCtx, h.Shell, h.Command)
	if ctx.WorkingDirectory != "" {
		cmd.Dir = ctx.WorkingDirectory
	}
	cmd.Env = append(os.Environ(),
		"CLAUDE_PROJECT_DIR="+ctx.WorkingDirectory,
		"CLAUDE_SESSION_ID="+string(ctx.SessionID),
		"CLAUDE_TRANSCRIPT_PATH="+metadataString(ctx.Metadata, tool.MetadataSessionPathKey),
	)
	cmd.Stdin = strings.NewReader(input + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		return stdout.String(), stderr.String(), -1, fmt.Errorf("hook command timed out after %s", timeout)
	}
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return stdout.String(), stderr.String(), -1, err
		}
	}
	return stdout.String(), stderr.String(), exitCode, nil
}

func (h HTTPHook) runHTTP(ctx tool.Context, input string) (string, int, error) {
	if len(h.AllowedURLPatterns) > 0 && !urlMatchesAnyPattern(h.URL, h.AllowedURLPatterns) {
		return "", 0, fmt.Errorf("HTTP hook blocked: %s does not match any pattern in allowedHttpHookUrls", h.URL)
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = defaultHTTPHookTimeout
	}
	base := ctx.Context
	if base == nil {
		base = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(base, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, h.URL, strings.NewReader(input))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range h.Headers {
		req.Header.Set(name, h.interpolateHeader(value))
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return string(data), resp.StatusCode, fmt.Errorf("HTTP hook returned status %d", resp.StatusCode)
	}
	return string(data), resp.StatusCode, nil
}

func (h HTTPHook) resultFromHTTPBody(event tool.HookEvent, body string, statusCode int) tool.HookResult {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "{") {
		if result, ok := hookResultFromJSON(event.Phase, trimmed); ok {
			if result.Metadata == nil {
				result.Metadata = map[string]any{}
			}
			for key, value := range h.metadata(body, statusCode, "") {
				result.Metadata[key] = value
			}
			return result
		}
	}
	result := tool.HookResult{Metadata: h.metadata(body, statusCode, "")}
	if trimmed != "" {
		result.Message = trimmed
	}
	return result
}

func shellCommand(ctx context.Context, shell string, command string) *exec.Cmd {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "powershell", "pwsh":
		return exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", command)
	}
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

func hookResultFromOutput(h CommandHook, event tool.HookEvent, stdout string, stderr string, exitCode int) tool.HookResult {
	if exitCode == 2 {
		return tool.HookResult{Block: true, Message: firstNonEmpty(strings.TrimSpace(stderr), strings.TrimSpace(stdout), "blocked by hook command"), Metadata: commandHookMetadata(h, stdout, stderr, exitCode)}
	}
	trimmed := strings.TrimSpace(stdout)
	if strings.HasPrefix(trimmed, "{") {
		if result, ok := hookResultFromJSON(event.Phase, trimmed); ok {
			if result.Metadata == nil {
				result.Metadata = map[string]any{}
			}
			for key, value := range commandHookMetadata(h, stdout, stderr, exitCode) {
				result.Metadata[key] = value
			}
			return result
		}
	}
	result := tool.HookResult{Metadata: commandHookMetadata(h, stdout, stderr, exitCode)}
	if message := firstNonEmpty(strings.TrimSpace(stdout), strings.TrimSpace(stderr)); message != "" {
		result.Message = message
	}
	return result
}

func commandHookMetadata(h CommandHook, stdout string, stderr string, exitCode int) map[string]any {
	metadata := map[string]any{
		"type":      "command",
		"command":   h.Command,
		"exit_code": exitCode,
	}
	if h.StatusMessage != "" {
		metadata["status_message"] = h.StatusMessage
	}
	if strings.TrimSpace(stdout) != "" {
		metadata["stdout"] = strings.TrimSpace(stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		metadata["stderr"] = strings.TrimSpace(stderr)
	}
	return metadata
}

func (h HTTPHook) metadata(body string, statusCode int, errText string) map[string]any {
	metadata := map[string]any{
		"type": "http",
		"url":  h.URL,
	}
	if statusCode != 0 {
		metadata["status_code"] = statusCode
	}
	if h.StatusMessage != "" {
		metadata["status_message"] = h.StatusMessage
	}
	if strings.TrimSpace(body) != "" {
		metadata["body"] = strings.TrimSpace(body)
	}
	if errText != "" {
		metadata["error"] = errText
	}
	return metadata
}

func hookResultFromJSON(phase string, raw string) (tool.HookResult, bool) {
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		return tool.HookResult{}, false
	}
	result := tool.HookResult{}
	if value, ok := object["systemMessage"].(string); ok && strings.TrimSpace(value) != "" {
		result.Message = strings.TrimSpace(value)
	}
	if cont, ok := object["continue"].(bool); ok && !cont {
		result.Block = true
		result.Message = firstNonEmpty(stringField(object, "stopReason"), stringField(object, "reason"), result.Message, "blocked by hook command")
	}
	switch strings.TrimSpace(stringField(object, "decision")) {
	case "block":
		result.Block = true
		result.Message = firstNonEmpty(stringField(object, "reason"), result.Message, "blocked by hook command")
	case "approve":
		result.PermissionDecision = &contracts.PermissionDecision{Behavior: contracts.PermissionAllow, Message: stringField(object, "reason")}
	}
	if hookSpecific, ok := object["hookSpecificOutput"].(map[string]any); ok {
		applyHookSpecificOutput(&result, phase, hookSpecific, object)
	}
	return result, true
}

func applyHookSpecificOutput(result *tool.HookResult, phase string, hookSpecific map[string]any, root map[string]any) {
	if eventName := stringField(hookSpecific, "hookEventName"); eventName != "" && eventName != phase {
		result.Message = firstNonEmpty(result.Message, "hook output ignored for mismatched event "+eventName)
		return
	}
	switch phase {
	case tool.HookPreToolUse:
		if updated, ok := hookSpecific["updatedInput"].(map[string]any); ok {
			if data, err := json.Marshal(updated); err == nil {
				result.UpdatedInput = json.RawMessage(data)
			}
		}
		switch stringField(hookSpecific, "permissionDecision") {
		case "deny":
			result.Block = true
			result.Message = firstNonEmpty(stringField(hookSpecific, "permissionDecisionReason"), stringField(root, "reason"), result.Message, "blocked by hook command")
			result.PermissionDecision = &contracts.PermissionDecision{Behavior: contracts.PermissionDeny, Message: result.Message}
		case "allow":
			result.PermissionDecision = &contracts.PermissionDecision{Behavior: contracts.PermissionAllow, Message: stringField(hookSpecific, "permissionDecisionReason")}
		case "ask":
			result.PermissionDecision = &contracts.PermissionDecision{Behavior: contracts.PermissionAsk, Message: stringField(hookSpecific, "permissionDecisionReason")}
		}
	case tool.HookPermissionRequest:
		if decision, ok := hookSpecific["decision"].(map[string]any); ok {
			switch stringField(decision, "behavior") {
			case "allow":
				result.PermissionDecision = &contracts.PermissionDecision{Behavior: contracts.PermissionAllow, Message: stringField(decision, "message")}
				if updated, ok := decision["updatedInput"].(map[string]any); ok {
					if data, err := json.Marshal(updated); err == nil {
						result.UpdatedInput = json.RawMessage(data)
					}
				}
			case "deny":
				message := firstNonEmpty(stringField(decision, "message"), stringField(root, "reason"), result.Message, "denied by hook command")
				result.Block = true
				result.Message = message
				result.PermissionDecision = &contracts.PermissionDecision{Behavior: contracts.PermissionDeny, Message: message}
			}
		}
	case tool.HookPostToolUse:
		if value := stringField(hookSpecific, "additionalContext"); value != "" {
			result.Message = value
		}
	}
}

func (h CommandHook) matchesIf(event tool.HookEvent, cwd string) bool {
	return matchesIf(h.If, event, cwd)
}

func (h HTTPHook) matchesIf(event tool.HookEvent, cwd string) bool {
	return matchesIf(h.If, event, cwd)
}

func matchesIf(condition string, event tool.HookEvent, cwd string) bool {
	if strings.TrimSpace(condition) == "" {
		return true
	}
	rule, err := permissions.ParseRule(contracts.PermissionSourceUserSettings, contracts.PermissionAllow, condition)
	if err == nil {
		return rule.Matches(permissions.Request{
			ToolName:         event.ToolName,
			Input:            event.Input,
			Command:          firstInputString(event.Input, "command", "cmd"),
			Path:             firstInputString(event.Input, "file_path", "notebook_path", "path"),
			WorkingDirectory: cwd,
		})
	}
	return matchesPattern(event.ToolName, condition)
}

func matchesPattern(matchQuery string, matcher string) bool {
	matcher = strings.TrimSpace(matcher)
	matchQuery = strings.TrimSpace(matchQuery)
	if matcher == "" || matcher == "*" {
		return true
	}
	if simpleHookMatcher(matcher) {
		for _, item := range strings.Split(matcher, "|") {
			if strings.EqualFold(strings.TrimSpace(item), matchQuery) {
				return true
			}
		}
		return false
	}
	regex, err := regexp.Compile(matcher)
	if err != nil {
		return false
	}
	return regex.MatchString(matchQuery)
}

func simpleHookMatcher(matcher string) bool {
	for _, r := range matcher {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '|' {
			continue
		}
		return false
	}
	return true
}

func urlMatchesAnyPattern(rawURL string, patterns []string) bool {
	for _, pattern := range patterns {
		if urlMatchesPattern(rawURL, pattern) {
			return true
		}
	}
	return false
}

func urlMatchesPattern(rawURL string, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	regex := regexp.QuoteMeta(pattern)
	regex = strings.ReplaceAll(regex, `\*`, ".*")
	return regexp.MustCompile("^" + regex + "$").MatchString(rawURL)
}

func (h HTTPHook) interpolateHeader(value string) string {
	allowed := map[string]struct{}{}
	policy := map[string]struct{}{}
	for _, name := range h.PolicyAllowedEnvVars {
		if strings.TrimSpace(name) != "" {
			policy[strings.TrimSpace(name)] = struct{}{}
		}
	}
	for _, name := range h.AllowedEnvVars {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if len(policy) > 0 {
			if _, ok := policy[name]; !ok {
				continue
			}
		}
		allowed[name] = struct{}{}
	}
	out := regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}|\$([A-Z_][A-Z0-9_]*)`).ReplaceAllStringFunc(value, func(match string) string {
		name := strings.TrimPrefix(strings.TrimPrefix(match, "${"), "$")
		name = strings.TrimSuffix(name, "}")
		if _, ok := allowed[name]; !ok {
			return ""
		}
		return os.Getenv(name)
	})
	return strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', 0:
			return -1
		default:
			return r
		}
	}, out)
}

func firstInputString(raw json.RawMessage, keys ...string) string {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
