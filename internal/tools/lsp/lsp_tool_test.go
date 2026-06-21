package lsptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestLSPToolValidatesOperation(t *testing.T) {
	toolImpl := NewLSPTool()
	if toolImpl.Name() != "LSP" {
		t.Fatalf("Name = %q want LSP", toolImpl.Name())
	}
	ctx := tool.Context{Context: context.Background()}
	// Unknown operation rejected.
	bad, _ := json.Marshal(map[string]any{"operation": "bogus", "filePath": "a.go", "line": 1, "character": 1})
	if err := toolImpl.Validate(ctx, bad); err == nil {
		t.Fatal("expected error for unknown operation")
	}
	// Each of the 9 ops with valid coords passes validation.
	for _, op := range []string{
		"goToDefinition", "findReferences", "hover", "documentSymbol",
		"workspaceSymbol", "goToImplementation", "prepareCallHierarchy",
		"incomingCalls", "outgoingCalls",
	} {
		raw, _ := json.Marshal(map[string]any{"operation": op, "filePath": "a.go", "line": 1, "character": 1})
		if err := toolImpl.Validate(ctx, raw); err != nil {
			t.Fatalf("operation %q failed validation: %v", op, err)
		}
	}
	// Non-positive line rejected.
	zero, _ := json.Marshal(map[string]any{"operation": "hover", "filePath": "a.go", "line": 0, "character": 1})
	if err := toolImpl.Validate(ctx, zero); err == nil {
		t.Fatal("expected error for line < 1")
	}
}

func TestLSPToolValidatesCharacter(t *testing.T) {
	toolImpl := NewLSPTool()
	ctx := tool.Context{Context: context.Background()}

	// Non-positive character rejected.
	raw, _ := json.Marshal(map[string]any{"operation": "hover", "filePath": "a.go", "line": 1, "character": 0})
	if err := toolImpl.Validate(ctx, raw); err == nil {
		t.Fatal("expected error for character < 1")
	}

	// Negative character rejected.
	raw, _ = json.Marshal(map[string]any{"operation": "hover", "filePath": "a.go", "line": 1, "character": -5})
	if err := toolImpl.Validate(ctx, raw); err == nil {
		t.Fatal("expected error for negative character")
	}
}

func TestLSPToolValidatesFilePath(t *testing.T) {
	toolImpl := NewLSPTool()
	ctx := tool.Context{Context: context.Background()}

	// Empty filePath rejected.
	raw, _ := json.Marshal(map[string]any{"operation": "hover", "filePath": "", "line": 1, "character": 1})
	if err := toolImpl.Validate(ctx, raw); err == nil {
		t.Fatal("expected error for empty filePath")
	}
}

func TestLSPToolDefinitionIsReadOnly(t *testing.T) {
	definition, err := NewLSPTool().(tool.DefinitionProvider).ContractDefinition(tool.PromptContext{})
	if err != nil {
		t.Fatal(err)
	}
	if definition.Name != "LSP" {
		t.Fatalf("Name = %q want LSP", definition.Name)
	}
	if !definition.ReadOnly {
		t.Fatal("expected ReadOnly = true")
	}
	if !definition.ConcurrencySafe {
		t.Fatal("expected ConcurrencySafe = true")
	}
}

func TestLSPToolPermissionAllows(t *testing.T) {
	toolImpl := NewLSPTool()
	raw, _ := json.Marshal(map[string]any{"operation": "hover", "filePath": "a.go", "line": 1, "character": 1})
	decision, err := toolImpl.CheckPermissions(tool.Context{Context: context.Background()}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != contracts.PermissionAllow {
		t.Fatalf("expected PermissionAllow, got %v", decision.Behavior)
	}
}

// TestLSPToolUnsupportedOpGracefulDegrade verifies that calling any of the 9
// operations returns "not supported" rather than a panic or an error, because
// the internal/lsp backend currently has no navigation methods.
func TestLSPToolUnsupportedOpGracefulDegrade(t *testing.T) {
	toolImpl := NewLSPTool()
	ctx := tool.Context{Context: context.Background()}

	for _, op := range []string{
		"goToDefinition", "findReferences", "hover", "documentSymbol",
		"workspaceSymbol", "goToImplementation", "prepareCallHierarchy",
		"incomingCalls", "outgoingCalls",
	} {
		raw, _ := json.Marshal(map[string]any{"operation": op, "filePath": "a.go", "line": 1, "character": 1})
		result, err := toolImpl.Call(ctx, raw, tool.NopProgressSink())
		if err != nil {
			t.Fatalf("operation %q: unexpected error: %v", op, err)
		}
		content, ok := result.Content.(string)
		if !ok {
			t.Fatalf("operation %q: expected string Content", op)
		}
		if !strings.Contains(content, "not supported") {
			t.Fatalf("operation %q: expected 'not supported' in Content, got: %q", op, content)
		}
		sc := result.StructuredContent
		if sc["supported"] != false {
			t.Fatalf("operation %q: expected supported=false in StructuredContent", op)
		}
	}
}

func TestLSPToolSchemaHasAllOps(t *testing.T) {
	definition, err := NewLSPTool().(tool.DefinitionProvider).ContractDefinition(tool.PromptContext{})
	if err != nil {
		t.Fatal(err)
	}
	props, ok := definition.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("no properties in schema: %#v", definition.InputSchema)
	}
	opSchema, ok := props["operation"].(map[string]any)
	if !ok {
		t.Fatal("no operation property in schema")
	}
	enum, ok := opSchema["enum"].([]any)
	if !ok {
		t.Fatal("operation has no enum in schema")
	}
	want := []string{
		"goToDefinition", "findReferences", "hover", "documentSymbol",
		"workspaceSymbol", "goToImplementation", "prepareCallHierarchy",
		"incomingCalls", "outgoingCalls",
	}
	if len(enum) != len(want) {
		t.Fatalf("enum len = %d want %d", len(enum), len(want))
	}
	enumSet := make(map[string]bool)
	for _, v := range enum {
		s, _ := v.(string)
		enumSet[s] = true
	}
	for _, op := range want {
		if !enumSet[op] {
			t.Fatalf("missing operation %q in schema enum", op)
		}
	}
}
