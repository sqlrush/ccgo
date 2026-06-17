package remote

import (
	"bytes"
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
	SessionID         contracts.ID `json:"session_id,omitempty"`
	RuntimeState      string       `json:"runtime_state"`
	Transport         string       `json:"transport,omitempty"`
	PollURL           string       `json:"poll_url,omitempty"`
	WebSocketURL      string       `json:"websocket_url,omitempty"`
	LastCursor        string       `json:"last_cursor,omitempty"`
	LastPollAt        string       `json:"last_poll_at,omitempty"`
	StreamStartedAt   string       `json:"stream_started_at,omitempty"`
	StreamEndedAt     string       `json:"stream_ended_at,omitempty"`
	StreamStopReason  string       `json:"stream_stop_reason,omitempty"`
	StatusCode        int          `json:"status_code,omitempty"`
	CloseCode         int          `json:"close_code,omitempty"`
	FrameCount        int          `json:"frame_count,omitempty"`
	ConnectCount      int          `json:"connect_count,omitempty"`
	ReconnectCount    int          `json:"reconnect_count,omitempty"`
	AckEventCount     int          `json:"ack_event_count,omitempty"`
	AckSentCount      int          `json:"ack_sent_count,omitempty"`
	AckErrorCount     int          `json:"ack_error_count,omitempty"`
	LeaseEventCount   int          `json:"lease_event_count,omitempty"`
	LeaseExpiredCount int          `json:"lease_expired_count,omitempty"`
	LeaseRenewSent    int          `json:"lease_renew_sent_count,omitempty"`
	LeaseRenewErrors  int          `json:"lease_renew_error_count,omitempty"`
	EventCount        int          `json:"event_count,omitempty"`
	DeliveredCount    int          `json:"delivered_count,omitempty"`
	DuplicateCount    int          `json:"duplicate_count,omitempty"`
	ErrorCount        int          `json:"error_count,omitempty"`
	LastError         string       `json:"last_error,omitempty"`
}

