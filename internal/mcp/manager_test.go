package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

// fakeDialer is a fake ClientOpenFunc for testing.
// It records dial calls and returns configurable results.
type fakeDialer struct {
	mu    sync.Mutex
	calls []string
	fails map[string]error // name → error to return on dial
	conns map[string]*fakeClient
}

func newFakeDialer() *fakeDialer {
	return &fakeDialer{
		fails: make(map[string]error),
		conns: make(map[string]*fakeClient),
	}
}

func (d *fakeDialer) openFunc() mcp.ClientOpenFunc {
	return func(ctx context.Context, name string, server contracts.MCPServer) (mcp.ClientHandle, error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.calls = append(d.calls, name)
		if err, ok := d.fails[name]; ok {
			return mcp.ClientHandle{}, err
		}
		fc := &fakeClient{name: name}
		d.conns[name] = fc
		return mcp.ClientHandle{Client: fc, Close: func() error { return nil }}, nil
	}
}

func (d *fakeDialer) dialCalls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.calls))
	copy(out, d.calls)
	return out
}

// fakeClient satisfies mcp.Client for testing.
type fakeClient struct {
	name string
}

func (f *fakeClient) ListTools(_ context.Context, _ string) ([]mcp.RemoteTool, error) {
	return nil, nil
}
func (f *fakeClient) CallTool(_ context.Context, _ string, _ string, _ json.RawMessage) (any, error) {
	return nil, fmt.Errorf("fake: CallTool not implemented")
}
func (f *fakeClient) ListResources(_ context.Context, _ string) ([]mcp.RemoteResource, error) {
	return nil, nil
}
func (f *fakeClient) ListResourceTemplates(_ context.Context, _ string) ([]mcp.RemoteResourceTemplate, error) {
	return nil, nil
}
func (f *fakeClient) ReadResource(_ context.Context, _ string, _ string) ([]mcp.ResourceContent, error) {
	return nil, fmt.Errorf("fake: ReadResource not implemented")
}
func (f *fakeClient) SubscribeResource(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("fake: SubscribeResource not implemented")
}
func (f *fakeClient) ListPrompts(_ context.Context, _ string) ([]mcp.RemotePrompt, error) {
	return nil, nil
}
func (f *fakeClient) GetPrompt(_ context.Context, _ string, _ string, _ map[string]string) (mcp.PromptResult, error) {
	return mcp.PromptResult{}, fmt.Errorf("fake: GetPrompt not implemented")
}

// ── Manager lifecycle ─────────────────────────────────────────────────────────

