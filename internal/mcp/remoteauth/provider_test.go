package remoteauth

import (
	"context"
	"testing"
	"time"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
)

// memStore is an in-memory CredentialStore for tests.
type memStore struct{ creds auth.Credentials }

func (m *memStore) Load(_ context.Context) (auth.Credentials, error) { return m.creds, nil }
func (m *memStore) Save(_ context.Context, c auth.Credentials) error {
	m.creds = c
	return nil
}
func (m *memStore) Delete(_ context.Context) error {
	m.creds = auth.Credentials{}
	return nil
}

func testServer() contracts.MCPServer {
	return contracts.MCPServer{
		Type:  "http",
		URL:   "https://x",
		OAuth: &contracts.MCPOAuthConfig{},
	}
}

func TestProviderUsesCachedToken(t *testing.T) {
	store := &memStore{creds: auth.Credentials{Source: auth.SourceOAuth, AccessToken: "cached"}}
	prov := RemoteOAuthAccessTokenProvider(store, AcquireOptions{})
	tp, err := prov(context.Background(), "srv", testServer())
	if err != nil {
		t.Fatalf("provider err: %v", err)
	}
	tok, err := tp.CurrentAccessToken(context.Background())
	if err != nil || tok != "cached" {
		t.Fatalf("token = %q err=%v want cached", tok, err)
	}
}

func TestProviderAcquiresWhenEmpty(t *testing.T) {
	acquired := false
	store := &memStore{}
	// Use a fake Authorizer that records the call, and serve a fake token
	// via httptest so AcquireToken can complete.
	// For simplicity we test this by providing a pre-populated store
	// in one variant; for the acquire path we need a real token server.
	// Here we verify that when creds are empty but we cannot call AcquireToken
	// (no server, no authorizer), we get an error (i.e. the provider did attempt
	// acquisition rather than silently returning empty).
	_ = acquired
	prov := RemoteOAuthAccessTokenProvider(store, AcquireOptions{
		// No Authorizer → AcquireToken will fail with a clear error.
		ServerURL: "https://example.com",
	})
	_, err := prov(context.Background(), "srv", testServer())
	if err == nil {
		t.Fatal("expected error when no authorizer configured")
	}
}

func TestProviderSkipsNonOAuthServer(t *testing.T) {
	store := &memStore{creds: auth.Credentials{Source: auth.SourceOAuth, AccessToken: "cached"}}
	prov := RemoteOAuthAccessTokenProvider(store, AcquireOptions{})
	srv := contracts.MCPServer{Type: "http", URL: "https://x"} // no OAuth field
	tp, err := prov(context.Background(), "srv", srv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp != nil {
		t.Fatal("expected nil provider for non-OAuth server")
	}
}

func TestProviderDoesNotReacquireWithValidCachedToken(t *testing.T) {
	// Token is valid (no expiry) — provider must not try to acquire.
	acquireCalled := false
	fakeAuth := &fakeAuthorizer{code: "SHOULD_NOT_BE_CALLED"}
	_ = fakeAuth
	store := &memStore{creds: auth.Credentials{
		Source:      auth.SourceOAuth,
		AccessToken: "valid",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}}
	prov := RemoteOAuthAccessTokenProvider(store, AcquireOptions{
		Authorizer: &countingAuthorizer{called: &acquireCalled},
	})
	tp, err := prov(context.Background(), "srv", testServer())
	if err != nil {
		t.Fatalf("provider err: %v", err)
	}
	tok, err := tp.CurrentAccessToken(context.Background())
	if err != nil || tok != "valid" {
		t.Fatalf("token = %q err=%v", tok, err)
	}
	if acquireCalled {
		t.Fatal("Authorizer.Authorize was called despite valid cached credentials")
	}
}

// countingAuthorizer records whether Authorize was called.
type countingAuthorizer struct{ called *bool }

func (c *countingAuthorizer) Authorize(_ context.Context, _, _, _ string) (string, error) {
	*c.called = true
	return "fake-code", nil
}
