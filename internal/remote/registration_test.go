package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegisterManifestPostsServiceManifest(t *testing.T) {
	now := time.Date(2026, 6, 17, 11, 0, 0, 0, time.UTC)
	var got Manifest
	var authHeader string
	var sessionHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		authHeader = r.Header.Get("Authorization")
		sessionHeader = r.Header.Get("X-CCGO-Session-ID")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"remoteSessionId":"remote-sess","registration_id":"reg-1","protocolVersion":"ccr.remote.v1","capabilities":["websocket_protocol","lease_renew","lease_renew"],"websocketUrl":"wss://remote/ws","pollUrl":"https://remote/events","lease":{"renewUrl":"https://remote/leases/renew?token=secret"},"message":"registered"}`))
	}))
	defer server.Close()

	state := RegisterManifest(context.Background(), RegistrationOptions{
		RegistrationURL: server.URL + "/register?token=secret",
		AuthToken:       "secret-token",
		ManifestPath:    "/state/remote-service.json",
		Manifest: Manifest{
			SessionID:     "sess_remote",
			EnvironmentID: "env-prod",
			GeneratedAt:   now.Format(time.RFC3339Nano),
			Services:      []Service{{Name: "bridge", RuntimeState: "running"}},
		},
		Now: now,
	})
	if state.RuntimeState != RegistrationRegistered || state.StatusCode != http.StatusOK || state.RemoteSessionID != "remote-sess" || state.RegistrationID != "reg-1" || state.ProtocolVersion != RemoteProtocolVersionV1 || state.WebSocketURL != "wss://remote/ws" || state.PollURL != "https://remote/events" || state.LeaseRenewURL != "https://remote/leases/renew?token=secret" {
		t.Fatalf("registration state = %#v", state)
	}
	if len(state.Capabilities) != 2 || state.Capabilities[0] != "websocket_protocol" || state.Capabilities[1] != "lease_renew" {
		t.Fatalf("registration state = %#v", state)
	}
	if got.SessionID != "sess_remote" || len(got.Services) != 1 || got.Services[0].Name != "bridge" {
		t.Fatalf("posted manifest = %#v", got)
	}
	if authHeader != "Bearer secret-token" || sessionHeader != "sess_remote" {
		t.Fatalf("headers auth=%q session=%q", authHeader, sessionHeader)
	}
	if strings.Contains(state.RegistrationURL, "secret") || strings.Contains(state.Error, "secret") {
		t.Fatalf("state leaked secret = %#v", state)
	}
}

func TestRegisterManifestAcceptsWrappedResponse(t *testing.T) {
	now := time.Date(2026, 6, 17, 11, 3, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"message":"registered",
			"data":{
				"sessionId":"remote-wrapped",
				"registration":{
					"id":"reg-wrapped",
					"protocol_version":"ccr.remote.v2",
					"features":"remote_trigger lease_refresh",
					"web_socket_url":"wss://remote/wrapped/ws",
					"eventsUrl":"https://remote/wrapped/events",
					"lease_refresh_url":"https://remote/wrapped/leases/refresh",
					"detail":"nested detail"
				}
			}
		}`))
	}))
	defer server.Close()

	state := RegisterManifest(context.Background(), RegistrationOptions{
		RegistrationURL: server.URL + "/register",
		Manifest: Manifest{
			SessionID:     "sess_wrapped",
			EnvironmentID: "env-prod",
			Services:      []Service{{Name: "daemon", RuntimeState: "running"}},
		},
		Now: now,
	})
	if state.RuntimeState != RegistrationRegistered || state.RemoteSessionID != "remote-wrapped" || state.RegistrationID != "reg-wrapped" || state.ProtocolVersion != RemoteProtocolVersionV2 || state.WebSocketURL != "wss://remote/wrapped/ws" || state.PollURL != "https://remote/wrapped/events" || state.LeaseRenewURL != "https://remote/wrapped/leases/refresh" || state.Message != "registered" {
		t.Fatalf("registration state = %#v", state)
	}
	if len(state.Capabilities) != 2 || state.Capabilities[0] != "remote_trigger" || state.Capabilities[1] != "lease_refresh" {
		t.Fatalf("registration state = %#v", state)
	}
}

