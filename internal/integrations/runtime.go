package integrations

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

type RuntimeState struct {
	SessionID        contracts.ID      `json:"session_id,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	GeneratedAt      string            `json:"generated_at"`
	Name             string            `json:"name"`
	Enabled          bool              `json:"enabled"`
	RuntimeState     string            `json:"runtime_state"`
	Detail           string            `json:"detail,omitempty"`
	Adapters         []Adapter         `json:"adapters,omitempty"`
	Artifacts        map[string]string `json:"artifacts,omitempty"`
}

func SessionRuntimeStatePath(sessionPath string, sessionID contracts.ID, name string) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	name = normalizeRuntimeName(name)
	if name == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), "integration-"+name+".json")
}

func BuildRuntimeState(sessionPath string, sessionID contracts.ID, cwd string, integration Integration) RuntimeState {
	name := normalizeRuntimeName(integration.Name)
	state := strings.TrimSpace(integration.RuntimeState)
	if state == "" {
		state = RuntimeStateDisabled
	}
	runtime := RuntimeState{
		SessionID:        sessionID,
		WorkingDirectory: cwd,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		Name:             name,
		Enabled:          integration.Enabled,
		RuntimeState:     state,
		Detail:           integration.Detail,
		Adapters:         append([]Adapter(nil), integration.Adapters...),
	}
	artifactPath := SessionRuntimeStatePath(sessionPath, sessionID, name)
	if artifactPath != "" {
		runtime.Artifacts = map[string]string{"state": artifactPath}
	}
	return runtime
}

func WriteRuntimeState(path string, state RuntimeState) error {
	if path == "" {
		return os.ErrInvalid
	}
	if state.GeneratedAt == "" {
		state.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	state.Name = normalizeRuntimeName(state.Name)
	if state.RuntimeState == "" {
		state.RuntimeState = RuntimeStateDisabled
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadRuntimeState(path string) (RuntimeState, error) {
	if path == "" {
		return RuntimeState{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RuntimeState{}, nil
	}
	if err != nil {
		return RuntimeState{}, err
	}
	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return RuntimeState{}, err
	}
	state.Name = normalizeRuntimeName(state.Name)
	return state, nil
}

func normalizeRuntimeName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, "-", "_")
	switch name {
	case "chrome", "voice", "computer_use":
		return name
	case "computeruse", "computer":
		return "computer_use"
	default:
		return ""
	}
}
