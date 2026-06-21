package auth

import (
	"errors"
	"os"
	"strings"
	"time"
)

type CredentialSource string

const (
	SourceNone   CredentialSource = "none"
	SourceAPIKey CredentialSource = "api_key"
	SourceOAuth  CredentialSource = "oauth"
)

type Credentials struct {
	Source           CredentialSource `json:"source"`
	APIKey           string           `json:"api_key,omitempty"`
	AccessToken      string           `json:"access_token,omitempty"`
	RefreshToken     string           `json:"refresh_token,omitempty"`
	Scopes           []string         `json:"scopes,omitempty"`
	ExpiresAt        time.Time        `json:"expires_at,omitempty"`
	// TokenEndpointURL is the token endpoint discovered during remote MCP OAuth
	// acquisition. When non-empty, token refresh must target this URL instead of
	// the Anthropic production endpoint so that third-party OAuth tokens are
	// refreshed against the correct authorization server.
	// omitempty keeps the field absent in existing Anthropic OAuth credentials,
	// preserving backward-compatible serialization.
	TokenEndpointURL string           `json:"token_endpoint_url,omitempty"`
}

func FromEnv() Credentials {
	if key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); key != "" {
		return Credentials{Source: SourceAPIKey, APIKey: key}
	}
	if refresh := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN")); refresh != "" {
		scopes := strings.FieldsFunc(os.Getenv("CLAUDE_CODE_OAUTH_SCOPES"), func(r rune) bool {
			return r == ',' || r == ' '
		})
		return Credentials{Source: SourceOAuth, RefreshToken: refresh, Scopes: scopes}
	}
	return Credentials{Source: SourceNone}
}

func (c Credentials) Validate() error {
	switch c.Source {
	case SourceAPIKey:
		if strings.TrimSpace(c.APIKey) == "" {
			return errors.New("api key source selected without api key")
		}
	case SourceOAuth:
		if strings.TrimSpace(c.AccessToken) == "" && strings.TrimSpace(c.RefreshToken) == "" {
			return errors.New("oauth source selected without tokens")
		}
	}
	return nil
}
