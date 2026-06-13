package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestOAuthTokenProviderRefreshesAccessToken(t *testing.T) {
	var calls int
	var saved Credentials
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("content-type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("content-type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh" || r.Form.Get("client_id") != "client" {
			t.Fatalf("form = %#v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fresh",
			"refresh_token": "next-refresh",
			"expires_in":    3600,
			"scope":         "user:profile user:mcp_servers",
			"token_type":    "Bearer",
		})
	}))
	defer server.Close()

	provider := NewOAuthTokenProvider(OAuthTokenProviderOptions{
		Credentials:   Credentials{Source: SourceOAuth, RefreshToken: "refresh"},
		Config:        OAuthConfig{TokenURL: server.URL, ClientID: "client"},
		HTTPClient:    server.Client(),
		Now:           func() time.Time { return now },
		OnCredentials: func(credentials Credentials) { saved = credentials },
	})
	token, err := provider.CurrentAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "fresh" {
		t.Fatalf("token = %q", token)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if saved.AccessToken != "fresh" || saved.RefreshToken != "next-refresh" || !saved.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("saved = %#v", saved)
	}
	if len(saved.Scopes) != 2 || saved.Scopes[1] != "user:mcp_servers" {
		t.Fatalf("scopes = %#v", saved.Scopes)
	}

	token, err = provider.CurrentAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "fresh" || calls != 1 {
		t.Fatalf("token=%q calls=%d", token, calls)
	}
}

func TestOAuthTokenProviderPersistsRefreshedCredentials(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fresh",
			"refresh_token": "next-refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	store := NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.json"))
	provider := NewOAuthTokenProvider(OAuthTokenProviderOptions{
		Credentials:     Credentials{Source: SourceOAuth, RefreshToken: "refresh"},
		Config:          OAuthConfig{TokenURL: server.URL, ClientID: "client"},
		HTTPClient:      server.Client(),
		Now:             func() time.Time { return now },
		CredentialStore: store,
	})
	token, err := provider.CurrentAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "fresh" {
		t.Fatalf("token = %q", token)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccessToken != "fresh" || loaded.RefreshToken != "next-refresh" || !loaded.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestOAuthTokenProviderUsesCachedAccessTokenWithoutExpiry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("unexpected token refresh")
	}))
	defer server.Close()

	provider := NewOAuthTokenProvider(OAuthTokenProviderOptions{
		Credentials: Credentials{Source: SourceOAuth, AccessToken: "cached", RefreshToken: "refresh"},
		Config:      OAuthConfig{TokenURL: server.URL, ClientID: "client"},
		HTTPClient:  server.Client(),
	})
	token, err := provider.CurrentAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "cached" {
		t.Fatalf("token = %q", token)
	}
}

func TestOAuthTokenProviderRefreshesExpiredAccessToken(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh"})
	}))
	defer server.Close()

	provider := NewOAuthTokenProvider(OAuthTokenProviderOptions{
		Credentials: Credentials{
			Source:       SourceOAuth,
			AccessToken:  "expired",
			RefreshToken: "refresh",
			ExpiresAt:    now.Add(-time.Minute),
		},
		Config:     OAuthConfig{TokenURL: server.URL, ClientID: "client"},
		HTTPClient: server.Client(),
		Now:        func() time.Time { return now },
	})
	token, err := provider.CurrentAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "fresh" || calls != 1 {
		t.Fatalf("token=%q calls=%d", token, calls)
	}
}

func TestOAuthTokenProviderReportsRefreshErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()

	provider := NewOAuthTokenProvider(OAuthTokenProviderOptions{
		Credentials: Credentials{Source: SourceOAuth, RefreshToken: "refresh"},
		Config:      OAuthConfig{TokenURL: server.URL, ClientID: "client"},
		HTTPClient:  server.Client(),
	})
	_, err := provider.CurrentAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected refresh error")
	}
}
