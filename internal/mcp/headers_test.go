package mcp

import (
	"context"
	"testing"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
)

type testAccessTokenProvider struct {
	token string
}

func (p testAccessTokenProvider) CurrentAccessToken(context.Context) (string, error) {
	return p.token, nil
}

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

func TestAuthTokenHeaderProvider(t *testing.T) {
	provider := AuthTokenHeaderProvider(func(ctx context.Context, server contracts.MCPServer) (string, error) {
		if server.Name != "remote" {
			t.Fatalf("server = %#v", server)
		}
		return "fresh", nil
	})
	headers, err := provider(context.Background(), contracts.MCPServer{Name: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	if headers["Authorization"] != "Bearer fresh" {
		t.Fatalf("headers = %#v", headers)
	}
}

func TestOAuthServerHeaderProviderUsesOAuthServerToken(t *testing.T) {
	provider := OAuthServerHeaderProvider(func(ctx context.Context, name string, server contracts.MCPServer) (AccessTokenProvider, error) {
		if name != "remote" || server.OAuth == nil {
			t.Fatalf("provider input name=%q server=%#v", name, server)
		}
		return testAccessTokenProvider{token: "fresh"}, nil
	})
	headers, err := provider(context.Background(), "remote", contracts.MCPServer{OAuth: &contracts.MCPOAuthConfig{ClientID: "client"}})
	if err != nil {
		t.Fatal(err)
	}
	if headers["Authorization"] != "Bearer fresh" {
		t.Fatalf("headers = %#v", headers)
	}
	headers, err = provider(context.Background(), "plain", contracts.MCPServer{})
	if err != nil {
		t.Fatal(err)
	}
	if headers != nil {
		t.Fatalf("plain headers = %#v", headers)
	}
}

func TestMergeTransportHeadersAllowsDynamicOverride(t *testing.T) {
	headers := MergeTransportHeaders(
		map[string]string{"authorization": "Bearer old", "X-Static": "yes"},
		map[string]string{"Authorization": "Bearer new", "X-Dynamic": "yes"},
	)
	if headers["Authorization"] != "Bearer new" || headers["X-Static"] != "yes" || headers["X-Dynamic"] != "yes" {
		t.Fatalf("headers = %#v", headers)
	}
	if _, ok := headers["authorization"]; ok {
		t.Fatalf("old authorization key still present: %#v", headers)
	}
	if got := MergeTransportHeaders(nil, nil); got != nil {
		t.Fatalf("empty headers = %#v", got)
	}
}