func TestManagerStartsWithConfiguredServers(t *testing.T) {
	dialer := newFakeDialer()
	servers := map[string]contracts.MCPServer{
		"alpha": {Type: "stdio", Command: "alpha"},
		"beta":  {Type: "stdio", Command: "beta"},
	}
	m := mcp.NewManager(servers, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	statuses := m.Status()
	if len(statuses) != 2 {
		t.Fatalf("want 2 statuses, got %d: %v", len(statuses), statuses)
	}
	for _, s := range statuses {
		if s.Status != "connected" {
			t.Errorf("server %q: want status=connected, got %q (error=%q)", s.Name, s.Status, s.Error)
		}
	}
}

func TestManagerStatusReturnsConnectedAndFailed(t *testing.T) {
	dialer := newFakeDialer()
	dialer.fails["bad"] = errors.New("dial refused")
	servers := map[string]contracts.MCPServer{
		"good": {Type: "stdio", Command: "good"},
		"bad":  {Type: "stdio", Command: "bad"},
	}
	m := mcp.NewManager(servers, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	statuses := m.Status()
	byName := make(map[string]mcp.ServerStatus)
	for _, s := range statuses {
		byName[s.Name] = s
	}
	if got := byName["good"].Status; got != "connected" {
		t.Errorf("good: want connected, got %q", got)
	}
	if got := byName["bad"].Status; got != "failed" {
		t.Errorf("bad: want failed, got %q", got)
	}
	if byName["bad"].Error == "" {
		t.Error("bad: want non-empty error message")
	}
}

func TestManagerStatusReturnsCopies(t *testing.T) {
	dialer := newFakeDialer()
	servers := map[string]contracts.MCPServer{
		"s": {Type: "stdio", Command: "s"},
	}
	m := mcp.NewManager(servers, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	s1 := m.Status()
	s2 := m.Status()
	// Mutate one slice — must not affect the other.
	if len(s1) > 0 {
		s1[0].Name = "mutated"
	}
	for _, s := range s2 {
		if s.Name == "mutated" {
			t.Error("Status() returned aliased slice; mutation propagated")
		}
	}
}

// ── Reconnect ─────────────────────────────────────────────────────────────────

func TestManagerReconnectRedialsFailedServer(t *testing.T) {
	dialer := newFakeDialer()
	dialer.fails["srv"] = errors.New("initial fail")
	servers := map[string]contracts.MCPServer{
		"srv": {Type: "stdio", Command: "srv"},
	}
	m := mcp.NewManager(servers, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	// Verify initial failure.
	for _, s := range m.Status() {
		if s.Name == "srv" && s.Status != "failed" {
			t.Fatalf("before reconnect: want failed, got %q", s.Status)
		}
	}

	// Remove the failure and reconnect.
	dialer.mu.Lock()
	delete(dialer.fails, "srv")
	dialer.mu.Unlock()

	if err := m.Reconnect(ctx, "srv"); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}

	for _, s := range m.Status() {
		if s.Name == "srv" {
			if s.Status != "connected" {
				t.Errorf("after reconnect: want connected, got %q (error=%q)", s.Status, s.Error)
			}
			return
		}
	}
	t.Error("server srv not found in status after reconnect")
}

func TestManagerReconnectUnknownServerErrors(t *testing.T) {
	m := mcp.NewManager(nil, newFakeDialer().openFunc())
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	err := m.Reconnect(ctx, "nonexistent")
	if err == nil {
		t.Error("Reconnect unknown server: want error, got nil")
	}
}

// ── Enable / Disable ──────────────────────────────────────────────────────────

func TestManagerDisableStopsServer(t *testing.T) {
	dialer := newFakeDialer()
	servers := map[string]contracts.MCPServer{
		"s": {Type: "stdio", Command: "s"},
	}
	m := mcp.NewManager(servers, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	if err := m.SetEnabled(ctx, "s", false); err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}

	for _, s := range m.Status() {
		if s.Name == "s" {
			if s.Status != "disabled" {
				t.Errorf("after disable: want disabled, got %q", s.Status)
			}
			return
		}
	}
	t.Error("server s not found after disable")
}

func TestManagerEnableRedialsServer(t *testing.T) {
	dialer := newFakeDialer()
	servers := map[string]contracts.MCPServer{
		"s": {Type: "stdio", Command: "s"},
	}
	m := mcp.NewManager(servers, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	// Disable then re-enable.
	if err := m.SetEnabled(ctx, "s", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := m.SetEnabled(ctx, "s", true); err != nil {
		t.Fatalf("enable: %v", err)
	}

	for _, s := range m.Status() {
		if s.Name == "s" {
			if s.Status != "connected" {
				t.Errorf("after enable: want connected, got %q (error=%q)", s.Status, s.Error)
			}
			return
		}
	}
	t.Error("server s not found after enable")
}

// ── SetServers ────────────────────────────────────────────────────────────────

func TestManagerSetServersAddsAndRemoves(t *testing.T) {
	dialer := newFakeDialer()
	servers := map[string]contracts.MCPServer{
		"old": {Type: "stdio", Command: "old"},
	}
	m := mcp.NewManager(servers, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	result, err := m.SetServers(ctx, map[string]contracts.MCPServer{
		"new": {Type: "stdio", Command: "new"},
	})
	if err != nil {
		t.Fatalf("SetServers: %v", err)
	}

	if len(result.Added) != 1 || result.Added[0] != "new" {
		t.Errorf("SetServers.Added: want [new], got %v", result.Added)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "old" {
		t.Errorf("SetServers.Removed: want [old], got %v", result.Removed)
	}

	statusMap := make(map[string]mcp.ServerStatus)
	for _, s := range m.Status() {
		statusMap[s.Name] = s
	}
	if _, exists := statusMap["old"]; exists {
		t.Error("old server still present after SetServers")
	}
	if s, exists := statusMap["new"]; !exists {
		t.Error("new server not present after SetServers")
	} else if s.Status != "connected" {
		t.Errorf("new server: want connected, got %q", s.Status)
	}
}

func TestManagerSetServersReportsErrors(t *testing.T) {
	dialer := newFakeDialer()
	dialer.fails["fail"] = errors.New("cannot connect")

	m := mcp.NewManager(nil, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	result, err := m.SetServers(ctx, map[string]contracts.MCPServer{
		"ok":   {Type: "stdio", Command: "ok"},
		"fail": {Type: "stdio", Command: "fail"},
	})
	if err != nil {
		t.Fatalf("SetServers: %v", err)
	}

	if _, exists := result.Errors["fail"]; !exists {
		t.Errorf("SetServers.Errors: want error for 'fail', got %v", result.Errors)
	}
}

// ── Client routing ────────────────────────────────────────────────────────────

func TestManagerClientForConnectedServer(t *testing.T) {
	dialer := newFakeDialer()
	servers := map[string]contracts.MCPServer{
		"s": {Type: "stdio", Command: "s"},
	}
	m := mcp.NewManager(servers, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	c, err := m.Client("s")
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if c == nil {
		t.Error("Client returned nil for connected server")
	}
}

func TestManagerClientForDisabledServerErrors(t *testing.T) {
	dialer := newFakeDialer()
	servers := map[string]contracts.MCPServer{
		"s": {Type: "stdio", Command: "s"},
	}
	m := mcp.NewManager(servers, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	_ = m.SetEnabled(ctx, "s", false)
	if _, err := m.Client("s"); err == nil {
		t.Error("Client disabled server: want error, got nil")
	}
}

// ── Concurrency safety (race detector) ───────────────────────────────────────

func TestManagerConcurrentStatusAndReconnect(t *testing.T) {
	dialer := newFakeDialer()
	servers := map[string]contracts.MCPServer{
		"s": {Type: "stdio", Command: "s"},
	}
	m := mcp.NewManager(servers, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = m.Status()
				_ = m.Reconnect(ctx, "s")
			}
		}()
	}
	wg.Wait()
}

func TestManagerConcurrentSetServers(t *testing.T) {
	dialer := newFakeDialer()
	m := mcp.NewManager(nil, dialer.openFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		name := fmt.Sprintf("s%d", i)
		go func(n string) {
			defer wg.Done()
			_, _ = m.SetServers(ctx, map[string]contracts.MCPServer{
				n: {Type: "stdio", Command: n},
			})
		}(name)
	}
	wg.Wait()
}
