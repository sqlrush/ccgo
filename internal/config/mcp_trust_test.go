package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyMCPApprovalYesAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")

	if err := ApplyMCPApproval(path, MCPApprovalYesAll, "myserver"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc, err := ReadSettingsDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if enabled, _ := doc["enableAllProjectMcpServers"].(bool); !enabled {
		t.Fatalf("enableAllProjectMcpServers = %v, want true", doc["enableAllProjectMcpServers"])
	}
}

func TestApplyMCPApprovalYes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")

	if err := ApplyMCPApproval(path, MCPApprovalYes, "server-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Apply again with a different server.
	if err := ApplyMCPApproval(path, MCPApprovalYes, "server-b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc, err := ReadSettingsDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	enabled, _ := doc["enabledMcpjsonServers"].([]any)
	if len(enabled) != 2 {
		t.Fatalf("enabledMcpjsonServers = %v, want 2 entries", enabled)
	}
}

func TestApplyMCPApprovalNo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")

	if err := ApplyMCPApproval(path, MCPApprovalNo, "bad-server"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc, err := ReadSettingsDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	disabled, _ := doc["disabledMcpjsonServers"].([]any)
	if len(disabled) != 1 {
		t.Fatalf("disabledMcpjsonServers = %v, want 1 entry", disabled)
	}
}

func TestApplyMCPApprovalYesDeduplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")

	if err := ApplyMCPApproval(path, MCPApprovalYes, "same"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMCPApproval(path, MCPApprovalYes, "same"); err != nil {
		t.Fatal(err)
	}

	doc, err := ReadSettingsDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	enabled, _ := doc["enabledMcpjsonServers"].([]any)
	if len(enabled) != 1 {
		t.Fatalf("expected deduplication, got %v", enabled)
	}
}

func TestApplyMCPApprovalPreservesExistingKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.local.json")
	// Pre-populate with unrelated key.
	if err := os.WriteFile(path, []byte(`{"model":"claude-opus"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ApplyMCPApproval(path, MCPApprovalNo, "untrusted"); err != nil {
		t.Fatal(err)
	}

	doc, err := ReadSettingsDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if model, _ := doc["model"].(string); model != "claude-opus" {
		t.Fatalf("existing model key lost: %v", doc)
	}
}
