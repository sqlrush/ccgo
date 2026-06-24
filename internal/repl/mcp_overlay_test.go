package repl

// G24: /mcp interactive overlay tests.
// Tests that the MCPOverlay wraps live server status and dispatches
// enable/disable/reconnect actions to the Manager.
//
// CC ref: src/commands/mcp/mcp.tsx:83 (MCPSettings panel with enable/disable/reconnect).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
	"ccgo/internal/tui"
)

// --- test helpers (reuse g11 pattern) ---

func g24FakeDialer() mcp.ClientOpenFunc {
	return func(_ context.Context, name string, _ contracts.MCPServer) (mcp.ClientHandle, error) {
		return mcp.ClientHandle{
			Client: &g24FakeClient{name: name},
			Close:  func() error { return nil },
		}, nil
	}
}

type g24FakeClient struct{ name string }

func (f *g24FakeClient) ListTools(_ context.Context, _ string) ([]mcp.RemoteTool, error) {
	return nil, nil
}
func (f *g24FakeClient) CallTool(_ context.Context, _ string, _ string, _ json.RawMessage) (any, error) {
	return nil, fmt.Errorf("fake: CallTool not implemented")
}
func (f *g24FakeClient) ListResources(_ context.Context, _ string) ([]mcp.RemoteResource, error) {
	return nil, nil
}
func (f *g24FakeClient) ListResourceTemplates(_ context.Context, _ string) ([]mcp.RemoteResourceTemplate, error) {
	return nil, nil
}
func (f *g24FakeClient) ReadResource(_ context.Context, _ string, _ string) ([]mcp.ResourceContent, error) {
	return nil, fmt.Errorf("fake: ReadResource not implemented")
}
func (f *g24FakeClient) SubscribeResource(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("fake: SubscribeResource not implemented")
}
func (f *g24FakeClient) ListPrompts(_ context.Context, _ string) ([]mcp.RemotePrompt, error) {
	return nil, nil
}
func (f *g24FakeClient) GetPrompt(_ context.Context, _ string, _ string, _ map[string]string) (mcp.PromptResult, error) {
	return mcp.PromptResult{}, fmt.Errorf("fake: GetPrompt not implemented")
}

func startG24Manager(t *testing.T, servers map[string]contracts.MCPServer) *mcp.Manager {
	t.Helper()
	mgr := mcp.NewManager(servers, g24FakeDialer())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	return mgr
}

// --- overlay tests ---

func TestMCPOverlayInitialCursor(t *testing.T) {
	mgr := startG24Manager(t, map[string]contracts.MCPServer{
		"server-a": {Type: "stdio", Command: "a"},
		"server-b": {Type: "stdio", Command: "b"},
	})
	defer mgr.Stop()

	ov := newMCPOverlay(mgr)
	if ov.Cursor() != 0 {
		t.Fatalf("initial cursor = %d want 0", ov.Cursor())
	}
}

func TestMCPOverlayNavigateDown(t *testing.T) {
	mgr := startG24Manager(t, map[string]contracts.MCPServer{
		"s1": {Type: "stdio", Command: "a"},
		"s2": {Type: "stdio", Command: "b"},
	})
	defer mgr.Stop()

	ov := newMCPOverlay(mgr)
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyDown})
	if !handled {
		t.Fatal("Down should be handled")
	}
	if res.Dismissed || res.Submit != "" {
		t.Fatalf("Down should not dismiss/submit: %+v", res)
	}
	if ov.Cursor() != 1 {
		t.Fatalf("cursor after Down = %d want 1", ov.Cursor())
	}
}

func TestMCPOverlayNavigateUp(t *testing.T) {
	mgr := startG24Manager(t, map[string]contracts.MCPServer{
		"s1": {Type: "stdio", Command: "a"},
		"s2": {Type: "stdio", Command: "b"},
	})
	defer mgr.Stop()

	ov := newMCPOverlay(mgr)
	ov.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor=1
	ov.ApplyKey(tui.Key{Type: tui.KeyUp})   // cursor=0
	if ov.Cursor() != 0 {
		t.Fatalf("cursor after Up = %d want 0", ov.Cursor())
	}
}

