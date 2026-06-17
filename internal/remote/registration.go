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

const registrationFileName = "remote-registration.json"

const (
	RegistrationDisabled   = "disabled"
	RegistrationRegistered = "registered"
	RegistrationFailed     = "failed"
)

const (
	RemoteProtocolVersionV1 = "ccr.remote.v1"
	RemoteProtocolVersionV2 = "ccr.remote.v2"
)

var supportedRemoteProtocolVersions = []string{RemoteProtocolVersionV1, RemoteProtocolVersionV2}

type RegistrationState struct {
	SessionID        contracts.ID `json:"session_id,omitempty"`
	EnvironmentID    string       `json:"environment_id,omitempty"`
	RuntimeState     string       `json:"runtime_state"`
	RegistrationURL  string       `json:"registration_url,omitempty"`
	ManifestPath     string       `json:"manifest_path,omitempty"`
	LastAttemptAt    string       `json:"last_attempt_at,omitempty"`
	RegisteredAt     string       `json:"registered_at,omitempty"`
	StatusCode       int          `json:"status_code,omitempty"`
	RemoteSessionID  string       `json:"remote_session_id,omitempty"`
	RegistrationID   string       `json:"registration_id,omitempty"`
	ProtocolVersion  string       `json:"protocol_version,omitempty"`
	Capabilities     []string     `json:"capabilities,omitempty"`
	WebSocketURL     string       `json:"websocket_url,omitempty"`
	PollURL          string       `json:"poll_url,omitempty"`
	LeaseRenewURL    string       `json:"lease_renew_url,omitempty"`
	Message          string       `json:"message,omitempty"`
	Error            string       `json:"error,omitempty"`
	ManifestServices int          `json:"manifest_services,omitempty"`
}

type RegistrationOptions struct {
	RegistrationURL string
	AuthToken       string
	ManifestPath    string
	Manifest        Manifest
	Now             time.Time
	Client          *http.Client
}

func SessionRegistrationPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), registrationFileName)
}

func DisabledRegistrationState(manifest Manifest, manifestPath string, now time.Time) RegistrationState {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return RegistrationState{
		SessionID:        manifest.SessionID,
		EnvironmentID:    strings.TrimSpace(manifest.EnvironmentID),
		RuntimeState:     RegistrationDisabled,
		ManifestPath:     strings.TrimSpace(manifestPath),
		LastAttemptAt:    now.UTC().Format(time.RFC3339Nano),
		ManifestServices: len(manifest.Services),
	}
}

func RegisterManifest(ctx context.Context, options RegistrationOptions) RegistrationState {
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state := RegistrationState{
		SessionID:        options.Manifest.SessionID,
		EnvironmentID:    strings.TrimSpace(options.Manifest.EnvironmentID),
		RuntimeState:     RegistrationFailed,
		RegistrationURL:  sanitizeRegistrationURL(options.RegistrationURL),
		ManifestPath:     strings.TrimSpace(options.ManifestPath),
		LastAttemptAt:    now.UTC().Format(time.RFC3339Nano),
		ManifestServices: len(options.Manifest.Services),
	}
	rawURL := strings.TrimSpace(options.RegistrationURL)
	if rawURL == "" {
		state.RuntimeState = RegistrationDisabled
		state.Error = ""
		return state
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		state.Error = fmt.Sprintf("invalid remote registration url: %s", state.RegistrationURL)
		return state
	}
	payload, err := json.Marshal(options.Manifest)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(payload))
	if err != nil {
		state.Error = err.Error()
		return state
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	if options.Manifest.SessionID != "" {
		req.Header.Set("x-ccgo-session-id", string(options.Manifest.SessionID))
	}
	if options.Manifest.EnvironmentID != "" {
		req.Header.Set("x-ccgo-environment-id", options.Manifest.EnvironmentID)
	}
	if strings.TrimSpace(options.AuthToken) != "" {
		req.Header.Set("authorization", "Bearer "+strings.TrimSpace(options.AuthToken))
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	defer resp.Body.Close()
	state.StatusCode = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		state.Error = remoteRegistrationError(resp.Status, body)
		return state
	}
	state.RuntimeState = RegistrationRegistered
	state.RegisteredAt = now.UTC().Format(time.RFC3339Nano)
	applyRegistrationResponse(&state, body)
	if err := validateRemoteProtocolVersion(state.ProtocolVersion); err != nil {
		state.RuntimeState = RegistrationFailed
		state.RegisteredAt = ""
		state.Error = err.Error()
		state.WebSocketURL = ""
		state.PollURL = ""
		state.LeaseRenewURL = ""
	}
	return state
}

