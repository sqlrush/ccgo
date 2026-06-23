package conversation

// G11 tests: verify that Runner.formatMCPCommandSummary uses live Manager
// status when r.MCPManager is set (MCP-53 live backend, §09/§16 parity).

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

// fakeDialerG11Runner is a minimal ClientOpenFunc that always connects.
type fakeDialerG11Runner struct{}

func (fakeDialerG11Runner) openFunc() mcp.ClientOpenFunc {
	return func(_ context.Context, name string, _ contracts.MCPServer) (mcp.ClientHandle, error) {
		return mcp.ClientHandle{
			Client: &g11RunnerFakeClient{name: name},
			Close:  func() error { return nil },
		}, nil
	}
}

type g11RunnerFakeClient struct{ name string }

func (f *g11RunnerFakeClient) ListTools(_ context.Context, _ string) ([]mcp.RemoteTool, error) {
	return nil, nil
}
func (f *g11RunnerFakeClient) CallTool(_ context.Context, _ string, _ string, _ json.RawMessage) (any, error) {
	return nil, fmt.Errorf("fake: CallTool not implemented")
}
func (f *g11RunnerFakeClient) ListResources(_ context.Context, _ string) ([]mcp.RemoteResource, error) {
	return nil, nil
}
func (f *g11RunnerFakeClient) ListResourceTemplates(_ context.Context, _ string) ([]mcp.RemoteResourceTemplate, error) {
	return nil, nil
}
func (f *g11RunnerFakeClient) ReadResource(_ context.Context, _ string, _ string) ([]mcp.ResourceContent, error) {
	return nil, fmt.Errorf("fake: ReadResource not implemented")
}
func (f *g11RunnerFakeClient) SubscribeResource(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("fake: SubscribeResource not implemented")
}
func (f *g11RunnerFakeClient) ListPrompts(_ context.Context, _ string) ([]mcp.RemotePrompt, error) {
	return nil, nil
}
func (f *g11RunnerFakeClient) GetPrompt(_ context.Context, _ string, _ string, _ map[string]string) (mcp.PromptResult, error) {
	return mcp.PromptResult{}, fmt.Errorf("fake: GetPrompt not implemented")
}

// newRunnerManagerForG11 creates and starts a Manager with the given servers.
func newRunnerManagerForG11(t *testing.T, servers map[string]contracts.MCPServer) *mcp.Manager {
	t.Helper()
	d := fakeDialerG11Runner{}
	m := mcp.NewManager(servers, d.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	return m
}

// TestFormatMCPCommandSummaryWithManagerShowsLiveStatus verifies that when a
// live Manager is set on the runner, /mcp status reflects live connection state.
// G11 / MCP-53 live backend.
func TestFormatMCPCommandSummaryWithManagerShowsLiveStatus(t *testing.T) {
	mgr := newRunnerManagerForG11(t, map[string]contracts.MCPServer{
		"alpha": {Type: "stdio", Command: "alpha"},
	})
	defer mgr.Stop()

	r := Runner{
		MCPManager: mgr,
		MCP: &MCPConfig{
			UserSettings: contracts.Settings{
				MCPServers: map[string]contracts.MCPServer{
					"alpha": {Type: "stdio", Command: "alpha"},
				},
			},
		},
	}

	out := r.formatMCPCommandSummary("")
	if !strings.Contains(out, "alpha") {
		t.Errorf("output missing 'alpha': %q", out)
	}
	if !strings.Contains(out, "connected") {
		t.Errorf("output missing 'connected' status: %q", out)
	}
}

// TestFormatMCPCommandSummaryWithManagerShowsFailedStatus verifies that failed
// connections are shown in the live status panel.
func TestFormatMCPCommandSummaryWithManagerShowsFailedStatus(t *testing.T) {
	failDialer := func(_ context.Context, name string, _ contracts.MCPServer) (mcp.ClientHandle, error) {
		return mcp.ClientHandle{}, fmt.Errorf("simulated dial failure for %s", name)
	}
	servers := map[string]contracts.MCPServer{
		"broken": {Type: "http", URL: "http://broken.example"},
	}
	mgr := mcp.NewManager(servers, failDialer)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	defer mgr.Stop()

	r := Runner{
		MCPManager: mgr,
		MCP: &MCPConfig{
			UserSettings: contracts.Settings{
				MCPServers: map[string]contracts.MCPServer{
					"broken": {Type: "http", URL: "http://broken.example"},
				},
			},
		},
	}

	out := r.formatMCPCommandSummary("")
	if !strings.Contains(out, "broken") {
		t.Errorf("output missing 'broken': %q", out)
	}
	if !strings.Contains(out, "failed") {
		t.Errorf("output missing 'failed' status: %q", out)
	}
}

// TestFormatMCPCommandSummaryWithoutManagerUsesStaticConfig verifies backward
// compatibility when no Manager is set.
func TestFormatMCPCommandSummaryWithoutManagerUsesStaticConfig(t *testing.T) {
	r := Runner{
		MCP: &MCPConfig{
			UserSettings: contracts.Settings{
				MCPServers: map[string]contracts.MCPServer{
					"static": {Type: "stdio", Command: "static-cmd"},
				},
			},
		},
	}

	out := r.formatMCPCommandSummary("")
	if !strings.Contains(out, "static") {
		t.Errorf("output missing 'static': %q", out)
	}
	// Without manager, should not show connection status.
	if strings.Contains(out, "connected") || strings.Contains(out, "failed") {
		t.Errorf("output should not contain live status without manager: %q", out)
	}
}

// TestRunnerMCPManagerStatusReturnsLiveData verifies that Runner.MCPManager.Status()
// returns proper data after Start.
func TestRunnerMCPManagerStatusReturnsLiveData(t *testing.T) {
	mgr := newRunnerManagerForG11(t, map[string]contracts.MCPServer{
		"s1": {Type: "stdio", Command: "s1"},
		"s2": {Type: "stdio", Command: "s2"},
	})
	defer mgr.Stop()

	r := Runner{MCPManager: mgr}
	statuses := r.MCPManager.Status()
	if len(statuses) != 2 {
		t.Fatalf("want 2 statuses, got %d: %v", len(statuses), statuses)
	}
	for _, s := range statuses {
		if s.Status != "connected" {
			t.Errorf("server %q: want connected, got %q", s.Name, s.Status)
		}
	}
}
