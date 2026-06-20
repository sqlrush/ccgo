package powershelltools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
	bashtools "ccgo/internal/tools/bash"
)

const (
	defaultTimeoutMillis = 120_000
	maxTimeoutMillis     = 600_000
	blockedSleepGuidance = "Run blocking commands in the background with run_in_background: true -- you'll get a completion notification when done. For streaming events, use the Monitor tool. If you genuinely need a delay, keep it under 2 seconds."
)

var powerShellSemanticNumberLiteralRE = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

var powerShellSemanticNumberKeys = map[string]struct{}{
	"timeout":    {},
	"tail_lines": {},
	"tailLines":  {},
}

var powerShellSemanticBooleanKeys = map[string]struct{}{
	"run_in_background":         {},
	"runInBackground":           {},
	"dangerouslyDisableSandbox": {},
}

type powerShellInput struct {
	Command                   string `json:"command"`
	Timeout                   *int   `json:"timeout,omitempty"`
	Description               string `json:"description,omitempty"`
	RunInBackground           bool   `json:"run_in_background,omitempty"`
	RunInBackgroundAlt        bool   `json:"runInBackground,omitempty"`
	DangerouslyDisableSandbox bool   `json:"dangerouslyDisableSandbox,omitempty"`
	hasRunInBackground        bool
	hasRunInBackgroundAlt     bool
}

type powerShellResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	TimedOut   bool
	Cancelled  bool
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
					"command":                   map[string]any{"type": "string"},
					"timeout":                   map[string]any{"type": "integer"},
					"description":               map[string]any{"type": "string"},
					"run_in_background":         map[string]any{"type": "boolean"},
					"runInBackground":           map[string]any{"type": "boolean"},
					"dangerouslyDisableSandbox": map[string]any{"type": "boolean"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Runs a PowerShell command in the current working directory. Provide command, optional timeout in milliseconds, optional short description, and run_in_background for background commands. Full sandbox parity and advanced interrupt controls are not implemented yet.", nil
		},
		NormalizeFunc:   normalizePowerShellRawInput,
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
		NormalizeFunc:   normalizePowerShellOutputRawInput,
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
	if !input.runInBackground() {
		if sleepPattern := detectBlockedPowerShellSleepPattern(input.Command); sleepPattern != "" {
			return fmt.Errorf("Blocked: %s. %s", sleepPattern, blockedSleepGuidance)
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

func callPowerShell(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodePowerShell(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if input.runInBackground() {
		return startBackgroundPowerShell(ctx, input, powerShellTimeout(input), sink)
	}
	result := runPowerShellCommand(ctx, strings.TrimSpace(input.Command), powerShellTimeout(input))
	return contracts.ToolResult{
		Content: formatPowerShellContent(result),
		IsError: result.TimedOut || result.ExitCode != 0,
		StructuredContent: map[string]any{
			"type":                        "powershell",
			"command":                     input.Command,
			"description":                 input.Description,
			"stdout":                      result.Stdout,
			"stderr":                      result.Stderr,
			"exit_code":                   result.ExitCode,
			"timed_out":                   result.TimedOut,
			"cancelled":                   result.Cancelled,
			"duration_ms":                 result.DurationMS,
			"timeout_ms":                  result.TimeoutMS,
			"executable":                  result.Executable,
			"dangerously_disable_sandbox": input.DangerouslyDisableSandbox,
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
			"tail_lines":    tailLines,
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
	if runCtx.Err() == context.Canceled {
		result.Cancelled = true
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

func startBackgroundPowerShell(ctx tool.Context, input powerShellInput, timeout time.Duration, sink tool.ProgressSink) (contracts.ToolResult, error) {
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
				"type":                        "powershell",
				"command":                     input.Command,
				"description":                 input.Description,
				"stdout":                      "",
				"stderr":                      result.Stderr,
				"exit_code":                   result.ExitCode,
				"timed_out":                   false,
				"cancelled":                   false,
				"duration_ms":                 result.DurationMS,
				"timeout_ms":                  result.TimeoutMS,
				"executable":                  "",
				"dangerously_disable_sandbox": input.DangerouslyDisableSandbox,
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
	sendPowerShellBackgroundProgress(sink, "powershell_background_started", task.Snapshot())
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
		sendPowerShellBackgroundProgress(sink, "powershell_background_finished", task.Snapshot())
	}()
	return contracts.ToolResult{
		Content: fmt.Sprintf("PowerShell command started in background with ID: %s", task.ID),
		StructuredContent: map[string]any{
			"type":                        "powershell_background",
			"powershell_id":               task.ID,
			"command":                     command,
			"description":                 input.Description,
			"running":                     true,
			"timeout_ms":                  task.TimeoutMS,
			"started_at":                  task.StartedAt.UTC().Format(time.RFC3339Nano),
			"executable":                  task.Executable,
			"dangerously_disable_sandbox": input.DangerouslyDisableSandbox,
		},
	}, nil
}

func sendPowerShellBackgroundProgress(sink tool.ProgressSink, progressType string, snapshot BackgroundTaskSnapshot) {
	status := "running"
	if !snapshot.Running {
		switch {
		case snapshot.Cancelled:
			status = "cancelled"
		case snapshot.TimedOut:
			status = "timed_out"
		case snapshot.ExitCode != 0:
			status = "failed"
		default:
			status = "completed"
		}
	}
	_ = tool.SendProgress(sink, "", progressType, map[string]any{
		"shell":         "powershell",
		"powershell_id": snapshot.ID,
		"status":        status,
		"running":       snapshot.Running,
		"exit_code":     snapshot.ExitCode,
		"timed_out":     snapshot.TimedOut,
		"cancelled":     snapshot.Cancelled,
		"duration_ms":   snapshot.DurationMS,
		"timeout_ms":    snapshot.TimeoutMS,
		"stdout_bytes":  len(snapshot.Stdout),
		"stderr_bytes":  len(snapshot.Stderr),
		"started_at":    snapshot.StartedAt.UTC().Format(time.RFC3339Nano),
		"ended_at":      formatOptionalTime(snapshot.EndedAt),
		"executable":    snapshot.Executable,
	})
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
	case result.Cancelled:
		status = "Command cancelled."
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
		Cancelled: snapshot.Cancelled,
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
		case "command", "timeout", "description", "run_in_background", "runInBackground", "dangerouslyDisableSandbox":
		default:
			return powerShellInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	coercePowerShellSemanticJSONStrings(obj, powerShellSemanticNumberKeys, powerShellSemanticBooleanKeys)
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

func normalizePowerShellRawInput(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "command", "timeout", "description", "run_in_background", "runInBackground", "dangerouslyDisableSandbox":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	coercePowerShellSemanticJSONStrings(obj, powerShellSemanticNumberKeys, powerShellSemanticBooleanKeys)
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return data, nil
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
	coercePowerShellSemanticJSONStrings(obj, powerShellSemanticNumberKeys, nil)
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

func normalizePowerShellOutputRawInput(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "powershell_id", "id", "tail_lines", "tailLines":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	coercePowerShellSemanticJSONStrings(obj, powerShellSemanticNumberKeys, nil)
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func coercePowerShellSemanticJSONStrings(obj map[string]json.RawMessage, numberKeys map[string]struct{}, boolKeys map[string]struct{}) {
	for key, raw := range obj {
		if len(raw) == 0 || raw[0] != '"' {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			continue
		}
		if _, ok := boolKeys[key]; ok {
			switch text {
			case "true", "false":
				obj[key] = json.RawMessage(text)
			}
			continue
		}
		if _, ok := numberKeys[key]; ok && powerShellSemanticNumberLiteralRE.MatchString(text) {
			obj[key] = json.RawMessage(text)
		}
	}
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

func detectBlockedPowerShellSleepPattern(command string) string {
	segments := splitPowerShellSegments(command)
	if len(segments) == 0 {
		return ""
	}
	words := powerShellWords(segments[0])
	if len(words) == 0 {
		return ""
	}
	name := canonicalCommand(words[0])
	secondsArg := ""
	switch {
	case name == "start-sleep" && len(words) == 2:
		secondsArg = words[1]
	case name == "start-sleep" && len(words) == 3 && isPowerShellSecondsFlag(words[1]):
		secondsArg = words[2]
	default:
		return ""
	}
	if !isUnsignedPowerShellInteger(secondsArg) {
		return ""
	}
	seconds, err := strconv.Atoi(secondsArg)
	if err != nil || seconds < 2 {
		return ""
	}
	rest := strings.TrimSpace(strings.Join(segments[1:], " "))
	if rest != "" {
		return fmt.Sprintf("Start-Sleep %d followed by: %s", seconds, rest)
	}
	return fmt.Sprintf("standalone Start-Sleep %d", seconds)
}

func isPowerShellSecondsFlag(value string) bool {
	value = strings.ToLower(value)
	return value == "-s" || value == "-seconds"
}

func isUnsignedPowerShellInteger(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
	command = strings.TrimSpace(stripPowerShellLineComments(command))
	if command == "" || !powerShellSyntaxComplete(command) || hasPowerShellMutationSyntax(command) || IsDestructiveCommand(command) {
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
	command = stripPowerShellLineComments(command)
	if hasNestedPowerShellDestructiveCommand(command) {
		return true
	}
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
		if ch == '`' && !inSingle {
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

func powerShellSyntaxComplete(command string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '`' && !inSingle {
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
	}
	return !inSingle && !inDouble && !escaped
}

func hasNestedPowerShellDestructiveCommand(command string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '`' && !inSingle {
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
		if ch == '$' && i+1 < len(command) && command[i+1] == '(' {
			end := findPowerShellClosingPair(command, i+1, '(', ')')
			if end < 0 {
				return false
			}
			if IsDestructiveCommand(command[i+2 : end]) {
				return true
			}
			i = end
			continue
		}
		if !inDouble && ch == '(' {
			end := findPowerShellClosingPair(command, i, '(', ')')
			if end < 0 {
				return false
			}
			if IsDestructiveCommand(command[i+1 : end]) {
				return true
			}
			i = end
			continue
		}
		if !inDouble && ch == '{' && (i == 0 || command[i-1] != '$') {
			end := findPowerShellClosingPair(command, i, '{', '}')
			if end < 0 {
				return false
			}
			if IsDestructiveCommand(command[i+1 : end]) {
				return true
			}
			i = end
		}
	}
	return false
}

func findPowerShellClosingPair(command string, open int, openCh byte, closeCh byte) int {
	depth := 1
	inSingle := false
	inDouble := false
	escaped := false
	for i := open + 1; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '`' && !inSingle {
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
		if inSingle || inDouble {
			continue
		}
		switch ch {
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func stripPowerShellLineComments(command string) string {
	var stripped strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	wordStart := true
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			stripped.WriteByte(ch)
			escaped = false
			wordStart = false
			continue
		}
		if ch == '`' && !inSingle {
			stripped.WriteByte(ch)
			escaped = true
			wordStart = false
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			stripped.WriteByte(ch)
			wordStart = false
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			stripped.WriteByte(ch)
			wordStart = false
			continue
		}
		if !inSingle && !inDouble && ch == '#' && wordStart {
			for i+1 < len(command) && command[i+1] != '\n' && command[i+1] != '\r' {
				i++
			}
			continue
		}
		stripped.WriteByte(ch)
		if !inSingle && !inDouble && (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == ';' || ch == '|' || ch == '&') {
			wordStart = true
		} else {
			wordStart = false
		}
	}
	return stripped.String()
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
		if ch == '`' && !inSingle {
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
		if !inSingle && !inDouble && (ch == ';' || ch == '|' || ch == '&' || ch == '\n' || ch == '\r') {
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
		if ch == '`' && !inSingle {
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

type powerShellReadOnlyConfig struct {
	allowedFlags                 map[string]bool
	unsafeFlags                  map[string]bool
	pathFlags                    map[string]bool
	valueFlags                   map[string]bool
	allowAllFlags                bool
	rejectExpressionValues       bool
	validatePositionalsAsPaths   bool
	pathPositionalsAfterLiterals int
}

type powerShellNativeReadOnlyConfig struct {
	allowedFlags                 map[string]bool
	pathFlags                    map[string]bool
	pathListFlags                map[string]bool
	valueFlags                   map[string]bool
	literalValueFlags            map[string]bool
	allowAllFlags                bool
	allowPositionals             bool
	rejectPositionals            bool
	validatePositionalsAsPaths   bool
	pathPositionalsAfterLiterals int
}

var powerShellCommonSwitchFlags = stringSet("verbose", "vb", "debug", "db")

var powerShellCommonValueFlags = stringSet(
	"erroraction",
	"ea",
	"warningaction",
	"wa",
	"informationaction",
	"infa",
	"progressaction",
	"proga",
	"errorvariable",
	"ev",
	"warningvariable",
	"wv",
	"informationvariable",
	"iv",
	"outbuffer",
	"ob",
)

var powerShellReadOnlyCmdlets = map[string]powerShellReadOnlyConfig{
	"get-childitem": {
		allowedFlags:               stringSet("path", "literalpath", "filter", "include", "exclude", "recurse", "depth", "name", "force", "attributes", "directory", "file", "hidden", "readonly", "system"),
		pathFlags:                  stringSet("path", "literalpath"),
		valueFlags:                 stringSet("filter", "include", "exclude", "depth", "attributes"),
		validatePositionalsAsPaths: true,
	},
	"get-content": {
		allowedFlags:               stringSet("path", "literalpath", "totalcount", "head", "first", "tail", "raw", "encoding", "delimiter", "readcount"),
		unsafeFlags:                stringSet("wait"),
		pathFlags:                  stringSet("path", "literalpath"),
		valueFlags:                 stringSet("totalcount", "head", "first", "tail", "encoding", "delimiter", "readcount"),
		validatePositionalsAsPaths: true,
	},
	"get-item": {
		allowedFlags:               stringSet("path", "literalpath", "force", "stream"),
		pathFlags:                  stringSet("path", "literalpath"),
		valueFlags:                 stringSet("stream"),
		validatePositionalsAsPaths: true,
	},
	"test-path": {
		allowedFlags:               stringSet("path", "literalpath", "pathtype", "filter", "include", "exclude", "isvalid", "newerthan", "olderthan"),
		pathFlags:                  stringSet("path", "literalpath"),
		valueFlags:                 stringSet("pathtype", "filter", "include", "exclude", "newerthan", "olderthan"),
		validatePositionalsAsPaths: true,
	},
	"resolve-path": {
		allowedFlags:               stringSet("path", "literalpath", "relative"),
		pathFlags:                  stringSet("path", "literalpath"),
		validatePositionalsAsPaths: true,
	},
	"split-path": {
		allowedFlags:               stringSet("path", "literalpath", "parent", "leaf", "leafbase", "extension", "qualifier", "noqualifier", "isabsolute"),
		pathFlags:                  stringSet("path", "literalpath"),
		validatePositionalsAsPaths: true,
	},
	"get-filehash": {
		allowedFlags:               stringSet("path", "literalpath", "algorithm"),
		unsafeFlags:                stringSet("inputstream"),
		pathFlags:                  stringSet("path", "literalpath"),
		valueFlags:                 stringSet("algorithm"),
		validatePositionalsAsPaths: true,
	},
	"get-acl": {
		allowedFlags:               stringSet("path", "literalpath", "audit", "filter", "include", "exclude"),
		pathFlags:                  stringSet("path", "literalpath"),
		valueFlags:                 stringSet("filter", "include", "exclude"),
		validatePositionalsAsPaths: true,
	},
	"format-hex": {
		allowedFlags:               stringSet("path", "literalpath", "inputobject", "encoding", "count", "offset"),
		pathFlags:                  stringSet("path", "literalpath"),
		valueFlags:                 stringSet("inputobject", "encoding", "count", "offset"),
		validatePositionalsAsPaths: true,
	},
	"select-string": {
		allowedFlags:                 stringSet("path", "literalpath", "pattern", "inputobject", "simplematch", "casesensitive", "quiet", "list", "notmatch", "allmatches", "encoding", "context", "raw", "noemphasis"),
		pathFlags:                    stringSet("path", "literalpath"),
		valueFlags:                   stringSet("pattern", "inputobject", "encoding", "context"),
		validatePositionalsAsPaths:   true,
		pathPositionalsAfterLiterals: 1,
	},
	"convertto-json": {
		allowedFlags: stringSet("inputobject", "depth", "compress", "enumsasstrings", "asarray"),
		valueFlags:   stringSet("inputobject", "depth"),
	},
	"convertfrom-json": {
		allowedFlags: stringSet("inputobject", "depth", "ashashtable", "noenumerate"),
		valueFlags:   stringSet("inputobject", "depth"),
	},
	"convertto-csv": {
		allowedFlags: stringSet("inputobject", "delimiter", "notypeinformation", "noheader", "usequotes"),
		valueFlags:   stringSet("inputobject", "delimiter", "usequotes"),
	},
	"convertfrom-csv": {
		allowedFlags: stringSet("inputobject", "delimiter", "header", "useculture"),
		valueFlags:   stringSet("inputobject", "delimiter", "header"),
	},
	"convertto-xml": {
		allowedFlags: stringSet("inputobject", "depth", "as", "notypeinformation"),
		valueFlags:   stringSet("inputobject", "depth", "as"),
	},
	"convertto-html": {
		allowedFlags: stringSet("inputobject", "property", "head", "title", "body", "pre", "post", "as", "fragment"),
		valueFlags:   stringSet("inputobject", "property", "head", "title", "body", "pre", "post", "as"),
	},
	"get-member": {
		allowedFlags: stringSet("inputobject", "membertype", "name", "static", "view", "force"),
		valueFlags:   stringSet("inputobject", "membertype", "name", "view"),
	},
	"get-unique": {
		allowedFlags: stringSet("inputobject", "asstring", "caseinsensitive", "ontype"),
		valueFlags:   stringSet("inputobject"),
	},
	"compare-object": {
		allowedFlags: stringSet("referenceobject", "differenceobject", "property", "syncwindow", "casesensitive", "culture", "excludedifferent", "includeequal", "passthru"),
		valueFlags:   stringSet("referenceobject", "differenceobject", "property", "syncwindow", "culture"),
	},
	"join-string": {
		allowedFlags: stringSet("inputobject", "property", "separator", "outputprefix", "outputsuffix", "singlequote", "doublequote", "formatstring"),
		valueFlags:   stringSet("inputobject", "property", "separator", "outputprefix", "outputsuffix", "formatstring"),
	},
	"get-random": {
		allowedFlags: stringSet("inputobject", "minimum", "maximum", "count", "setseed", "shuffle"),
		valueFlags:   stringSet("inputobject", "minimum", "maximum", "count", "setseed"),
	},
	"convert-path": {
		allowedFlags:               stringSet("path", "literalpath"),
		pathFlags:                  stringSet("path", "literalpath"),
		validatePositionalsAsPaths: true,
	},
	"get-itemproperty": {
		allowedFlags:               stringSet("path", "literalpath", "name"),
		pathFlags:                  stringSet("path", "literalpath"),
		valueFlags:                 stringSet("name"),
		validatePositionalsAsPaths: true,
	},
	"get-itempropertyvalue": {
		allowedFlags:               stringSet("path", "literalpath", "name"),
		pathFlags:                  stringSet("path", "literalpath"),
		valueFlags:                 stringSet("name"),
		validatePositionalsAsPaths: true,
	},
	"get-hotfix": {
		allowedFlags: stringSet("id", "description"),
		valueFlags:   stringSet("id", "description"),
	},
	"get-psprovider": {
		allowedFlags: stringSet("psprovider"),
		valueFlags:   stringSet("psprovider"),
	},
	"get-process": {
		allowedFlags: stringSet("name", "id", "module", "fileversioninfo", "includeusername"),
		valueFlags:   stringSet("name", "id"),
	},
	"get-service": {
		allowedFlags: stringSet("name", "displayname", "dependentservices", "requiredservices", "include", "exclude"),
		valueFlags:   stringSet("name", "displayname", "include", "exclude"),
	},
	"get-location": {
		allowedFlags: stringSet("psprovider", "psdrive", "stack", "stackname"),
		valueFlags:   stringSet("psprovider", "psdrive", "stackname"),
	},
	"get-date": {
		allowedFlags: stringSet("date", "format", "uformat", "displayhint", "asutc"),
		valueFlags:   stringSet("date", "format", "uformat", "displayhint"),
	},
	"get-psdrive": {
		allowedFlags: stringSet("name", "psprovider", "scope"),
		valueFlags:   stringSet("name", "psprovider", "scope"),
	},
	"get-module": {
		allowedFlags: stringSet("name", "listavailable", "all", "fullyqualifiedname", "psedition"),
		valueFlags:   stringSet("name", "fullyqualifiedname", "psedition"),
	},
	"get-alias": {
		allowedFlags: stringSet("name", "definition", "scope", "exclude"),
		valueFlags:   stringSet("name", "definition", "scope", "exclude"),
	},
	"get-history": {
		allowedFlags: stringSet("id", "count"),
		valueFlags:   stringSet("id", "count"),
	},
	"get-timezone": {
		allowedFlags: stringSet("name", "id", "listavailable"),
		valueFlags:   stringSet("name", "id"),
	},
	"get-computerinfo": {
		allowAllFlags: true,
	},
	"get-host": {
		allowAllFlags: true,
	},
	"get-culture": {
		allowAllFlags: true,
	},
	"get-uiculture": {
		allowAllFlags: true,
	},
	"get-uptime": {
		allowAllFlags: true,
	},
	"get-netadapter": {
		allowedFlags: stringSet("name", "interfacedescription", "interfaceindex", "physical"),
		valueFlags:   stringSet("name", "interfacedescription", "interfaceindex"),
	},
	"get-netipaddress": {
		allowedFlags: stringSet("interfaceindex", "interfacealias", "addressfamily", "type"),
		valueFlags:   stringSet("interfaceindex", "interfacealias", "addressfamily", "type"),
	},
	"get-netipconfiguration": {
		allowedFlags: stringSet("interfaceindex", "interfacealias", "detailed", "all"),
		valueFlags:   stringSet("interfaceindex", "interfacealias"),
	},
	"get-netroute": {
		allowedFlags: stringSet("interfaceindex", "interfacealias", "addressfamily", "destinationprefix"),
		valueFlags:   stringSet("interfaceindex", "interfacealias", "addressfamily", "destinationprefix"),
	},
	"get-dnsclientcache": {
		allowedFlags: stringSet("entry", "name", "type", "status", "section", "data"),
		valueFlags:   stringSet("entry", "name", "type", "status", "section", "data"),
	},
	"get-dnsclient": {
		allowedFlags: stringSet("interfaceindex", "interfacealias"),
		valueFlags:   stringSet("interfaceindex", "interfacealias"),
	},
	"get-eventlog": {
		allowedFlags: stringSet("logname", "newest", "after", "before", "entrytype", "index", "instanceid", "message", "source", "username", "asbaseobject", "list"),
		valueFlags:   stringSet("logname", "newest", "after", "before", "entrytype", "index", "instanceid", "message", "source", "username"),
	},
	"get-winevent": {
		allowedFlags:               stringSet("logname", "listlog", "listprovider", "providername", "path", "maxevents", "filterxpath", "force", "oldest"),
		pathFlags:                  stringSet("path"),
		valueFlags:                 stringSet("logname", "listlog", "listprovider", "providername", "maxevents", "filterxpath"),
		validatePositionalsAsPaths: true,
	},
	"get-cimclass": {
		allowedFlags: stringSet("classname", "namespace", "methodname", "propertyname", "qualifiername"),
		valueFlags:   stringSet("classname", "namespace", "methodname", "propertyname", "qualifiername"),
	},
	"start-sleep": {
		allowedFlags:           stringSet("seconds", "milliseconds", "duration"),
		valueFlags:             stringSet("seconds", "milliseconds", "duration"),
		rejectExpressionValues: true,
	},
	"format-table": {
		allowAllFlags:          true,
		rejectExpressionValues: true,
	},
	"format-list": {
		allowAllFlags:          true,
		rejectExpressionValues: true,
	},
	"format-wide": {
		allowAllFlags:          true,
		rejectExpressionValues: true,
	},
	"format-custom": {
		allowAllFlags:          true,
		rejectExpressionValues: true,
	},
	"measure-object": {
		allowAllFlags:          true,
		rejectExpressionValues: true,
	},
	"select-object": {
		allowAllFlags:          true,
		rejectExpressionValues: true,
	},
	"sort-object": {
		allowAllFlags:          true,
		rejectExpressionValues: true,
	},
	"group-object": {
		allowAllFlags:          true,
		rejectExpressionValues: true,
	},
	"where-object": {
		allowAllFlags:          true,
		rejectExpressionValues: true,
	},
	"out-string": {
		allowAllFlags:          true,
		rejectExpressionValues: true,
	},
	"out-host": {
		allowAllFlags:          true,
		unsafeFlags:            stringSet("paging"),
		rejectExpressionValues: true,
	},
	"write-output": {
		allowedFlags:           stringSet("inputobject", "noenumerate"),
		valueFlags:             stringSet("inputobject"),
		rejectExpressionValues: true,
	},
	"write-host": {
		allowedFlags:           stringSet("object", "nonewline", "separator", "foregroundcolor", "backgroundcolor"),
		valueFlags:             stringSet("object", "separator", "foregroundcolor", "backgroundcolor"),
		rejectExpressionValues: true,
	},
}

var powerShellNativeReadOnlyCommands = map[string]powerShellNativeReadOnlyConfig{
	"ipconfig": {
		allowedFlags:      stringSet("/all", "/displaydns", "/allcompartments"),
		rejectPositionals: true,
	},
	"netstat": {
		allowedFlags: stringSet("-a", "-b", "-e", "-f", "-n", "-o", "-p", "-q", "-r", "-s", "-t", "-x", "-y"),
	},
	"systeminfo": {
		allowedFlags: stringSet("/fo", "/nh"),
		valueFlags:   stringSet("/fo"),
	},
	"tasklist": {
		allowedFlags: stringSet("/m", "/svc", "/v", "/fi", "/fo", "/nh"),
		valueFlags:   stringSet("/m", "/fi", "/fo"),
	},
	"where.exe": {
		allowedFlags:     stringSet("/r", "/q", "/f", "/t"),
		pathFlags:        stringSet("/r"),
		valueFlags:       stringSet("/r"),
		allowPositionals: true,
	},
	"hostname": {
		allowedFlags:      stringSet("-a", "-d", "-f", "-i", "-s", "-y", "-a"),
		rejectPositionals: true,
	},
	"whoami": {
		allowedFlags: stringSet("/user", "/groups", "/claims", "/priv", "/logonid", "/all", "/fo", "/nh"),
		valueFlags:   stringSet("/fo"),
	},
	"ver": {
		allowAllFlags: true,
	},
	"arp": {
		allowedFlags: stringSet("-a", "-g", "-v", "-n"),
		valueFlags:   stringSet("-n"),
	},
	"getmac": {
		allowedFlags: stringSet("/fo", "/nh", "/v"),
		valueFlags:   stringSet("/fo"),
	},
	"file": {
		allowedFlags:               stringSet("-b", "--brief", "-i", "--mime", "-l", "--dereference", "--mime-type", "--mime-encoding", "-z", "--uncompress", "-p", "--preserve-date", "-k", "--keep-going", "-r", "--raw", "-v", "--version", "-0", "--print0", "-s", "--special-files", "-l", "--separator", "-e", "-p", "-n", "--no-pad", "-e", "--extension"),
		valueFlags:                 stringSet("--separator", "-e"),
		allowPositionals:           true,
		validatePositionalsAsPaths: true,
	},
	"tree": {
		allowedFlags:               stringSet("/f", "/a", "/q", "/l"),
		valueFlags:                 stringSet("/l"),
		allowPositionals:           true,
		validatePositionalsAsPaths: true,
	},
	"findstr": {
		allowedFlags:                 stringSet("/b", "/e", "/l", "/r", "/s", "/i", "/x", "/v", "/n", "/m", "/o", "/p", "/c", "/g", "/d", "/a"),
		pathFlags:                    stringSet("/g", "/d"),
		pathListFlags:                stringSet("/d"),
		valueFlags:                   stringSet("/c", "/g", "/d", "/a"),
		literalValueFlags:            stringSet("/c"),
		allowPositionals:             true,
		validatePositionalsAsPaths:   true,
		pathPositionalsAfterLiterals: 1,
	},
	"md5sum":    powerShellChecksumReadOnlyConfig,
	"sha1sum":   powerShellChecksumReadOnlyConfig,
	"sha224sum": powerShellChecksumReadOnlyConfig,
	"sha256sum": powerShellChecksumReadOnlyConfig,
	"sha384sum": powerShellChecksumReadOnlyConfig,
	"sha512sum": powerShellChecksumReadOnlyConfig,
	"b2sum":     powerShellB2SumReadOnlyConfig,
	"shasum":    powerShellSHASumReadOnlyConfig,
	"cksum":     powerShellCKSumReadOnlyConfig,
	"sum":       powerShellSumReadOnlyConfig,
}

var powerShellDotnetReadOnlyFlags = stringSet("--version", "--info", "--list-runtimes", "--list-sdks")

var powerShellChecksumReadOnlyConfig = powerShellNativeReadOnlyConfig{
	allowedFlags:               stringSet("-b", "--binary", "-t", "--text", "--tag", "-z", "--zero"),
	allowPositionals:           true,
	validatePositionalsAsPaths: true,
}

var powerShellB2SumReadOnlyConfig = powerShellNativeReadOnlyConfig{
	allowedFlags:               stringSet("-b", "--binary", "-l", "--length", "--tag", "-z", "--zero"),
	valueFlags:                 stringSet("-l", "--length"),
	allowPositionals:           true,
	validatePositionalsAsPaths: true,
}

var powerShellSHASumReadOnlyConfig = powerShellNativeReadOnlyConfig{
	allowedFlags:               stringSet("-a", "--algorithm", "-b", "--binary", "-t", "--text", "-u", "--universal", "-0", "--portable"),
	valueFlags:                 stringSet("-a", "--algorithm"),
	allowPositionals:           true,
	validatePositionalsAsPaths: true,
}

var powerShellCKSumReadOnlyConfig = powerShellNativeReadOnlyConfig{
	allowedFlags:               stringSet("-a", "--algorithm", "--base64", "--raw"),
	valueFlags:                 stringSet("-a", "--algorithm"),
	allowPositionals:           true,
	validatePositionalsAsPaths: true,
}

var powerShellSumReadOnlyConfig = powerShellNativeReadOnlyConfig{
	allowedFlags:               stringSet("-r", "-s"),
	allowPositionals:           true,
	validatePositionalsAsPaths: true,
}

var powerShellFCReadOnlyFlags = stringSet("/a", "/b", "/c", "/l", "/n", "/t", "/u", "/w", "/off[line]")

var powerShellCompReadOnlyFlags = stringSet("/d", "/a", "/l", "/c", "/n", "/off[line]")

var powerShellCompValueFlags = stringSet("/n")

var powerShellNativeSortReadOnlyFlags = stringSet("/r")

func readOnlyWords(words []string) bool {
	command := canonicalCommand(words[0])
	switch command {
	case "git":
		return bashtools.IsReadOnlyCommand(powerShellGitCommand(words))
	case "certutil":
		return readOnlyCertutil(words[1:])
	case "fc.exe":
		return readOnlyNativeTwoPathCompare(words[1:], powerShellFCReadOnlyFlags, nil)
	case "comp.exe":
		return readOnlyNativeTwoPathCompare(words[1:], powerShellCompReadOnlyFlags, powerShellCompValueFlags)
	case "sort.exe":
		return readOnlyNativeOnePathCommand(words[1:], powerShellNativeSortReadOnlyFlags, nil)
	case "more.com":
		return readOnlyNativeOnePathCommand(words[1:], nil, nil)
	case "docker":
		return readOnlyDocker(words[1:])
	case "dotnet":
		return readOnlyDotnet(words[1:])
	case "route":
		return readOnlyRoute(words[1:])
	default:
		if config, ok := powerShellReadOnlyCmdlets[command]; ok {
			return readOnlyPowerShellArgs(words[1:], config)
		}
		config, ok := powerShellNativeReadOnlyCommands[command]
		return ok && readOnlyNativeArgs(words[1:], config)
	}
}

func readOnlyPowerShellArgs(words []string, config powerShellReadOnlyConfig) bool {
	positionals := 0
	for i := 0; i < len(words); i++ {
		word := words[i]
		if word == "--%" {
			return false
		}
		if strings.HasPrefix(word, "-") {
			option, value, hasValue := splitPowerShellOption(word)
			if unsafePowerShellOption(option) || config.unsafeFlags[option] {
				return false
			}
			takesValue := config.pathFlags[option] || config.valueFlags[option] || powerShellCommonValueFlags[option]
			switch {
			case config.allowAllFlags || config.allowedFlags[option] || powerShellCommonSwitchFlags[option] || powerShellCommonValueFlags[option]:
			default:
				return false
			}
			if hasValue {
				if !takesValue && !config.allowAllFlags {
					return false
				}
				if config.pathFlags[option] {
					if !safeRelativePowerShellPath(value) {
						return false
					}
					continue
				}
				if !safePowerShellParameterValue(value, config.rejectExpressionValues) {
					return false
				}
				continue
			}
			if takesValue {
				if !hasValue {
					i++
					if i >= len(words) {
						return false
					}
					value = words[i]
				}
				if config.pathFlags[option] {
					if !safeRelativePowerShellPath(value) {
						return false
					}
					continue
				}
				if !safePowerShellParameterValue(value, config.rejectExpressionValues) {
					return false
				}
			}
			continue
		}
		if config.validatePositionalsAsPaths && positionals >= config.pathPositionalsAfterLiterals {
			if !safeRelativePowerShellPath(word) {
				return false
			}
		} else if !safePowerShellParameterValue(word, config.rejectExpressionValues) {
			return false
		}
		positionals++
	}
	return true
}

func safePowerShellParameterValue(value string, rejectExpressions bool) bool {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	if value == "" || value == "--%" || strings.ContainsRune(value, '\x00') {
		return false
	}
	return !rejectExpressions || !strings.ContainsAny(value, "$@{[")
}

func readOnlyNativeArgs(words []string, config powerShellNativeReadOnlyConfig) bool {
	positionals := 0
	for i := 0; i < len(words); i++ {
		word := words[i]
		if word == "--%" {
			return false
		}
		name, value, hasValue, isFlag := splitNativeFlag(word)
		if isFlag {
			if !nativeFlagAllowed(name, config) {
				return false
			}
			takesValue := config.pathFlags[name] || config.valueFlags[name]
			if hasValue {
				if !takesValue {
					return false
				}
				if config.pathFlags[name] {
					if !safeNativePathValue(name, value, config) {
						return false
					}
				} else if !safeNativeValue(value) {
					return false
				}
				if config.literalValueFlags[name] && positionals < config.pathPositionalsAfterLiterals {
					positionals = config.pathPositionalsAfterLiterals
				}
				continue
			}
			if takesValue {
				i++
				if i >= len(words) || looksLikeNativeFlag(words[i]) {
					return false
				}
				if config.pathFlags[name] {
					if !safeNativePathValue(name, words[i], config) {
						return false
					}
				} else if !safeNativeValue(words[i]) {
					return false
				}
				if config.literalValueFlags[name] && positionals < config.pathPositionalsAfterLiterals {
					positionals = config.pathPositionalsAfterLiterals
				}
			}
			continue
		}
		if config.rejectPositionals || !config.allowPositionals {
			return false
		}
		if config.validatePositionalsAsPaths && positionals >= config.pathPositionalsAfterLiterals {
			if !safeRelativePowerShellPath(word) {
				return false
			}
		} else if !safeNativeValue(word) {
			return false
		}
		positionals++
	}
	return true
}

func safeNativePathValue(name string, value string, config powerShellNativeReadOnlyConfig) bool {
	if config.pathListFlags[name] {
		return safeRelativePowerShellPathList(value)
	}
	return safeRelativePowerShellPath(value)
}

func safeRelativePowerShellPathList(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	if value == "" {
		return false
	}
	for _, part := range strings.Split(value, ";") {
		if !safeRelativePowerShellPath(part) {
			return false
		}
	}
	return true
}

func nativeFlagAllowed(name string, config powerShellNativeReadOnlyConfig) bool {
	if config.allowAllFlags || config.allowedFlags[name] {
		return true
	}
	if len(name) <= 2 || !strings.HasPrefix(name, "-") || strings.HasPrefix(name, "--") {
		return false
	}
	for _, r := range name[1:] {
		if !config.allowedFlags["-"+string(r)] {
			return false
		}
	}
	return true
}

func readOnlyDotnet(words []string) bool {
	if len(words) == 0 {
		return false
	}
	for _, word := range words {
		if !powerShellDotnetReadOnlyFlags[strings.ToLower(strings.TrimSpace(word))] {
			return false
		}
	}
	return true
}

func readOnlyCertutil(words []string) bool {
	if len(words) < 2 || len(words) > 3 {
		return false
	}
	for _, word := range words {
		if strings.TrimSpace(word) == "--%" {
			return false
		}
	}
	if strings.ToLower(strings.Trim(strings.TrimSpace(words[0]), `"'`)) != "-hashfile" {
		return false
	}
	if !safeRelativePowerShellPath(words[1]) {
		return false
	}
	return len(words) == 2 || safeCertutilHashAlgorithm(words[2])
}

func safeCertutilHashAlgorithm(value string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`)) {
	case "md2", "md4", "md5", "sha1", "sha256", "sha384", "sha512":
		return true
	default:
		return false
	}
}

func readOnlyNativeTwoPathCompare(words []string, allowedFlags map[string]bool, valueFlags map[string]bool) bool {
	paths := 0
	for i := 0; i < len(words); i++ {
		word := words[i]
		if strings.TrimSpace(word) == "--%" {
			return false
		}
		name, value, hasValue, isFlag := splitNativeFlag(word)
		if isFlag {
			if !allowedFlags[name] {
				return false
			}
			takesValue := valueFlags[name]
			if hasValue {
				if !takesValue || !safeNativeValue(value) {
					return false
				}
				continue
			}
			if takesValue {
				i++
				if i >= len(words) || looksLikeNativeFlag(words[i]) || !safeNativeValue(words[i]) {
					return false
				}
			}
			continue
		}
		if !safeRelativePowerShellPath(word) {
			return false
		}
		paths++
	}
	return paths == 2
}

func readOnlyNativeOnePathCommand(words []string, allowedFlags map[string]bool, valueFlags map[string]bool) bool {
	paths := 0
	for i := 0; i < len(words); i++ {
		word := words[i]
		if strings.TrimSpace(word) == "--%" {
			return false
		}
		name, value, hasValue, isFlag := splitNativeFlag(word)
		if isFlag {
			if !allowedFlags[name] {
				return false
			}
			takesValue := valueFlags[name]
			if hasValue {
				if !takesValue || !safeNativeValue(value) {
					return false
				}
				continue
			}
			if takesValue {
				i++
				if i >= len(words) || looksLikeNativeFlag(words[i]) || !safeNativeValue(words[i]) {
					return false
				}
			}
			continue
		}
		if !safeRelativePowerShellPath(word) {
			return false
		}
		paths++
	}
	return paths == 1
}

func readOnlyDocker(words []string) bool {
	if len(words) == 0 {
		return true
	}
	for _, word := range words {
		if strings.Contains(word, "$") {
			return false
		}
	}
	subcommand := strings.ToLower(strings.TrimSpace(words[0]))
	args := words[1:]
	switch subcommand {
	case "ps", "images":
		return true
	case "logs":
		config := powerShellNativeReadOnlyConfig{
			allowedFlags:     stringSet("--tail", "-n", "--timestamps", "-t", "--since", "--until", "--details"),
			valueFlags:       stringSet("--tail", "-n", "--since", "--until"),
			allowPositionals: true,
		}
		return readOnlyNativeArgs(args, config)
	case "inspect":
		config := powerShellNativeReadOnlyConfig{
			allowedFlags:     stringSet("--format", "-f", "--type", "--size", "-s"),
			valueFlags:       stringSet("--format", "-f", "--type"),
			allowPositionals: true,
		}
		return readOnlyDockerInspectArgs(args, config)
	default:
		return false
	}
}

func readOnlyDockerInspectArgs(words []string, config powerShellNativeReadOnlyConfig) bool {
	for i := 0; i < len(words); i++ {
		word := words[i]
		if word == "--%" {
			return false
		}
		name, value, hasValue, isFlag := splitNativeFlag(word)
		if isFlag {
			if !nativeFlagAllowed(name, config) {
				return false
			}
			if hasValue {
				if !safeDockerInspectValue(name, value) {
					return false
				}
				continue
			}
			if config.valueFlags[name] {
				i++
				if i >= len(words) || !safeDockerInspectValue(name, words[i]) {
					return false
				}
			}
			continue
		}
		if !safeNativeValue(word) {
			return false
		}
	}
	return true
}

func safeDockerInspectValue(flag string, value string) bool {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	if value == "" || value == "--%" || strings.ContainsAny(value, "$`\x00") {
		return false
	}
	if flag == "--type" {
		return safeNativeValue(value)
	}
	return true
}

func readOnlyRoute(words []string) bool {
	sawPrint := false
	for _, word := range words {
		lower := strings.ToLower(strings.TrimSpace(word))
		switch {
		case lower == "-4" || lower == "-6":
			if sawPrint {
				return false
			}
		case lower == "print":
			if sawPrint {
				return false
			}
			sawPrint = true
		default:
			return false
		}
	}
	return sawPrint
}

func splitNativeFlag(word string) (name string, value string, hasValue bool, isFlag bool) {
	word = strings.TrimSpace(word)
	if !looksLikeNativeFlag(word) {
		return "", "", false, false
	}
	for _, sep := range []string{":", "="} {
		if idx := strings.Index(word, sep); idx > 0 {
			return strings.ToLower(word[:idx]), word[idx+1:], true, true
		}
	}
	return strings.ToLower(word), "", false, true
}

func looksLikeNativeFlag(word string) bool {
	return strings.HasPrefix(word, "-") || strings.HasPrefix(word, "/")
}

func safeNativeValue(value string) bool {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	return value != "" && value != "--%" && !strings.ContainsAny(value, "$`@{[()\x00")
}

func stringSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[strings.ToLower(value)] = true
	}
	return result
}

func destructiveWords(words []string) bool {
	command := canonicalCommand(words[0])
	switch command {
	case "git":
		return bashtools.IsDestructiveCommand(powerShellGitCommand(words))
	case "remove-item", "set-content", "add-content", "clear-content", "clear-item", "out-file", "new-item", "move-item", "copy-item", "rename-item", "set-item", "stop-process", "stop-service", "restart-computer", "invoke-expression", "iex", "start-process", "start-transcript", "stop-transcript":
		return true
	default:
		return strings.HasPrefix(command, "remove-") || strings.HasPrefix(command, "set-") || strings.HasPrefix(command, "new-") || strings.HasPrefix(command, "export-")
	}
}

func powerShellGitCommand(words []string) string {
	if len(words) == 0 {
		return ""
	}
	normalized := append([]string(nil), words...)
	normalized[0] = "git"
	return strings.Join(normalized, " ")
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
	case "asjob", "cimsession", "computername", "credential", "jobname", "outfile", "out-file", "outvariable", "ov", "pipelinevariable", "pv", "throttlelimit":
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
	if !strings.ContainsAny(name, `/\`) {
		if stem, ok := stripPowerShellExecutableSuffix(name); ok {
			if powerShellAliasTarget(stem) != "" || preservePowerShellNativeExecutableStem(stem) {
				return name
			}
			name = stem
		}
	}
	if target := powerShellAliasTarget(name); target != "" {
		return target
	}
	return name
}

func stripPowerShellExecutableSuffix(name string) (string, bool) {
	for _, suffix := range []string{".exe", ".cmd", ".bat", ".com"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix), true
		}
	}
	return name, false
}

func preservePowerShellNativeExecutableStem(name string) bool {
	switch name {
	case "comp", "more":
		return true
	default:
		return false
	}
}

func powerShellAliasTarget(name string) string {
	switch name {
	case "cat", "gc":
		return "get-content"
	case "type":
		return "get-content"
	case "ls", "dir", "gci":
		return "get-childitem"
	case "pwd", "gl":
		return "get-location"
	case "gi":
		return "get-item"
	case "rvpa":
		return "resolve-path"
	case "gp", "gip":
		return "get-itemproperty"
	case "gpv":
		return "get-itempropertyvalue"
	case "ps", "gps":
		return "get-process"
	case "kill", "spps":
		return "stop-process"
	case "start", "saps":
		return "start-process"
	case "gsv":
		return "get-service"
	case "gal":
		return "get-alias"
	case "gdr":
		return "get-psdrive"
	case "gmo":
		return "get-module"
	case "h", "history", "ghy":
		return "get-history"
	case "gm":
		return "get-member"
	case "gu":
		return "get-unique"
	case "rm", "rmdir", "ri":
		return "remove-item"
	case "rd", "del", "erase":
		return "remove-item"
	case "mv", "move", "mi":
		return "move-item"
	case "ci", "cp", "copy", "cpi":
		return "copy-item"
	case "ren", "rni":
		return "rename-item"
	case "ni", "mkdir", "md":
		return "new-item"
	case "sc":
		return "set-content"
	case "ac":
		return "add-content"
	case "clc":
		return "clear-content"
	case "cli":
		return "clear-item"
	case "si":
		return "set-item"
	case "sls":
		return "select-string"
	case "echo", "write":
		return "write-output"
	case "sleep":
		return "start-sleep"
	case "select":
		return "select-object"
	case "sort":
		return "sort-object"
	case "group":
		return "group-object"
	case "where", "?":
		return "where-object"
	case "ft":
		return "format-table"
	case "fl":
		return "format-list"
	case "fw":
		return "format-wide"
	case "fc":
		return "format-custom"
	case "compare", "diff":
		return "compare-object"
	case "measure":
		return "measure-object"
	default:
		return ""
	}
}
