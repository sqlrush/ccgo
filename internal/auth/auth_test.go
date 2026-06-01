package auth

import "testing"

func TestFromEnvAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "key")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	creds := FromEnv()
	if creds.Source != SourceAPIKey || creds.APIKey != "key" {
		t.Fatalf("creds = %#v", creds)
	}
	if err := creds.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFromEnvOAuth(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "refresh")
	t.Setenv("CLAUDE_CODE_OAUTH_SCOPES", "a,b c")
	creds := FromEnv()
	if creds.Source != SourceOAuth || creds.RefreshToken != "refresh" {
		t.Fatalf("creds = %#v", creds)
	}
	if len(creds.Scopes) != 3 {
		t.Fatalf("scopes = %#v", creds.Scopes)
	}
}
