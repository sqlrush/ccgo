package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const defaultOAuthRefreshMargin = time.Minute
const defaultOAuthTokenResponseLimit int64 = 1 << 20

type OAuthTokenProviderOptions struct {
	Credentials      Credentials
	Config           OAuthConfig
	HTTPClient       *http.Client
	Now              func() time.Time
	RefreshMargin    time.Duration
	OnCredentials    func(Credentials)
	MaxResponseBytes int64
}

type OAuthTokenProvider struct {
	client           *http.Client
	config           OAuthConfig
	now              func() time.Time
	refreshMargin    time.Duration
	onCredentials    func(Credentials)
	maxResponseBytes int64

	mu          sync.Mutex
	credentials Credentials
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

func NewOAuthTokenProvider(options OAuthTokenProviderOptions) *OAuthTokenProvider {
	config := options.Config
	if config.TokenURL == "" || config.ClientID == "" {
		production := ProductionOAuthConfig()
		if config.TokenURL == "" {
			config.TokenURL = production.TokenURL
		}
		if config.ClientID == "" {
			config.ClientID = production.ClientID
		}
	}
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	refreshMargin := options.RefreshMargin
	if refreshMargin == 0 {
		refreshMargin = defaultOAuthRefreshMargin
	}
	maxResponseBytes := options.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultOAuthTokenResponseLimit
	}
	return &OAuthTokenProvider{
		client:           client,
		config:           config,
		now:              now,
		refreshMargin:    refreshMargin,
		onCredentials:    options.OnCredentials,
		maxResponseBytes: maxResponseBytes,
		credentials:      options.Credentials,
	}
}

func (p *OAuthTokenProvider) CurrentAccessToken(ctx context.Context) (string, error) {
	if p == nil {
		return "", nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if token := strings.TrimSpace(p.credentials.AccessToken); token != "" && !p.accessTokenNeedsRefreshLocked() {
		return token, nil
	}
	return p.refreshAccessTokenLocked(ctx)
}

func (p *OAuthTokenProvider) RefreshAccessToken(ctx context.Context) (string, error) {
	if p == nil {
		return "", nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refreshAccessTokenLocked(ctx)
}

func (p *OAuthTokenProvider) Credentials() Credentials {
	if p == nil {
		return Credentials{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.credentials
}

func (p *OAuthTokenProvider) accessTokenNeedsRefreshLocked() bool {
	if p.credentials.ExpiresAt.IsZero() {
		return false
	}
	now := p.now()
	if now.IsZero() {
		now = time.Now()
	}
	return !p.credentials.ExpiresAt.After(now.Add(p.refreshMargin))
}

func (p *OAuthTokenProvider) refreshAccessTokenLocked(ctx context.Context) (string, error) {
	refreshToken := strings.TrimSpace(p.credentials.RefreshToken)
	if refreshToken == "" {
		if token := strings.TrimSpace(p.credentials.AccessToken); token != "" {
			return token, nil
		}
		return "", fmt.Errorf("oauth refresh token is required")
	}
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", p.config.ClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	limit := p.maxResponseBytes
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(body)) > limit {
		return "", fmt.Errorf("oauth token response exceeds %d bytes", limit)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("oauth token refresh status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResponse oauthTokenResponse
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", fmt.Errorf("decode oauth token response: %w", err)
	}
	accessToken := strings.TrimSpace(tokenResponse.AccessToken)
	if accessToken == "" {
		return "", fmt.Errorf("oauth token response missing access_token")
	}
	p.credentials.Source = SourceOAuth
	p.credentials.AccessToken = accessToken
	if refreshToken := strings.TrimSpace(tokenResponse.RefreshToken); refreshToken != "" {
		p.credentials.RefreshToken = refreshToken
	}
	if scopes := ParseScopes(tokenResponse.Scope); len(scopes) > 0 {
		p.credentials.Scopes = scopes
	}
	if tokenResponse.ExpiresIn > 0 {
		p.credentials.ExpiresAt = p.now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
	} else {
		p.credentials.ExpiresAt = time.Time{}
	}
	if p.onCredentials != nil {
		p.onCredentials(p.credentials)
	}
	return accessToken, nil
}
