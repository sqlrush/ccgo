package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const stateFileName = "daemon-state.json"

const (
	RuntimeDisabled = "disabled"
	RuntimeRunning  = "running"
	RuntimeFailed   = "failed"
	RuntimeStale    = "stale"
)

type State struct {
	SessionID        contracts.ID `json:"session_id,omitempty"`
	WorkingDirectory string       `json:"working_directory,omitempty"`
	GeneratedAt      string       `json:"generated_at"`
	RuntimeState     string       `json:"runtime_state"`
	PID              int          `json:"pid,omitempty"`
	Endpoint         string       `json:"endpoint,omitempty"`
	StartedAt        string       `json:"started_at,omitempty"`
	HeartbeatAt      string       `json:"heartbeat_at,omitempty"`
	Error            string       `json:"error,omitempty"`
}

type DiscoveredState struct {
	Path         string
	State        State
	RuntimeState string
	GeneratedAt  time.Time
}

func SessionStatePath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), stateFileName)
}

func DiscoverStates(projectDir string, now time.Time, staleAfter time.Duration) ([]DiscoveredState, error) {
	if projectDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(projectDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	discovered := make([]DiscoveredState, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(projectDir, entry.Name(), stateFileName)
		state, err := LoadState(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if state.GeneratedAt == "" {
			continue
		}
		generatedAt, _ := time.Parse(time.RFC3339Nano, state.GeneratedAt)
		if generatedAt.IsZero() {
			if info, err := os.Stat(path); err == nil {
				generatedAt = info.ModTime()
			}
		}
		discovered = append(discovered, DiscoveredState{
			Path:         path,
			State:        state,
			RuntimeState: RuntimeStateAt(state, now, staleAfter),
			GeneratedAt:  generatedAt,
		})
	}
	sort.Slice(discovered, func(i, j int) bool {
		leftRank := runtimeStateRank(discovered[i].RuntimeState)
		rightRank := runtimeStateRank(discovered[j].RuntimeState)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if !discovered[i].GeneratedAt.Equal(discovered[j].GeneratedAt) {
			return discovered[i].GeneratedAt.After(discovered[j].GeneratedAt)
		}
		return discovered[i].Path < discovered[j].Path
	})
	return discovered, nil
}

func LatestStatePath(projectDir string, now time.Time, staleAfter time.Duration) (string, error) {
	discovered, err := DiscoverStates(projectDir, now, staleAfter)
	if err != nil || len(discovered) == 0 {
		return "", err
	}
	return discovered[0].Path, nil
}

func BuildState(sessionID contracts.ID, cwd string, runtimeState string, pid int, endpoint string, now time.Time, err error) State {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nowText := now.UTC().Format(time.RFC3339Nano)
	state := State{
		SessionID:        sessionID,
		WorkingDirectory: cwd,
		GeneratedAt:      nowText,
		RuntimeState:     strings.TrimSpace(runtimeState),
		PID:              pid,
		Endpoint:         strings.TrimSpace(endpoint),
	}
	if state.RuntimeState == "" {
		state.RuntimeState = RuntimeDisabled
	}
	if state.RuntimeState == RuntimeRunning {
		state.HeartbeatAt = nowText
		if state.StartedAt == "" {
			state.StartedAt = nowText
		}
	}
	if err != nil {
		state.Error = err.Error()
		if state.RuntimeState == RuntimeDisabled {
			state.RuntimeState = RuntimeFailed
		}
	}
	return state
}

func WriteState(path string, state State) error {
	if path == "" {
		return os.ErrInvalid
	}
	if state.GeneratedAt == "" {
		state.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if state.RuntimeState == "" {
		state.RuntimeState = RuntimeDisabled
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadState(path string) (State, error) {
	if path == "" {
		return State{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func RuntimeStateAt(state State, now time.Time, staleAfter time.Duration) string {
	runtimeState := strings.TrimSpace(state.RuntimeState)
	if runtimeState == "" {
		return RuntimeDisabled
	}
	if runtimeState != RuntimeRunning || staleAfter <= 0 {
		return runtimeState
	}
	heartbeatAt := strings.TrimSpace(state.HeartbeatAt)
	if heartbeatAt == "" {
		return RuntimeStale
	}
	heartbeat, err := time.Parse(time.RFC3339Nano, heartbeatAt)
	if err != nil {
		return RuntimeStale
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.UTC().Sub(heartbeat) > staleAfter {
		return RuntimeStale
	}
	return RuntimeRunning
}

func runtimeStateRank(runtimeState string) int {
	switch strings.TrimSpace(runtimeState) {
	case RuntimeRunning:
		return 0
	case RuntimeStale:
		return 1
	case RuntimeFailed:
		return 2
	case RuntimeDisabled:
		return 3
	default:
		return 4
	}
}
