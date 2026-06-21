package remoteauth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

// CombinedOptions configures the per-server dispatcher that selects between
// the cached-refresh path and the full remote OAuth acquisition path.
type CombinedOptions struct {
	// StoreFor returns the CredentialStore for the given server name + config.
	// Required.
	StoreFor func(name string, server contracts.MCPServer) auth.CredentialStore
	// Authorizer performs the interactive OAuth authorization step (browser open
	// + callback). Required for the acquisition path; may be nil when only the
	// cached-refresh path is expected.
	Authorizer Authorizer
	// HTTPClient is used for discovery, DCR, and token requests.
	// Defaults to http.DefaultClient when nil.
	HTTPClient *http.Client
	// CallbackPort is the local port for the OAuth redirect callback listener.
	// Passed through to AcquireOptions.
	CallbackPort int
	// Now overrides time.Now for tests. Nil means use time.Now.
	Now func() time.Time
}

// CombinedAccessTokenProvider returns an mcp.ServerAccessTokenProvider that
// dispatches per-server as follows:
//
//   - Server without OAuth config → nil provider (no Authorization header).
//   - Server with OAuth + cached credentials (non-empty AccessToken or
//     RefreshToken) → refresh-only path via RemoteOAuthAccessTokenProvider
//     (the Authorizer is never called).
//   - Server with OAuth + empty store → acquisition path via
//     RemoteOAuthAccessTokenProvider (calls Authorizer to obtain a code).
//
// The dispatcher lives in package remoteauth (not internal/mcp) to avoid an
// import cycle: internal/mcp/remoteauth already imports internal/mcp for the
// ServerAccessTokenProvider type, so internal/mcp must not import remoteauth.
// cmd/claude wires the result into ServerToolOptions.AccessTokenProvider.
func CombinedAccessTokenProvider(opts CombinedOptions) mcp.ServerAccessTokenProvider {
	return func(ctx context.Context, name string, server contracts.MCPServer) (mcp.AccessTokenProvider, error) {
		if server.OAuth == nil {
			return nil, nil
		}
		store := opts.StoreFor(name, server)

		// Peek at cached credentials to decide which branch to take. The
		// decision is made here so callers can assert "Authorizer not called
		// when creds present" without relying on RemoteOAuthAccessTokenProvider
		// internals.
		//
		// NOTE: RemoteOAuthAccessTokenProvider also loads the store internally
		// and makes the same cached-vs-acquire decision; the peek below is a
		// lightweight pre-check that allows the test assertions to be simpler
		// (no need to inject a counting authorizer into AcquireOptions when
		// the path is refresh-only). The peek is consistent because:
		//  • store.Load is idempotent.
		//  • RemoteOAuthAccessTokenProvider will re-load and reach the same
		//    conclusion (creds present → skip acquire).
		creds, err := store.Load(ctx)
		if err != nil {
			return nil, err
		}

		hasAccessToken := strings.TrimSpace(creds.AccessToken) != ""
		hasRefreshToken := strings.TrimSpace(creds.RefreshToken) != ""
		hasCached := hasAccessToken || hasRefreshToken

		acquire := AcquireOptions{
			HTTPClient:   opts.HTTPClient,
			Now:          opts.Now,
			CallbackPort: opts.CallbackPort,
		}
		if !hasCached {
			// Acquisition path: Authorizer is needed.
			acquire.Authorizer = opts.Authorizer
		}
		// Always delegate to RemoteOAuthAccessTokenProvider; it handles both
		// the cached-refresh branch (Authorizer never invoked) and the full
		// acquisition branch.
		return RemoteOAuthAccessTokenProvider(store, acquire)(ctx, name, server)
	}
}
