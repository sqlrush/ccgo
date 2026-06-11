package powershelltools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	Command               string `json:"command"`
	Timeout               *int   `json:"timeout,omitempty"`
	Description           string `json:"description,omitempty"`
	RunInBackground       bool   `json:"run_in_background,omitempty"`
	RunInBackgroundAlt    bool   `json:"runInBackground,omitempty"`
	hasRunInBackground    bool
	hasRunInBackgroundAlt bool
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

type powerShellOutputInput struct {
	PowerShellID string `json:"powershell_id,omitempty"`
	ID           string `json:"id,omitempty"`
	TailLines    *int   `json:"tail_lines,omitempty"`
	TailLinesAlt *int   `json:"tailLines,omitempty"`
}

type powerShellKillInput struct {
	PowerShellID string `json:"powershell_id,omitempty"`
	ID           string `json:"id,omitempty"`
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
					"command":           map[string]any{"type": "string"},
					"timeout":           map[string]any{"type": "integer"},
					"description":       map[string]any{"type": "string"},
					"run_in_background": map[string]any{"type": "boolean"},
					"runInBackground":   map[string]any{"type": "boolean"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Runs a PowerShell command in the current working directory. Provide command, optional timeout in milliseconds, optional short description, and run_in_background for background commands. Full sandbox parity and interrupt controls are not implemented yet.", nil
		},
		ValidateFunc:    validatePowerShell,
		CallFunc:        callPowerShell,
		ReadOnlyFunc:    powerShellReadOnlyInput,
		ConcurrencyFunc: powerShellReadOnlyInput,
		DestructiveFunc: powerShellDestructiveInput,
	}
}

func NewPowerShellOutputTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "PowerShellOutput",
			Description:        "Read output from a background PowerShell command.",
			SearchHint:         "read background powershell command output",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"powershell_id": map[string]any{"type": "string"},
					"id":            map[string]any{"type": "string"},
					"tail_lines":    map[string]any{"type": "integer"},
					"tailLines":     map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Reads stdout, stderr, and status for a PowerShell command started with run_in_background.", nil
		},
		ValidateFunc:    validatePowerShellOutput,
		CallFunc:        callPowerShellOutput,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewKillPowerShellTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "KillPowerShell",
			Description:     "Cancel a background PowerShell command.",
			SearchHint:      "kill cancel background powershell command",
			ConcurrencySafe: true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"powershell_id": map[string]any{"type": "string"},
					"id":            map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Cancels a PowerShell command started with run_in_background. Use PowerShellOutput to read the final output and status.", nil
		},
		ValidateFunc:    validateKillPowerShell,
		CallFunc:        callKillPowerShell,
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
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

func validatePowerShellOutput(_ tool.Context, raw json.RawMessage) error {
	input, err := decodePowerShellOutput(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.backgroundID()) == "" {
		return fmt.Errorf("powershell_id is required")
	}
	if input.TailLines != nil && *input.TailLines <= 0 {
		return fmt.Errorf("tail_lines must be positive")
	}
	if input.TailLinesAlt != nil && *input.TailLinesAlt <= 0 {
		return fmt.Errorf("tail_lines must be positive")
	}
	return nil
}

func validateKillPowerShell(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeKillPowerShell(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.backgroundID()) == "" {
		return fmt.Errorf("powershell_id is required")
	}
	return nil
}

