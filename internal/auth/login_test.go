package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// fakeBrowser, instead of opening a browser, parses the authorize URL,
// extracts the redirect_uri + state, and GETs the loopback callback — exactly
// what a real IdP would do after the user approves.
type fakeBrowser struct{ t *testing.T }

func (b fakeBrowser) Open(authURL string) error {
	u, err := url.Parse(authURL)
	if err != nil {
		return err
	}
	q := u.Query()
	redirect := q.Get("redirect_uri")
	state := q.Get("state")
	go func() {
		resp, err := http.Get(redirect + "?code=GRANTED&state=" + url.QueryEscape(state))
		if err == nil {
			resp.Body.Close()
		}
	}()
	return nil
}

// memStore is an in-memory CredentialStore for tests.
type memStore struct{ saved Credentials }

func (m *memStore) Load(context.Context) (Credentials, error)  { return m.saved, nil }
func (m *memStore) Save(_ context.Context, c Credentials) error { m.saved = c; return nil }
func (m *memStore) Delete(context.Context) error               { m.saved = Credentials{}; return nil }

func TestRunLoginFlow(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["code"] != "GRANTED" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"AT","refresh_token":"RT","expires_in":3600,"scope":"user:profile user:inference"}`))
	}))
	defer tokenSrv.Close()

	cfg := ProductionOAuthConfig()
	cfg.TokenURL = tokenSrv.URL

	store := &memStore{}
	var sawURL string
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	creds, err := RunLoginFlow(ctx, LoginOptions{
		Config:     cfg,
		HTTPClient: tokenSrv.Client(),
		Browser:    fakeBrowser{t: t},
		Store:      store,
		OnURL:      func(u string) { sawURL = u },
	})
	if err != nil {
		t.Fatalf("RunLoginFlow err: %v", err)
	}
	if creds.AccessToken != "AT" || creds.Source != SourceOAuth {
		t.Fatalf("creds = %+v", creds)
	}
	if store.saved.AccessToken != "AT" {
		t.Fatal("credentials not persisted to store")
	}
	if sawURL == "" {
		t.Fatal("OnURL was not called with the authorize URL")
	}
}

func TestRunLoginFlowBrowserFailureStillPrintsURL(t *testing.T) {
	// If the browser can't open, the flow must still surface the URL (manual
	// paste) rather than aborting before the user can authenticate.
	cfg := ProductionOAuthConfig()
	cfg.TokenURL = "http://unused"
	var sawURL string
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _ = RunLoginFlow(ctx, LoginOptions{
		Config:  cfg,
		Browser: failingBrowser{},
		Store:   &memStore{},
		OnURL:   func(u string) { sawURL = u },
	})
	if sawURL == "" {
		t.Fatal("authorize URL must be shown even when browser open fails")
	}
}

type failingBrowser struct{}

func (failingBrowser) Open(string) error { return http.ErrServerClosed }
