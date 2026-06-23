package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/config"
)

// TestBuildOverlaySubmitHandlerNilBothReturnsNil verifies that when neither
// onOverlay nor mcpApprovalPath are set, nil is returned (MCP-C04).
func TestBuildOverlaySubmitHandlerNilBothReturnsNil(t *testing.T) {
	if h := buildOverlaySubmitHandler(nil, ""); h != nil {
		t.Fatal("expected nil handler when both inputs are nil/empty")
	}
}

// TestBuildOverlaySubmitHandlerCallsOnOverlay verifies the callback is invoked
// for non-mcp: submissions (MCP-C04).
func TestBuildOverlaySubmitHandlerCallsOnOverlay(t *testing.T) {
	called := ""
	h := buildOverlaySubmitHandler(func(s string) { called = s }, "")
	h("theme:dark")
	if called != "theme:dark" {
		t.Fatalf("expected onOverlay called with 'theme:dark', got %q", called)
	}
}

// TestTryApplyMCPApprovalYesAll verifies that "mcp:yes_all:srv" writes
// enableAllProjectMcpServers=true to the path (MCP-C04).
func TestTryApplyMCPApprovalYesAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")

	tryApplyMCPApproval(path, "mcp:yes_all:my-server")

	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if enabled, _ := doc["enableAllProjectMcpServers"].(bool); !enabled {
		t.Fatalf("enableAllProjectMcpServers should be true, got %v", doc)
	}
}

// TestTryApplyMCPApprovalYes verifies that "mcp:yes:srv" appends srv to
// enabledMcpjsonServers (MCP-C04).
func TestTryApplyMCPApprovalYes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")

	tryApplyMCPApproval(path, "mcp:yes:fs-tool")

	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	enabled, _ := doc["enabledMcpjsonServers"].([]any)
	if len(enabled) != 1 || enabled[0].(string) != "fs-tool" {
		t.Fatalf("enabledMcpjsonServers = %v", enabled)
	}
}

// TestTryApplyMCPApprovalNo verifies that "mcp:no:srv" appends srv to
// disabledMcpjsonServers (MCP-C04).
func TestTryApplyMCPApprovalNo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")

	tryApplyMCPApproval(path, "mcp:no:bad-server")

	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	disabled, _ := doc["disabledMcpjsonServers"].([]any)
	if len(disabled) != 1 || disabled[0].(string) != "bad-server" {
		t.Fatalf("disabledMcpjsonServers = %v", disabled)
	}
}

// TestTryApplyMCPApprovalUnrelated verifies that non-mcp: submissions do not
// write to path (MCP-C04).
func TestTryApplyMCPApprovalUnrelated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")

	tryApplyMCPApproval(path, "theme:dark")

	// File should not exist.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("path should not be created for non-mcp: submission")
	}
}

// TestBuildOverlaySubmitHandlerPersistsMCPApproval tests the full pipeline:
// overlay handler writes to path AND calls onOverlay (MCP-C04).
func TestBuildOverlaySubmitHandlerPersistsMCPApproval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")
	var called []string
	h := buildOverlaySubmitHandler(func(s string) { called = append(called, s) }, path)

	h("mcp:yes:git-tool")
	h("theme:dark")

	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	enabled, _ := doc["enabledMcpjsonServers"].([]any)
	if len(enabled) != 1 {
		t.Fatalf("enabledMcpjsonServers = %v", enabled)
	}
	if len(called) != 2 || called[0] != "mcp:yes:git-tool" || called[1] != "theme:dark" {
		t.Fatalf("onOverlay calls = %v", called)
	}
}

// TestBuildOverlaySubmitHandlerEmptyServerNameIsNoop tests that a mcp: prefix
// with empty server name is silently ignored (defensive).
func TestBuildOverlaySubmitHandlerEmptyServerNameIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")
	h := buildOverlaySubmitHandler(nil, path)
	h("mcp:yes:")
	// No file should be written for empty server name.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		// Read file and check it's empty/default
		data, _ := os.ReadFile(path)
		if len(strings.TrimSpace(string(data))) > 2 { // "{}" is fine
			t.Fatalf("unexpected file content: %s", data)
		}
	}
}
