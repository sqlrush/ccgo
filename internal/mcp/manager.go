package mcp

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"ccgo/internal/contracts"
)

// ServerStatusValue enumerates the live connection states a Manager tracks.
const (
	ServerStatusConnected  = "connected"
	ServerStatusConnecting = "connecting"
	ServerStatusFailed     = "failed"
	ServerStatusDisabled   = "disabled"
)

// ServerStatus is a snapshot of one MCP server's live connection state.
// All fields are value types so callers receive an immutable copy.
type ServerStatus struct {
	Name   string
	Status string // connected | connecting | failed | disabled
	Error  string // non-empty when Status == "failed"
	Scope  string // from contracts.MCPServer.Scope
}

// SetServersResult reports the outcome of a Manager.SetServers call.
type SetServersResult struct {
	Added   []string
	Removed []string
	Errors  map[string]string
}

// serverEntry holds the live state for one configured MCP server.
type serverEntry struct {
	config contracts.MCPServer
	client Client
	close  func() error
	status string
	errMsg string
}

// Manager holds the set of configured+connected MCP clients, tracks per-server
// status (connected/connecting/failed/disabled), and supports reconnect,
// enable/disable, and set-servers (reconfigure at runtime).
// All exported methods are safe for concurrent use.
type Manager struct {
	mu      sync.Mutex
	servers map[string]*serverEntry
	open    ClientOpenFunc
}

// NewManager creates a new Manager with the given server configurations and a
// ClientOpenFunc used to dial each server. Pass nil for servers to start empty.
func NewManager(servers map[string]contracts.MCPServer, open ClientOpenFunc) *Manager {
	m := &Manager{
		servers: make(map[string]*serverEntry, len(servers)),
		open:    open,
	}
	for name, cfg := range servers {
		m.servers[name] = &serverEntry{
			config: cfg,
			status: ServerStatusConnecting,
		}
	}
	return m
}

// Start dials all configured servers concurrently. Dial errors set the server
// status to "failed" but do not cause Start to return an error — the caller
// should inspect Status() for per-server outcomes.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			m.dial(ctx, n)
		}(name)
	}
	wg.Wait()
	return nil
}

// Stop closes all live connections. It is safe to call multiple times.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, entry := range m.servers {
		if entry.close != nil {
			_ = entry.close()
			entry.close = nil
			entry.client = nil
			m.servers[name] = entry
		}
	}
}

// Status returns an immutable snapshot of all servers' statuses.
// The returned slice is a fresh copy; callers may modify it safely.
func (m *Manager) Status() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ServerStatus, 0, len(names))
	for _, name := range names {
		e := m.servers[name]
		out = append(out, ServerStatus{
			Name:   name,
			Status: e.status,
			Error:  e.errMsg,
			Scope:  e.config.Scope,
		})
	}
	return out
}

// Reconnect closes the existing connection (if any) and re-dials the named
// server. Returns an error if the server is not known to the manager.
func (m *Manager) Reconnect(ctx context.Context, name string) error {
	m.mu.Lock()
	entry, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("mcp manager: server %q not found", name)
	}
	// Close existing connection.
	if entry.close != nil {
		_ = entry.close()
		entry.close = nil
		entry.client = nil
	}
	// Mark as connecting so concurrent Status() reads reflect it.
	entry.status = ServerStatusConnecting
	entry.errMsg = ""
	m.mu.Unlock()

	m.dial(ctx, name)
	return nil
}

// SetEnabled enables or disables the named server. Disabling closes the live
// connection; enabling re-dials it. Returns an error if the server is not known.
func (m *Manager) SetEnabled(ctx context.Context, name string, enabled bool) error {
	m.mu.Lock()
	entry, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("mcp manager: server %q not found", name)
	}
	if !enabled {
		// Disable: close and mark.
		if entry.close != nil {
			_ = entry.close()
			entry.close = nil
			entry.client = nil
		}
		entry.status = ServerStatusDisabled
		entry.errMsg = ""
		m.mu.Unlock()
		return nil
	}
	// Enable: clear disabled state and re-dial.
	entry.status = ServerStatusConnecting
	entry.errMsg = ""
	m.mu.Unlock()

	m.dial(ctx, name)
	return nil
}

