package auth

import (
	"net/url"
	"testing"
	"time"
)

func TestBuildAuthURL(t *testing.T) {
	got, err := BuildAuthURL(AuthURLParams{
		CodeChallenge:     "challenge",
		State:             "state",
		Port:              12345,
		LoginWithClaudeAI: true,
		OrgUUID:           "org_1",
		LoginHint:         "user@example.com",
		LoginMethod:       "sso",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.String() == "" || q.Get("client_id") == "" || q.Get("redirect_uri") != "http://localhost:12345/callback" {
		t.Fatalf("url = %s", got)
	}
	if q.Get("scope") != "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload" {
		t.Fatalf("scope = %q", q.Get("scope"))
	}
	if q.Get("orgUUID") != "org_1" || q.Get("login_hint") != "user@example.com" || q.Get("login_method") != "sso" {
		t.Fatalf("query = %s", u.RawQuery)
	}
}

func TestPKCEHelpers(t *testing.T) {
	verifier := "test-verifier"
	challenge := GenerateCodeChallenge(verifier)
	if challenge == "" || challenge == verifier {
		t.Fatalf("challenge = %q", challenge)
	}
	randomVerifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if len(randomVerifier) < 40 {
		t.Fatalf("verifier too short: %q", randomVerifier)
	}
}

func TestOAuthScopesAndExpiry(t *testing.T) {
	if !ShouldUseClaudeAIAuth(ParseScopes("user:profile user:inference")) {
		t.Fatalf("expected claude.ai auth")
	}
	if !IsOAuthTokenExpired(time.Now().Add(-time.Second), time.Now()) {
		t.Fatalf("expired token not detected")
	}
	if IsOAuthTokenExpired(time.Now().Add(time.Hour), time.Now()) {
		t.Fatalf("fresh token marked expired")
	}
}
