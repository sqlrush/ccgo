package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNavigationParamsGoToDefinition verifies that goToDefinition maps to
// textDocument/definition with 1-based → 0-based coordinate conversion.
func TestNavigationParamsGoToDefinition(t *testing.T) {
	method, params, err := NavigationParams("goToDefinition", "file:///work/main.go", 5, 3)
	if err != nil {
		t.Fatal(err)
	}
	if method != "textDocument/definition" {
		t.Fatalf("method = %q, want textDocument/definition", method)
	}
	data, _ := json.Marshal(params)
	s := string(data)
	if !strings.Contains(s, `"line":4`) {
		t.Fatalf("expected 0-based line 4 in params: %s", s)
	}
	if !strings.Contains(s, `"character":2`) {
		t.Fatalf("expected 0-based character 2 in params: %s", s)
	}
}

// TestNavigationParamsFindReferences verifies findReferences includes
// includeDeclaration context.
func TestNavigationParamsFindReferences(t *testing.T) {
	method, params, err := NavigationParams("findReferences", "file:///work/main.go", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if method != "textDocument/references" {
		t.Fatalf("method = %q, want textDocument/references", method)
	}
	data, _ := json.Marshal(params)
	s := string(data)
	if !strings.Contains(s, "includeDeclaration") {
		t.Fatalf("expected includeDeclaration in params: %s", s)
	}
}

// TestNavigationParamsHover verifies hover maps to textDocument/hover.
func TestNavigationParamsHover(t *testing.T) {
	method, _, err := NavigationParams("hover", "file:///work/main.go", 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if method != "textDocument/hover" {
		t.Fatalf("method = %q, want textDocument/hover", method)
	}
}

// TestNavigationParamsDocumentSymbol verifies documentSymbol omits position.
func TestNavigationParamsDocumentSymbol(t *testing.T) {
	method, params, err := NavigationParams("documentSymbol", "file:///work/main.go", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if method != "textDocument/documentSymbol" {
		t.Fatalf("method = %q, want textDocument/documentSymbol", method)
	}
	data, _ := json.Marshal(params)
	s := string(data)
	if strings.Contains(s, "position") {
		t.Fatalf("documentSymbol params should not contain position: %s", s)
	}
}

// TestNavigationParamsWorkspaceSymbol verifies workspaceSymbol uses workspace/symbol.
func TestNavigationParamsWorkspaceSymbol(t *testing.T) {
	method, _, err := NavigationParams("workspaceSymbol", "file:///work/main.go", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if method != "workspace/symbol" {
		t.Fatalf("method = %q, want workspace/symbol", method)
	}
}

// TestNavigationParamsCallHierarchyOps verifies that incomingCalls and outgoingCalls
// use prepareCallHierarchy as the first step (matching CC's two-step approach).
func TestNavigationParamsCallHierarchyOps(t *testing.T) {
	for _, op := range []string{"prepareCallHierarchy", "incomingCalls", "outgoingCalls"} {
		method, _, err := NavigationParams(op, "file:///work/main.go", 1, 1)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if method != "textDocument/prepareCallHierarchy" {
			t.Fatalf("%s: method = %q, want textDocument/prepareCallHierarchy", op, method)
		}
	}
}

// TestNavigationParamsUnknownOp verifies an error is returned for unknown ops.
func TestNavigationParamsUnknownOp(t *testing.T) {
	_, _, err := NavigationParams("badOp", "file:///work/main.go", 1, 1)
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
}

// TestFormatNavigationResultLocations verifies that Location arrays are rendered
// with file:line:character in 1-based coords.
func TestFormatNavigationResultLocations(t *testing.T) {
	raw := json.RawMessage(`[{"uri":"file:///work/main.go","range":{"start":{"line":9,"character":4},"end":{"line":9,"character":8}}}]`)
	result := FormatNavigationResult("goToDefinition", raw)
	if result.ResultCount != 1 {
		t.Fatalf("result_count = %d, want 1", result.ResultCount)
	}
	if result.FileCount != 1 {
		t.Fatalf("file_count = %d, want 1", result.FileCount)
	}
	if !strings.Contains(result.Formatted, "main.go:10:5") {
		t.Fatalf("expected main.go:10:5 in formatted, got: %q", result.Formatted)
	}
}

// TestFormatNavigationResultNullRaw verifies that a null response yields
// "No results found.".
func TestFormatNavigationResultNullRaw(t *testing.T) {
	result := FormatNavigationResult("goToDefinition", json.RawMessage(`null`))
	if result.Formatted != "No results found." {
		t.Fatalf("expected 'No results found.', got: %q", result.Formatted)
	}
}

// TestFormatNavigationResultHover verifies hover markdown extraction.
func TestFormatNavigationResultHover(t *testing.T) {
	raw := json.RawMessage(`{"contents":{"kind":"markdown","value":"## func Foo\nreturns int"}}`)
	result := FormatNavigationResult("hover", raw)
	if !strings.Contains(result.Formatted, "func Foo") {
		t.Fatalf("expected func Foo in hover result, got: %q", result.Formatted)
	}
	if result.ResultCount != 1 {
		t.Fatalf("hover result_count = %d, want 1", result.ResultCount)
	}
}

// TestFormatNavigationResultDocumentSymbols verifies symbol list formatting.
func TestFormatNavigationResultDocumentSymbols(t *testing.T) {
	raw := json.RawMessage(`[{"name":"Bar","kind":6,"range":{"start":{"line":3,"character":0},"end":{"line":5,"character":1}}}]`)
	result := FormatNavigationResult("documentSymbol", raw)
	if result.ResultCount != 1 {
		t.Fatalf("symbol result_count = %d, want 1", result.ResultCount)
	}
	if !strings.Contains(result.Formatted, "Bar") {
		t.Fatalf("expected symbol name Bar in formatted, got: %q", result.Formatted)
	}
	if !strings.Contains(result.Formatted, "line:4") {
		t.Fatalf("expected 1-based line:4 in formatted, got: %q", result.Formatted)
	}
}

// TestURIToPath verifies file:// URI stripping.
func TestURIToPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"file:///work/main.go", "/work/main.go"},
		{"file:///path/with%20space/a.go", "/path/with space/a.go"},
		{"/no/scheme.go", "/no/scheme.go"},
	}
	for _, tt := range tests {
		got := uriToPath(tt.input)
		if got != tt.want {
			t.Errorf("uriToPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
