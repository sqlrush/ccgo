package remoteauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverProtectedResource(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"https://api.example.com","authorization_servers":["https://as.example.com"]}`))
	}))
	defer srv.Close()

	md, err := DiscoverProtectedResource(context.Background(), srv.Client(), srv.URL+"/.well-known/oauth-protected-resource", 1<<20)
	if err != nil {
		t.Fatalf("discover err: %v", err)
	}
	if len(md.AuthorizationServers) != 1 || md.AuthorizationServers[0] != "https://as.example.com" {
		t.Fatalf("authorization_servers = %v", md.AuthorizationServers)
	}
}

func TestDiscoverAuthorizationServer(t *testing.T) {
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Use the server's own URL as issuer so RFC 8414 §3.3 issuer binding passes.
		_, _ = fmt.Fprintf(w, `{
			"issuer":%q,
			"authorization_endpoint":%q,
			"token_endpoint":%q,
			"registration_endpoint":%q,
			"code_challenge_methods_supported":["S256"]
		}`, srvURL, srvURL+"/authorize", srvURL+"/token", srvURL+"/register")
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	md, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL, 1<<20)
	if err != nil {
		t.Fatalf("discover err: %v", err)
	}
	if md.TokenEndpoint != srv.URL+"/token" || md.RegistrationEndpoint != srv.URL+"/register" {
		t.Fatalf("endpoints wrong: %+v", md)
	}
}

func TestDiscoverAuthorizationServerRejectsMissingTokenEndpoint(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"https://as.example.com","authorization_endpoint":"https://as.example.com/a"}`))
	}))
	defer srv.Close()
	if _, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL, 1<<20); err == nil {
		t.Fatal("expected validation error for missing token_endpoint")
	}
}

func TestDiscoverAuthorizationServerRejectsBodyOverLimit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Write more bytes than maxBytes allows.
		for i := 0; i < 110; i++ {
			_, _ = w.Write([]byte("xxxxxxxxxx"))
		}
	}))
	defer srv.Close()
	if _, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL, 100); err == nil {
		t.Fatal("expected error for oversized body")
	}
}

func TestDiscoverAuthorizationServerPathAwareVariant(t *testing.T) {
	var srvURL string
	mux := http.NewServeMux()
	// Path-aware variant: /.well-known/oauth-authorization-server/mypath
	mux.HandleFunc("/.well-known/oauth-authorization-server/mypath", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Issuer must match the discovery issuer (srv.URL+"/mypath") for RFC 8414 §3.3.
		issuer := srvURL + "/mypath"
		_, _ = fmt.Fprintf(w, `{
			"issuer":%q,
			"authorization_endpoint":%q,
			"token_endpoint":%q
		}`, issuer, srvURL+"/authorize", srvURL+"/token")
	})
	// Root well-known returns 404.
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	md, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL+"/mypath", 1<<20)
	if err != nil {
		t.Fatalf("discover err: %v", err)
	}
	if md.TokenEndpoint != srv.URL+"/token" {
		t.Fatalf("token_endpoint wrong: %q", md.TokenEndpoint)
	}
}

// TestDiscoverAuthServerRejectsHTTPTokenEndpoint proves that metadata
// advertising a plaintext http token_endpoint on a non-loopback host is
// rejected (OAuth downgrade / MITM protection).
func TestDiscoverAuthServerRejectsHTTPTokenEndpoint(t *testing.T) {
	var srvURL string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"issuer":%q,
			"authorization_endpoint":%q,
			"token_endpoint":"http://attacker.example.com/token"
		}`, srvURL, srvURL+"/authorize")
	}))
	defer srv.Close()
	srvURL = srv.URL

	_, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL, 1<<20)
	if err == nil {
		t.Fatal("expected validation error for http token_endpoint on non-loopback host")
	}
	if !strings.Contains(err.Error(), "token_endpoint") {
		t.Fatalf("error should mention token_endpoint, got: %v", err)
	}
}

// TestDiscoverAuthServerAcceptsLoopbackHTTP proves that a loopback http server
// (httptest.NewServer, non-TLS) is accepted so tests do not require TLS.
func TestDiscoverAuthServerAcceptsLoopbackHTTP(t *testing.T) {
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"issuer":%q,
			"authorization_endpoint":%q,
			"token_endpoint":%q
		}`, srvURL, srvURL+"/authorize", srvURL+"/token")
	})
	// Non-TLS server — uses http://127.0.0.1:PORT.
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	md, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL, 1<<20)
	if err != nil {
		t.Fatalf("loopback http should be accepted, got err: %v", err)
	}
	if md.TokenEndpoint != srv.URL+"/token" {
		t.Fatalf("token_endpoint wrong: %q", md.TokenEndpoint)
	}
}

// TestDiscoverAuthServerRejectsIssuerMismatch proves that a metadata document
// claiming a different issuer than the one used for discovery is rejected
// (RFC 8414 §3.3 confused-deputy protection).
func TestDiscoverAuthServerRejectsIssuerMismatch(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Claim a different issuer than the discovery URL.
		_, _ = w.Write([]byte(`{
			"issuer":"https://evil.example.com",
			"authorization_endpoint":"https://evil.example.com/authorize",
			"token_endpoint":"https://evil.example.com/token"
		}`))
	}))
	defer srv.Close()

	_, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL, 1<<20)
	if err == nil {
		t.Fatal("expected issuer binding error for mismatched issuer")
	}
	if !strings.Contains(err.Error(), "issuer binding mismatch") {
		t.Fatalf("error should mention issuer binding mismatch, got: %v", err)
	}
}