// SetServers replaces the full set of configured servers at runtime.
// Servers no longer in the new map are closed; new servers are dialed.
// Servers present in both are reconnected with the new config.
// Returns a SetServersResult describing what changed.
func (m *Manager) SetServers(ctx context.Context, newServers map[string]contracts.MCPServer) (SetServersResult, error) {
	m.mu.Lock()

	oldNames := make(map[string]struct{}, len(m.servers))
	for name := range m.servers {
		oldNames[name] = struct{}{}
	}
	newNames := make(map[string]struct{}, len(newServers))
	for name := range newServers {
		newNames[name] = struct{}{}
	}

	var removed []string
	for name, entry := range m.servers {
		if _, keep := newNames[name]; !keep {
			removed = append(removed, name)
			if entry.close != nil {
				_ = entry.close()
			}
			delete(m.servers, name)
		}
	}

	var added []string
	for name := range newNames {
		if _, existed := oldNames[name]; !existed {
			added = append(added, name)
		}
		// Add or replace the entry (reconnect if already existed).
		if existing, ok := m.servers[name]; ok && existing.close != nil {
			_ = existing.close()
		}
		m.servers[name] = &serverEntry{
			config: newServers[name],
			status: ServerStatusConnecting,
		}
	}
	sort.Strings(removed)
	sort.Strings(added)

	m.mu.Unlock()

	// Dial all new/replaced servers concurrently.
	var wg sync.WaitGroup
	for _, name := range func() []string {
		m.mu.Lock()
		names := make([]string, 0, len(m.servers))
		for n := range m.servers {
			names = append(names, n)
		}
		m.mu.Unlock()
		return names
	}() {
		if _, isNew := newNames[name]; isNew {
			wg.Add(1)
			go func(n string) {
				defer wg.Done()
				m.dial(ctx, n)
			}(name)
		}
	}
	wg.Wait()

	// Collect dial errors for newly added servers.
	errMap := make(map[string]string)
	m.mu.Lock()
	for _, name := range added {
		entry, ok := m.servers[name]
		if ok && entry.status == ServerStatusFailed {
			errMap[name] = entry.errMsg
		}
	}
	m.mu.Unlock()

	return SetServersResult{
		Added:   added,
		Removed: removed,
		Errors:  errMap,
	}, nil
}

// Client returns the live mcp.Client for the named server.
// Returns an error if the server is unknown, disabled, or in a failed state.
func (m *Manager) Client(name string) (Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.servers[name]
	if !ok {
		return nil, fmt.Errorf("mcp manager: server %q not found", name)
	}
	if entry.status == ServerStatusDisabled {
		return nil, fmt.Errorf("mcp manager: server %q is disabled", name)
	}
	if entry.status == ServerStatusFailed {
		return nil, fmt.Errorf("mcp manager: server %q failed to connect: %s", name, entry.errMsg)
	}
	if entry.client == nil {
		return nil, fmt.Errorf("mcp manager: server %q is not yet connected", name)
	}
	return entry.client, nil
}

// dial opens a connection to the named server and updates its status.
// It is safe to call from multiple goroutines.
func (m *Manager) dial(ctx context.Context, name string) {
	m.mu.Lock()
	entry, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return
	}
	cfg := entry.config
	open := m.open
	m.mu.Unlock()

	handle, err := open(ctx, name, cfg)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Re-fetch entry in case concurrent operations modified it.
	entry, ok = m.servers[name]
	if !ok {
		// Server was removed between dial start and completion; close the handle.
		if err == nil && handle.Close != nil {
			_ = handle.Close()
		}
		return
	}

	if err != nil {
		entry.status = ServerStatusFailed
		entry.errMsg = err.Error()
		entry.client = nil
		entry.close = nil
	} else {
		entry.status = ServerStatusConnected
		entry.errMsg = ""
		entry.client = handle.Client
		entry.close = handle.Close
	}
}
