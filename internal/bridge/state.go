package bridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const directStateFileName = "bridge-direct.json"

const (
	DirectRuntimeDisabled = "disabled"
	DirectRuntimeRunning  = "running"
	DirectRuntimeFailed   = "failed"
)

type DirectState struct {
	SessionID        contracts.ID `json:"session_id,omitempty"`
	WorkingDirectory string       `json:"working_directory,omitempty"`
	GeneratedAt      string       `json:"generated_at"`
	RuntimeState     string       `json:"runtime_state"`
	URL              string       `json:"url,omitempty"`
	WebSocketURL     string       `json:"websocket_url,omitempty"`
	TokenRequired    bool         `json:"token_required,omitempty"`
	Commands         int          `json:"commands"`
	Error            string       `json:"error,omitempty"`
}

func SessionDirectStatePath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), directStateFileName)
}

func BuildDirectState(sessionID contracts.ID, cwd string, manifest Manifest, server *DirectServer, token string, runtimeState string, err error) DirectState {
	state := DirectState{
		SessionID:        sessionID,
		WorkingDirectory: cwd,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		RuntimeState:     strings.TrimSpace(runtimeState),
		TokenRequired:    strings.TrimSpace(token) != "",
		Commands:         len(manifest.Commands),
	}
	if state.RuntimeState == "" {
		state.RuntimeState = DirectRuntimeDisabled
	}
	if server != nil && strings.TrimSpace(server.URL()) != "" {
		state.URL = server.URL()
		state.WebSocketURL = directWebSocketURL(server.URL())
	}
	if err != nil {
		state.Error = err.Error()
		if state.RuntimeState == DirectRuntimeDisabled {
			state.RuntimeState = DirectRuntimeFailed
		}
	}
	return state
}

func WriteDirectState(path string, state DirectState) error {
	if path == "" {
		return os.ErrInvalid
	}
	if state.GeneratedAt == "" {
		state.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadDirectState(path string) (DirectState, error) {
	if path == "" {
		return DirectState{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DirectState{}, nil
	}
	if err != nil {
		return DirectState{}, err
	}
	var state DirectState
	if err := json.Unmarshal(data, &state); err != nil {
		return DirectState{}, err
	}
	return state, nil
}

func directWebSocketURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http://") {
		return "ws://" + strings.TrimPrefix(raw, "http://") + "/ws"
	}
	if strings.HasPrefix(raw, "https://") {
		return "wss://" + strings.TrimPrefix(raw, "https://") + "/ws"
	}
	if raw == "" {
		return ""
	}
	return raw + "/ws"
}
