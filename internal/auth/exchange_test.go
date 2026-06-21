package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExchangeAuthorizationCodeSuccess(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600,"scope":"user:profile user:inference"}`))
	}))
	defer srv.Close()

	cfg := ProductionOAuthConfig()
	cfg.TokenURL = srv.URL

	creds, err := ExchangeAuthorizationCode(context.Background(), srv.Client(), cfg, ExchangeParams{
		Code:         "the-code",
		CodeVerifier: "the-verifier",
		RedirectURI:  "http://localhost:55555/callback",
		State:        "the-state",
	})
	if err != nil {
		t.Fatalf("exchange err: %v", err)
	}
	if creds.Source != SourceOAuth || creds.AccessToken != "at-1" || creds.RefreshToken != "rt-1" {
		t.Fatalf("creds = %+v", creds)
	}
	if creds.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt should be set from expires_in")
	}
	// Verify the request body matched the spec.
	if gotBody["grant_type"] != "authorization_code" || gotBody["code"] != "the-code" ||
		gotBody["code_verifier"] != "the-verifier" || gotBody["redirect_uri"] != "http://localhost:55555/callback" {
		t.Fatalf("request body = %#v", gotBody)
	}
	if gotBody["client_id"] == "" || gotBody["client_id"] == nil {
		t.Fatalf("client_id missing in body: %#v", gotBody)
	}
	// Verify state is included in the request body.
	if gotBody["state"] != "the-state" {
		t.Fatalf("state not sent in request body: %#v", gotBody)
	}
}

func TestExchangeAuthorizationCodeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","secret_hint":"super-secret"}`))
	}))
	defer srv.Close()
	cfg := ProductionOAuthConfig()
	cfg.TokenURL = srv.URL
	_, err := ExchangeAuthorizationCode(context.Background(), srv.Client(), cfg, ExchangeParams{Code: "x", CodeVerifier: "v", RedirectURI: "http://localhost:1/callback"})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	// Error must surface status but MUST NOT echo a token-bearing body wholesale.
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("error should mention status: %v", err)
	}
	// Verify error does not leak secrets from the response body.
	errMsg := err.Error()
	if strings.Contains(errMsg, "super-secret") {
		t.Fatalf("error leaked secret hint: %v", err)
	}
	if strings.Contains(errMsg, "secret_hint") {
		t.Fatalf("error leaked response body field: %v", err)
	}
}

func TestExchangeAuthorizationCodeMissingAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"refresh_token":"rt"}`))
	}))
	defer srv.Close()
	cfg := ProductionOAuthConfig()
	cfg.TokenURL = srv.URL
	if _, err := ExchangeAuthorizationCode(context.Background(), srv.Client(), cfg, ExchangeParams{Code: "x", CodeVerifier: "v", RedirectURI: "http://localhost:1/callback"}); err == nil {
		t.Fatal("expected error when access_token absent")
	}
}

func TestExchangeAuthorizationCodeValidatesParams(t *testing.T) {
	cfg := ProductionOAuthConfig()
	cfg.TokenURL = "http://unused"
	if _, err := ExchangeAuthorizationCode(context.Background(), http.DefaultClient, cfg, ExchangeParams{Code: "", CodeVerifier: "v", RedirectURI: "r"}); err == nil {
		t.Fatal("empty code must be rejected before any network call")
	}
}
