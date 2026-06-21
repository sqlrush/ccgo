package remoteauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ccgo/internal/auth"
)

const defaultFlowMaxBytes = 1 << 20

// Authorizer is the browser+callback seam for the OAuth authorization step.
// The production implementation opens a browser and starts a local callback
// listener. Tests use a fake that returns a canned code.
type Authorizer interface {
	Authorize(ctx context.Context, authURL, redirectURI, state string) (code string, err error)
}

// AcquireOptions configures the remote OAuth acquisition flow.
type AcquireOptions struct {
	// ServerURL is the MCP server URL. Used to derive ResourceMetadataURL when
	// that field is empty.
	ServerURL string
	// ResourceMetadataURL overrides the RFC 9728 metadata URL discovery.
	ResourceMetadataURL string
	// Scope is the OAuth scope string to request (optional).
	Scope string
	// CallbackPort is the local port for the OAuth redirect callback listener.
	CallbackPort int
	// HTTPClient is used for all discovery, DCR, and token-exchange requests.
	// Defaults to http.DefaultClient when nil.
	HTTPClient *http.Client
	// Authorizer performs the interactive authorization step (open browser,
	// wait for code). Required when AcquireToken must run the initial flow.
	Authorizer Authorizer
	// ConfiguredClientID skips DCR when non-empty; the provided ID is used
	// directly as the OAuth client_id.
	ConfiguredClientID string
	// Now overrides time.Now for tests. Nil means use time.Now.
	Now func() time.Time
}

// AcquireToken performs the full remote OAuth acquisition flow:
//  1. RFC 9728: discover authorization server(s) from the protected-resource metadata.
//  2. RFC 8414: fetch authorization-server metadata.
//  3. RFC 7591: Dynamic Client Registration (skipped when ConfiguredClientID is set).
//  4. Build PKCE authorize URL and invoke opts.Authorizer to get the authorization code.
//  5. Exchange the code for credentials via auth.ExchangeAuthorizationCode.
//
// The PKCE verifier and state are generated once and used consistently throughout
// (verifier→challenge in the auth URL; verifier in the token exchange; state in
// both the auth URL and exchange body to prevent CSRF).
func AcquireToken(ctx context.Context, opts AcquireOptions) (auth.Credentials, RegisteredClient, error) {
	if opts.Authorizer == nil {
		return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("remoteauth: an Authorizer is required to acquire a remote OAuth token")
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}

	// Step 1: RFC 9728 — discover authorization server(s) from the resource.
	resourceURL := strings.TrimSpace(opts.ResourceMetadataURL)
	if resourceURL == "" {
		resourceURL = strings.TrimRight(opts.ServerURL, "/") + "/.well-known/oauth-protected-resource"
	}
	pr, err := DiscoverProtectedResource(ctx, hc, resourceURL, defaultFlowMaxBytes)
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("remoteauth: discover protected resource: %w", err)
	}

	// Step 2: RFC 8414 — authorization-server metadata.
	as, err := DiscoverAuthorizationServer(ctx, hc, pr.AuthorizationServers[0], defaultFlowMaxBytes)
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("remoteauth: discover authorization server: %w", err)
	}

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", opts.CallbackPort)

	// Step 3: RFC 7591 Dynamic Client Registration — skipped when a client ID is configured.
	var rc RegisteredClient
	clientID := strings.TrimSpace(opts.ConfiguredClientID)
	if clientID == "" {
		if strings.TrimSpace(as.RegistrationEndpoint) == "" {
			return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("remoteauth: server has no registration_endpoint and no client id was provided")
		}
		rc, err = RegisterClient(ctx, hc, as.RegistrationEndpoint, ClientMetadata{
			ClientName:              "Claude Code (ccgo)",
			RedirectURIs:            []string{redirectURI},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "none",
			Scope:                   opts.Scope,
		}, defaultFlowMaxBytes)
		if err != nil {
			return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("remoteauth: register client: %w", err)
		}
		clientID = rc.ClientID
	} else {
		rc = RegisteredClient{ClientID: clientID}
	}

	// Step 4: PKCE + authorization.
	// Generate verifier, challenge, and state once — they are used consistently
	// throughout the flow (PKCE: verifier→challenge linkage; CSRF: single state).
	verifier, err := auth.GenerateCodeVerifier()
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("remoteauth: generate code verifier: %w", err)
	}
	state, err := auth.GenerateState()
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("remoteauth: generate state: %w", err)
	}
	challenge := auth.GenerateCodeChallenge(verifier)

	authURL := buildAuthorizeURL(as.AuthorizationEndpoint, clientID, redirectURI, challenge, state, opts.Scope)

	code, err := opts.Authorizer.Authorize(ctx, authURL, redirectURI, state)
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("remoteauth: authorization failed: %w", err)
	}

	// Step 5: Exchange authorization code for credentials.
	cfg := auth.OAuthConfig{
		TokenURL: as.TokenEndpoint,
		ClientID: clientID,
	}
	creds, err := auth.ExchangeAuthorizationCode(ctx, hc, cfg, auth.ExchangeParams{
		Code:         code,
		CodeVerifier: verifier,
		RedirectURI:  redirectURI,
		State:        state,
	})
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("remoteauth: exchange authorization code: %w", err)
	}
	return creds, rc, nil
}

// buildAuthorizeURL constructs the authorization URL with PKCE and CSRF parameters.
func buildAuthorizeURL(endpoint, clientID, redirectURI, challenge, state, scope string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	if strings.TrimSpace(scope) != "" {
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
