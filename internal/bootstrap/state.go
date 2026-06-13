package bootstrap

import (
	"os"
	"path/filepath"
	"sync"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
)

type State struct {
	mu          sync.RWMutex
	sessionID   contracts.ID
	originalCWD string
	cwd         string
	features    map[string]bool
	clientType  string
}

func New() (*State, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(cwd)
	if err == nil {
		cwd = resolved
	}
	return &State{
		sessionID:   contracts.NewID(),
		originalCWD: cwd,
		cwd:         cwd,
		features:    map[string]bool{},
		clientType:  "cli",
	}, nil
}

func (s *State) SessionID() contracts.ID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

func (s *State) CWD() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cwd
}

func (s *State) OriginalCWD() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.originalCWD
}

func (s *State) SetCWD(cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cwd = cwd
}

func (s *State) SetFeature(name string, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.features[name] = enabled
}

func (s *State) Feature(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.features[name]
}

func (s *State) ClientType() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clientType
}

func (s *State) ConversationRunner() (conversation.Runner, error) {
	s.mu.RLock()
	sessionID := s.sessionID
	cwd := s.cwd
	s.mu.RUnlock()

	mcpConfig, err := conversation.LoadMCPConfigFromSettingsFiles(cwd)
	if err != nil {
		return conversation.Runner{}, err
	}
	return conversation.Runner{
		SessionID:        sessionID,
		WorkingDirectory: cwd,
		MCP:              mcpConfig,
	}, nil
}
