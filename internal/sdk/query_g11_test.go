package sdk

// G11 tests: verify that Query wires the live mcp.Manager into the Controller
// so mcp_status/mcp_set_servers/mcp_message/mcp_reconnect/mcp_toggle act on
// real state instead of returning "not supported" errors.
//
// All tests use a fake dialer (no real MCP server, no network).
//
// CC refs: docs/cc-parity/sections/16-sdk.md SDK-32/38/39/41/42.

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

// fakeDialerG11 is a minimal ClientOpenFunc that always connects successfully.
type fakeDialerG11 struct{}

func (fakeDialerG11) openFunc() mcp.ClientOpenFunc {
	return func(_ context.Context, name string, _ contracts.MCPServer) (mcp.ClientHandle, error) {
		return mcp.ClientHandle{
			Client: &fakeG11Client{name: name},
			Close:  func() error { return nil },
		}, nil
	}
}

type fakeG11Client struct{ name string }

func (f *fakeG11Client) ListTools(_ context.Context, _ string) ([]mcp.RemoteTool, error) {
	return nil, nil
}
func (f *fakeG11Client) CallTool(_ context.Context, _ string, _ string, _ json.RawMessage) (any, error) {
	return nil, fmt.Errorf("fakeG11: CallTool not implemented")
}
func (f *fakeG11Client) ListResources(_ context.Context, _ string) ([]mcp.RemoteResource, error) {
	return nil, nil
}
func (f *fakeG11Client) ListResourceTemplates(_ context.Context, _ string) ([]mcp.RemoteResourceTemplate, error) {
	return nil, nil
}
func (f *fakeG11Client) ReadResource(_ context.Context, _ string, _ string) ([]mcp.ResourceContent, error) {
	return nil, fmt.Errorf("fakeG11: ReadResource not implemented")
}
func (f *fakeG11Client) SubscribeResource(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("fakeG11: SubscribeResource not implemented")
}
func (f *fakeG11Client) ListPrompts(_ context.Context, _ string) ([]mcp.RemotePrompt, error) {
	return nil, nil
}
func (f *fakeG11Client) GetPrompt(_ context.Context, _ string, _ string, _ map[string]string) (mcp.PromptResult, error) {
	return mcp.PromptResult{}, fmt.Errorf("fakeG11: GetPrompt not implemented")
}

// newManagerForG11 creates and starts a Manager with a single "s1" server.
func newManagerForG11(t *testing.T) *mcp.Manager {
	t.Helper()
	d := fakeDialerG11{}
	servers := map[string]contracts.MCPServer{
		"s1": {Type: "stdio", Command: "s1"},
	}
	m := mcp.NewManager(servers, d.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	return m
}

// TestQueryWiresMCPStatus_G11 verifies that mcp_status returns live statuses
// from the Manager (connected vs configured). SDK-32 live backend.
func TestQueryWiresMCPStatus_G11(t *testing.T) {
	mgr := newManagerForG11(t)
	defer mgr.Stop()

	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil, func(o *Options) {
		o.MCPManager = mgr
	})
	defer cancel()

	g1WaitReady(t, ready, done)
	g1SendRequest(inPW, "mcp_status", nil)
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("mcp_status response subtype = %v want success", response["subtype"])
	}
	inner, _ := response["response"].(map[string]any)
	servers, _ := inner["mcpServers"].([]any)
	if len(servers) == 0 {
		t.Fatalf("mcp_status mcpServers: want at least one entry, got %v", servers)
	}
	// Verify the first entry has status "connected" (live manager, not "configured").
	first, _ := servers[0].(map[string]any)
	if first["status"] != "connected" {
		t.Errorf("mcpServers[0].status = %v want connected", first["status"])
	}
}

// TestQueryWiresMCPSetServers_G11 verifies that mcp_set_servers reconfigures
// the live Manager. SDK-39 live backend.
func TestQueryWiresMCPSetServers_G11(t *testing.T) {
	mgr := newManagerForG11(t)
	defer mgr.Stop()

	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil, func(o *Options) {
		o.MCPManager = mgr
	})
	defer cancel()

	g1WaitReady(t, ready, done)
	g1SendRequest(inPW, "mcp_set_servers", map[string]any{
		"servers": map[string]any{
			"s2": map[string]any{"type": "stdio", "command": "s2"},
		},
	})
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("mcp_set_servers response subtype = %v want success", response["subtype"])
	}
	inner, _ := response["response"].(map[string]any)
	added, _ := inner["added"].([]any)
	removed, _ := inner["removed"].([]any)
	if len(added) == 0 {
		t.Errorf("mcp_set_servers added: want non-empty, got %v", added)
	}
	if len(removed) == 0 {
		t.Errorf("mcp_set_servers removed: want non-empty, got %v", removed)
	}
}

// TestQueryWiresMCPReconnect_G11 verifies that mcp_reconnect triggers a live
// reconnection via the Manager. SDK-41 live backend.
func TestQueryWiresMCPReconnect_G11(t *testing.T) {
	mgr := newManagerForG11(t)
	defer mgr.Stop()

	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil, func(o *Options) {
		o.MCPManager = mgr
	})
	defer cancel()

	g1WaitReady(t, ready, done)
	g1SendRequest(inPW, "mcp_reconnect", map[string]any{
		"serverName": "s1",
	})
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("mcp_reconnect response subtype = %v want success", response["subtype"])
	}
}

// TestQueryWiresMCPToggle_G11 verifies that mcp_toggle enables/disables via
// the Manager. SDK-42 live backend.
func TestQueryWiresMCPToggle_G11(t *testing.T) {
	mgr := newManagerForG11(t)
	defer mgr.Stop()

	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil, func(o *Options) {
		o.MCPManager = mgr
	})
	defer cancel()

	g1WaitReady(t, ready, done)
	g1SendRequest(inPW, "mcp_toggle", map[string]any{
		"serverName": "s1",
		"enabled":    false,
	})
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("mcp_toggle response subtype = %v want success", response["subtype"])
	}

	// Verify disable took effect in Manager.
	found := false
	for _, s := range mgr.Status() {
		if s.Name == "s1" {
			found = true
			if s.Status != "disabled" {
				t.Errorf("after mcp_toggle disable: want disabled, got %q", s.Status)
			}
		}
	}
	if !found {
		t.Error("server s1 not in Manager.Status() after toggle")
	}
}

// TestQueryWiresMCPMessage_G11 verifies that mcp_message routes to the server
// client. An unknown server should return an error, not "not supported".
// SDK-38 live backend.
func TestQueryWiresMCPMessage_G11(t *testing.T) {
	mgr := newManagerForG11(t)
	defer mgr.Stop()

	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil, func(o *Options) {
		o.MCPManager = mgr
	})
	defer cancel()

	g1WaitReady(t, ready, done)
	// Route a message to a non-existent server — expect an error response,
	// but crucially not the "not supported in this context" message.
	g1SendRequest(inPW, "mcp_message", map[string]any{
		"server_name": "unknown-server",
		"message":     map[string]any{"method": "ping"},
	})
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	// Expect "error" (server not found), NOT "not supported" (unregistered).
	if response["subtype"] != "error" {
		t.Errorf("mcp_message unknown server: response subtype = %v want error", response["subtype"])
	}
	// The error should mention the server name, not "callback not registered".
	errMsg, _ := response["error"].(string)
	if errMsg == "" {
		t.Error("mcp_message: want non-empty error message")
	}
	for _, bad := range []string{"callback not registered", "not supported in this context"} {
		if strings.Contains(errMsg, bad) {
			t.Errorf("mcp_message: got unregistered error %q, want real error", errMsg)
		}
	}
}