func WriteRegistrationState(path string, state RegistrationState) error {
	if path == "" {
		return os.ErrInvalid
	}
	if state.RuntimeState == "" {
		state.RuntimeState = RegistrationDisabled
	}
	if state.LastAttemptAt == "" {
		state.LastAttemptAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadRegistrationState(path string) (RegistrationState, error) {
	if path == "" {
		return RegistrationState{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RegistrationState{}, nil
	}
	if err != nil {
		return RegistrationState{}, err
	}
	var state RegistrationState
	if err := json.Unmarshal(data, &state); err != nil {
		return RegistrationState{}, err
	}
	return state, nil
}

func applyRegistrationResponse(state *RegistrationState, body []byte) {
	if len(bytes.TrimSpace(body)) == 0 {
		return
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		state.Message = strings.TrimSpace(string(body))
		return
	}
	applyRegistrationResponseFields(state, raw)
	applyNestedRegistrationResponseFields(state, raw)
}

func applyRegistrationResponseFields(state *RegistrationState, raw map[string]any) {
	if state.RemoteSessionID == "" {
		state.RemoteSessionID = firstString(raw, "remote_session_id", "remoteSessionId", "session_id", "sessionId")
	}
	if state.RegistrationID == "" {
		state.RegistrationID = firstString(raw, "registration_id", "registrationId", "id")
	}
	if state.ProtocolVersion == "" {
		state.ProtocolVersion = firstString(raw, "protocol_version", "protocolVersion", "remote_protocol_version", "remoteProtocolVersion", "version")
	}
	if len(state.Capabilities) == 0 {
		state.Capabilities = firstStringList(raw, "capabilities", "features", "supported_capabilities", "supportedCapabilities")
	}
	if state.WebSocketURL == "" {
		state.WebSocketURL = firstString(raw, "websocket_url", "websocketUrl", "web_socket_url", "ws_url", "wsUrl")
	}
	if state.PollURL == "" {
		state.PollURL = firstString(raw, "poll_url", "pollUrl", "events_url", "eventsUrl")
	}
	if state.LeaseRenewURL == "" {
		state.LeaseRenewURL = firstString(raw, "lease_renew_url", "leaseRenewUrl", "lease_refresh_url", "leaseRefreshUrl", "renew_url", "renewUrl")
	}
	if state.LeaseRenewURL == "" {
		state.LeaseRenewURL = stringFromNestedMap(raw["lease"], "renew_url", "renewUrl", "refresh_url", "refreshUrl", "url")
	}
	if state.Message == "" {
		state.Message = firstString(raw, "message", "status", "detail")
	}
}

func applyNestedRegistrationResponseFields(state *RegistrationState, raw map[string]any) {
	for _, key := range []string{"data", "session", "remote_session", "remoteSession", "registration", "result", "payload"} {
		nested, ok := raw[key].(map[string]any)
		if !ok {
			continue
		}
		applyRegistrationResponseFields(state, nested)
		applyNestedRegistrationResponseFields(state, nested)
	}
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func firstStringList(values map[string]any, keys ...string) []string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if list := normalizeStringList(value); len(list) > 0 {
				return list
			}
		}
	}
	return nil
}

func normalizeStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return cleanStringList(typed)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, fmt.Sprint(item))
		}
		return cleanStringList(items)
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		if strings.Contains(text, ",") {
			return cleanStringList(strings.Split(text, ","))
		}
		return cleanStringList(strings.Fields(text))
	default:
		text := strings.TrimSpace(fmt.Sprint(typed))
		if text == "" || text == "<nil>" {
			return nil
		}
		return []string{text}
	}
}

func cleanStringList(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" || text == "<nil>" || seen[text] {
			continue
		}
		seen[text] = true
		cleaned = append(cleaned, text)
	}
	return cleaned
}

func validateRemoteProtocolVersion(version string) error {
	text := strings.TrimSpace(version)
	if text == "" {
		return nil
	}
	for _, supported := range supportedRemoteProtocolVersions {
		if text == supported {
			return nil
		}
	}
	return fmt.Errorf("unsupported remote protocol version %q; supported: %s", text, strings.Join(supportedRemoteProtocolVersions, ", "))
}

func remoteRegistrationError(status string, body []byte) string {
	bodyText := strings.TrimSpace(string(body))
	if bodyText == "" {
		return status
	}
	if len(bodyText) > 500 {
		bodyText = bodyText[:500]
	}
	return status + ": " + bodyText
}

func sanitizeRegistrationURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil
	return parsed.String()
}
