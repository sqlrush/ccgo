package remoteauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterClient(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"generated-id","client_id_issued_at":1700000000}`))
	}))
	defer srv.Close()

	meta := ClientMetadata{
		ClientName:              "Claude Code (test)",
		RedirectURIs:            []string{"http://127.0.0.1:7777/callback"},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
	}
	rc, err := RegisterClient(context.Background(), srv.Client(), srv.URL+"/register", meta, 1<<20)
	if err != nil {
		t.Fatalf("register err: %v", err)
	}
	if rc.ClientID != "generated-id" {
		t.Fatalf("client_id = %q", rc.ClientID)
	}
	if got, _ := gotBody["redirect_uris"].([]any); len(got) != 1 {
		t.Fatalf("redirect_uris not sent: %v", gotBody)
	}
	if gotBody["token_endpoint_auth_method"] != "none" {
		t.Fatalf("auth method not sent: %v", gotBody)
	}
}

func TestRegisterClientRejectsEmptyID(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_secret":"x"}`)) // no client_id
	}))
	defer srv.Close()
	_, err := RegisterClient(context.Background(), srv.Client(), srv.URL, ClientMetadata{}, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "client_id") {
		t.Fatalf("expected client_id validation error, got %v", err)
	}
}

func TestRegisterClientNon2xx(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_redirect_uri"}`))
	}))
	defer srv.Close()

	meta := ClientMetadata{
		RedirectURIs: []string{"http://127.0.0.1:7777/callback"},
	}
	_, err := RegisterClient(context.Background(), srv.Client(), srv.URL+"/register", meta, 1<<20)
	if err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected status 400 in error, got: %v", err)
	}
}

func TestRegisterClientOversizedBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Write more than the cap.
		_, _ = w.Write([]byte(`{"client_id":"` + strings.Repeat("x", 100) + `"}`))
	}))
	defer srv.Close()

	meta := ClientMetadata{
		RedirectURIs: []string{"http://127.0.0.1:7777/callback"},
	}
	// Set maxBytes to 10 so any real response exceeds it.
	_, err := RegisterClient(context.Background(), srv.Client(), srv.URL+"/register", meta, 10)
	if err == nil {
		t.Fatal("expected error for oversized body, got nil")
	}
	if !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("expected byte-cap error, got: %v", err)
	}
}

func TestRegisterClientRejectsNonHTTPS(t *testing.T) {
	// Use a plain (non-TLS) httptest server to get an http:// URL on a non-loopback-intended
	// remote address; we just pass the URL directly without hitting the server.
	_, err := RegisterClient(context.Background(), http.DefaultClient, "http://example.com/register", ClientMetadata{}, 1<<20)
	if err == nil {
		t.Fatal("expected error for non-https non-loopback endpoint, got nil")
	}
}
