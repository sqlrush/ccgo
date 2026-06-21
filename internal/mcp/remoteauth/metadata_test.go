package remoteauth

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer":"https://as.example.com",
			"authorization_endpoint":"https://as.example.com/authorize",
			"token_endpoint":"https://as.example.com/token",
			"registration_endpoint":"https://as.example.com/register",
			"code_challenge_methods_supported":["S256"]
		}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	md, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL, 1<<20)
	if err != nil {
		t.Fatalf("discover err: %v", err)
	}
	if md.TokenEndpoint != "https://as.example.com/token" || md.RegistrationEndpoint != "https://as.example.com/register" {
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
	mux := http.NewServeMux()
	// Path-aware variant: /.well-known/oauth-authorization-server/mypath
	mux.HandleFunc("/.well-known/oauth-authorization-server/mypath", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer":"https://as.example.com",
			"authorization_endpoint":"https://as.example.com/authorize",
			"token_endpoint":"https://as.example.com/token"
		}`))
	})
	// Root well-known returns 404.
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	md, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL+"/mypath", 1<<20)
	if err != nil {
		t.Fatalf("discover err: %v", err)
	}
	if md.TokenEndpoint != "https://as.example.com/token" {
		t.Fatalf("token_endpoint wrong: %q", md.TokenEndpoint)
	}
}
