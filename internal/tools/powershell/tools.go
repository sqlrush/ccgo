package powershelltools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const (
	defaultTimeoutMillis = 120_000
	maxTimeoutMillis     = 600_000
)

type powerShellInput struct {
	Command     string `json:"command"`
	Timeout     *int   `json:"timeout,omitempty"`
	Description string `json:"description,omitempty"`
}

type powerShellResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	TimedOut   bool
	DurationMS int64
	TimeoutMS  int
	Executable string
}

func NewPowerShellTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "PowerShell",
			Description:        "Run a PowerShell command.",
			SearchHint:         "run powershell command",
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"command"},
				"properties": map[string]any{
					"command":     map[string]any{"type": "string"},
					"timeout":     map[string]any{"type": "integer"},
					"description": map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Runs a PowerShell command in the current working directory. Provide command, optional timeout in milliseconds, and optional short description. Background PowerShell execution is not implemented yet.", nil
		},
		ValidateFunc:    validatePowerShell,
		CallFunc:        callPowerShell,
		ReadOnlyFunc:    powerShellReadOnlyInput,
		ConcurrencyFunc: powerShellReadOnlyInput,
		DestructiveFunc: powerShellDestructiveInput,
	}
}

func validatePowerShell(_ tool.Context, raw json.RawMessage) error {
	input, err := decodePowerShell(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Command) == "" {
		return fmt.Errorf("command is required")
	}
	if input.Timeout != nil {
		if *input.Timeout <= 0 {
			return fmt.Errorf("timeout must be positive")
		}
		if *input.Timeout > maxTimeoutMillis {
			return fmt.Errorf("timeout must be at most %d milliseconds", maxTimeoutMillis)
		}
	}
	return nil
}

