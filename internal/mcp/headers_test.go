package mcp

import (
	"testing"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
)

func TestTransportHeadersAddsAuthTokenAndOAuthBeta(t *testing.T) {
	headers := TransportHeaders(contracts.MCPServer{
		AuthToken: "token",
		OAuth:     &contracts.MCPOAuthConfig{ClientID: "client"},
	})
	if headers["Authorization"] != "Bearer token" {
		t.Fatalf("authorization = %#v", headers)
	}
	if headers["anthropic-beta"] != auth.OAuthBetaHeader {
		t.Fatalf("beta = %#v", headers)
	}
}

func TestTransportHeadersPreservesExplicitAuthorizationAndDedupesBeta(t *testing.T) {
	headers := TransportHeaders(contracts.MCPServer{
		Headers: map[string]string{
			"authorization":  "Bearer explicit",
			"anthropic-beta": "other," + auth.OAuthBetaHeader,
		},
		AuthToken: "ignored",
		OAuth:     &contracts.MCPOAuthConfig{ClientID: "client"},
	})
	if headers["authorization"] != "Bearer explicit" {
		t.Fatalf("authorization = %#v", headers)
	}
	if headers["anthropic-beta"] != "other,"+auth.OAuthBetaHeader {
		t.Fatalf("beta = %#v", headers)
	}
}

func TestBearerHeaderValuePreservesExistingScheme(t *testing.T) {
	if got := bearerHeaderValue("Bearer existing"); got != "Bearer existing" {
		t.Fatalf("bearer = %q", got)
	}
	if got := bearerHeaderValue("raw"); got != "Bearer raw" {
		t.Fatalf("bearer = %q", got)
	}
}
