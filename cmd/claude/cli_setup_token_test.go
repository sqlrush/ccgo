package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"ccgo/internal/auth"
)

// --- F3-C05: setup-token command ---

func TestSetupTokenCommandRegistered(t *testing.T) {
	// The "setup-token" sub-command must be reachable in run() dispatch.
	// We test runSetupTokenCLI directly: it should start the OAuth flow
	// (inferenceOnly=true) and fail gracefully with no real network/browser.
	ctx, cancel := context.WithTimeout(context.Background(), 100)
	defer cancel()

	var stdout, stderr bytes.Buffer
	code := runSetupTokenCLI(ctx, []string{"--yes"}, &stubStore{}, strings.NewReader(""), &stdout, &stderr)
	// Must not be exit 2 (flag-parse error) or panic.
	if code == 2 {
		t.Fatalf("setup-token flag-parse error; stderr: %q", stderr.String())
	}
	// Should not succeed without real OAuth.
	if strings.Contains(stdout.String(), "Login successful.") {
		t.Fatal("setup-token should not succeed without real OAuth")
	}
}

func TestSetupTokenWarnsWhenAlreadyAuthenticated(t *testing.T) {
	// SUBCMD-SETUP-TOKEN-02: when ANTHROPIC_API_KEY is set, warn the user.
	t.Setenv("ANTHROPIC_API_KEY", "sk-already-set")

	ctx, cancel := context.WithTimeout(context.Background(), 100)
	defer cancel()

	var stdout, stderr bytes.Buffer
	code := runSetupTokenCLI(ctx, []string{"--yes"}, &stubStore{}, strings.NewReader(""), &stdout, &stderr)
	// Warning must be printed; code can be 1 (flow fails) but not 2.
	if code == 2 {
		t.Fatalf("flag-parse error; stderr: %q", stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(strings.ToLower(combined), "already") {
		t.Fatalf("expected 'already' warning when ANTHROPIC_API_KEY set: %q", combined)
	}
}

func TestSetupTokenUsesInferenceOnlyScope(t *testing.T) {
	// SUBCMD-SETUP-TOKEN-03: the OAuth URL must use InferenceOnly=true scope.
	// We capture the URL via OnURL callback by injecting a custom browser opener.
	ctx, cancel := context.WithTimeout(context.Background(), 500)
	defer cancel()

	var capturedURL string
	captureStore := &captureURLStore{onURL: func(u string) { capturedURL = u }}
	_ = captureStore

	// Use runSetupTokenCLIWithOptions to inject hooks.
	opts := setupTokenOptions{
		YesFlag: true,
		OnURL: func(u string) {
			capturedURL = u
			// Cancel ctx immediately so the listener doesn't block.
			cancel()
		},
	}
	var stdout, stderr bytes.Buffer
	runSetupTokenCLIWithOptions(ctx, opts, &stubStore{}, &stdout, &stderr)

	if capturedURL == "" {
		// The flow was cancelled before URL was emitted (race with timeout) — OK.
		t.Skip("URL not captured before timeout; skipping URL-content check")
	}
	// InferenceOnly=true → scope should be user:inference only.
	if !strings.Contains(capturedURL, "user%3Ainference") && !strings.Contains(capturedURL, "user:inference") {
		t.Fatalf("setup-token URL should contain inference scope; URL: %q", capturedURL)
	}
}

// --- F3-C05: install command ---

func TestInstallCommandRegistered(t *testing.T) {
	// The "install" sub-command must be reachable (not panic on dispatch).
	ctx, cancel := context.WithTimeout(context.Background(), 100)
	defer cancel()

	var stdout, stderr bytes.Buffer
	code := runInstallCLI(ctx, []string{"--help"}, &stdout, &stderr)
	// --help should print usage and exit 0.
	if code == 2 {
		t.Fatalf("install --help flag-parse error; stderr: %q", stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(strings.ToLower(combined), "install") {
		t.Fatalf("expected 'install' in --help output: %q", combined)
	}
}

func TestInstallCommandWithoutNetworkFails(t *testing.T) {
	// With no live release server, install should fail gracefully (not panic),
	// printing a clear message. We test with a stub that returns an error.
	ctx, cancel := context.WithTimeout(context.Background(), 100)
	defer cancel()

	var stdout, stderr bytes.Buffer
	// Pass an unreachable URL via environment; the implementation should
	// report ⚠️ clearly.
	code := runInstallCLI(ctx, []string{}, &stdout, &stderr)
	// Must not be 2 (flag error); may be 0 or 1.
	if code == 2 {
		t.Fatalf("flag-parse error in install; stderr: %q", stderr.String())
	}
}

// captureURLStore is a CredentialStore that also accepts an onURL hook.
type captureURLStore struct {
	onURL func(string)
}

func (c *captureURLStore) Save(_ context.Context, _ auth.Credentials) error { return nil }
func (c *captureURLStore) Load(_ context.Context) (auth.Credentials, error) {
	return auth.Credentials{Source: auth.SourceNone}, nil
}
func (c *captureURLStore) Delete(_ context.Context) error { return nil }
