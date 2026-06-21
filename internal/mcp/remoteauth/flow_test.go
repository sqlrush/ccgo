package remoteauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ccgo/internal/auth"
)

type fakeAuthorizer struct {
	wantState string
	code      string
	gotURL    string
}

func (f *fakeAuthorizer) Authorize(_ context.Context, authURL, _ string, state string) (string, error) {
	f.gotURL = authURL
	f.wantState = state
	return f.code, nil
}

func serverURLFromReq(r *http.Request) string {
	return "https://" + r.Host
}

func TestAcquireTokenFullFlow(t *testing.T) {
	mux := http.NewServeMux()
	// RFC 9728 protected-resource on the resource server.
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		base := serverURLFromReq(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"` + base + `","authorization_servers":["` + base + `"]}`))
	})
	// RFC 8414 authorization-server metadata.
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		base := serverURLFromReq(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issuer":"` + base + `","authorization_endpoint":"` + base + `/authorize","token_endpoint":"` + base + `/token","registration_endpoint":"` + base + `/register"}`))
	})
	// RFC 7591 DCR.
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"dyn-client"}`))
	})
	// Token endpoint (authorization_code). ExchangeAuthorizationCode sends JSON.
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			GrantType string `json:"grant_type"`
			Code      string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.GrantType != "authorization_code" || body.Code != "AUTHCODE" {
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"AT","refresh_token":"RT","expires_in":3600,"scope":"read"}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	authz := &fakeAuthorizer{code: "AUTHCODE"}
	creds, rc, err := AcquireToken(context.Background(), AcquireOptions{
		ServerURL:           srv.URL,
		ResourceMetadataURL: srv.URL + "/.well-known/oauth-protected-resource",
		CallbackPort:        7777,
		HTTPClient:          srv.Client(),
		Authorizer:          authz,
	})
	if err != nil {
		t.Fatalf("AcquireToken err: %v", err)
	}
	if creds.AccessToken != "AT" || creds.RefreshToken != "RT" || creds.Source != auth.SourceOAuth {
		t.Fatalf("creds wrong: %+v", creds)
	}
	if rc.ClientID != "dyn-client" {
		t.Fatalf("client_id = %q", rc.ClientID)
	}
	if !strings.Contains(authz.gotURL, "code_challenge=") || !strings.Contains(authz.gotURL, "client_id=dyn-client") {
		t.Fatalf("auth URL missing PKCE/client_id: %q", authz.gotURL)
	}
}

func TestAcquireTokenConfiguredClientIDSkipsDCR(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		base := serverURLFromReq(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"` + base + `","authorization_servers":["` + base + `"]}`))
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		base := serverURLFromReq(r)
		w.Header().Set("Content-Type", "application/json")
		// No registration_endpoint — DCR would fail if attempted.
		_, _ = w.Write([]byte(`{"issuer":"` + base + `","authorization_endpoint":"` + base + `/authorize","token_endpoint":"` + base + `/token"}`))
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		t.Error("DCR should not be called when ConfiguredClientID is set")
		http.Error(w, "unexpected", http.StatusInternalServerError)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ClientID string `json:"client_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.ClientID != "preconfigured" {
			http.Error(w, "wrong client_id", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"TOK","refresh_token":"RTK","expires_in":3600}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	authz := &fakeAuthorizer{code: "CODE2"}
	creds, rc, err := AcquireToken(context.Background(), AcquireOptions{
		ServerURL:          srv.URL,
		CallbackPort:       7778,
		HTTPClient:         srv.Client(),
		Authorizer:         authz,
		ConfiguredClientID: "preconfigured",
	})
	if err != nil {
		t.Fatalf("AcquireToken err: %v", err)
	}
	if creds.AccessToken != "TOK" {
		t.Fatalf("wrong access token: %q", creds.AccessToken)
	}
	if rc.ClientID != "preconfigured" {
		t.Fatalf("registered client_id wrong: %q", rc.ClientID)
	}
}

func TestAcquireTokenPKCSStateInURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		base := serverURLFromReq(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"` + base + `","authorization_servers":["` + base + `"]}`))
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		base := serverURLFromReq(r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issuer":"` + base + `","authorization_endpoint":"` + base + `/authorize","token_endpoint":"` + base + `/token","registration_endpoint":"` + base + `/register"}`))
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"pkce-client"}`))
	})
	var capturedVerifier string
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CodeVerifier string `json:"code_verifier"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		capturedVerifier = body.CodeVerifier
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"A","expires_in":3600}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	var capturedAuthURL string
	authz := &fakeAuthorizer{code: "C"}
	creds, _, err := AcquireToken(context.Background(), AcquireOptions{
		ServerURL:    srv.URL,
		CallbackPort: 7779,
		HTTPClient:   srv.Client(),
		Authorizer:   authz,
	})
	capturedAuthURL = authz.gotURL
	if err != nil {
		t.Fatalf("AcquireToken err: %v", err)
	}
	if creds.AccessToken != "A" {
		t.Fatalf("wrong token: %q", creds.AccessToken)
	}
	// Verify PKCE: the challenge in the auth URL must match the verifier sent to token endpoint.
	if capturedVerifier == "" {
		t.Fatal("code_verifier not sent to token endpoint")
	}
	if !strings.Contains(capturedAuthURL, "code_challenge=") {
		t.Fatalf("auth URL missing code_challenge: %q", capturedAuthURL)
	}
	if !strings.Contains(capturedAuthURL, "state=") {
		t.Fatalf("auth URL missing state: %q", capturedAuthURL)
	}
	// Verify that verifier produces the challenge that appeared in the URL.
	challenge := auth.GenerateCodeChallenge(capturedVerifier)
	if !strings.Contains(capturedAuthURL, "code_challenge="+challenge) {
		t.Fatalf("auth URL challenge does not match verifier %q → challenge %q; URL: %q", capturedVerifier, challenge, capturedAuthURL)
	}
}