func callPowerShell(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodePowerShell(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if input.runInBackground() {
		return startBackgroundPowerShell(ctx, input, powerShellTimeout(input))
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

func callPowerShellOutput(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodePowerShellOutput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	state := EnsureBackgroundState(ctx)
	if state == nil {
		return contracts.ToolResult{}, fmt.Errorf("background powershell state is not available")
	}
	task, ok := state.Get(input.backgroundID())
	if !ok {
		return contracts.ToolResult{}, fmt.Errorf("background powershell command not found: %s", input.backgroundID())
	}
	snapshot := task.Snapshot()
	tailLines := powerShellOutputTailLines(input)
	if tailLines > 0 {
		snapshot.Stdout = tailText(snapshot.Stdout, tailLines)
		snapshot.Stderr = tailText(snapshot.Stderr, tailLines)
	}
	return contracts.ToolResult{
		Content: formatBackgroundOutput(snapshot),
		IsError: !snapshot.Running && (snapshot.TimedOut || snapshot.ExitCode != 0),
		StructuredContent: map[string]any{
			"type":          "powershell_output",
			"powershell_id": snapshot.ID,
			"command":       snapshot.Command,
			"description":   snapshot.Description,
			"stdout":        snapshot.Stdout,
			"stderr":        snapshot.Stderr,
			"running":       snapshot.Running,
			"exit_code":     snapshot.ExitCode,
			"timed_out":     snapshot.TimedOut,
			"cancelled":     snapshot.Cancelled,
			"duration_ms":   snapshot.DurationMS,
			"timeout_ms":    snapshot.TimeoutMS,
			"started_at":    snapshot.StartedAt.UTC().Format(time.RFC3339Nano),
			"ended_at":      formatOptionalTime(snapshot.EndedAt),
			"error":         snapshot.Error,
			"executable":    snapshot.Executable,
		},
	}, nil
}

func callKillPowerShell(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeKillPowerShell(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	state := EnsureBackgroundState(ctx)
	if state == nil {
		return contracts.ToolResult{}, fmt.Errorf("background powershell state is not available")
	}
	task, ok := state.Get(input.backgroundID())
	if !ok {
		return contracts.ToolResult{}, fmt.Errorf("background powershell command not found: %s", input.backgroundID())
	}
	killed := task.Cancel()
	snapshot := task.Snapshot()
	content := fmt.Sprintf("Kill requested for background PowerShell command %s.", snapshot.ID)
	if !killed {
		content = fmt.Sprintf("Background PowerShell command %s is not running.", snapshot.ID)
	}
	return contracts.ToolResult{
		Content: content,
		StructuredContent: map[string]any{
			"type":          "kill_powershell",
			"powershell_id": snapshot.ID,
			"command":       snapshot.Command,
			"running":       snapshot.Running,
			"killed":        killed,
			"cancelled":     snapshot.Cancelled,
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

func startBackgroundPowerShell(ctx tool.Context, input powerShellInput, timeout time.Duration) (contracts.ToolResult, error) {
	state := EnsureBackgroundState(ctx)
	if state == nil {
		return contracts.ToolResult{}, fmt.Errorf("background powershell state is not available")
	}
	name, ok := powerShellExecutable()
	if !ok {
		result := powerShellResult{
			Stderr:     "PowerShell executable not found. Install pwsh or powershell to use this tool.",
			ExitCode:   -1,
			TimeoutMS:  int(timeout / time.Millisecond),
			DurationMS: 0,
		}
		return contracts.ToolResult{
			Content: formatPowerShellContent(result),
			IsError: true,
			StructuredContent: map[string]any{
				"type":        "powershell",
				"command":     input.Command,
				"description": input.Description,
				"stdout":      "",
				"stderr":      result.Stderr,
				"exit_code":   result.ExitCode,
				"timed_out":   false,
				"duration_ms": result.DurationMS,
				"timeout_ms":  result.TimeoutMS,
				"executable":  "",
			},
		}, nil
	}
	command := strings.TrimSpace(input.Command)
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	cmd := exec.CommandContext(runCtx, name, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
	configurePowerShellCommand(cmd)
	if ctx.WorkingDirectory != "" {
		cmd.Dir = ctx.WorkingDirectory
	}
	task := &BackgroundTask{
		ID:          "powershell_" + string(contracts.NewID()),
		Command:     command,
		Description: input.Description,
		StartedAt:   time.Now(),
		TimeoutMS:   int(timeout / time.Millisecond),
		Executable:  name,
		Running:     true,
		ExitCode:    0,
	}
	cmd.Stdout = &task.stdout
	cmd.Stderr = &task.stderr
	task.SetCancel(func() {
		if cmd.Cancel != nil {
			_ = cmd.Cancel()
		}
		cancel()
	})
	if err := cmd.Start(); err != nil {
		cancel()
		return contracts.ToolResult{}, err
	}
	state.Add(task)
	go func() {
		defer cancel()
		err := cmd.Wait()
		durationMS := time.Since(task.StartedAt).Milliseconds()
		timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
		cancelled := task.IsCancelled()
		exitCode := 0
		errText := ""
		if err != nil {
			exitCode = 1
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else if !timedOut && !cancelled {
				errText = err.Error()
			}
			if timedOut || cancelled {
				exitCode = -1
			}
		}
		if cancelled && errText == "" {
			errText = "cancelled"
		}
		task.Finish(exitCode, timedOut, durationMS, errText, time.Now())
	}()
	return contracts.ToolResult{
		Content: fmt.Sprintf("PowerShell command started in background with ID: %s", task.ID),
		StructuredContent: map[string]any{
			"type":          "powershell_background",
			"powershell_id": task.ID,
			"command":       command,
			"description":   input.Description,
			"running":       true,
			"timeout_ms":    task.TimeoutMS,
			"started_at":    task.StartedAt.UTC().Format(time.RFC3339Nano),
			"executable":    task.Executable,
		},
	}, nil
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

func formatBackgroundOutput(snapshot BackgroundTaskSnapshot) string {
	var status string
	if snapshot.Running {
		status = fmt.Sprintf("Background PowerShell command %s is still running.", snapshot.ID)
	} else if snapshot.Cancelled {
		status = fmt.Sprintf("Background PowerShell command %s was cancelled.", snapshot.ID)
	} else if snapshot.TimedOut {
		status = fmt.Sprintf("Background PowerShell command %s timed out after %dms.", snapshot.ID, snapshot.TimeoutMS)
	} else {
		status = fmt.Sprintf("Background PowerShell command %s completed with exit code %d.", snapshot.ID, snapshot.ExitCode)
	}
	output := formatPowerShellContent(powerShellResult{
		Stdout:    snapshot.Stdout,
		Stderr:    snapshot.Stderr,
		ExitCode:  snapshot.ExitCode,
		TimedOut:  snapshot.TimedOut,
		TimeoutMS: snapshot.TimeoutMS,
	})
	if snapshot.Running && snapshot.Stdout == "" && snapshot.Stderr == "" {
		return status
	}
	if output == "" {
		return status
	}
	return status + "\n\n" + output
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
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
		case "command", "timeout", "description", "run_in_background", "runInBackground":
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
	if _, ok := obj["run_in_background"]; ok {
		input.hasRunInBackground = true
	}
	if _, ok := obj["runInBackground"]; ok {
		input.hasRunInBackgroundAlt = true
	}
	return input, nil
}

func decodePowerShellOutput(raw json.RawMessage) (powerShellOutputInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return powerShellOutputInput{}, err
	}
	for key := range obj {
		switch key {
		case "powershell_id", "id", "tail_lines", "tailLines":
		default:
			return powerShellOutputInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input powerShellOutputInput
	data, err := json.Marshal(obj)
	if err != nil {
		return powerShellOutputInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return powerShellOutputInput{}, err
	}
	return input, nil
}

func decodeKillPowerShell(raw json.RawMessage) (powerShellKillInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return powerShellKillInput{}, err
	}
	for key := range obj {
		switch key {
		case "powershell_id", "id":
		default:
			return powerShellKillInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input powerShellKillInput
	data, err := json.Marshal(obj)
	if err != nil {
		return powerShellKillInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return powerShellKillInput{}, err
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

func (input powerShellInput) runInBackground() bool {
	if input.hasRunInBackground {
		return input.RunInBackground
	}
	if input.hasRunInBackgroundAlt {
		return input.RunInBackgroundAlt
	}
	return false
}

func (input powerShellOutputInput) backgroundID() string {
	if strings.TrimSpace(input.PowerShellID) != "" {
		return strings.TrimSpace(input.PowerShellID)
	}
	return strings.TrimSpace(input.ID)
}

func (input powerShellKillInput) backgroundID() string {
	if strings.TrimSpace(input.PowerShellID) != "" {
		return strings.TrimSpace(input.PowerShellID)
	}
	return strings.TrimSpace(input.ID)
}

func powerShellOutputTailLines(input powerShellOutputInput) int {
	if input.TailLines != nil {
		return *input.TailLines
	}
	if input.TailLinesAlt != nil {
		return *input.TailLinesAlt
	}
	return 0
}

func tailText(text string, lines int) string {
	if lines <= 0 || text == "" {
		return text
	}
	trimmed := strings.TrimRight(text, "\n")
	parts := strings.Split(trimmed, "\n")
	if len(parts) <= lines {
		return trimmed
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
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
		if ch == '>' || ch == '$' || ch == '=' || ch == '&' || ch == '(' || ch == ')' || ch == '{' || ch == '}' {
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
	command := canonicalCommand(words[0])
	switch command {
	case "get-content", "get-item", "test-path", "resolve-path", "get-childitem", "get-filehash", "get-acl", "format-hex", "select-string":
		return readOnlyFileWords(words[1:])
	case "get-process", "get-service", "get-location", "write-output", "write-host":
		return readOnlyNonFileWords(words[1:])
	default:
		return false
	}
}

func readOnlyFileWords(words []string) bool {
	for i := 0; i < len(words); i++ {
		word := words[i]
		if word == "--%" {
			return false
		}
		if strings.HasPrefix(word, "-") {
			option, value, hasValue := splitPowerShellOption(word)
			if unsafePowerShellOption(option) {
				return false
			}
			if powerShellPathOption(option) {
				if !hasValue {
					i++
					if i >= len(words) {
						return false
					}
					value = words[i]
				}
				if !safeRelativePowerShellPath(value) {
					return false
				}
			}
			continue
		}
		if !safeRelativePowerShellPath(word) {
			return false
		}
	}
	return true
}

func readOnlyNonFileWords(words []string) bool {
	for _, word := range words {
		if word == "--%" {
			return false
		}
		option, _, _ := splitPowerShellOption(word)
		if unsafePowerShellOption(option) {
			return false
		}
	}
	return true
}

func destructiveWords(words []string) bool {
	switch canonicalCommand(words[0]) {
	case "remove-item", "del", "erase", "set-content", "add-content", "clear-content", "out-file", "new-item", "move-item", "stop-process", "stop-service", "restart-computer", "invoke-expression", "iex", "start-process":
		return true
	default:
		return strings.HasPrefix(canonicalCommand(words[0]), "remove-") || strings.HasPrefix(canonicalCommand(words[0]), "set-") || strings.HasPrefix(canonicalCommand(words[0]), "new-")
	}
}

func splitPowerShellOption(word string) (string, string, bool) {
	if !strings.HasPrefix(word, "-") {
		return "", "", false
	}
	option := strings.TrimLeft(word, "-")
	for _, sep := range []string{":", "="} {
		if idx := strings.Index(option, sep); idx >= 0 {
			return strings.ToLower(strings.TrimSpace(option[:idx])), strings.TrimSpace(option[idx+1:]), true
		}
	}
	return strings.ToLower(strings.TrimSpace(option)), "", false
}

func unsafePowerShellOption(option string) bool {
	switch strings.ToLower(strings.TrimSpace(option)) {
	case "outfile", "out-file", "outvariable", "ov", "pipelinevariable", "pv":
		return true
	default:
		return false
	}
}

func powerShellPathOption(option string) bool {
	switch strings.ToLower(strings.TrimSpace(option)) {
	case "path", "literalpath", "pspath", "filepath":
		return true
	default:
		return false
	}
}

func safeRelativePowerShellPath(path string) bool {
	path = strings.Trim(strings.TrimSpace(path), `"'`)
	if path == "" {
		return false
	}
	lower := strings.ToLower(path)
	if strings.ContainsAny(path, "$`\x00") {
		return false
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) || strings.HasPrefix(path, "~") {
		return false
	}
	if len(path) >= 2 && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) && path[1] == ':' {
		return false
	}
	if strings.Contains(path, ":") || strings.HasPrefix(lower, "env:") || strings.HasPrefix(lower, "hklm:") || strings.HasPrefix(lower, "hkcu:") {
		return false
	}
	normalized := strings.ReplaceAll(path, `\`, "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return false
		}
	}
	return true
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
