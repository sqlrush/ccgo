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
		_, _ = w.Write([]byte(`{"remoteSessionId":"remote-sess","registration_id":"reg-1","websocketUrl":"wss://remote/ws","pollUrl":"https://remote/events","message":"registered"}`))
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
	if state.RuntimeState != RegistrationRegistered || state.StatusCode != http.StatusOK || state.RemoteSessionID != "remote-sess" || state.RegistrationID != "reg-1" || state.WebSocketURL != "wss://remote/ws" || state.PollURL != "https://remote/events" {
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
	if err := WriteRegistrationState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRegistrationState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_remote" || loaded.RuntimeState != RegistrationDisabled || loaded.ManifestPath != "/state/remote-service.json" {
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
