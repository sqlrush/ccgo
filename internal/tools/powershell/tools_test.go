package powershelltools

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func powerShellExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewPowerShellTool())
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
		"Get-Content README.md",
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

func TestPowerShellRunsWhenAvailable(t *testing.T) {
	if _, ok := powerShellExecutable(); !ok {
		t.Skip("PowerShell executable is not installed")
	}
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
