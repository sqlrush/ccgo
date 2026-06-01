package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const ClaudeAIInferenceScope = "user:inference"
const ClaudeAIProfileScope = "user:profile"
const OAuthBetaHeader = "oauth-2025-04-20"

var ConsoleOAuthScopes = []string{"org:create_api_key", ClaudeAIProfileScope}
var ClaudeAIOAuthScopes = []string{ClaudeAIProfileScope, ClaudeAIInferenceScope, "user:sessions:claude_code", "user:mcp_servers", "user:file_upload"}

type OAuthConfig struct {
	BaseAPIURL           string
	ConsoleAuthorizeURL  string
	ClaudeAIAuthorizeURL string
	ClaudeAIOrigin       string
	TokenURL             string
	APIKeyURL            string
	RolesURL             string
	ConsoleSuccessURL    string
	ClaudeAISuccessURL   string
	ManualRedirectURL    string
	ClientID             string
	MCPProxyURL          string
	MCPProxyPath         string
	ClientMetadataURL    string
}

func ProductionOAuthConfig() OAuthConfig {
	return OAuthConfig{
		BaseAPIURL:           "https://api.anthropic.com",
		ConsoleAuthorizeURL:  "https://platform.claude.com/oauth/authorize",
		ClaudeAIAuthorizeURL: "https://claude.com/cai/oauth/authorize",
		ClaudeAIOrigin:       "https://claude.ai",
		TokenURL:             "https://platform.claude.com/v1/oauth/token",
		APIKeyURL:            "https://api.anthropic.com/api/oauth/claude_cli/create_api_key",
		RolesURL:             "https://api.anthropic.com/api/oauth/claude_cli/roles",
		ConsoleSuccessURL:    "https://platform.claude.com/buy_credits?returnUrl=/oauth/code/success%3Fapp%3Dclaude-code",
		ClaudeAISuccessURL:   "https://platform.claude.com/oauth/code/success?app=claude-code",
		ManualRedirectURL:    "https://platform.claude.com/oauth/code/callback",
		ClientID:             "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
		MCPProxyURL:          "https://mcp-proxy.anthropic.com",
		MCPProxyPath:         "/v1/mcp/{server_id}",
		ClientMetadataURL:    "https://claude.ai/oauth/claude-code-client-metadata",
	}
}

type AuthURLParams struct {
	CodeChallenge     string
	State             string
	Port              int
	Manual            bool
	LoginWithClaudeAI bool
	InferenceOnly     bool
	OrgUUID           string
	LoginHint         string
	LoginMethod       string
	Config            OAuthConfig
}

func ParseScopes(scopeString string) []string {
	return strings.Fields(scopeString)
}

func ShouldUseClaudeAIAuth(scopes []string) bool {
	for _, scope := range scopes {
		if scope == ClaudeAIInferenceScope {
			return true
		}
	}
	return false
}

func AllOAuthScopes() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, scope := range append(append([]string{}, ConsoleOAuthScopes...), ClaudeAIOAuthScopes...) {
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func BuildAuthURL(params AuthURLParams) (string, error) {
	config := params.Config
	if config.ClientID == "" {
		config = ProductionOAuthConfig()
	}
	base := config.ConsoleAuthorizeURL
	if params.LoginWithClaudeAI {
		base = config.ClaudeAIAuthorizeURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("code", "true")
	q.Set("client_id", config.ClientID)
	q.Set("response_type", "code")
	if params.Manual {
		q.Set("redirect_uri", config.ManualRedirectURL)
	} else {
		q.Set("redirect_uri", fmt.Sprintf("http://localhost:%d/callback", params.Port))
	}
	scopes := AllOAuthScopes()
	if params.InferenceOnly {
		scopes = []string{ClaudeAIInferenceScope}
	}
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("code_challenge", params.CodeChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", params.State)
	if params.OrgUUID != "" {
		q.Set("orgUUID", params.OrgUUID)
	}
	if params.LoginHint != "" {
		q.Set("login_hint", params.LoginHint)
	}
	if params.LoginMethod != "" {
		q.Set("login_method", params.LoginMethod)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func GenerateCodeVerifier() (string, error) {
	return randomBase64URL(32)
}

func GenerateState() (string, error) {
	return randomBase64URL(32)
}

func GenerateCodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func IsOAuthTokenExpired(expiresAt time.Time, now time.Time) bool {
	if expiresAt.IsZero() {
		return true
	}
	if now.IsZero() {
		now = time.Now()
	}
	return !expiresAt.After(now)
}

func randomBase64URL(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
