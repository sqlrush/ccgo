package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ExchangeParams carries the inputs to the authorization_code grant.
type ExchangeParams struct {
	Code         string
	CodeVerifier string
	RedirectURI  string
	State        string
}

func (p ExchangeParams) validate() error {
	if strings.TrimSpace(p.Code) == "" {
		return fmt.Errorf("auth: authorization code is required")
	}
	if strings.TrimSpace(p.CodeVerifier) == "" {
		return fmt.Errorf("auth: code_verifier is required")
	}
	if strings.TrimSpace(p.RedirectURI) == "" {
		return fmt.Errorf("auth: redirect_uri is required")
	}
	return nil
}

// authCodeRequest is the JSON body CC posts (services/oauth/client.ts:115-133).
// CC uses JSON (not form-encoded) for the authorization_code grant.
type authCodeRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	RedirectURI  string `json:"redirect_uri"`
	ClientID     string `json:"client_id"`
	CodeVerifier string `json:"code_verifier"`
	State        string `json:"state,omitempty"`
}

// ExchangeAuthorizationCode swaps an authorization code for OAuth credentials.
// It posts a JSON body to config.TokenURL following the CC reference implementation
// at services/oauth/client.ts:exchangeCodeForTokens.
func ExchangeAuthorizationCode(ctx context.Context, client *http.Client, config OAuthConfig, params ExchangeParams) (Credentials, error) {
	if err := params.validate(); err != nil {
		return Credentials{}, err
	}
	if config.TokenURL == "" || config.ClientID == "" {
		production := ProductionOAuthConfig()
		if config.TokenURL == "" {
			config.TokenURL = production.TokenURL
		}
		if config.ClientID == "" {
			config.ClientID = production.ClientID
		}
	}
	if client == nil {
		client = http.DefaultClient
	}

	body, err := json.Marshal(authCodeRequest{
		GrantType:    "authorization_code",
		Code:         params.Code,
		RedirectURI:  params.RedirectURI,
		ClientID:     config.ClientID,
		CodeVerifier: params.CodeVerifier,
		State:        params.State,
	})
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: marshal token exchange request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.TokenURL, bytes.NewReader(body))
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: build token exchange request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	limit := defaultOAuthTokenResponseLimit
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: read token exchange response: %w", err)
	}
	if int64(len(raw)) > limit {
		return Credentials{}, fmt.Errorf("auth: token response exceeds %d bytes", limit)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Surface status only — never echo the body which may contain tokens.
		return Credentials{}, fmt.Errorf("auth: token exchange failed with status %d", resp.StatusCode)
	}

	var tr oauthTokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return Credentials{}, fmt.Errorf("auth: decode token exchange response: %w", err)
	}
	accessToken := strings.TrimSpace(tr.AccessToken)
	if accessToken == "" {
		return Credentials{}, fmt.Errorf("auth: token response missing access_token")
	}

	creds := Credentials{
		Source:       SourceOAuth,
		AccessToken:  accessToken,
		RefreshToken: strings.TrimSpace(tr.RefreshToken),
		Scopes:       ParseScopes(tr.Scope),
	}
	if tr.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return creds, nil
}
