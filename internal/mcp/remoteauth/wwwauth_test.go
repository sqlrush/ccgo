package remoteauth

import "testing"

func TestParseWWWAuthenticate(t *testing.T) {
	header := `Bearer realm="https://api.example.com", scope="read write", resource_metadata="https://api.example.com/.well-known/oauth-protected-resource"`
	url, scope := ParseWWWAuthenticate(header)
	if url != "https://api.example.com/.well-known/oauth-protected-resource" {
		t.Fatalf("resource_metadata = %q", url)
	}
	if scope != "read write" {
		t.Fatalf("scope = %q", scope)
	}
}

func TestParseWWWAuthenticateEmpty(t *testing.T) {
	if url, _ := ParseWWWAuthenticate("Bearer"); url != "" {
		t.Fatalf("expected empty url, got %q", url)
	}
}

func TestParseWWWAuthenticateUnquoted(t *testing.T) {
	header := `Bearer resource_metadata=https://api.example.com/meta scope=read`
	url, scope := ParseWWWAuthenticate(header)
	if url != "https://api.example.com/meta" {
		t.Fatalf("resource_metadata = %q", url)
	}
	if scope != "read" {
		t.Fatalf("scope = %q", scope)
	}
}

func TestParseWWWAuthenticateEmptyString(t *testing.T) {
	url, scope := ParseWWWAuthenticate("")
	if url != "" || scope != "" {
		t.Fatalf("expected empty results, got url=%q scope=%q", url, scope)
	}
}
