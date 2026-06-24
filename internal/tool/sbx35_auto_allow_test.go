package tool_test

// SBX-35 AutoAllowBashIfSandboxed: Executor.SandboxBashAutoAllow promotes a
// PermissionAsk decision to PermissionAllow for Bash tool calls.
// CC ref: src/utils/sandbox/sandbox-adapter.ts:471 (autoAllowBashIfSandboxed).

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// fakeBashPermAskTool is a tool that always returns PermissionAsk.
type fakeBashPermAskTool struct{}

func (fakeBashPermAskTool) Name() string        { return "Bash" }
func (fakeBashPermAskTool) Aliases() []string   { return nil }
func (fakeBashPermAskTool) InputSchema(_ tool.PromptContext) contracts.JSONSchema {
	return contracts.JSONSchema{"type": "object"}
}
func (fakeBashPermAskTool) ContractDefinition(_ tool.PromptContext) (contracts.ToolDefinition, error) {
	return contracts.ToolDefinition{Name: "Bash"}, nil
}
func (fakeBashPermAskTool) Prompt(_ tool.PromptContext) (string, error)   { return "", nil }
func (fakeBashPermAskTool) Validate(_ tool.Context, _ json.RawMessage) error { return nil }
func (fakeBashPermAskTool) CheckPermissions(_ tool.Context, _ json.RawMessage) (contracts.PermissionDecision, error) {
	return contracts.PermissionDecision{
		Behavior: contracts.PermissionAsk,
		Message:  "permission ask",
	}, nil
}
func (fakeBashPermAskTool) Call(_ tool.Context, _ json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	return contracts.ToolResult{Content: "bash ran"}, nil
}
func (fakeBashPermAskTool) IsReadOnly(_ json.RawMessage) bool        { return false }
func (fakeBashPermAskTool) IsConcurrencySafe(_ json.RawMessage) bool { return false }
func (fakeBashPermAskTool) IsDestructive(_ json.RawMessage) bool     { return true }
func (fakeBashPermAskTool) InterruptBehavior() string                { return "" }
func (fakeBashPermAskTool) MaxResultSizeChars() int                  { return 0 }

// TestSandboxBashAutoAllowBypassesPermissionAsk verifies that when
// SandboxBashAutoAllow=true and the tool is Bash, a PermissionAsk is promoted
// to PermissionAllow (SBX-35).
func TestSandboxBashAutoAllowBypassesPermissionAsk(t *testing.T) {
	reg, err := tool.NewRegistry(fakeBashPermAskTool{})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	exec := tool.NewExecutor(reg)
	exec.SandboxBashAutoAllow = true
	// No Asker injected — without SandboxBashAutoAllow this would return a
	// PermissionError because Asker is nil when behavior is Ask.

	use := contracts.ToolUse{
		ID:    "tu1",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"echo hi"}`),
	}
	result, err := exec.Execute(tool.Context{Context: context.Background()}, use, nil)
	if err != nil {
		t.Fatalf("Execute with SandboxBashAutoAllow=true returned error: %v", err)
	}
	if result.IsError {
		t.Errorf("result.IsError=true; expected tool to run; content=%v", result.Content)
	}
	if result.Content != "bash ran" {
		t.Errorf("content=%v want 'bash ran'", result.Content)
	}
}

// TestSandboxBashAutoAllowNotSetBlocksPermissionAsk verifies that without
// SandboxBashAutoAllow, a PermissionAsk is still blocked when Asker is nil.
func TestSandboxBashAutoAllowNotSetBlocksPermissionAsk(t *testing.T) {
	reg, err := tool.NewRegistry(fakeBashPermAskTool{})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	exec := tool.NewExecutor(reg)
	// SandboxBashAutoAllow defaults to false; Asker is nil → blocked.

	use := contracts.ToolUse{
		ID:    "tu2",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":"echo hi"}`),
	}
	_, err = exec.Execute(tool.Context{Context: context.Background()}, use, nil)
	if err == nil {
		t.Fatal("expected PermissionError when SandboxBashAutoAllow=false and Asker=nil")
	}
	var permErr tool.PermissionError
	if !isPermissionError(err, &permErr) {
		t.Fatalf("expected PermissionError, got %T: %v", err, err)
	}
}

// TestSandboxBashAutoAllowDoesNotApplyToNonBashTools verifies that
// SandboxBashAutoAllow only applies to Bash family tools.
func TestSandboxBashAutoAllowDoesNotApplyToNonBashTools(t *testing.T) {
	// Use a fake tool named "Read" that returns PermissionAsk.
	reg, err := tool.NewRegistry(fakeReadPermAskTool{})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	exec := tool.NewExecutor(reg)
	exec.SandboxBashAutoAllow = true

	use := contracts.ToolUse{
		ID:    "tu3",
		Name:  "Read",
		Input: json.RawMessage(`{}`),
	}
	_, err = exec.Execute(tool.Context{Context: context.Background()}, use, nil)
	if err == nil {
		t.Fatal("expected PermissionError for non-Bash tool when Asker=nil")
	}
}

// fakeReadPermAskTool simulates a non-Bash tool that returns PermissionAsk.
type fakeReadPermAskTool struct{}

func (fakeReadPermAskTool) Name() string        { return "Read" }
func (fakeReadPermAskTool) Aliases() []string   { return nil }
func (fakeReadPermAskTool) InputSchema(_ tool.PromptContext) contracts.JSONSchema {
	return contracts.JSONSchema{"type": "object"}
}
func (fakeReadPermAskTool) ContractDefinition(_ tool.PromptContext) (contracts.ToolDefinition, error) {
	return contracts.ToolDefinition{Name: "Read"}, nil
}
func (fakeReadPermAskTool) Prompt(_ tool.PromptContext) (string, error)   { return "", nil }
func (fakeReadPermAskTool) Validate(_ tool.Context, _ json.RawMessage) error { return nil }
func (fakeReadPermAskTool) CheckPermissions(_ tool.Context, _ json.RawMessage) (contracts.PermissionDecision, error) {
	return contracts.PermissionDecision{Behavior: contracts.PermissionAsk}, nil
}
func (fakeReadPermAskTool) Call(_ tool.Context, _ json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	return contracts.ToolResult{Content: "read ran"}, nil
}
func (fakeReadPermAskTool) IsReadOnly(_ json.RawMessage) bool        { return true }
func (fakeReadPermAskTool) IsConcurrencySafe(_ json.RawMessage) bool { return true }
func (fakeReadPermAskTool) IsDestructive(_ json.RawMessage) bool     { return false }
func (fakeReadPermAskTool) InterruptBehavior() string                { return "" }
func (fakeReadPermAskTool) MaxResultSizeChars() int                  { return 0 }

func isPermissionError(err error, target *tool.PermissionError) bool {
	if err == nil {
		return false
	}
	switch e := err.(type) {
	case tool.PermissionError:
		if target != nil {
			*target = e
		}
		return true
	}
	return false
}
