package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestBrowserCommand(t *testing.T) {
	cases := []struct {
		goos     string
		wantName string
		wantArg0 string
	}{
		{"darwin", "open", "https://example.com/x"},
		{"linux", "xdg-open", "https://example.com/x"},
		{"windows", "rundll32", "url.dll,FileProtocolHandler"},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			name, args := browserCommand(tc.goos, "https://example.com/x")
			if name != tc.wantName {
				t.Fatalf("name = %q want %q", name, tc.wantName)
			}
			if len(args) == 0 || args[0] != tc.wantArg0 {
				t.Fatalf("args = %v want first %q", args, tc.wantArg0)
			}
		})
	}
}

func TestValidateBrowserURL(t *testing.T) {
	if err := validateBrowserURL("https://platform.claude.com/oauth/authorize"); err != nil {
		t.Fatalf("https should be valid: %v", err)
	}
	if err := validateBrowserURL("file:///etc/passwd"); err == nil {
		t.Fatal("file:// must be rejected")
	}
	if err := validateBrowserURL("javascript:alert(1)"); err == nil {
		t.Fatal("javascript: must be rejected")
	}
}

func TestOSBrowserOpenerInvokesRunner(t *testing.T) {
	var gotName string
	var gotArgs []string
	op := &osBrowserOpener{runner: func(name string, args ...string) error {
		gotName, gotArgs = name, args
		return nil
	}}
	if err := op.Open("https://example.com/cb"); err != nil {
		t.Fatalf("Open err: %v", err)
	}
	if gotName == "" || len(gotArgs) == 0 {
		t.Fatalf("runner not invoked: name=%q args=%v", gotName, gotArgs)
	}
	// The URL must appear in the argv (exact position is OS-dependent).
	if !strings.Contains(strings.Join(gotArgs, " "), "https://example.com/cb") {
		t.Fatalf("url not passed to runner: %v", gotArgs)
	}
}

func TestOSBrowserOpenerRejectsBadScheme(t *testing.T) {
	op := &osBrowserOpener{runner: func(string, ...string) error {
		t.Fatal("runner must not run for invalid scheme")
		return nil
	}}
	if err := op.Open("file:///etc/passwd"); err == nil || !errors.Is(err, errInvalidBrowserURL) {
		t.Fatalf("expected errInvalidBrowserURL, got %v", err)
	}
}