func TestMCPOverlayEscDismisses(t *testing.T) {
	mgr := startG24Manager(t, nil)
	defer mgr.Stop()

	ov := newMCPOverlay(mgr)
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if !res.Dismissed {
		t.Fatalf("Esc should dismiss, got %+v", res)
	}
}

func TestMCPOverlayRenderNonEmpty(t *testing.T) {
	mgr := startG24Manager(t, map[string]contracts.MCPServer{
		"myserver": {Type: "stdio", Command: "x"},
	})
	defer mgr.Stop()

	ov := newMCPOverlay(mgr)
	lines := ov.Render(80, 24)
	if len(lines) == 0 {
		t.Fatal("Render should return non-empty lines")
	}
}

func TestMCPOverlayRenderShowsServerName(t *testing.T) {
	mgr := startG24Manager(t, map[string]contracts.MCPServer{
		"myserver": {Type: "stdio", Command: "x"},
	})
	defer mgr.Stop()

	ov := newMCPOverlay(mgr)
	lines := ov.Render(80, 24)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "myserver") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Render missing 'myserver': %v", lines)
	}
}

func TestMCPOverlayDisableAction(t *testing.T) {
	mgr := startG24Manager(t, map[string]contracts.MCPServer{
		"srv": {Type: "stdio", Command: "x"},
	})
	defer mgr.Stop()

	ov := newMCPOverlay(mgr)
	// 'd' key → disable selected server
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'd'})
	if !handled {
		t.Fatal("'d' should be handled")
	}
	if !strings.HasPrefix(res.Submit, "mcp:disable:") {
		t.Fatalf("submit = %q want prefix mcp:disable:", res.Submit)
	}
	// Verify manager sees disabled status
	statuses := mgr.Status()
	if len(statuses) == 0 {
		t.Fatal("expected at least one server")
	}
	if statuses[0].Status != mcp.ServerStatusDisabled {
		t.Fatalf("after disable: status = %q want disabled", statuses[0].Status)
	}
}

func TestMCPOverlayEnableAction(t *testing.T) {
	mgr := startG24Manager(t, map[string]contracts.MCPServer{
		"srv": {Type: "stdio", Command: "x"},
	})
	defer mgr.Stop()

	// First disable then re-enable.
	ctx := context.Background()
	_ = mgr.SetEnabled(ctx, "srv", false)

	ov := newMCPOverlay(mgr)
	// 'e' key → enable selected server
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'e'})
	if !handled {
		t.Fatal("'e' should be handled")
	}
	if !strings.HasPrefix(res.Submit, "mcp:enable:") {
		t.Fatalf("submit = %q want prefix mcp:enable:", res.Submit)
	}
}

func TestMCPOverlayReconnectAction(t *testing.T) {
	mgr := startG24Manager(t, map[string]contracts.MCPServer{
		"srv": {Type: "stdio", Command: "x"},
	})
	defer mgr.Stop()

	ov := newMCPOverlay(mgr)
	// 'r' key → reconnect selected server
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'r'})
	if !handled {
		t.Fatal("'r' should be handled")
	}
	if !strings.HasPrefix(res.Submit, "mcp:reconnect:") {
		t.Fatalf("submit = %q want prefix mcp:reconnect:", res.Submit)
	}
}

func TestMCPOverlayNilManagerNoOp(t *testing.T) {
	ov := newMCPOverlay(nil)
	lines := ov.Render(80, 24)
	if len(lines) == 0 {
		t.Fatal("Render should return non-empty lines even with nil manager")
	}
}

func TestMCPHandlerWithManagerOpensOverlay(t *testing.T) {
	mgr := startG24Manager(t, map[string]contracts.MCPServer{
		"srv": {Type: "stdio", Command: "x"},
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
		t.Fatal("handler must return an Overlay")
	}
}

func TestMCPOverlayHandlerNilManagerFallback(t *testing.T) {
	h := mcpOverlayHandlerWith(nil)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be Handled")
	}
	// nil manager → text fallback (no overlay)
	if out.Status == "" {
		t.Fatal("nil manager: Status should be non-empty")
	}
}