type PollEvent struct {
	EventID        string `json:"event_id,omitempty"`
	TeamID         string `json:"team_id,omitempty"`
	Target         string `json:"target,omitempty"`
	Source         string `json:"source,omitempty"`
	Event          string `json:"event,omitempty"`
	Message        string `json:"message,omitempty"`
	AckURL         string `json:"ack_url,omitempty"`
	LeaseID        string `json:"lease_id,omitempty"`
	LeaseExpiresAt string `json:"lease_expires_at,omitempty"`
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

type AckOptions struct {
	AckURL         string
	AuthToken      string
	EventID        string
	Status         string
	SentCount      int
	Duplicate      bool
	Error          string
	AllowedOrigins []string
	Client         *http.Client
}

type AckResult struct {
	AckedAt    string `json:"acked_at,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
}

type LeaseRenewOptions struct {
	LeaseRenewURL  string
	AuthToken      string
	EventID        string
	LeaseID        string
	AllowedOrigins []string
	Client         *http.Client
}

type LeaseRenewResult struct {
	RenewedAt      string `json:"renewed_at,omitempty"`
	StatusCode     int    `json:"status_code,omitempty"`
	LeaseExpiresAt string `json:"lease_expires_at,omitempty"`
	Error          string `json:"error,omitempty"`
}

func SessionPumpPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), pumpFileName)
}

func SendAck(ctx context.Context, options AckOptions) AckResult {
	result := AckResult{AckedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	rawURL := strings.TrimSpace(options.AckURL)
	if rawURL == "" {
		result.Error = "remote ack url is unavailable"
		return result
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		result.Error = fmt.Sprintf("invalid remote ack url: %s", DisplayEndpoint(rawURL))
		return result
	}
	if !remoteAckOriginAllowed(parsed, options.AllowedOrigins) {
		result.Error = fmt.Sprintf("remote ack url is not allowed: %s", DisplayEndpoint(rawURL))
		return result
	}
	payload, err := json.Marshal(map[string]any{
		"event_id":   strings.TrimSpace(options.EventID),
		"status":     strings.TrimSpace(options.Status),
		"sent_count": options.SentCount,
		"duplicate":  options.Duplicate,
		"error":      strings.TrimSpace(options.Error),
	})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(payload))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Header.Set("content-type", "application/json")
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
		result.Error = fmt.Sprintf("remote ack request failed: %s", DisplayEndpoint(rawURL))
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = remoteRegistrationError(resp.Status, body)
	}
	return result
}

func SendLeaseRenewal(ctx context.Context, options LeaseRenewOptions) LeaseRenewResult {
	result := LeaseRenewResult{RenewedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	rawURL := strings.TrimSpace(options.LeaseRenewURL)
	if rawURL == "" {
		result.Error = "remote lease renew url is unavailable"
		return result
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		result.Error = fmt.Sprintf("invalid remote lease renew url: %s", DisplayEndpoint(rawURL))
		return result
	}
	if !remoteAckOriginAllowed(parsed, options.AllowedOrigins) {
		result.Error = fmt.Sprintf("remote lease renew url is not allowed: %s", DisplayEndpoint(rawURL))
		return result
	}
	payload, err := json.Marshal(map[string]any{
		"event_id": strings.TrimSpace(options.EventID),
		"lease_id": strings.TrimSpace(options.LeaseID),
	})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(payload))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Header.Set("content-type", "application/json")
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
		result.Error = fmt.Sprintf("remote lease renew request failed: %s", DisplayEndpoint(rawURL))
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = remoteRegistrationError(resp.Status, body)
		return result
	}
	result.LeaseExpiresAt = leaseExpiresAtFromRenewResponse(body)
	return result
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
			events, nestedCursor, decoded := decodePollEventValue(nested)
			if cursor == "" {
				cursor = nestedCursor
			}
			if decoded && (len(events) > 0 || nestedCursor != "") {
				return events, cursor, nil
			}
		}
		if nested, ok := firstAny(value, "event", "remote_event", "remoteEvent", "delivery", "payload"); ok {
			events, nestedCursor, decoded := decodePollEventValue(nested)
			if cursor == "" {
				cursor = nestedCursor
			}
			if decoded && (len(events) > 0 || nestedCursor != "") {
				return events, cursor, nil
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

func leaseExpiresAtFromRenewResponse(body []byte) string {
	if len(bytes.TrimSpace(body)) == 0 {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	if text := firstString(raw, "lease_expires_at", "leaseExpiresAt", "expires_at", "expiresAt"); text != "" {
		return text
	}
	return stringFromNestedMap(raw["lease"], "lease_expires_at", "leaseExpiresAt", "expires_at", "expiresAt")
}

func decodePollEventValue(value any) ([]PollEvent, string, bool) {
	switch typed := value.(type) {
	case []any:
		return decodePollEventList(typed), "", true
	case map[string]any:
		cursor := firstString(typed, "next_cursor", "nextCursor", "cursor", "after", "after_id", "afterId")
		if nested, ok := firstAny(typed, "events", "items", "messages", "deliveries"); ok {
			events, nestedCursor, decoded := decodePollEventValue(nested)
			if cursor == "" {
				cursor = nestedCursor
			}
			if decoded {
				return events, cursor, true
			}
		}
		if event, ok := decodePollEventMap(typed); ok {
			return []PollEvent{event}, cursor, true
		}
		return nil, cursor, cursor != ""
	default:
		return nil, "", false
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

func remoteAckOriginAllowed(ack *url.URL, allowed []string) bool {
	if ack == nil {
		return false
	}
	if len(allowed) == 0 {
		return false
	}
	ackOrigin := remoteHTTPOrigin(ack)
	if ackOrigin == "" {
		return false
	}
	for _, raw := range allowed {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		if remoteHTTPOrigin(parsed) == ackOrigin {
			return true
		}
	}
	return false
}

func remoteHTTPOrigin(parsed *url.URL) string {
	if parsed == nil || parsed.Host == "" {
		return ""
	}
	scheme := parsed.Scheme
	switch scheme {
	case "ws":
		scheme = "http"
	case "wss":
		scheme = "https"
	case "http", "https":
	default:
		return ""
	}
	return scheme + "://" + strings.ToLower(parsed.Host)
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
		EventID:        firstString(obj, "event_id", "eventId", "remote_event_id", "remoteEventId", "delivery_id", "deliveryId", "id"),
		TeamID:         firstString(obj, "team_id", "teamId", "team"),
		Target:         firstString(obj, "target", "recipient", "recipients", "audience", "scope"),
		Source:         firstString(obj, "source", "remote", "origin"),
		Event:          firstString(obj, "event", "event_type", "eventType", "type"),
		Message:        firstString(obj, "message", "text", "content", "prompt", "input"),
		AckURL:         firstString(obj, "ack_url", "ackUrl", "acknowledge_url", "acknowledgeUrl", "receipt_url", "receiptUrl"),
		LeaseID:        firstString(obj, "lease_id", "leaseId"),
		LeaseExpiresAt: firstString(obj, "lease_expires_at", "leaseExpiresAt", "lease_expiry", "leaseExpiry", "expires_at", "expiresAt"),
	}
	if event.Message == "" {
		event.Message = messageFromPayload(obj["payload"])
	}
	if event.AckURL == "" {
		event.AckURL = stringFromNestedMap(obj["ack"], "url", "href", "endpoint")
	}
	if event.LeaseID == "" {
		event.LeaseID = stringFromNestedMap(obj["lease"], "id", "lease_id", "leaseId")
	}
	if event.LeaseExpiresAt == "" {
		event.LeaseExpiresAt = stringFromNestedMap(obj["lease"], "expires_at", "expiresAt", "lease_expires_at", "leaseExpiresAt")
	}
	if event.TeamID == "" || event.Message == "" {
		return PollEvent{}, false
	}
	return event, true
}

func stringFromNestedMap(value any, keys ...string) string {
	nested, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return firstString(nested, keys...)
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
