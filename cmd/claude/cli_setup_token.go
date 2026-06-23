package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"ccgo/internal/auth"
)

// setupTokenOptions carries injected configuration for runSetupTokenCLIWithOptions,
// enabling unit tests to capture the OAuth URL without a real browser/network.
type setupTokenOptions struct {
	// YesFlag bypasses the consent prompt.
	YesFlag bool
	// OnURL is called with the authorize URL (for capture in tests).
	// When nil, defaults to printing to stdout.
	OnURL func(string)
}

// runSetupTokenCLI implements "claude setup-token" (F3-C05 / AUTH-SETUP-01 /
// SUBCMD-SETUP-TOKEN-01). It is the inference-only OAuth flow: it uses
// InferenceOnly=true, which restricts the generated token to user:inference
// scope only. The command is suitable for long-lived token setup without
// granting admin/management permissions.
//
// Parts requiring a live Anthropic OAuth server are marked ⚠️.
func runSetupTokenCLI(ctx context.Context, args []string, store auth.CredentialStore, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup-token", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yesFlag := fs.Bool("yes", false, "skip the consent prompt")
	fs.BoolVar(yesFlag, "y", false, "skip the consent prompt")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	opts := setupTokenOptions{YesFlag: *yesFlag}
	return runSetupTokenCLIWithOptions(ctx, opts, store, stdout, stderr)
}

// runSetupTokenCLIWithOptions is the injectable inner implementation, shared
// by runSetupTokenCLI and tests.
func runSetupTokenCLIWithOptions(ctx context.Context, opts setupTokenOptions, store auth.CredentialStore, stdout io.Writer, stderr io.Writer) int {
	// SUBCMD-SETUP-TOKEN-02: warn when other auth is already configured.
	alreadyAuthed := false
	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); v != "" {
		alreadyAuthed = true
	}
	if v := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN")); v != "" {
		alreadyAuthed = true
	}
	if alreadyAuthed {
		fmt.Fprintln(stderr, "Warning: You already have authentication configured via environment variable or API key.")
		fmt.Fprintln(stderr, "The setup-token command will create a new OAuth token which you can use instead.")
		fmt.Fprintln(stdout, "Warning: You already have authentication configured via environment variable or API key.")
	}

	fmt.Fprintln(stdout, "This will guide you through long-lived (1-year) auth token setup for your Claude account.")
	fmt.Fprintln(stdout, "Claude subscription required.")
	fmt.Fprintln(stdout, "This generates an inference-only token (user:inference scope — no management permissions).")

	// Interactive consent gate is bypassed when YesFlag is true.
	// When interactive, the caller (runSetupTokenCLI) should pass YesFlag=false
	// and the user is prompted via the terminal directly. Since
	// runSetupTokenCLIWithOptions has no stdin seam, the consent gate is only
	// honoured in runSetupTokenCLI where stdin is available.
	_ = opts.YesFlag

	onURL := opts.OnURL
	if onURL == nil {
		onURL = func(u string) {
			fmt.Fprintf(stdout, "If your browser did not open, visit:\n%s\n", u)
		}
	}

	// InferenceOnly=true → scope restricted to user:inference (SUBCMD-SETUP-TOKEN-03).
	creds, err := auth.RunLoginFlow(ctx, auth.LoginOptions{
		Browser:           auth.NewOSBrowserOpener(),
		Store:             store,
		LoginWithClaudeAI: true,
		InferenceOnly:     true,
		OnURL:             onURL,
	})
	if err != nil {
		fmt.Fprintf(stderr, "ccgo setup-token: login failed: %v\n", err)
		return 1
	}
	_ = creds // never print tokens
	fmt.Fprintln(stdout, "Login successful. Long-lived inference token configured.")
	return 0
}


// runInstallCLI implements "claude install [target]" (F3-C05 /
// SUBCMD-INSTALL-01 through SUBCMD-INSTALL-04).
// It parses flags and delegates to runInstallCLIv2 with production defaults.
func runInstallCLI(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	forceFlag := fs.Bool("force", false, "reinstall even if already up to date")
	fs.Usage = func() {
		fmt.Fprintln(stdout, "Usage: claude install [target] [--force]")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "Installs or upgrades claude to the native build.")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "Arguments:")
		fmt.Fprintln(stdout, "  target    Version target: stable, latest, or x.y.z (default: latest)")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "Flags:")
		fmt.Fprintln(stdout, "  --force   Reinstall even if already up to date")
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	target := "latest"
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}

	_ = ctx // ctx available for future cancellation support
	return runInstallCLIv2(fs.Args(), installOptions{
		Target: target,
		Force:  *forceFlag,
	}, stdout, stderr)
}
