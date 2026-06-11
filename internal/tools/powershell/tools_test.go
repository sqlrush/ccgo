package powershelltools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func powerShellExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewPowerShellTool(), NewPowerShellOutputTool(), NewKillPowerShellTool())
	if err != nil {
		t.Fatal(err)
	}
	return tool.NewExecutor(registry)
}

func TestPowerShellValidation(t *testing.T) {
	executor := powerShellExecutor(t)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty command", input: `{"command":"  "}`, want: "command is required"},
		{name: "invalid timeout", input: `{"command":"Get-Location","timeout":0}`, want: "timeout must be positive"},
		{name: "unknown field", input: `{"command":"Get-Location","extra":true}`, want: "input.extra is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
				ID:    "toolu_invalid",
				Name:  "PowerShell",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPowerShellCommandClassification(t *testing.T) {
	readOnly := []string{
		"Get-Location",
		"pwd",
		"Get-ChildItem -Force",
		"Get-ChildItem ./internal",
		"Get-Content README.md",
		"Get-Content -Raw README.md",
		`Get-Content .\README.md`,
		"Select-String TODO README.md",
		"Get-Process | Select-String go",
		"Write-Output hello",
	}
	for _, command := range readOnly {
		if !IsReadOnlyCommand(command) {
			t.Fatalf("%q should be read-only", command)
		}
		if IsDestructiveCommand(command) {
			t.Fatalf("%q should not be destructive", command)
		}
	}

	notReadOnly := []string{
		"Get-Content $env:USERPROFILE",
		"Get-Content README.md > out.txt",
		"$x = Get-Process",
		"Get-Location; Set-Location ..",
		"Get-Location && Get-Process",
		"Get-Content /etc/passwd",
		"Get-Content ../secret.txt",
		`Get-Content C:\Users\secret.txt`,
		`Get-Content \\server\share\secret.txt`,
		"Get-ChildItem ~",
		`Get-Item HKLM:\Software`,
		"Get-Content '$HOME/file'",
		"Select-String TODO /etc/passwd",
		"Get-Content --% README.md",
		"Write-Output (Remove-Item out.txt)",
		"Invoke-RestMethod https://example.com",
	}
	for _, command := range notReadOnly {
		if IsReadOnlyCommand(command) {
			t.Fatalf("%q should not be read-only", command)
		}
	}

	destructive := []string{
		"Remove-Item build -Recurse",
		"rm build",
		"Set-Content out.txt hi",
		"Add-Content out.txt hi",
		"New-Item out.txt",
		"Move-Item a b",
		"Stop-Process -Id 1",
		"Invoke-Expression $x",
		"Get-Location && Remove-Item out.txt",
	}
	for _, command := range destructive {
		if !IsDestructiveCommand(command) {
			t.Fatalf("%q should be destructive", command)
		}
	}
}

func TestPowerShellToolDynamicSafetyFlags(t *testing.T) {
	ps := NewPowerShellTool()
	if !ps.IsReadOnly(json.RawMessage(`{"command":"Get-Location"}`)) {
		t.Fatalf("Get-Location should be read-only")
	}
	if !ps.IsConcurrencySafe(json.RawMessage(`{"command":"Get-Location"}`)) {
		t.Fatalf("read-only PowerShell command should be concurrency safe")
	}
	if ps.IsReadOnly(json.RawMessage(`{"command":"Set-Content out.txt hi"}`)) {
		t.Fatalf("Set-Content should not be read-only")
	}
	if !ps.IsDestructive(json.RawMessage(`{"command":"Remove-Item out.txt"}`)) {
		t.Fatalf("Remove-Item should be destructive")
	}
}

func TestPowerShellMissingExecutableReturnsStructuredError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	executor := powerShellExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_missing_powershell",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Write-Output hello"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("missing executable should mark the tool result as an error")
	}
	if result.StructuredContent["exit_code"] != -1 {
		t.Fatalf("exit code = %#v", result.StructuredContent["exit_code"])
	}
	stderr, _ := result.StructuredContent["stderr"].(string)
	if !strings.Contains(stderr, "PowerShell executable not found") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestPowerShellRunInBackgroundAndReadOutput(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	start, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Write-Output 'one'; Write-Output 'two'; Write-Output 'three'","run_in_background":true,"description":"background print"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(start.Content.(string), "PowerShell command started in background with ID: powershell_") {
		t.Fatalf("start content = %#v", start.Content)
	}
	powerShellID := start.StructuredContent["powershell_id"].(string)
	if powerShellID == "" || start.StructuredContent["running"] != true {
		t.Fatalf("start structured content = %#v", start.StructuredContent)
	}

	output := waitForPowerShellOutput(t, executor, ctx, powerShellID)
	if output.IsError {
		t.Fatalf("output should not be error: %#v", output)
	}
	if output.StructuredContent["running"] != false || output.StructuredContent["exit_code"] != 0 {
		t.Fatalf("output structured content = %#v", output.StructuredContent)
	}
	if normalizeNewlines(output.StructuredContent["stdout"].(string)) != "one\ntwo\nthree\n" {
		t.Fatalf("stdout = %#v", output.StructuredContent["stdout"])
	}

	tail, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_tail",
		Name:  "PowerShellOutput",
		Input: json.RawMessage(`{"powershell_id":` + strconvQuote(powerShellID) + `,"tail_lines":2}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if normalizeNewlines(tail.StructuredContent["stdout"].(string)) != "two\nthree" {
		t.Fatalf("tail stdout = %#v", tail.StructuredContent["stdout"])
	}
}

func TestPowerShellBackgroundTimeout(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	start, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_timeout",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Start-Sleep -Milliseconds 1000","runInBackground":true,"timeout":50}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	powerShellID := start.StructuredContent["powershell_id"].(string)
	output := waitForPowerShellOutput(t, executor, ctx, powerShellID)
	if !output.IsError {
		t.Fatalf("timeout output should be error: %#v", output)
	}
	if output.StructuredContent["timed_out"] != true || output.StructuredContent["exit_code"] != -1 {
		t.Fatalf("timeout structured content = %#v", output.StructuredContent)
	}
	if !strings.Contains(output.Content.(string), "timed out after 50ms") {
		t.Fatalf("timeout content = %#v", output.Content)
	}
}

func TestKillPowerShellCancelsBackgroundCommand(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	start, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_kill_start",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Start-Sleep -Seconds 5","run_in_background":true,"timeout":5000}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	powerShellID := start.StructuredContent["powershell_id"].(string)

	killed, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_kill",
		Name:  "KillPowerShell",
		Input: json.RawMessage(`{"powershell_id":` + strconvQuote(powerShellID) + `}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if killed.StructuredContent["killed"] != true || killed.StructuredContent["cancelled"] != true {
		t.Fatalf("kill structured content = %#v", killed.StructuredContent)
	}

	output := waitForPowerShellOutput(t, executor, ctx, powerShellID)
	if !output.IsError {
		t.Fatalf("cancelled output should be error: %#v", output)
	}
	if output.StructuredContent["cancelled"] != true || output.StructuredContent["timed_out"] != false {
		t.Fatalf("cancelled structured content = %#v", output.StructuredContent)
	}
	if !strings.Contains(output.Content.(string), "was cancelled") {
		t.Fatalf("cancelled content = %#v", output.Content)
	}
}

func TestPowerShellOutputValidationAndMissingTask(t *testing.T) {
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing id", input: `{}`, want: "powershell_id is required"},
		{name: "bad tail", input: `{"powershell_id":"powershell_missing","tail_lines":0}`, want: "tail_lines must be positive"},
		{name: "unknown field", input: `{"powershell_id":"powershell_missing","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing task", input: `{"id":"powershell_missing"}`, want: "background powershell command not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(ctx, contracts.ToolUse{
				ID:    "toolu_powershell_output_invalid",
				Name:  "PowerShellOutput",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPowerShellOutputPersistsOversizedBackgroundOutput(t *testing.T) {
	executor := powerShellExecutor(t)
	executor.ResultStoreDir = t.TempDir()
	state := NewBackgroundState()
	task := &BackgroundTask{
		ID:          "powershell_large",
		Command:     "Write-Output large",
		Description: "large output",
		StartedAt:   time.Now().Add(-time.Second),
		EndedAt:     time.Now(),
		TimeoutMS:   defaultTimeoutMillis,
		Running:     false,
		ExitCode:    0,
	}
	_, _ = task.stdout.Write([]byte(strings.Repeat("x", 110_000)))
	state.Add(task)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, state)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_large_output",
		Name:  "PowerShellOutput",
		Input: json.RawMessage(`{"powershell_id":"powershell_large"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta["truncated"] != true {
		t.Fatalf("result should be truncated: meta=%#v", result.Meta)
	}
	path, _ := result.Meta["full_output_path"].(string)
	if path == "" {
		t.Fatalf("full output path missing: meta=%#v", result.Meta)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), strings.Repeat("x", 110_000)) {
		t.Fatalf("persisted output does not contain full stdout")
	}
	if !strings.Contains(result.Content.(string), "Tool output truncated") {
		t.Fatalf("truncated content marker missing: %#v", result.Content)
	}
}

func TestKillPowerShellValidationAndMissingTask(t *testing.T) {
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing id", input: `{}`, want: "powershell_id is required"},
		{name: "unknown field", input: `{"powershell_id":"powershell_missing","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing task", input: `{"id":"powershell_missing"}`, want: "background powershell command not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(ctx, contracts.ToolUse{
				ID:    "toolu_powershell_kill_invalid",
				Name:  "KillPowerShell",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPowerShellRunsWhenAvailable(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_powershell",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Write-Output hello","description":"print greeting"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result should not be an error: %#v", result)
	}
	if strings.TrimSpace(result.StructuredContent["stdout"].(string)) != "hello" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if result.StructuredContent["description"] != "print greeting" {
		t.Fatalf("description = %#v", result.StructuredContent["description"])
	}
}

func waitForPowerShellOutput(t *testing.T, executor tool.Executor, ctx tool.Context, powerShellID string) contracts.ToolResult {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		output, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    "toolu_powershell_background_output",
			Name:  "PowerShellOutput",
			Input: json.RawMessage(`{"powershell_id":` + strconvQuote(powerShellID) + `}`),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if output.StructuredContent["running"] == false {
			return output
		}
		if time.Now().After(deadline) {
			t.Fatalf("background PowerShell command %s did not finish; last output = %#v", powerShellID, output.StructuredContent)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func requirePowerShell(t *testing.T) {
	t.Helper()
	if _, ok := powerShellExecutable(); !ok {
		t.Skip("PowerShell executable is not installed")
	}
}

func normalizeNewlines(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}

func strconvQuote(value string) string {
	escaped, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(escaped)
}

func TestPowerShellExecutableDetectionAllowsMissingBinary(t *testing.T) {
	_, ok := powerShellExecutable()
	if !ok {
		if _, err := exec.LookPath("pwsh"); err == nil {
			t.Fatalf("pwsh exists but detector did not find it")
		}
		if _, err := exec.LookPath("powershell"); err == nil {
			t.Fatalf("powershell exists but detector did not find it")
		}
	}
}
