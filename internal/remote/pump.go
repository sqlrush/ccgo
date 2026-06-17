package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const pumpFileName = "remote-pump.json"

const (
	PumpDisabled = "disabled"
	PumpRunning  = "running"
	PumpFailed   = "failed"
)

type PumpState struct {
	SessionID      contracts.ID `json:"session_id,omitempty"`
	RuntimeState   string       `json:"runtime_state"`
	Transport      string       `json:"transport,omitempty"`
	PollURL        string       `json:"poll_url,omitempty"`
	WebSocketURL   string       `json:"websocket_url,omitempty"`
	LastCursor     string       `json:"last_cursor,omitempty"`
	LastPollAt     string       `json:"last_poll_at,omitempty"`
	StatusCode     int          `json:"status_code,omitempty"`
	FrameCount     int          `json:"frame_count,omitempty"`
	ConnectCount   int          `json:"connect_count,omitempty"`
	ReconnectCount int          `json:"reconnect_count,omitempty"`
	EventCount     int          `json:"event_count,omitempty"`
	DeliveredCount int          `json:"delivered_count,omitempty"`
	DuplicateCount int          `json:"duplicate_count,omitempty"`
	ErrorCount     int          `json:"error_count,omitempty"`
	LastError      string       `json:"last_error,omitempty"`
}

type PollEvent struct {
	EventID string `json:"event_id,omitempty"`
	TeamID  string `json:"team_id,omitempty"`
	Target  string `json:"target,omitempty"`
	Source  string `json:"source,omitempty"`
	Event   string `json:"event,omitempty"`
	Message string `json:"message,omitempty"`
}

type PollOptions struct {
	PollURL   string
	Cursor    string
	AuthToken string
	Client    *http.Client
}

type PollResult struct {
	CheckedAt  string      `json:"checked_at,omitempty"`
	StatusCode int         `json:"status_code,omitempty"`
	NextCursor string      `json:"next_cursor,omitempty"`
	Events     []PollEvent `json:"events,omitempty"`
	Error      string      `json:"error,omitempty"`
}

func SessionPumpPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), pumpFileName)
}

func FetchPollEvents(ctx context.Context, options PollOptions) PollResult {
	result := PollResult{CheckedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	rawURL := strings.TrimSpace(options.PollURL)
	if rawURL == "" {
		result.Error = "remote poll url is unavailable"
		return result
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		result.Error = fmt.Sprintf("invalid remote poll url: %s", DisplayEndpoint(rawURL))
		return result
	}
	if strings.TrimSpace(options.Cursor) != "" {
		query := parsed.Query()
		query.Set("cursor", strings.TrimSpace(options.Cursor))
		parsed.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Header.Set("accept", "application/json")
	if strings.TrimSpace(options.AuthToken) != "" {
		req.Header.Set("authorization", "Bearer "+strings.TrimSpace(options.AuthToken))
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = remoteRegistrationError(resp.Status, body)
		return result
	}
	events, cursor, err := DecodePollEvents(body)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Events = events
	result.NextCursor = cursor
	return result
}

func DecodePollEvents(data []byte) ([]PollEvent, string, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, "", nil
	}
	var raw any
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, "", err
	}
	switch value := raw.(type) {
	case []any:
		return decodePollEventList(value), "", nil
	case map[string]any:
		cursor := firstString(value, "next_cursor", "nextCursor", "cursor", "after", "after_id", "afterId")
		if nested, ok := firstAny(value, "events", "items", "messages", "deliveries", "data"); ok {
			if nestedMap, ok := nested.(map[string]any); ok {
				if nestedEvents, ok := firstAny(nestedMap, "events", "items", "messages", "deliveries"); ok {
					nested = nestedEvents
				}
				if cursor == "" {
					cursor = firstString(nestedMap, "next_cursor", "nextCursor", "cursor", "after", "after_id", "afterId")
				}
			}
			if list, ok := nested.([]any); ok {
				return decodePollEventList(list), cursor, nil
			}
		}
		if event, ok := decodePollEventMap(value); ok {
			return []PollEvent{event}, cursor, nil
		}
		return nil, cursor, nil
	default:
		return nil, "", fmt.Errorf("remote poll response must be an object or array")
	}
}

func WritePumpState(path string, state PumpState) error {
	if path == "" {
		return os.ErrInvalid
	}
	if state.RuntimeState == "" {
		state.RuntimeState = PumpDisabled
	}
	if state.LastPollAt == "" {
		state.LastPollAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadPumpState(path string) (PumpState, error) {
	if path == "" {
		return PumpState{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return PumpState{}, nil
	}
	if err != nil {
		return PumpState{}, err
	}
	var state PumpState
	if err := json.Unmarshal(data, &state); err != nil {
		return PumpState{}, err
	}
	return state, nil
}

func DisplayEndpoint(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil
	return parsed.String()
}

func decodePollEventList(list []any) []PollEvent {
	events := make([]PollEvent, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		event, ok := decodePollEventMap(obj)
		if !ok {
			continue
		}
		events = append(events, event)
	}
	return events
}

func decodePollEventMap(obj map[string]any) (PollEvent, bool) {
	event := PollEvent{
		EventID: firstString(obj, "event_id", "eventId", "remote_event_id", "remoteEventId", "delivery_id", "deliveryId", "id"),
		TeamID:  firstString(obj, "team_id", "teamId", "team"),
		Target:  firstString(obj, "target", "recipient", "recipients", "audience", "scope"),
		Source:  firstString(obj, "source", "remote", "origin"),
		Event:   firstString(obj, "event", "event_type", "eventType", "type"),
		Message: firstString(obj, "message", "text", "content", "prompt", "input"),
	}
	if event.Message == "" {
		event.Message = messageFromPayload(obj["payload"])
	}
	if event.TeamID == "" || event.Message == "" {
		return PollEvent{}, false
	}
	return event, true
}

func firstAny(values map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func messageFromPayload(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case map[string]any:
		if text := firstString(typed, "message", "text", "content", "body", "summary"); text != "" {
			return text
		}
		data, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(data)
	default:
		text := strings.TrimSpace(fmt.Sprint(typed))
		if text == "<nil>" {
			return ""
		}
		return text
	}
}
