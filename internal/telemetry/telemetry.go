package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

type Event struct {
	Timestamp      string       `json:"timestamp"`
	SessionID      contracts.ID `json:"session_id,omitempty"`
	Type           string       `json:"type"`
	Model          string       `json:"model,omitempty"`
	MessageType    string       `json:"message_type,omitempty"`
	MessageUUID    contracts.ID `json:"message_uuid,omitempty"`
	ToolUseID      contracts.ID `json:"tool_use_id,omitempty"`
	ToolName       string       `json:"tool_name,omitempty"`
	ToolResultErr  bool         `json:"tool_result_error,omitempty"`
	ProgressType   string       `json:"progress_type,omitempty"`
	ProgressKeys   []string     `json:"progress_keys,omitempty"`
	TokenState     string       `json:"token_state,omitempty"`
	TokenUsage     int          `json:"token_usage,omitempty"`
	CompactTrigger string       `json:"compact_trigger,omitempty"`
	Error          string       `json:"error,omitempty"`
}

func SessionPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), "telemetry.jsonl")
}

func Append(path string, event Event) error {
	if path == "" {
		return os.ErrInvalid
	}
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := platform.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func SortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