func TestRegisterManifestRejectsUnsupportedProtocolVersion(t *testing.T) {
	now := time.Date(2026, 6, 17, 11, 4, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"remoteSessionId":"remote-future",
			"protocolVersion":"ccr.remote.v99",
			"capabilities":["websocket_protocol"],
			"websocketUrl":"wss://remote/future/ws?token=secret",
			"pollUrl":"https://remote/future/events?token=secret",
			"leaseRenewUrl":"https://remote/future/leases/renew?token=secret"
		}`))
	}))
	defer server.Close()

	state := RegisterManifest(context.Background(), RegistrationOptions{
		RegistrationURL: server.URL + "/register",
		Manifest: Manifest{
			SessionID:     "sess_future",
			EnvironmentID: "env-prod",
			Services:      []Service{{Name: "daemon", RuntimeState: "running"}},
		},
		Now: now,
	})
	if state.RuntimeState != RegistrationFailed || state.StatusCode != http.StatusOK || state.ProtocolVersion != "ccr.remote.v99" || !strings.Contains(state.Error, "unsupported remote protocol version") || !strings.Contains(state.Error, RemoteProtocolVersionV1) || !strings.Contains(state.Error, RemoteProtocolVersionV2) {
		t.Fatalf("registration state = %#v", state)
	}
	if state.RegisteredAt != "" || state.WebSocketURL != "" || state.PollURL != "" || state.LeaseRenewURL != "" || strings.Contains(state.Error, "token=secret") {
		t.Fatalf("unsupported state leaked usable endpoint or secret = %#v", state)
	}
}

func TestRegisterManifestHandlesDisabledAndFailedState(t *testing.T) {
	now := time.Date(2026, 6, 17, 11, 5, 0, 0, time.UTC)
	manifest := Manifest{SessionID: "sess_remote", EnvironmentID: "env-prod"}
	disabled := RegisterManifest(context.Background(), RegistrationOptions{Manifest: manifest, Now: now})
	if disabled.RuntimeState != RegistrationDisabled || disabled.Error != "" || disabled.SessionID != "sess_remote" {
		t.Fatalf("disabled = %#v", disabled)
	}
	failed := RegisterManifest(context.Background(), RegistrationOptions{
		RegistrationURL: "ftp://user:pass@example.invalid/register?token=secret",
		Manifest:        manifest,
		Now:             now,
	})
	if failed.RuntimeState != RegistrationFailed || !strings.Contains(failed.Error, "invalid remote registration url") || strings.Contains(failed.Error, "secret") || strings.Contains(failed.Error, "pass") {
		t.Fatalf("failed = %#v", failed)
	}
}

func TestWriteAndLoadRegistrationState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_remote", registrationFileName)
	state := DisabledRegistrationState(Manifest{SessionID: "sess_remote"}, "/state/remote-service.json", time.Date(2026, 6, 17, 11, 10, 0, 0, time.UTC))
	state.ProtocolVersion = RemoteProtocolVersionV1
	state.Capabilities = []string{"websocket_protocol", "lease_renew"}
	state.LeaseRenewURL = "https://remote/leases/renew"
	if err := WriteRegistrationState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRegistrationState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_remote" || loaded.RuntimeState != RegistrationDisabled || loaded.ManifestPath != "/state/remote-service.json" || loaded.ProtocolVersion != RemoteProtocolVersionV1 || loaded.LeaseRenewURL != "https://remote/leases/renew" || len(loaded.Capabilities) != 2 {
		t.Fatalf("loaded = %#v", loaded)
	}
	missing, err := LoadRegistrationState(filepath.Join(t.TempDir(), registrationFileName))
	if err != nil {
		t.Fatal(err)
	}
	if missing.RuntimeState != "" {
		t.Fatalf("missing = %#v", missing)
	}
}
