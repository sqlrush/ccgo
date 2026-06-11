package bashtools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const (
	defaultTimeoutMillis = 120_000
	maxTimeoutMillis     = 600_000
)

type bashInput struct {
	Command               string `json:"command"`
	Timeout               *int   `json:"timeout,omitempty"`
	Description           string `json:"description,omitempty"`
	RunInBackground       bool   `json:"run_in_background,omitempty"`
	RunInBackgroundAlt    bool   `json:"runInBackground,omitempty"`
	hasRunInBackground    bool
	hasRunInBackgroundAlt bool
}

type bashResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	TimedOut   bool
	DurationMS int64
	TimeoutMS  int
}

type bashOutputInput struct {
	BashID       string `json:"bash_id,omitempty"`
	ID           string `json:"id,omitempty"`
	TailLines    *int   `json:"tail_lines,omitempty"`
	TailLinesAlt *int   `json:"tailLines,omitempty"`
}

type bashKillInput struct {
	BashID string `json:"bash_id,omitempty"`
	ID     string `json:"id,omitempty"`
}

func NewBashTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "Bash",
			Description:        "Run a shell command.",
			SearchHint:         "run shell command",
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
			return "Runs a shell command in the current working directory. Provide command, optional timeout in milliseconds, optional short description, and run_in_background for background commands. Full sandbox parity and interrupt controls are not implemented yet.", nil
		},
		ValidateFunc:    validateBash,
		CallFunc:        callBash,
		ReadOnlyFunc:    bashReadOnlyInput,
		ConcurrencyFunc: bashReadOnlyInput,
		DestructiveFunc: bashDestructiveInput,
	}
}

func NewBashOutputTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "BashOutput",
			Description:     "Read output from a background Bash command.",
			SearchHint:      "read background shell command output",
			ReadOnly:        true,
			ConcurrencySafe: true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"bash_id":    map[string]any{"type": "string"},
					"id":         map[string]any{"type": "string"},
					"tail_lines": map[string]any{"type": "integer"},
					"tailLines":  map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Reads stdout, stderr, and status for a Bash command started with run_in_background.", nil
		},
		ValidateFunc:    validateBashOutput,
		CallFunc:        callBashOutput,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewKillBashTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "KillBash",
			Description:     "Cancel a background Bash command.",
			SearchHint:      "kill cancel background shell command",
			ConcurrencySafe: true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"bash_id": map[string]any{"type": "string"},
					"id":      map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Cancels a Bash command started with run_in_background. Use BashOutput to read the final output and status.", nil
		},
		ValidateFunc:    validateKillBash,
		CallFunc:        callKillBash,
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func validateBash(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeBash(raw)
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

func validateBashOutput(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeBashOutput(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.backgroundID()) == "" {
		return fmt.Errorf("bash_id is required")
	}
	if input.TailLines != nil && *input.TailLines <= 0 {
		return fmt.Errorf("tail_lines must be positive")
	}
	if input.TailLinesAlt != nil && *input.TailLinesAlt <= 0 {
		return fmt.Errorf("tail_lines must be positive")
	}
	return nil
}

func validateKillBash(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeKillBash(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.backgroundID()) == "" {
		return fmt.Errorf("bash_id is required")
	}
	return nil
}

func callBash(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeBash(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	timeout := bashTimeout(input)
	if input.runInBackground() {
		return startBackgroundBash(ctx, input, timeout)
	}
	result := runBashCommand(ctx, strings.TrimSpace(input.Command), timeout)
	return contracts.ToolResult{
		Content: formatBashContent(result),
		IsError: result.TimedOut || result.ExitCode != 0,
		StructuredContent: map[string]any{
			"type":        "bash",
			"command":     input.Command,
			"description": input.Description,
			"stdout":      result.Stdout,
			"stderr":      result.Stderr,
			"exit_code":   result.ExitCode,
			"timed_out":   result.TimedOut,
			"duration_ms": result.DurationMS,
			"timeout_ms":  result.TimeoutMS,
		},
	}, nil
}

func callBashOutput(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeBashOutput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	state := EnsureBackgroundState(ctx)
	if state == nil {
		return contracts.ToolResult{}, fmt.Errorf("background bash state is not available")
	}
	task, ok := state.Get(input.backgroundID())
	if !ok {
		return contracts.ToolResult{}, fmt.Errorf("background bash command not found: %s", input.backgroundID())
	}
	snapshot := task.Snapshot()
	tailLines := bashOutputTailLines(input)
	if tailLines > 0 {
		snapshot.Stdout = tailText(snapshot.Stdout, tailLines)
		snapshot.Stderr = tailText(snapshot.Stderr, tailLines)
	}
	return contracts.ToolResult{
		Content: formatBackgroundOutput(snapshot),
		IsError: !snapshot.Running && (snapshot.TimedOut || snapshot.ExitCode != 0),
		StructuredContent: map[string]any{
			"type":        "bash_output",
			"bash_id":     snapshot.ID,
			"command":     snapshot.Command,
			"description": snapshot.Description,
			"stdout":      snapshot.Stdout,
			"stderr":      snapshot.Stderr,
			"running":     snapshot.Running,
			"exit_code":   snapshot.ExitCode,
			"timed_out":   snapshot.TimedOut,
			"cancelled":   snapshot.Cancelled,
			"duration_ms": snapshot.DurationMS,
			"timeout_ms":  snapshot.TimeoutMS,
			"started_at":  snapshot.StartedAt.UTC().Format(time.RFC3339Nano),
			"ended_at":    formatOptionalTime(snapshot.EndedAt),
			"error":       snapshot.Error,
		},
	}, nil
}

func callKillBash(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeKillBash(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	state := EnsureBackgroundState(ctx)
	if state == nil {
		return contracts.ToolResult{}, fmt.Errorf("background bash state is not available")
	}
	task, ok := state.Get(input.backgroundID())
	if !ok {
		return contracts.ToolResult{}, fmt.Errorf("background bash command not found: %s", input.backgroundID())
	}
	killed := task.Cancel()
	snapshot := task.Snapshot()
	content := fmt.Sprintf("Kill requested for background command %s.", snapshot.ID)
	if !killed {
		content = fmt.Sprintf("Background command %s is not running.", snapshot.ID)
	}
	return contracts.ToolResult{
		Content: content,
		StructuredContent: map[string]any{
			"type":      "kill_bash",
			"bash_id":   snapshot.ID,
			"command":   snapshot.Command,
			"running":   snapshot.Running,
			"killed":    killed,
			"cancelled": snapshot.Cancelled,
		},
	}, nil
}

func runBashCommand(ctx tool.Context, command string, timeout time.Duration) bashResult {
	baseCtx := ctx.Context
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	start := time.Now()
	runCtx, cancel := context.WithTimeout(baseCtx, timeout)
	defer cancel()

	name, args := shellCommand(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	configureBashCommand(cmd)
	if ctx.WorkingDirectory != "" {
		cmd.Dir = ctx.WorkingDirectory
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	durationMS := time.Since(start).Milliseconds()
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		if timedOut {
			exitCode = -1
		}
	}
	return bashResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		ExitCode:   exitCode,
		TimedOut:   timedOut,
		DurationMS: durationMS,
		TimeoutMS:  int(timeout / time.Millisecond),
	}
}

func startBackgroundBash(ctx tool.Context, input bashInput, timeout time.Duration) (contracts.ToolResult, error) {
	state := EnsureBackgroundState(ctx)
	if state == nil {
		return contracts.ToolResult{}, fmt.Errorf("background bash state is not available")
	}
	command := strings.TrimSpace(input.Command)
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	name, args := shellCommand(command)
	cmd := exec.CommandContext(runCtx, name, args...)
	configureBashCommand(cmd)
	if ctx.WorkingDirectory != "" {
		cmd.Dir = ctx.WorkingDirectory
	}
	task := &BackgroundTask{
		ID:          "bash_" + string(contracts.NewID()),
		Command:     command,
		Description: input.Description,
		StartedAt:   time.Now(),
		TimeoutMS:   int(timeout / time.Millisecond),
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
		Content: fmt.Sprintf("Command started in background with ID: %s", task.ID),
		StructuredContent: map[string]any{
			"type":        "bash_background",
			"bash_id":     task.ID,
			"command":     command,
			"description": input.Description,
			"running":     true,
			"timeout_ms":  task.TimeoutMS,
			"started_at":  task.StartedAt.UTC().Format(time.RFC3339Nano),
		},
	}, nil
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func configureBashCommand(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return killBashProcessGroup(cmd)
	}
}

func killBashProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func formatBashContent(result bashResult) string {
	var b strings.Builder
	if result.Stdout != "" {
		b.WriteString(result.Stdout)
	}
	if result.Stderr != "" {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(result.Stderr)
	}
	content := strings.TrimRight(b.String(), "\n")
	if result.TimedOut {
		return appendStatusLine(content, fmt.Sprintf("Command timed out after %dms.", result.TimeoutMS))
	}
	if result.ExitCode != 0 {
		return appendStatusLine(content, fmt.Sprintf("Command exited with code %d.", result.ExitCode))
	}
	if content == "" {
		return "Command completed successfully with no output."
	}
	return content
}

func appendStatusLine(content string, status string) string {
	if content == "" {
		return status
	}
	return content + "\n" + status
}

func formatBackgroundOutput(snapshot BackgroundTaskSnapshot) string {
	var status string
	if snapshot.Running {
		status = fmt.Sprintf("Background command %s is still running.", snapshot.ID)
	} else if snapshot.Cancelled {
		status = fmt.Sprintf("Background command %s was cancelled.", snapshot.ID)
	} else if snapshot.TimedOut {
		status = fmt.Sprintf("Background command %s timed out after %dms.", snapshot.ID, snapshot.TimeoutMS)
	} else {
		status = fmt.Sprintf("Background command %s completed with exit code %d.", snapshot.ID, snapshot.ExitCode)
	}
	output := formatBashContent(bashResult{
		Stdout:    snapshot.Stdout,
		Stderr:    snapshot.Stderr,
		ExitCode:  snapshot.ExitCode,
		TimedOut:  snapshot.TimedOut,
		TimeoutMS: snapshot.TimeoutMS,
	})
	if snapshot.Running && snapshot.Stdout == "" && snapshot.Stderr == "" {
		return status
	}
	if output == "Command completed successfully with no output." {
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

func bashTimeout(input bashInput) time.Duration {
	if input.Timeout == nil {
		return time.Duration(defaultTimeoutMillis) * time.Millisecond
	}
	return time.Duration(*input.Timeout) * time.Millisecond
}

func decodeBash(raw json.RawMessage) (bashInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return bashInput{}, err
	}
	for key := range obj {
		switch key {
		case "command", "timeout", "description", "run_in_background", "runInBackground":
		default:
			return bashInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input bashInput
	data, err := json.Marshal(obj)
	if err != nil {
		return bashInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return bashInput{}, err
	}
	if _, ok := obj["run_in_background"]; ok {
		input.hasRunInBackground = true
	}
	if _, ok := obj["runInBackground"]; ok {
		input.hasRunInBackgroundAlt = true
	}
	return input, nil
}

func decodeBashOutput(raw json.RawMessage) (bashOutputInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return bashOutputInput{}, err
	}
	for key := range obj {
		switch key {
		case "bash_id", "id", "tail_lines", "tailLines":
		default:
			return bashOutputInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input bashOutputInput
	data, err := json.Marshal(obj)
	if err != nil {
		return bashOutputInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return bashOutputInput{}, err
	}
	return input, nil
}

func decodeKillBash(raw json.RawMessage) (bashKillInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return bashKillInput{}, err
	}
	for key := range obj {
		switch key {
		case "bash_id", "id":
		default:
			return bashKillInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input bashKillInput
	data, err := json.Marshal(obj)
	if err != nil {
		return bashKillInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return bashKillInput{}, err
	}
	return input, nil
}

func (input bashInput) runInBackground() bool {
	if input.hasRunInBackground {
		return input.RunInBackground
	}
	if input.hasRunInBackgroundAlt {
		return input.RunInBackgroundAlt
	}
	return false
}

func (input bashOutputInput) backgroundID() string {
	if strings.TrimSpace(input.BashID) != "" {
		return strings.TrimSpace(input.BashID)
	}
	return strings.TrimSpace(input.ID)
}

func (input bashKillInput) backgroundID() string {
	if strings.TrimSpace(input.BashID) != "" {
		return strings.TrimSpace(input.BashID)
	}
	return strings.TrimSpace(input.ID)
}

func bashOutputTailLines(input bashOutputInput) int {
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
	parts := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(parts) <= lines {
		return text
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}

func bashReadOnlyInput(raw json.RawMessage) bool {
	input, err := decodeBash(raw)
	if err != nil {
		return false
	}
	return IsReadOnlyCommand(input.Command)
}

func bashDestructiveInput(raw json.RawMessage) bool {
	input, err := decodeBash(raw)
	if err != nil {
		return false
	}
	return IsDestructiveCommand(input.Command)
}

func IsReadOnlyCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || hasShellMutationSyntax(command) || IsDestructiveCommand(command) {
		return false
	}
	segments := splitCommandSegments(command)
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		words := shellWords(segment)
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
	for _, segment := range splitCommandSegments(command) {
		words := shellWords(segment)
		if len(words) == 0 {
			continue
		}
		if destructiveWords(words) {
			return true
		}
	}
	return false
}

func hasShellMutationSyntax(command string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
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
		if inDouble {
			if ch == '$' || ch == '`' {
				return true
			}
			continue
		}
		switch ch {
		case '>', '<', '$', '`':
			return true
		case '&':
			if i+1 >= len(command) || command[i+1] != '&' {
				return true
			}
		}
	}
	return false
}

func splitCommandSegments(command string) []string {
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
		if ch == '\\' {
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
		if !inSingle && !inDouble {
			switch ch {
			case ';', '|':
				segments = appendNonemptySegment(segments, current.String())
				current.Reset()
				continue
			case '&':
				if i+1 < len(command) && command[i+1] == '&' {
					segments = appendNonemptySegment(segments, current.String())
					current.Reset()
					i++
					continue
				}
			}
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

func shellWords(command string) []string {
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
		if ch == '\\' {
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
	cmd := filepathBase(words[0])
	switch cmd {
	case "pwd", "ls", "cat", "head", "tail", "wc", "grep", "egrep", "fgrep", "rg", "find", "stat", "file", "du", "df", "printf", "echo", "date", "whoami", "id", "uname", "env", "printenv", "which", "type":
		return true
	case "git":
		return len(words) >= 2 && readOnlyGitSubcommand(words[1])
	case "go":
		return len(words) >= 2 && words[1] == "list"
	default:
		return false
	}
}

func readOnlyGitSubcommand(subcommand string) bool {
	switch subcommand {
	case "status", "diff", "log", "show", "branch", "tag", "remote", "rev-parse", "ls-files", "grep":
		return true
	default:
		return false
	}
}

func destructiveWords(words []string) bool {
	cmd := filepathBase(words[0])
	switch cmd {
	case "rm", "rmdir", "dd", "mkfs", "shutdown", "reboot", "halt", "poweroff", "kill", "pkill", "killall", "sudo", "su":
		return true
	case "git":
		return destructiveGit(words)
	case "chmod", "chown", "chgrp":
		return hasRecursiveFlag(words)
	default:
		return false
	}
}

func destructiveGit(words []string) bool {
	if len(words) < 2 {
		return false
	}
	switch words[1] {
	case "reset":
		return containsWord(words[2:], "--hard")
	case "clean":
		return true
	case "checkout", "restore":
		return containsWord(words[2:], ".") || containsWord(words[2:], "--")
	default:
		return false
	}
}

func hasRecursiveFlag(words []string) bool {
	for _, word := range words[1:] {
		if word == "-R" || word == "--recursive" || strings.Contains(word, "R") && strings.HasPrefix(word, "-") {
			return true
		}
	}
	return false
}

func containsWord(words []string, want string) bool {
	for _, word := range words {
		if word == want {
			return true
		}
	}
	return false
}

func filepathBase(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.Trim(name, `"'`)
	name = strings.TrimSuffix(name, ".exe")
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
}
