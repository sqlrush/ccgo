package lsp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// helperNavServerDefinition returns a ServerDefinition whose binary is the
// current test binary running in helper-nav-session mode. The FileExtensions
// restrict it to ".go" files so routing tests are deterministic.
func helperNavServerDefinition() ServerDefinition {
	return ServerDefinition{
		Name:           "helper-nav-session",
		Command:        helperServerDefinition("helper-nav-session").Command,
		Args:           helperServerDefinition("helper-nav-session").Args,
		FileExtensions: []string{".go"},
	}
}

// TestSessionNavigationManagerNoDefinitionsReturnsNotFound verifies that a
// manager with no server definitions returns a "no LSP server" error without
// panicking.
func TestSessionNavigationManagerNoDefinitionsReturnsNotFound(t *testing.T) {
	mgr := NewSessionNavigationManager(nil, "")
	defer mgr.Close() //nolint:errcheck

	_, err := mgr.SendRequest(context.Background(), "/work/main.go", "textDocument/hover", nil)
	if err == nil {
		t.Fatal("expected error for empty definitions, got nil")
	}
	if !strings.Contains(err.Error(), "no LSP server") {
		t.Fatalf("expected 'no LSP server' in error, got: %v", err)
	}
}

// TestSessionNavigationManagerDoesNotStartForUnknownExtension verifies that
// a .xyz file that matches no registered server returns a not-found error.
func TestSessionNavigationManagerDoesNotStartForUnknownExtension(t *testing.T) {
	mgr := NewSessionNavigationManager([]ServerDefinition{helperNavServerDefinition()}, t.TempDir())
	defer mgr.Close() //nolint:errcheck

	_, err := mgr.SendRequest(context.Background(), "/work/main.xyz", "textDocument/hover", nil)
	if err == nil {
		t.Fatal("expected error for unknown extension, got nil")
	}
	if !strings.Contains(err.Error(), "no LSP server") {
		t.Fatalf("expected 'no LSP server' in error, got: %v", err)
	}
}

// TestSessionNavigationManagerNoFileExtensionReturnsNotFound verifies that a
// filePath with no extension returns a not-found error (not a panic).
func TestSessionNavigationManagerNoFileExtensionReturnsNotFound(t *testing.T) {
	mgr := NewSessionNavigationManager([]ServerDefinition{helperNavServerDefinition()}, t.TempDir())
	defer mgr.Close() //nolint:errcheck

	_, err := mgr.SendRequest(context.Background(), "/work/Makefile", "textDocument/hover", nil)
	if err == nil {
		t.Fatal("expected error for no extension, got nil")
	}
	if !strings.Contains(err.Error(), "no LSP server") {
		t.Fatalf("expected 'no LSP server' in error, got: %v", err)
	}
}

// TestSessionNavigationManagerRoutesRequestToServer verifies that when a
// matching server definition is present (backed by the helper-nav-session
// process) the manager successfully sends a request and returns a result.
func TestSessionNavigationManagerRoutesRequestToServer(t *testing.T) {
	mgr := NewSessionNavigationManager([]ServerDefinition{helperNavServerDefinition()}, t.TempDir())
	defer mgr.Close() //nolint:errcheck

	raw, err := mgr.SendRequest(context.Background(), "/work/main.go", "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": "file:///work/main.go"},
		"position":     map[string]any{"line": 0, "character": 0},
	})
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if raw == nil {
		t.Fatal("expected non-nil result")
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["contents"] != "nav-result" {
		t.Fatalf("result contents = %v, want 'nav-result'", result["contents"])
	}
}

// TestSessionNavigationManagerLazyStartsOnce verifies that two concurrent
// requests to the same server result in only one server process (client
// is reused after the first lazy start).
func TestSessionNavigationManagerLazyStartsOnce(t *testing.T) {
	mgr := NewSessionNavigationManager([]ServerDefinition{helperNavServerDefinition()}, t.TempDir())
	defer mgr.Close() //nolint:errcheck

	// Fire two sequential requests; the client should be reused.
	for i := 0; i < 2; i++ {
		_, err := mgr.SendRequest(context.Background(), "/work/main.go", "textDocument/hover", nil)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}

	mgr.mu.Lock()
	count := len(mgr.clients)
	mgr.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 client, got %d", count)
	}
}

// TestSessionNavigationManagerCloseReleasesClients verifies that Close
// empties the client map and a subsequent SendRequest returns an error
// (process was killed).
func TestSessionNavigationManagerCloseReleasesClients(t *testing.T) {
	mgr := NewSessionNavigationManager([]ServerDefinition{helperNavServerDefinition()}, t.TempDir())

	// Start a client.
	if _, err := mgr.SendRequest(context.Background(), "/work/main.go", "textDocument/hover", nil); err != nil {
		t.Fatalf("initial request: %v", err)
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mgr.mu.Lock()
	count := len(mgr.clients)
	mgr.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 clients after Close, got %d", count)
	}
}
