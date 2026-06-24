package lsp

// G29: §05 LSP remaining ops — stub-server round-trip tests.
//
// Tests that workspaceSymbol, goToImplementation, and outgoingCalls
// round-trip correctly: NavigationParams returns the right LSP method + params,
// the result from a stub server is correctly formatted by FormatNavigationResult.
//
// These tests use a stubNavigationClient (fake) so no real language server is needed.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// stubNavigationClient implements NavigationClient for testing.
// It records the last request and returns a preset response.
type stubNavigationClient struct {
	lastMethod string
	lastParams any
	response   json.RawMessage
	err        error
}

func (s *stubNavigationClient) SendRequest(ctx context.Context, filePath, method string, params any) (json.RawMessage, error) {
	s.lastMethod = method
	s.lastParams = params
	return s.response, s.err
}

// TestWorkspaceSymbolRoundTrip verifies that workspaceSymbol uses workspace/symbol
// and the response is correctly formatted as symbols.
func TestWorkspaceSymbolRoundTrip(t *testing.T) {
	stub := &stubNavigationClient{
		response: json.RawMessage(`[
			{"name":"FooBar","kind":6,"location":{"uri":"file:///work/foo.go","range":{"start":{"line":4,"character":0},"end":{"line":10,"character":1}}}}
		]`),
	}

	method, params, err := NavigationParams("workspaceSymbol", "file:///work/foo.go", 1, 1)
	if err != nil {
		t.Fatalf("NavigationParams err: %v", err)
	}
	if method != "workspace/symbol" {
		t.Fatalf("method = %q, want workspace/symbol", method)
	}

	raw, err := stub.SendRequest(context.Background(), "/work/foo.go", method, params)
	if err != nil {
		t.Fatalf("SendRequest err: %v", err)
	}

	result := FormatNavigationResult("workspaceSymbol", raw)
	if result.ResultCount != 1 {
		t.Fatalf("result_count = %d, want 1", result.ResultCount)
	}
	if !strings.Contains(result.Formatted, "FooBar") {
		t.Fatalf("formatted missing FooBar: %q", result.Formatted)
	}
}

// TestGoToImplementationRoundTrip verifies that goToImplementation uses
// textDocument/implementation and results are formatted as locations.
func TestGoToImplementationRoundTrip(t *testing.T) {
	stub := &stubNavigationClient{
		response: json.RawMessage(`[
			{"uri":"file:///work/impl.go","range":{"start":{"line":14,"character":5},"end":{"line":14,"character":12}}}
		]`),
	}

	method, params, err := NavigationParams("goToImplementation", "file:///work/iface.go", 3, 8)
	if err != nil {
		t.Fatalf("NavigationParams err: %v", err)
	}
	if method != "textDocument/implementation" {
		t.Fatalf("method = %q, want textDocument/implementation", method)
	}

	// Verify coordinate conversion (3,8) → (2,7) in 0-based.
	data, _ := json.Marshal(params)
	s := string(data)
	if !strings.Contains(s, `"line":2`) {
		t.Fatalf("expected 0-based line 2 in params: %s", s)
	}
	if !strings.Contains(s, `"character":7`) {
		t.Fatalf("expected 0-based char 7 in params: %s", s)
	}

	raw, err := stub.SendRequest(context.Background(), "/work/iface.go", method, params)
	if err != nil {
		t.Fatalf("SendRequest err: %v", err)
	}

	result := FormatNavigationResult("goToImplementation", raw)
	if result.ResultCount != 1 {
		t.Fatalf("result_count = %d, want 1", result.ResultCount)
	}
	if !strings.Contains(result.Formatted, "impl.go:15:6") {
		t.Fatalf("formatted missing impl.go:15:6 (1-based): %q", result.Formatted)
	}
}

// TestOutgoingCallsRoundTrip verifies that outgoingCalls uses
// textDocument/prepareCallHierarchy (first step in CC's two-step approach)
// and the response is formatted as call hierarchy items.
func TestOutgoingCallsRoundTrip(t *testing.T) {
	stub := &stubNavigationClient{
		// prepareCallHierarchy returns an array of CallHierarchyItem.
		response: json.RawMessage(`[
			{"name":"caller","kind":6,"uri":"file:///work/main.go","range":{"start":{"line":2,"character":0},"end":{"line":5,"character":1}},"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":11}}}
		]`),
	}

	method, params, err := NavigationParams("outgoingCalls", "file:///work/main.go", 3, 6)
	if err != nil {
		t.Fatalf("NavigationParams err: %v", err)
	}
	if method != "textDocument/prepareCallHierarchy" {
		t.Fatalf("method = %q, want textDocument/prepareCallHierarchy", method)
	}

	raw, err := stub.SendRequest(context.Background(), "/work/main.go", method, params)
	if err != nil {
		t.Fatalf("SendRequest err: %v", err)
	}

	result := FormatNavigationResult("outgoingCalls", raw)
	if result.ResultCount < 1 {
		t.Fatalf("outgoingCalls result_count = %d, want >= 1", result.ResultCount)
	}
	if !strings.Contains(result.Formatted, "caller") && !strings.Contains(result.Formatted, "main.go") {
		t.Fatalf("outgoingCalls formatted missing expected content: %q", result.Formatted)
	}
}

// TestWorkspaceSymbolNullResultFormatted verifies null response from server
// renders as "No results found."
func TestWorkspaceSymbolNullResultFormatted(t *testing.T) {
	result := FormatNavigationResult("workspaceSymbol", json.RawMessage(`null`))
	if result.Formatted != "No results found." {
		t.Fatalf("null result = %q, want 'No results found.'", result.Formatted)
	}
}

// TestGoToImplementationNullResultFormatted verifies null response for
// goToImplementation.
func TestGoToImplementationNullResultFormatted(t *testing.T) {
	result := FormatNavigationResult("goToImplementation", json.RawMessage(`null`))
	if result.Formatted != "No results found." {
		t.Fatalf("null result = %q, want 'No results found.'", result.Formatted)
	}
}
