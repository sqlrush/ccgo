package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// SessionNavigationManager lazily starts ConcreteNavigationClient connections
// keyed by ServerDefinition name. It implements NavigationClient by routing
// each request to the server whose FileExtensions match the requested filePath.
//
// The manager is safe for concurrent use.
type SessionNavigationManager struct {
	definitions   []ServerDefinition
	workspaceRoot string

	mu      sync.Mutex
	clients map[string]*ConcreteNavigationClient // keyed by server name
}

// NewSessionNavigationManager creates a manager that will lazily start LSP
// servers from the provided definitions when tools first request a navigation
// operation on a matching file extension.
//
// workspaceRoot is forwarded to each ConcreteNavigationClient as the LSP
// rootUri / rootPath during the initialize handshake.
func NewSessionNavigationManager(definitions []ServerDefinition, workspaceRoot string) *SessionNavigationManager {
	defs := make([]ServerDefinition, len(definitions))
	for i, d := range definitions {
		defs[i] = normalizeServerDefinition(d)
	}
	return &SessionNavigationManager{
		definitions:   defs,
		workspaceRoot: workspaceRoot,
		clients:       map[string]*ConcreteNavigationClient{},
	}
}

// SendRequest routes the request to the language server whose FileExtensions
// match the extension of filePath, lazily starting the server if needed.
//
// Returns an error wrapping "no LSP server" when no definition matches the
// file extension or the matching server cannot be started.
func (m *SessionNavigationManager) SendRequest(ctx context.Context, filePath, method string, params any) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	def, ok := m.definitionForFile(filePath)
	if !ok {
		return nil, fmt.Errorf("no LSP server configured for file %q", filePath)
	}
	client, err := m.clientForServer(ctx, def)
	if err != nil {
		return nil, fmt.Errorf("lsp session manager: start server %q: %w", def.Name, err)
	}
	return client.SendRequest(ctx, filePath, method, params)
}

// Close shuts down all active client connections and releases resources.
func (m *SessionNavigationManager) Close() error {
	m.mu.Lock()
	clients := m.clients
	m.clients = map[string]*ConcreteNavigationClient{}
	m.mu.Unlock()

	var firstErr error
	for _, c := range clients {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// definitionForFile returns the first ServerDefinition whose FileExtensions
// contains the extension of filePath (case-insensitive, leading dot normalised).
func (m *SessionNavigationManager) definitionForFile(filePath string) (ServerDefinition, bool) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return ServerDefinition{}, false
	}
	for _, def := range m.definitions {
		for _, fe := range def.FileExtensions {
			fe = strings.ToLower(strings.TrimSpace(fe))
			if !strings.HasPrefix(fe, ".") {
				fe = "." + fe
			}
			if fe == ext {
				return def, true
			}
		}
	}
	return ServerDefinition{}, false
}

// clientForServer returns (or lazily creates) the ConcreteNavigationClient for
// the given server definition.
func (m *SessionNavigationManager) clientForServer(ctx context.Context, def ServerDefinition) (*ConcreteNavigationClient, error) {
	m.mu.Lock()
	c, exists := m.clients[def.Name]
	m.mu.Unlock()
	if exists {
		return c, nil
	}

	// Spawn the server process and create a bidirectional client.
	cmd := exec.CommandContext(ctx, def.Command, def.Args...) //nolint:gosec
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	rw := readWriteCloser{ReadCloser: stdout, WriteCloser: stdin}
	client, err := NewConcreteNavigationClient(ctx, ConcreteNavigationClientOptions{
		ReadWriter:    rw,
		WorkspaceRoot: m.workspaceRoot,
		ClientName:    "ccgo",
	})
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("initialize: %w", err)
	}

	m.mu.Lock()
	// Double-checked: another goroutine may have won the race.
	if existing, exists := m.clients[def.Name]; exists {
		m.mu.Unlock()
		_ = client.Close()
		return existing, nil
	}
	m.clients[def.Name] = client
	m.mu.Unlock()
	return client, nil
}
