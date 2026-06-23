package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"ccgo/internal/auth"
)

// stubStore is a no-op CredentialStore for unit tests that avoids touching
// any real keychain or filesystem.
type stubStore struct{}

func (s *stubStore) Save(_ context.Context, _ auth.Credentials) error { return nil }
func (s *stubStore) Load(_ context.Context) (auth.Credentials, error) {
	return auth.Credentials{Source: auth.SourceNone}, nil
}
func (s *stubStore) Delete(_ context.Context) error { return nil }

// --- F3-C01: auth login advanced flags ---

func TestAuthLoginConsoleFlag(t *testing.T) {
	// --console should NOT use claude.ai login (loginWithClaudeAI=false).
	// We can't run the real OAuth flow, so we test that the function accepts
	// --console without flag-parse error, reaches the OAuth step, and fails
	// gracefully (no real callback server succeeds).
	ctx, cancel := context.WithTimeout(context.Background(), 100)
	defer cancel()

	var stdout, stderr bytes.Buffer
	// Provide --yes so the consent gate doesn't block.
	// The OAuth flow will fail immediately because no browser or callback happens.
	code := runAuthLogin(ctx, []string{"--yes", "--console"}, &stubStore{}, strings.NewReader(""), &stdout, &stderr)
	// Must fail (no real OAuth) but NOT with exit 2 (which would mean flag error).
	if code == 2 {
		t.Fatalf("--console caused flag-parse error; stderr: %q", stderr.String())
	}
	// Must not print "Login successful." since the flow can't complete.
	if strings.Contains(stdout.String(), "Login successful.") {
		t.Fatal("expected flow to not complete without real OAuth")
	}
}

func TestAuthLoginClaudeAIFlag(t *testing.T) {
	// --claudeai is the explicit default; same as omitting it.
	ctx, cancel := context.WithTimeout(context.Background(), 100)
	defer cancel()

	var stdout, stderr bytes.Buffer
	code := runAuthLogin(ctx, []string{"--yes", "--claudeai"}, &stubStore{}, strings.NewReader(""), &stdout, &stderr)
	if code == 2 {
		t.Fatalf("--claudeai caused flag-parse error; stderr: %q", stderr.String())
	}
}

func TestAuthLoginConsolePlusClaudeAIMutualExclusion(t *testing.T) {
	// --console and --claudeai together must produce exit 1 (error), not 0.
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	code := runAuthLogin(ctx, []string{"--yes", "--console", "--claudeai"}, &stubStore{}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit when --console and --claudeai both specified")
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "--console") || !strings.Contains(combined, "--claudeai") {
		t.Fatalf("expected mutual-exclusion error message; combined output: %q", combined)
	}
}

func TestAuthLoginSSOFlag(t *testing.T) {
	// --sso flag should be accepted without flag-parse error.
	ctx, cancel := context.WithTimeout(context.Background(), 100)
	defer cancel()

	var stdout, stderr bytes.Buffer
	code := runAuthLogin(ctx, []string{"--yes", "--sso"}, &stubStore{}, strings.NewReader(""), &stdout, &stderr)
	if code == 2 {
		t.Fatalf("--sso caused flag-parse error; stderr: %q", stderr.String())
	}
}

func TestAuthLoginEmailFlag(t *testing.T) {
	// --email flag should be accepted without flag-parse error.
	ctx, cancel := context.WithTimeout(context.Background(), 100)
	defer cancel()

	var stdout, stderr bytes.Buffer
	code := runAuthLogin(ctx, []string{"--yes", "--email", "user@example.com"}, &stubStore{}, strings.NewReader(""), &stdout, &stderr)
	if code == 2 {
		t.Fatalf("--email caused flag-parse error; stderr: %q", stderr.String())
	}
}

// --- F3-C02: auth status --json ---

func TestAuthStatusJSONFlag(t *testing.T) {
	// --json should produce valid JSON output.
	ctx := context.Background()
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key-for-status")

	var stdout, stderr bytes.Buffer
	code := runAuthStatus(ctx, "", &stdout, &stderr, true /* jsonMode */)
	if code != 0 {
		t.Fatalf("expected exit 0 when API key set, got %d; stderr: %q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "loggedIn") {
		t.Fatalf("JSON output missing 'loggedIn' field: %q", out)
	}
	if !strings.Contains(out, "authMethod") {
		t.Fatalf("JSON output missing 'authMethod' field: %q", out)
	}
}

func TestAuthStatusJSONUnauthenticated(t *testing.T) {
	// When no credentials exist, --json should exit 1 with loggedIn=false.
	ctx := context.Background()
	t.Setenv("ANTHROPIC_API_KEY", "")
	// Also clear the OAuth env vars.
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_OAUTH_SCOPES", "")

	var stdout, stderr bytes.Buffer
	code := runAuthStatus(ctx, "", &stdout, &stderr, true /* jsonMode */)
	if code != 1 {
		t.Fatalf("expected exit 1 when not authenticated; got %d; stderr: %q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "false") {
		t.Fatalf("JSON output should contain loggedIn:false: %q", out)
	}
}

func TestAuthStatusPlainText(t *testing.T) {
	// Without --json, text mode should still work (backward-compat).
	ctx := context.Background()
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key-plain")

	var stdout, stderr bytes.Buffer
	code := runAuthStatus(ctx, "", &stdout, &stderr, false /* jsonMode */)
	if code != 0 {
		t.Fatalf("expected exit 0; got %d; stderr: %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Authenticated") {
		t.Fatalf("expected 'Authenticated' in plain-text output: %q", stdout.String())
	}
}
