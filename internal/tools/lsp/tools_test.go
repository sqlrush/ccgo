package lsptools

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/lsp"
	"ccgo/internal/tool"
)

func TestDiagnosticsToolReadsSessionSnapshot(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	path := lsp.SessionDiagnosticsPath(sessionPath, "sess_lsp")
	if err := lsp.WriteSnapshot(path, []lsp.Diagnostic{
		{FilePath: "main.go", Severity: "error", Message: "broken", Range: lsp.Range{Start: lsp.Position{Line: 4, Character: 2}}, Source: "gopls"},
		{FilePath: "main.go", Severity: "warning", Message: "unused", Range: lsp.Range{Start: lsp.Position{Line: 6}}},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := NewDiagnosticsTool().Call(tool.Context{
		SessionID: "sess_lsp",
		Metadata:  map[string]any{tool.MetadataSessionPathKey: sessionPath},
	}, json.RawMessage(`{"severity":"error"}`), tool.NopProgressSink())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content.(string), "main.go:5:3 error [gopls]: broken") {
		t.Fatalf("content = %q", result.Content)
	}
	if result.StructuredContent["count"] != 1 || result.StructuredContent["total_count"] != 2 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	diagnostics, ok := result.StructuredContent["diagnostics"].([]lsp.Diagnostic)
	if !ok || len(diagnostics) != 1 || diagnostics[0].Message != "broken" {
		t.Fatalf("diagnostics = %#v", result.StructuredContent["diagnostics"])
	}
}

func TestDiagnosticsToolNoSessionPath(t *testing.T) {
	result, err := NewDiagnosticsTool().Call(tool.Context{SessionID: "sess_lsp"}, nil, tool.NopProgressSink())
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["available"] != false || result.StructuredContent["count"] != 0 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
}

func TestDiagnosticsToolValidatesSeverity(t *testing.T) {
	err := NewDiagnosticsTool().Validate(tool.Context{}, json.RawMessage(`{"severity":"fatal"}`))
	if err == nil || !strings.Contains(err.Error(), "input.severity must be one of error, warning, info, hint") {
		t.Fatalf("severity validation err = %v", err)
	}
}

func TestDiagnosticsToolDefinitionIsReadOnly(t *testing.T) {
	definition, err := NewDiagnosticsTool().(tool.DefinitionProvider).ContractDefinition(tool.PromptContext{})
	if err != nil {
		t.Fatal(err)
	}
	if definition.Name != "LSPDiagnostics" || !definition.ReadOnly || !definition.ConcurrencySafe {
		t.Fatalf("definition = %#v", definition)
	}
	if _, ok := definition.InputSchema["properties"].(map[string]any)["file_path"]; !ok {
		t.Fatalf("schema = %#v", definition.InputSchema)
	}
}

func TestDiagnosticsToolAllowsWithoutPermissionPrompt(t *testing.T) {
	decision, err := NewDiagnosticsTool().CheckPermissions(tool.Context{}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != contracts.PermissionAllow {
		t.Fatalf("decision = %#v", decision)
	}
}
