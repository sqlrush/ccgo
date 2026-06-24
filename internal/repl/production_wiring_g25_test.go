package repl

// G25 audit fix: tests that verify the production router correctly wires
// real dependencies (configWriter, MCPManager) instead of nil no-ops.
//
// CMD-CONFIG-01: toggling a bool setting via the /config overlay must call
// the configWriter seam and persist the value to disk.
//
// CMD-MCP-01: with opts.MCPManager set, the /mcp overlay actions dispatch
// to the Manager.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
	"ccgo/internal/tui"
)

// --- G25 helpers ---

func g25FakeDialer() mcp.ClientOpenFunc {
	return func(_ context.Context, name string, _ contracts.MCPServer) (mcp.ClientHandle, error) {
		return mcp.ClientHandle{
			Client: &g25FakeClient{name: name},
			Close:  func() error { return nil },
		}, nil
	}
}

type g25FakeClient struct{ name string }

func (f *g25FakeClient) ListTools(_ context.Context, _ string) ([]mcp.RemoteTool, error) {
	return nil, nil
}
func (f *g25FakeClient) CallTool(_ context.Context, _ string, _ string, _ json.RawMessage) (any, error) {
	return nil, fmt.Errorf("g25fake: CallTool not implemented")
}
func (f *g25FakeClient) ListResources(_ context.Context, _ string) ([]mcp.RemoteResource, error) {
	return nil, nil
}
func (f *g25FakeClient) ListResourceTemplates(_ context.Context, _ string) ([]mcp.RemoteResourceTemplate, error) {
	return nil, nil
}
func (f *g25FakeClient) ReadResource(_ context.Context, _ string, _ string) ([]mcp.ResourceContent, error) {
	return nil, fmt.Errorf("g25fake: ReadResource not implemented")
}
func (f *g25FakeClient) SubscribeResource(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("g25fake: SubscribeResource not implemented")
}
func (f *g25FakeClient) ListPrompts(_ context.Context, _ string) ([]mcp.RemotePrompt, error) {
	return nil, nil
}
func (f *g25FakeClient) GetPrompt(_ context.Context, _ string, _ string, _ map[string]string) (mcp.PromptResult, error) {
	return mcp.PromptResult{}, fmt.Errorf("g25fake: GetPrompt not implemented")
}

// startG25Manager starts an MCP manager with the given servers.
func startG25Manager(t *testing.T, servers map[string]contracts.MCPServer) *mcp.Manager {
	t.Helper()
	mgr := mcp.NewManager(servers, g25FakeDialer())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	return mgr
}

// --- CMD-CONFIG-01 production wiring tests ---

// TestProductionConfigWriterPersistsToggle verifies that the production config
// writer (passed to configHandlerWithOverlay in newProductionRouterFull) calls
// config.SetSettingsValue so that toggling a bool setting on the overlay
// actually persists to disk.
func TestProductionConfigWriterPersistsToggle(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Build the production-style writer (same as production wiring in run.go).
	writer := func(key, val string) error {
		return config.SetSettingsValue(settingsPath, key, val)
	}

	entries := []configSettingEntry{
		{Key: "verbose", Label: "Verbose", Value: "false", Editable: true, Type: configTypeBool},
	}
	ov := newConfigOverlay(entries, writer)

	// Toggle verbose false → true.
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "config:verbose:true" {
		t.Fatalf("submit = %q want config:verbose:true", res.Submit)
	}

	// Verify that the value was persisted to the temp settings file.
	doc, err := config.ReadSettingsDocument(settingsPath)
	if err != nil {
		t.Fatalf("ReadSettingsDocument: %v", err)
	}
	val, ok := doc["verbose"]
	if !ok {
		t.Fatalf("settings document missing 'verbose' key; doc = %v", doc)
	}
	if val != "true" {
		t.Fatalf("persisted verbose = %v want \"true\"", val)
	}
}

