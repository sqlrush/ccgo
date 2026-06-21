package remoteauth

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

// RemoteOAuthAccessTokenProvider returns an mcp.ServerAccessTokenProvider that:
//  1. Loads cached credentials from store.
//  2. If no usable credentials exist (no access token and no refresh token),
//     runs AcquireToken once to perform the full interactive OAuth flow, then
//     saves the result to store.
//  3. Wraps the credentials in auth.NewOAuthTokenProvider so that transparent
//     token refresh on 401 works at the protocol-client level.
//
// The existing mcp.FileOAuthAccessTokenProvider is refresh-only; this provider
// adds the initial acquisition step.
//
// Token refresh uses the endpoint stored in Credentials.TokenEndpointURL (set
// during AcquireToken from the discovered authorization-server metadata). When
// that field is absent (Anthropic OAuth credentials) the refresh falls back to
// ProductionOAuthConfig().TokenURL, preserving existing behavior.
func RemoteOAuthAccessTokenProvider(store auth.CredentialStore, opts AcquireOptions) mcp.ServerAccessTokenProvider {
	return func(ctx context.Context, name string, server contracts.MCPServer) (mcp.AccessTokenProvider, error) {
		if server.OAuth == nil {
			return nil, nil
		}

		creds, err := store.Load(ctx)
		if err != nil {
			return nil, fmt.Errorf("remoteauth: load cached credentials: %w", err)
		}

		// Perform the full acquisition flow only when there are no usable credentials.
		// A credential is considered usable when it has either an access token or a
		// refresh token (auth.NewOAuthTokenProvider will refresh automatically when
		// the access token has expired, as long as a refresh token is present).
		hasAccessToken := strings.TrimSpace(creds.AccessToken) != ""
		hasRefreshToken := strings.TrimSpace(creds.RefreshToken) != ""
		if !hasAccessToken && !hasRefreshToken {
			serverOpts := opts
			serverOpts.ServerURL = firstNonEmptyString(opts.ServerURL, server.URL)
			serverOpts.ResourceMetadataURL = firstNonEmptyString(opts.ResourceMetadataURL, server.OAuth.AuthServerMetadataURL)
			serverOpts.ConfiguredClientID = firstNonEmptyString(opts.ConfiguredClientID, server.OAuth.ClientID)
			if server.OAuth.CallbackPort != nil && *server.OAuth.CallbackPort > 0 && serverOpts.CallbackPort == 0 {
				serverOpts.CallbackPort = *server.OAuth.CallbackPort
			}
			acquired, _, err := AcquireToken(ctx, serverOpts)
			if err != nil {
				return nil, err
			}
			if err := store.Save(ctx, acquired); err != nil {
				return nil, fmt.Errorf("remoteauth: save acquired credentials: %w", err)
			}
			creds = acquired
		}

		// Build the token-provider config. TokenURL comes from the discovered
		// endpoint persisted in creds (third-party MCP servers) so that refresh
		// requests target the correct authorization server on process restart.
		// When TokenEndpointURL is absent (Anthropic OAuth creds) we fall back to
		// ProductionOAuthConfig so existing behavior is unchanged.
		cfg := auth.ProductionOAuthConfig()
		if creds.TokenEndpointURL != "" {
			cfg.TokenURL = creds.TokenEndpointURL
		}
		// ClientID can come from multiple sources (in priority order):
		// 1. persisted ClientID from DCR (third-party MCP OAuth)
		// 2. server-specific OAuth config
		// 3. options-provided configured client ID
		// When all are absent, fall back to Anthropic's default (ProductionOAuthConfig).
		if creds.ClientID != "" {
			cfg.ClientID = creds.ClientID
		} else if clientID := firstNonEmptyString(server.OAuth.ClientID, opts.ConfiguredClientID); clientID != "" {
			cfg.ClientID = clientID
		}
		return auth.NewOAuthTokenProvider(auth.OAuthTokenProviderOptions{
			Credentials:     creds,
			Config:          cfg,
			HTTPClient:      opts.HTTPClient,
			CredentialStore: store,
			Now:             opts.Now,
		}), nil
	}
}

// firstNonEmptyString returns the first non-blank value from values, or "" if all are blank.
func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
