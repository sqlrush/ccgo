package remoteauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
)

// TestCombinedProviderPrefersCached verifies that when cached credentials
// (with a non-empty AccessToken) exist, the combined provider selects the
// refresh-only path and does NOT call the Authorizer.
func TestCombinedProviderPrefersCached(t *testing.T) {
	authorizerCalled := false
	cached := &memStore{creds: auth.Credentials{Source: auth.SourceOAuth, AccessToken: "have"}}
	prov := CombinedAccessTokenProvider(CombinedOptions{
		StoreFor: func(string, contracts.MCPServer) auth.CredentialStore { return cached },
		Authorizer: &countingAuthorizer{called: &authorizerCalled},
	})
	tp, err := prov(context.Background(), "srv", testServer())
	if err != nil {
		t.Fatalf("provider err: %v", err)
	}
	tok, err := tp.CurrentAccessToken(context.Background())
	if err != nil || tok != "have" {
		t.Fatalf("token = %q err=%v want have", tok, err)
	}
	if authorizerCalled {
		t.Fatal("Authorizer.Authorize was called despite cached credentials being present")
	}
}

// TestCombinedProviderNilOAuthReturnsNil verifies that a server without an
// OAuth configuration block yields a nil AccessTokenProvider (no auth header).
func TestCombinedProviderNilOAuthReturnsNil(t *testing.T) {
	prov := CombinedAccessTokenProvider(CombinedOptions{
		StoreFor: func(string, contracts.MCPServer) auth.CredentialStore { return &memStore{} },
	})
	tp, err := prov(context.Background(), "srv", contracts.MCPServer{Type: "http", URL: "https://x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if tp != nil {
		t.Fatal("expected nil provider for server without oauth")
	}
}

// TestCombinedProviderAcquisitionPath verifies that when no cached credentials
// exist (empty store), the combined provider attempts acquisition via the
// Authorizer. A fake httptest server simulates the full OAuth discovery flow.
func TestCombinedProviderAcquisitionPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		base := "https://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"` + base + `","authorization_servers":["` + base + `"]}`))
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		base := "https://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issuer":"` + base + `","authorization_endpoint":"` + base + `/authorize","token_endpoint":"` + base + `/token","registration_endpoint":"` + base + `/register"}`))
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"combined-client"}`))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"COMBINED_AT","refresh_token":"COMBINED_RT","expires_in":3600}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	store := &memStore{} // empty — triggers acquisition
	authorizerCalled := false
	authz := &countingAuthorizer{called: &authorizerCalled}

	prov := CombinedAccessTokenProvider(CombinedOptions{
		StoreFor:   func(string, contracts.MCPServer) auth.CredentialStore { return store },
		Authorizer: authz,
		HTTPClient: srv.Client(),
		CallbackPort: 7790,
	})
	srv2 := contracts.MCPServer{
		Type:  "http",
		URL:   srv.URL,
		OAuth: &contracts.MCPOAuthConfig{},
	}
	tp, err := prov(context.Background(), "srv", srv2)
	if err != nil {
		t.Fatalf("provider err: %v", err)
	}
	tok, err := tp.CurrentAccessToken(context.Background())
	if err != nil {
		t.Fatalf("CurrentAccessToken: %v", err)
	}
	if tok != "COMBINED_AT" {
		t.Fatalf("token = %q, want COMBINED_AT", tok)
	}
	if !authorizerCalled {
		t.Fatal("expected Authorizer to be called for empty credentials; it was not")
	}
}
