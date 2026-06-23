package lsptools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/lsp"
	"ccgo/internal/tool"
)

// mockNavigationClient implements lsp.NavigationClient for tests.
type mockNavigationClient struct {
	// response is returned as the raw JSON result for any request.
	response json.RawMessage
	// err is returned instead of response when non-nil.
	err error
	// lastMethod is set to the most recent method passed to SendRequest.
	lastMethod string
	// lastFilePath is set to the most recent filePath passed to SendRequest.
	lastFilePath string
}

func (m *mockNavigationClient) SendRequest(_ context.Context, filePath, method string, _ any) (json.RawMessage, error) {
	m.lastMethod = method
	m.lastFilePath = filePath
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

// lspCtxWithClient returns a tool.Context with the given NavigationClient
// injected under MetadataLSPNavigationKey.
func lspCtxWithClient(client lsp.NavigationClient) tool.Context {
	return tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			tool.MetadataLSPNavigationKey: client,
		},
	}
}

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

// --- dispatch seam tests (TOOL-LSP-01..05) ---

// TestDispatchLSPNoClientGracefulDegrade verifies that when no NavigationClient
// is in the context, dispatchLSP returns supported=false and callLSP produces
// the graceful "not supported" message (unchanged existing behaviour).
func TestDispatchLSPNoClientGracefulDegrade(t *testing.T) {
	toolImpl := NewLSPTool()
	ctx := tool.Context{Context: context.Background()}
	raw, _ := json.Marshal(map[string]any{"operation": "goToDefinition", "filePath": "a.go", "line": 1, "character": 1})
	result, err := toolImpl.Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatal(err)
	}
	content, _ := result.Content.(string)
	if !strings.Contains(content, "not supported") {
		t.Fatalf("expected 'not supported' without client, got: %q", content)
	}
	if result.StructuredContent["supported"] != false {
		t.Fatalf("expected supported=false without client, got: %v", result.StructuredContent["supported"])
	}
}

// TestDispatchLSPWithMockClientGoToDefinition verifies that when a NavigationClient
// is present, goToDefinition is dispatched to the correct LSP method and the
// formatted result is returned (TOOL-LSP-01 seam).
func TestDispatchLSPWithMockClientGoToDefinition(t *testing.T) {
	mock := &mockNavigationClient{
		response: json.RawMessage(`[{"uri":"file:///work/main.go","range":{"start":{"line":9,"character":4},"end":{"line":9,"character":8}}}]`),
	}
	ctx := lspCtxWithClient(mock)
	raw, _ := json.Marshal(map[string]any{"operation": "goToDefinition", "filePath": "/work/a.go", "line": 5, "character": 3})
	result, err := NewLSPTool().Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	if mock.lastMethod != "textDocument/definition" {
		t.Fatalf("method = %q, want textDocument/definition", mock.lastMethod)
	}
	content, _ := result.Content.(string)
	if !strings.Contains(content, "main.go:10:5") {
		t.Fatalf("expected location in content, got: %q", content)
	}
	if result.StructuredContent["supported"] != true {
		t.Fatalf("expected supported=true with client, got: %v", result.StructuredContent["supported"])
	}
}

// TestDispatchLSPWithMockClientFindReferences verifies findReferences dispatch
// uses textDocument/references with includeDeclaration=true (TOOL-LSP-02 seam).
func TestDispatchLSPWithMockClientFindReferences(t *testing.T) {
	mock := &mockNavigationClient{
		response: json.RawMessage(`[{"uri":"file:///work/a.go","range":{"start":{"line":2,"character":0},"end":{"line":2,"character":4}}}]`),
	}
	ctx := lspCtxWithClient(mock)
	raw, _ := json.Marshal(map[string]any{"operation": "findReferences", "filePath": "/work/a.go", "line": 1, "character": 1})
	result, err := NewLSPTool().Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatal(err)
	}
	if mock.lastMethod != "textDocument/references" {
		t.Fatalf("method = %q, want textDocument/references", mock.lastMethod)
	}
	if result.StructuredContent["result_count"] != 1 {
		t.Fatalf("result_count = %v, want 1", result.StructuredContent["result_count"])
	}
}

// TestDispatchLSPWithMockClientHover verifies hover dispatch (TOOL-LSP-03 seam).
func TestDispatchLSPWithMockClientHover(t *testing.T) {
	mock := &mockNavigationClient{
		response: json.RawMessage(`{"contents":{"kind":"markdown","value":"func Foo() int"}}`),
	}
	ctx := lspCtxWithClient(mock)
	raw, _ := json.Marshal(map[string]any{"operation": "hover", "filePath": "/work/a.go", "line": 3, "character": 5})
	result, err := NewLSPTool().Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatal(err)
	}
	if mock.lastMethod != "textDocument/hover" {
		t.Fatalf("method = %q, want textDocument/hover", mock.lastMethod)
	}
	content, _ := result.Content.(string)
	if !strings.Contains(content, "func Foo") {
		t.Fatalf("expected hover content, got: %q", content)
	}
}

// TestDispatchLSPWithMockClientDocumentSymbol verifies documentSymbol dispatch
// (TOOL-LSP-04 seam).
func TestDispatchLSPWithMockClientDocumentSymbol(t *testing.T) {
	mock := &mockNavigationClient{
		response: json.RawMessage(`[{"name":"Foo","kind":12,"range":{"start":{"line":4,"character":0},"end":{"line":6,"character":1}}}]`),
	}
	ctx := lspCtxWithClient(mock)
	raw, _ := json.Marshal(map[string]any{"operation": "documentSymbol", "filePath": "/work/a.go", "line": 1, "character": 1})
	result, err := NewLSPTool().Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatal(err)
	}
	if mock.lastMethod != "textDocument/documentSymbol" {
		t.Fatalf("method = %q, want textDocument/documentSymbol", mock.lastMethod)
	}
	content, _ := result.Content.(string)
	if !strings.Contains(content, "Foo") {
		t.Fatalf("expected symbol name in content, got: %q", content)
	}
}

// TestDispatchLSPWithMockClientServerError verifies that a NavigationClient
// error is surfaced in the result (supported=true, error set).
func TestDispatchLSPWithMockClientServerError(t *testing.T) {
	mock := &mockNavigationClient{err: errors.New("language server not running")}
	ctx := lspCtxWithClient(mock)
	raw, _ := json.Marshal(map[string]any{"operation": "goToDefinition", "filePath": "/work/a.go", "line": 1, "character": 1})
	result, err := NewLSPTool().Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["supported"] != true {
		t.Fatalf("expected supported=true when server returns error, got: %v", result.StructuredContent["supported"])
	}
	if _, ok := result.StructuredContent["error"]; !ok {
		t.Fatalf("expected error field in structured content")
	}
}

// TestDispatchLSPNullResultFormatting verifies that a null LSP response is
// formatted as "No results found." rather than an error.
func TestDispatchLSPNullResultFormatting(t *testing.T) {
	mock := &mockNavigationClient{response: json.RawMessage(`null`)}
	ctx := lspCtxWithClient(mock)
	raw, _ := json.Marshal(map[string]any{"operation": "goToDefinition", "filePath": "/work/a.go", "line": 1, "character": 1})
	result, err := NewLSPTool().Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatal(err)
	}
	content, _ := result.Content.(string)
	if !strings.Contains(content, "No results found") {
		t.Fatalf("expected 'No results found' for null response, got: %q", content)
	}
}
