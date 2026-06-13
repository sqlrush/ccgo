package mcp

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

type ServerCredentialStoreProvider func(context.Context, string, contracts.MCPServer) (auth.CredentialStore, error)

type FileOAuthAccessTokenProviderOptions struct {
	CredentialStore  ServerCredentialStoreProvider
	CredentialPath   func(string, contracts.MCPServer) string
	Config           auth.OAuthConfig
	HTTPClient       *http.Client
	Now              func() time.Time
	RefreshMargin    time.Duration
	MaxResponseBytes int64
}

func FileOAuthAccessTokenProvider(options FileOAuthAccessTokenProviderOptions) ServerAccessTokenProvider {
	return func(ctx context.Context, name string, server contracts.MCPServer) (AccessTokenProvider, error) {
		if server.OAuth == nil {
			return nil, nil
		}
		store, err := oauthCredentialStore(ctx, name, server, options)
		if err != nil {
			return nil, err
		}
		credentials, err := store.Load(ctx)
		if err != nil {
			return nil, err
		}
		config := options.Config
		if clientID := strings.TrimSpace(server.OAuth.ClientID); clientID != "" {
			config.ClientID = clientID
		}
		return auth.NewOAuthTokenProvider(auth.OAuthTokenProviderOptions{
			Credentials:      credentials,
			Config:           config,
			HTTPClient:       options.HTTPClient,
			Now:              options.Now,
			RefreshMargin:    options.RefreshMargin,
			CredentialStore:  store,
			MaxResponseBytes: options.MaxResponseBytes,
		}), nil
	}
}

func oauthCredentialStore(ctx context.Context, name string, server contracts.MCPServer, options FileOAuthAccessTokenProviderOptions) (auth.CredentialStore, error) {
	if options.CredentialStore != nil {
		return options.CredentialStore(ctx, name, server)
	}
	path := ""
	if options.CredentialPath != nil {
		path = options.CredentialPath(name, server)
	}
	if path == "" {
		path = DefaultMCPServerCredentialsPath(name)
	}
	return auth.NewFileCredentialStore(path), nil
}

func DefaultMCPServerCredentialsPath(name string) string {
	normalized := strings.Trim(NormalizeNameForMCP(name), "_")
	if normalized == "" {
		normalized = "server"
	}
	return filepath.Join(platform.ClaudeHomeDir(), "mcp", normalized+".credentials.json")
}