func callPowerShell(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodePowerShell(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	result := runPowerShellCommand(ctx, strings.TrimSpace(input.Command), powerShellTimeout(input))
	return contracts.ToolResult{
		Content: formatPowerShellContent(result),
		IsError: result.TimedOut || result.ExitCode != 0,
		StructuredContent: map[string]any{
			"type":        "powershell",
			"command":     input.Command,
			"description": input.Description,
			"stdout":      result.Stdout,
			"stderr":      result.Stderr,
			"exit_code":   result.ExitCode,
			"timed_out":   result.TimedOut,
			"duration_ms": result.DurationMS,
			"timeout_ms":  result.TimeoutMS,
			"executable":  result.Executable,
		},
	}, nil
}

func runPowerShellCommand(ctx tool.Context, command string, timeout time.Duration) powerShellResult {
	start := time.Now()
	result := powerShellResult{
		ExitCode:  -1,
		TimeoutMS: int(timeout / time.Millisecond),
	}
	name, ok := powerShellExecutable()
	if !ok {
		result.Stderr = "PowerShell executable not found. Install pwsh or powershell to use this tool."
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	result.Executable = name
	runCtx := ctx.Context
	if runCtx == nil {
		runCtx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(runCtx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, name, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
	if ctx.WorkingDirectory != "" {
		cmd.Dir = ctx.WorkingDirectory
	}
	configurePowerShellCommand(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.DurationMS = time.Since(start).Milliseconds()
	if runCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		return result
	}
	if err == nil {
		result.ExitCode = 0
		return result
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		return result
	}
	if result.Stderr != "" {
		result.Stderr += "\n"
	}
	result.Stderr += err.Error()
	return result
}

func powerShellExecutable() (string, bool) {
	for _, candidate := range []string{"pwsh", "powershell"} {
		path, err := exec.LookPath(candidate)
		if err == nil {
			return path, true
		}
	}
	return "", false
}

func formatPowerShellContent(result powerShellResult) string {
	output := strings.TrimRight(result.Stdout, "\n")
	stderr := strings.TrimRight(result.Stderr, "\n")
	status := ""
	switch {
	case result.TimedOut:
		status = fmt.Sprintf("Command timed out after %dms.", result.TimeoutMS)
	case result.ExitCode != 0:
		status = fmt.Sprintf("Command exited with code %d.", result.ExitCode)
	}
	if stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr
	}
	if status == "" {
		return output
	}
	if output == "" {
		return status
	}
	return status + "\n\n" + output
}

func powerShellTimeout(input powerShellInput) time.Duration {
	if input.Timeout == nil {
		return time.Duration(defaultTimeoutMillis) * time.Millisecond
	}
	return time.Duration(*input.Timeout) * time.Millisecond
}

func decodePowerShell(raw json.RawMessage) (powerShellInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return powerShellInput{}, err
	}
	for key := range obj {
		switch key {
		case "command", "timeout", "description":
		default:
			return powerShellInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input powerShellInput
	data, err := json.Marshal(obj)
	if err != nil {
		return powerShellInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return powerShellInput{}, err
	}
	return input, nil
}

func powerShellReadOnlyInput(raw json.RawMessage) bool {
	input, err := decodePowerShell(raw)
	if err != nil {
		return false
	}
	return IsReadOnlyCommand(input.Command)
}

func powerShellDestructiveInput(raw json.RawMessage) bool {
	input, err := decodePowerShell(raw)
	if err != nil {
		return false
	}
	return IsDestructiveCommand(input.Command)
}

func IsReadOnlyCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || hasPowerShellMutationSyntax(command) || IsDestructiveCommand(command) {
		return false
	}
	segments := splitPowerShellSegments(command)
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		words := powerShellWords(segment)
		if len(words) == 0 {
			return false
		}
		if !readOnlyWords(words) {
			return false
		}
	}
	return true
}

func IsDestructiveCommand(command string) bool {
	for _, segment := range splitPowerShellSegments(command) {
		words := powerShellWords(segment)
		if len(words) == 0 {
			continue
		}
		if destructiveWords(words) {
			return true
		}
	}
	return false
}

func hasPowerShellMutationSyntax(command string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '`' {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle {
			continue
		}
		if ch == '>' || ch == '$' || ch == '=' || ch == '&' {
			return true
		}
	}
	return false
}

func splitPowerShellSegments(command string) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '`' {
			current.WriteByte(ch)
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteByte(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteByte(ch)
			continue
		}
		if !inSingle && !inDouble && (ch == ';' || ch == '|' || ch == '&') {
			segments = appendNonemptySegment(segments, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	return appendNonemptySegment(segments, current.String())
}

func appendNonemptySegment(segments []string, segment string) []string {
	segment = strings.TrimSpace(segment)
	if segment != "" {
		segments = append(segments, segment)
	}
	return segments
}

func powerShellWords(command string) []string {
	var words []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '`' {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if !inSingle && !inDouble && (ch == ' ' || ch == '\t' || ch == '\n') {
			flush()
			continue
		}
		current.WriteByte(ch)
	}
	flush()
	return words
}

func readOnlyWords(words []string) bool {
	switch canonicalCommand(words[0]) {
	case "get-content", "get-item", "test-path", "resolve-path", "get-process", "get-service", "get-childitem", "get-location", "get-filehash", "get-acl", "format-hex", "select-string", "write-output", "write-host":
		return true
	default:
		return false
	}
}

func destructiveWords(words []string) bool {
	switch canonicalCommand(words[0]) {
	case "remove-item", "del", "erase", "set-content", "add-content", "clear-content", "out-file", "new-item", "move-item", "stop-process", "stop-service", "restart-computer", "invoke-expression", "iex", "start-process":
		return true
	default:
		return strings.HasPrefix(canonicalCommand(words[0]), "remove-") || strings.HasPrefix(canonicalCommand(words[0]), "set-") || strings.HasPrefix(canonicalCommand(words[0]), "new-")
	}
}

func canonicalCommand(command string) string {
	name := strings.ToLower(strings.Trim(strings.TrimSpace(command), `"'`))
	switch name {
	case "cat", "gc":
		return "get-content"
	case "ls", "dir", "gci":
		return "get-childitem"
	case "pwd", "gl":
		return "get-location"
	case "rm", "rmdir", "ri":
		return "remove-item"
	case "mv", "mi":
		return "move-item"
	case "echo", "write":
		return "write-output"
	default:
		return name
	}
}
