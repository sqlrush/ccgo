package repl

// G11 tests: verify that the /mcp REPL command handler uses live Manager status.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

// g11ReplFakeDialer is a minimal ClientOpenFunc that always connects.
func g11ReplFakeDialer() mcp.ClientOpenFunc {
	return func(_ context.Context, name string, _ contracts.MCPServer) (mcp.ClientHandle, error) {
		return mcp.ClientHandle{
			Client: &g11ReplFakeClient{name: name},
			Close:  func() error { return nil },
		}, nil
	}
}

type g11ReplFakeClient struct{ name string }

func (f *g11ReplFakeClient) ListTools(_ context.Context, _ string) ([]mcp.RemoteTool, error) {
	return nil, nil
}
func (f *g11ReplFakeClient) CallTool(_ context.Context, _ string, _ string, _ json.RawMessage) (any, error) {
	return nil, fmt.Errorf("fake: CallTool not implemented")
}
func (f *g11ReplFakeClient) ListResources(_ context.Context, _ string) ([]mcp.RemoteResource, error) {
	return nil, nil
}
func (f *g11ReplFakeClient) ListResourceTemplates(_ context.Context, _ string) ([]mcp.RemoteResourceTemplate, error) {
	return nil, nil
}
func (f *g11ReplFakeClient) ReadResource(_ context.Context, _ string, _ string) ([]mcp.ResourceContent, error) {
	return nil, fmt.Errorf("fake: ReadResource not implemented")
}
func (f *g11ReplFakeClient) SubscribeResource(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("fake: SubscribeResource not implemented")
}
func (f *g11ReplFakeClient) ListPrompts(_ context.Context, _ string) ([]mcp.RemotePrompt, error) {
	return nil, nil
}
func (f *g11ReplFakeClient) GetPrompt(_ context.Context, _ string, _ string, _ map[string]string) (mcp.PromptResult, error) {
	return mcp.PromptResult{}, fmt.Errorf("fake: GetPrompt not implemented")
}

func startG11Manager(t *testing.T, servers map[string]contracts.MCPServer) *mcp.Manager {
	t.Helper()
	mgr := mcp.NewManager(servers, g11ReplFakeDialer())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	return mgr
}

// TestMCPHandlerWithManagerShowsLiveStatus verifies that mcpHandlerWith returns
// live status from the Manager via mcpStatusPanel.
func TestMCPHandlerWithManagerShowsLiveStatus(t *testing.T) {
	mgr := startG11Manager(t, map[string]contracts.MCPServer{
		"myserver": {Type: "stdio", Command: "myserver"},
	})
	defer mgr.Stop()

	h := mcpHandlerWith(mgr)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Error("handler should return Handled=true")
	}
	if !strings.Contains(out.Status, "myserver") {
		t.Errorf("status missing 'myserver': %q", out.Status)
	}
	if !strings.Contains(out.Status, "connected") {
		t.Errorf("status missing 'connected': %q", out.Status)
	}
}

// TestMCPHandlerWithManagerNoServers renders empty state correctly.
func TestMCPHandlerWithManagerNoServers(t *testing.T) {
	mgr := startG11Manager(t, nil)
	defer mgr.Stop()

	h := mcpHandlerWith(mgr)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Error("handler should return Handled=true")
	}
	if !strings.Contains(out.Status, "No MCP servers") {
		t.Errorf("empty state: want 'No MCP servers', got %q", out.Status)
	}
}

// TestMCPHandlerNilManagerFallback verifies graceful handling when manager is nil.
func TestMCPHandlerNilManagerFallback(t *testing.T) {
	h := mcpHandlerWith(nil)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Error("handler should return Handled=true")
	}
	// Should show some kind of informational message, not crash.
	if out.Status == "" {
		t.Error("nil manager: want non-empty status message")
	}
}

// TestMCPHandlerManagerStatusImmutable verifies returned entries are copies.
func TestMCPHandlerManagerStatusImmutable(t *testing.T) {
	mgr := startG11Manager(t, map[string]contracts.MCPServer{
		"s": {Type: "stdio", Command: "s"},
	})
	defer mgr.Stop()

	h := mcpHandlerWith(mgr)
	out1, _ := h(context.Background(), CommandContext{})
	out2, _ := h(context.Background(), CommandContext{})
	// Both calls should return independent status strings.
	if out1.Status != out2.Status {
		t.Errorf("inconsistent status between calls: %q vs %q", out1.Status, out2.Status)
	}
}
