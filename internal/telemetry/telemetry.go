package telemetry

import (
	"bufio"
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

type Filter struct {
	Type  string
	Model string
	Limit int
}

type Summary struct {
	Total         int            `json:"total"`
	ByType        map[string]int `json:"by_type,omitempty"`
	ByModel       map[string]int `json:"by_model,omitempty"`
	ToolEvents    int            `json:"tool_events,omitempty"`
	ToolErrors    int            `json:"tool_errors,omitempty"`
	ErrorEvents   int            `json:"error_events,omitempty"`
	Compactions   int            `json:"compactions,omitempty"`
	TokenWarnings int            `json:"token_warnings,omitempty"`
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

func Load(path string) ([]Event, error) {
	if path == "" {
		return nil, os.ErrInvalid
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var events []Event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func FilterEvents(events []Event, filter Filter) []Event {
	eventType := strings.TrimSpace(filter.Type)
	model := strings.TrimSpace(filter.Model)
	out := make([]Event, 0, len(events))
	for _, event := range events {
		if eventType != "" && event.Type != eventType {
			continue
		}
		if model != "" && event.Model != model {
			continue
		}
		out = append(out, event)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out
}

func Summarize(events []Event) Summary {
	summary := Summary{
		Total:   len(events),
		ByType:  map[string]int{},
		ByModel: map[string]int{},
	}
	for _, event := range events {
		if event.Type != "" {
			summary.ByType[event.Type]++
		}
		if event.Model != "" {
			summary.ByModel[event.Model]++
		}
		if event.ToolUseID != "" {
			summary.ToolEvents++
		}
		if event.ToolResultErr {
			summary.ToolErrors++
		}
		if event.Error != "" {
			summary.ErrorEvents++
		}
		if event.CompactTrigger != "" {
			summary.Compactions++
		}
		if event.TokenState != "" {
			summary.TokenWarnings++
		}
	}
	if len(summary.ByType) == 0 {
		summary.ByType = nil
	}
	if len(summary.ByModel) == 0 {
		summary.ByModel = nil
	}
	return summary
}

func SortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
