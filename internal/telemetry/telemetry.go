package telemetry

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
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
	TraceID        string       `json:"trace_id,omitempty"`
	SpanID         string       `json:"span_id,omitempty"`
	ParentSpanID   string       `json:"parent_span_id,omitempty"`
	Type           string       `json:"type"`
	Model          string       `json:"model,omitempty"`
	MessageType    string       `json:"message_type,omitempty"`
	MessageUUID    contracts.ID `json:"message_uuid,omitempty"`
	ToolUseID      contracts.ID `json:"tool_use_id,omitempty"`
	ToolName       string       `json:"tool_name,omitempty"`
	ToolResultErr  bool         `json:"tool_result_error,omitempty"`
	ProgressType   string       `json:"progress_type,omitempty"`
	ProgressKeys   []string     `json:"progress_keys,omitempty"`
	RetryAttempt   int          `json:"retry_attempt,omitempty"`
	RetryMax       int          `json:"retry_max_attempts,omitempty"`
	RetryFailed    string       `json:"retry_failed_model,omitempty"`
	RetryNext      string       `json:"retry_next_model,omitempty"`
	RetryFallback  bool         `json:"retry_fallback,omitempty"`
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
	Traces        int            `json:"traces,omitempty"`
	Spans         int            `json:"spans,omitempty"`
	ToolEvents    int            `json:"tool_events,omitempty"`
	ToolErrors    int            `json:"tool_errors,omitempty"`
	ErrorEvents   int            `json:"error_events,omitempty"`
	Compactions   int            `json:"compactions,omitempty"`
	TokenWarnings int            `json:"token_warnings,omitempty"`
}

type Export struct {
	GeneratedAt string  `json:"generated_at"`
	SourcePath  string  `json:"source_path,omitempty"`
	EventCount  int     `json:"event_count"`
	Summary     Summary `json:"summary"`
	Events      []Event `json:"events,omitempty"`
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
	event = PrepareEvent(event)
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

func ExportSummary(path string, outPath string, filter Filter) (Export, error) {
	if path == "" || outPath == "" {
		return Export{}, os.ErrInvalid
	}
	events, err := Load(path)
	if err != nil {
		return Export{}, err
	}
	events = FilterEvents(events, filter)
	export := Export{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		SourcePath:  path,
		EventCount:  len(events),
		Summary:     Summarize(events),
		Events:      events,
	}
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return Export{}, err
	}
	data = append(data, '\n')
	if err := platform.AtomicWriteFile(outPath, data, 0o644); err != nil {
		return Export{}, err
	}
	return export, nil
}

func PrepareEvent(event Event) Event {
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if event.TraceID == "" {
		event.TraceID = TraceID(event.SessionID)
	}
	if event.SpanID == "" {
		event.SpanID = SpanID(event)
	}
	return event
}

func TraceID(sessionID contracts.ID) string {
	session := strings.TrimSpace(string(sessionID))
	if session == "" {
		return ""
	}
	return traceHex("trace:"+session, 16)
}

func SpanID(event Event) string {
	parts := []string{
		"span",
		event.Timestamp,
		string(event.SessionID),
		event.Type,
		event.Model,
		event.MessageType,
		string(event.MessageUUID),
		string(event.ToolUseID),
		event.ToolName,
		event.ProgressType,
		event.TokenState,
		event.CompactTrigger,
		event.Error,
	}
	return traceHex(strings.Join(parts, "\x00"), 8)
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
	traces := map[string]struct{}{}
	spans := map[string]struct{}{}
	for _, event := range events {
		if event.Type != "" {
			summary.ByType[event.Type]++
		}
		if event.Model != "" {
			summary.ByModel[event.Model]++
		}
		if event.TraceID != "" {
			traces[event.TraceID] = struct{}{}
		}
		if event.SpanID != "" {
			spans[event.SpanID] = struct{}{}
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
	summary.Traces = len(traces)
	summary.Spans = len(spans)
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

func traceHex(value string, bytes int) string {
	if bytes <= 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	if bytes > len(sum) {
		bytes = len(sum)
	}
	return hex.EncodeToString(sum[:bytes])
}
