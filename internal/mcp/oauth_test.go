package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
)

func TestFileOAuthAccessTokenProviderLoadsAndPersistsServerCredentials(t *testing.T) {
	now := time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC)
	var gotClientID string
	var gotRefreshToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		gotClientID = r.Form.Get("client_id")
		gotRefreshToken = r.Form.Get("refresh_token")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fresh",
			"refresh_token": "next-refresh",
			"expires_in":    3600,
			"scope":         "user:mcp_servers",
		})
	}))
	defer server.Close()

	storePath := filepath.Join(t.TempDir(), "remote.credentials.json")
	store := auth.NewFileCredentialStore(storePath)
	if err := store.Save(context.Background(), auth.Credentials{
		Source:       auth.SourceOAuth,
		AccessToken:  "expired",
		RefreshToken: "refresh",
		ExpiresAt:    now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	provider := FileOAuthAccessTokenProvider(FileOAuthAccessTokenProviderOptions{
		CredentialPath: func(name string, server contracts.MCPServer) string {
			if name != "remote" || server.OAuth == nil {
				t.Fatalf("provider input name=%q server=%#v", name, server)
			}
			return storePath
		},
		Config:     auth.OAuthConfig{TokenURL: server.URL},
		HTTPClient: server.Client(),
		Now:        func() time.Time { return now },
	})
	tokenProvider, err := provider(context.Background(), "remote", contracts.MCPServer{
		OAuth: &contracts.MCPOAuthConfig{ClientID: "server-client"},
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokenProvider.CurrentAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "fresh" || gotClientID != "server-client" || gotRefreshToken != "refresh" {
		t.Fatalf("token=%q clientID=%q refresh=%q", token, gotClientID, gotRefreshToken)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccessToken != "fresh" || loaded.RefreshToken != "next-refresh" || !loaded.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("loaded = %#v", loaded)
	}
	if len(loaded.Scopes) != 1 || loaded.Scopes[0] != "user:mcp_servers" {
		t.Fatalf("scopes = %#v", loaded.Scopes)
	}
}

func TestFileOAuthAccessTokenProviderSkipsNonOAuthServers(t *testing.T) {
	provider := FileOAuthAccessTokenProvider(FileOAuthAccessTokenProviderOptions{})
	tokenProvider, err := provider(context.Background(), "plain", contracts.MCPServer{})
	if err != nil {
		t.Fatal(err)
	}
	if tokenProvider != nil {
		t.Fatalf("token provider = %#v", tokenProvider)
	}
}

func TestDefaultMCPServerCredentialsPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	got := DefaultMCPServerCredentialsPath("github.com/server one")
	want := filepath.Join(root, "mcp", "github_com_server_one.credentials.json")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
