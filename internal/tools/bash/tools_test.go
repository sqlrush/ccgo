package bashtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
	"ccgo/internal/tool"
)

func bashExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewBashTool())
	if err != nil {
		t.Fatal(err)
	}
	return tool.NewExecutor(registry)
}

func TestBashRunsCommandAndReturnsStructuredOutput(t *testing.T) {
	dir := t.TempDir()
	executor := bashExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context:          context.Background(),
		WorkingDirectory: dir,
		Metadata:         map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_bash",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"printf hello","description":"print greeting"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result should not be an error: %#v", result)
	}
	if result.Content != "hello" {
		t.Fatalf("content = %#v", result.Content)
	}
	if result.StructuredContent["stdout"] != "hello" || result.StructuredContent["stderr"] != "" || result.StructuredContent["exit_code"] != 0 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if result.StructuredContent["description"] != "print greeting" {
		t.Fatalf("description = %#v", result.StructuredContent["description"])
	}
}

func TestBashCapturesStderrAndExitCode(t *testing.T) {
	executor := bashExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_bash_fail",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"printf problem >&2; exit 3"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("result should be an error: %#v", result)
	}
	content := result.Content.(string)
	if !strings.Contains(content, "problem") || !strings.Contains(content, "Command exited with code 3.") {
		t.Fatalf("content = %#v", content)
	}
	if result.StructuredContent["stderr"] != "problem" || result.StructuredContent["exit_code"] != 3 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
}

func TestBashTimeout(t *testing.T) {
	executor := bashExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_bash_timeout",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"sleep 1","timeout":50}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("timeout should be an error: %#v", result)
	}
	if !strings.Contains(result.Content.(string), "Command timed out after 50ms.") {
		t.Fatalf("content = %#v", result.Content)
	}
	if result.StructuredContent["timed_out"] != true || result.StructuredContent["exit_code"] != -1 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
}

func TestBashValidation(t *testing.T) {
	executor := bashExecutor(t)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty command", input: `{"command":"  "}`, want: "command is required"},
		{name: "invalid timeout", input: `{"command":"pwd","timeout":0}`, want: "timeout must be positive"},
		{name: "unknown field", input: `{"command":"pwd","extra":true}`, want: "input.extra is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
				ID:    "toolu_invalid",
				Name:  "Bash",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBashCommandClassification(t *testing.T) {
	readOnly := []string{
		"pwd",
		"ls -la",
		"git status --short",
		"rg TODO internal",
		"printf hello",
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
		"echo hi > out.txt",
		`echo "$(date)"`,
		"make build",
		"git commit -m test",
		"ls && echo hi > out.txt",
	}
	for _, command := range notReadOnly {
		if IsReadOnlyCommand(command) {
			t.Fatalf("%q should not be read-only", command)
		}
	}

	destructive := []string{
		"rm -rf build",
		"git reset --hard",
		"git clean -fd",
		"sudo make install",
		"chmod -R 777 .",
	}
	for _, command := range destructive {
		if !IsDestructiveCommand(command) {
			t.Fatalf("%q should be destructive", command)
		}
	}
}

func TestBashToolDynamicSafetyFlags(t *testing.T) {
	bash := NewBashTool()
	if !bash.IsReadOnly(json.RawMessage(`{"command":"git status --short"}`)) {
		t.Fatalf("git status should be read-only")
	}
	if !bash.IsConcurrencySafe(json.RawMessage(`{"command":"git status --short"}`)) {
		t.Fatalf("read-only bash command should be concurrency safe")
	}
	if bash.IsReadOnly(json.RawMessage(`{"command":"git commit -m test"}`)) {
		t.Fatalf("git commit should not be read-only")
	}
	if !bash.IsDestructive(json.RawMessage(`{"command":"rm -rf build"}`)) {
		t.Fatalf("rm should be destructive")
	}
}

func TestBashDynamicFlagsFeedPermissionDecision(t *testing.T) {
	bash := NewBashTool()
	ctx := tool.Context{
		Context: context.Background(),
		Permissions: tool.NewEnginePermissionDecider(permissions.NewEngine(contracts.PermissionContext{
			Mode: contracts.PermissionDefault,
		})),
	}
	readDecision, err := bash.CheckPermissions(ctx, json.RawMessage(`{"command":"git status --short"}`))
	if err != nil {
		t.Fatal(err)
	}
	if readDecision.Behavior != contracts.PermissionAllow {
		t.Fatalf("read decision = %#v", readDecision)
	}
	mutateDecision, err := bash.CheckPermissions(ctx, json.RawMessage(`{"command":"echo hi > out.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if mutateDecision.Behavior != contracts.PermissionAsk {
		t.Fatalf("mutate decision = %#v", mutateDecision)
	}

	allowed := NewBashTool()
	allowCtx := tool.Context{
		Context: context.Background(),
		Permissions: tool.NewEnginePermissionDecider(permissions.NewEngine(
			contracts.PermissionContext{Mode: contracts.PermissionDefault},
			permissions.MustParseRule(contracts.PermissionSourceSession, contracts.PermissionAllow, "Bash(make build*)"),
		)),
	}
	ruleDecision, err := allowed.CheckPermissions(allowCtx, json.RawMessage(`{"command":"make build-fast"}`))
	if err != nil {
		t.Fatal(err)
	}
	if ruleDecision.Behavior != contracts.PermissionAllow {
		t.Fatalf("rule decision = %#v", ruleDecision)
	}
}