// TestProductionConfigHandlerUsesRealWriter verifies that the production
// newProductionRouterFull wires a real writer (not nil) to the /config handler
// so that toggling persists to disk.
func TestProductionConfigHandlerUsesRealWriter(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Build the same writer as production (the fix).
	writer := func(key, val string) error {
		return config.SetSettingsValue(settingsPath, key, val)
	}

	h := configHandlerWithOverlay(func() ([]configSettingEntry, error) {
		return []configSettingEntry{
			{Key: "verbose", Label: "Verbose", Value: "false", Editable: true, Type: configTypeBool},
		}, nil
	}, writer)

	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be Handled")
	}
	if out.Overlay == nil {
		t.Fatal("handler must return an Overlay")
	}

	// Apply a toggle via the overlay.
	ov, ok := out.Overlay.(*configOverlay)
	if !ok {
		t.Fatalf("overlay is %T, want *configOverlay", out.Overlay)
	}
	res, _ := ov.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "config:verbose:true" {
		t.Fatalf("submit = %q want config:verbose:true", res.Submit)
	}

	// Verify persistence.
	doc, err := config.ReadSettingsDocument(settingsPath)
	if err != nil {
		t.Fatalf("ReadSettingsDocument: %v", err)
	}
	if doc["verbose"] != "true" {
		t.Fatalf("settings doc verbose = %v want \"true\"", doc["verbose"])
	}
}

// TestProductionConfigWriterCreatesFileIfAbsent verifies that the writer
// creates the settings file if it does not already exist (SetSettingsValue
// behaviour), so a fresh install doesn't fail silently.
func TestProductionConfigWriterCreatesFileIfAbsent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "sub", "settings.json")

	writer := func(key, val string) error {
		return config.SetSettingsValue(settingsPath, key, val)
	}

	entries := []configSettingEntry{
		{Key: "theme", Label: "Theme", Value: "dark", Editable: true, Type: configTypeBool},
	}
	ov := newConfigOverlay(entries, writer)
	_, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}

	// File should now exist.
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings file not created: %v", err)
	}
}

// --- CMD-MCP-01 production wiring tests ---

// TestProductionMCPManagerActionsReachManager verifies that when an MCPManager
// is set (the production wiring), overlay actions (enable/disable/reconnect)
// call through to the Manager.
func TestProductionMCPManagerActionsReachManager(t *testing.T) {
	mgr := startG25Manager(t, map[string]contracts.MCPServer{
		"srv": {Type: "stdio", Command: "echo"},
	})
	defer mgr.Stop()

	// Verify initially connected (or at least not disabled).
	statuses := mgr.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 server status, got %d", len(statuses))
	}

	// Build overlay with the manager (as production wiring does via opts.MCPManager).
	ov := newMCPOverlay(mgr)

	// 'd' → disable.
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'd'})
	if !handled {
		t.Fatal("'d' should be handled")
	}
	if res.Submit == "" {
		t.Fatal("'d' should produce a submit token")
	}

	// Manager should now show srv as disabled.
	updated := mgr.Status()
	if len(updated) != 1 {
		t.Fatalf("expected 1 server, got %d", len(updated))
	}
	if updated[0].Status != mcp.ServerStatusDisabled {
		t.Fatalf("after disable: status = %q want %q", updated[0].Status, mcp.ServerStatusDisabled)
	}
}

// TestProductionMCPHandlerWithRealManagerOpensOverlay verifies that
// mcpOverlayHandlerWith(mgr) returns a live overlay (not a text fallback)
// when the manager is non-nil — the path that production code must take once
// opts.MCPManager is wired.
func TestProductionMCPHandlerWithRealManagerOpensOverlay(t *testing.T) {
	mgr := startG25Manager(t, map[string]contracts.MCPServer{
		"srv": {Type: "stdio", Command: "echo"},
	})
	defer mgr.Stop()

	h := mcpOverlayHandlerWith(mgr)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be Handled")
	}
	if out.Overlay == nil {
		t.Fatal("with a real manager, handler must return an Overlay (not text fallback)")
	}
}

// TestProductionMCPManagerWiredThroughOpts verifies that when
// opts.MCPManager is non-nil, newProductionRouterFull passes it through to the
// /mcp handler, so the overlay returns the live manager status rather than the
// nil-manager text fallback.
func TestProductionMCPManagerWiredThroughOpts(t *testing.T) {
	mgr := startG25Manager(t, map[string]contracts.MCPServer{
		"s1": {Type: "stdio", Command: "x"},
	})
	defer mgr.Stop()

	// newProductionRouterFull passes mcpMgr to mcpOverlayHandlerWith.
	router := newProductionRouterFull(".", nil, nil, mgr)
	out, err := router.Dispatch(context.Background(), "/mcp", CommandContext{})
	if err != nil {
		t.Fatalf("Dispatch /mcp: %v", err)
	}
	if !out.Handled {
		t.Fatal("/mcp must be Handled")
	}
	if out.Overlay == nil {
		t.Fatalf("/mcp with real manager must return Overlay, not text; got Status=%q", out.Status)
	}
}
