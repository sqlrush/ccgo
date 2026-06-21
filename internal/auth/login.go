package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// LoginOptions configures an interactive OAuth login. Browser/Store/HTTPClient
// are seams so the flow is fully testable without a real browser or network.
type LoginOptions struct {
	Config            OAuthConfig
	HTTPClient        *http.Client
	Browser           BrowserOpener
	Store             CredentialStore
	LoginWithClaudeAI bool
	OrgUUID           string
	LoginHint         string
	InferenceOnly     bool
	Now               func() time.Time
	// OnURL is called with the authorize URL so the caller can print the
	// manual-paste fallback. Always invoked after browser open is attempted.
	OnURL func(string)
}

// RunLoginFlow performs the full PKCE authorization-code login:
// generate PKCE -> start loopback listener -> build URL -> open browser ->
// invoke OnURL -> wait for callback -> exchange code -> persist credentials.
// Returns the new credentials on success.
func RunLoginFlow(ctx context.Context, opts LoginOptions) (Credentials, error) {
	config := opts.Config
	if config.ClientID == "" || config.TokenURL == "" {
		config = mergeWithProduction(config)
	}

	verifier, err := GenerateCodeVerifier()
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: generate code verifier: %w", err)
	}
	state, err := GenerateState()
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: generate state: %w", err)
	}
	challenge := GenerateCodeChallenge(verifier)

	listener, err := StartCallbackListener(state)
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: start callback listener: %w", err)
	}
	defer listener.Close()

	authURL, err := BuildAuthURL(AuthURLParams{
		CodeChallenge:     challenge,
		State:             state,
		Port:              listener.Port(),
		LoginWithClaudeAI: opts.LoginWithClaudeAI,
		InferenceOnly:     opts.InferenceOnly,
		OrgUUID:           opts.OrgUUID,
		LoginHint:         opts.LoginHint,
		Config:            config,
	})
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: build authorize URL: %w", err)
	}

	// Attempt the browser open first (non-fatal), then surface the URL.
	// The user can always paste the URL manually if the browser fails.
	if opts.Browser != nil {
		_ = opts.Browser.Open(authURL)
	}
	if opts.OnURL != nil {
		opts.OnURL(authURL)
	}

	result, err := listener.Wait(ctx)
	if err != nil {
		return Credentials{}, err
	}
	if result.State != state {
		return Credentials{}, fmt.Errorf("auth: callback state mismatch")
	}

	creds, err := ExchangeAuthorizationCode(ctx, opts.HTTPClient, config, ExchangeParams{
		Code:         result.Code,
		CodeVerifier: verifier,
		RedirectURI:  listener.RedirectURI(),
		State:        state,
	})
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: exchange authorization code: %w", err)
	}

	if opts.Store != nil {
		if err := opts.Store.Save(ctx, creds); err != nil {
			return Credentials{}, fmt.Errorf("auth: persist credentials: %w", err)
		}
	}
	return creds, nil
}

// mergeWithProduction fills in any zero-value fields in config from the
// production OAuth configuration, returning a new value (no mutation).
func mergeWithProduction(config OAuthConfig) OAuthConfig {
	production := ProductionOAuthConfig()
	if config.ClientID == "" {
		config.ClientID = production.ClientID
	}
	if config.TokenURL == "" {
		config.TokenURL = production.TokenURL
	}
	if config.ConsoleAuthorizeURL == "" {
		config.ConsoleAuthorizeURL = production.ConsoleAuthorizeURL
	}
	if config.ClaudeAIAuthorizeURL == "" {
		config.ClaudeAIAuthorizeURL = production.ClaudeAIAuthorizeURL
	}
	if config.ManualRedirectURL == "" {
		config.ManualRedirectURL = production.ManualRedirectURL
	}
	return config
}
