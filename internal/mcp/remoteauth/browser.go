package remoteauth

import (
	"context"
	"fmt"

	"ccgo/internal/auth"
)

// BrowserAuthorizer implements Authorizer by:
//  1. Starting a local callback listener on an OS-assigned port.
//  2. Opening the authorization URL in the user's default browser via Opener.
//  3. Waiting for the OAuth redirect callback, then returning the code.
//
// It is the production implementation of Authorizer for cmd/claude.
type BrowserAuthorizer struct {
	// Opener opens a URL in the system browser. NewOSBrowserOpener is used in
	// production; tests substitute a fake.
	Opener auth.BrowserOpener
}

// NewBrowserAuthorizer returns a BrowserAuthorizer backed by the OS browser.
func NewBrowserAuthorizer() *BrowserAuthorizer {
	return &BrowserAuthorizer{Opener: auth.NewOSBrowserOpener()}
}

// Authorize starts a callback listener, opens authURL in the browser, and
// waits for the OAuth redirect to arrive at the loopback URI.
func (a *BrowserAuthorizer) Authorize(ctx context.Context, authURL, _, state string) (string, error) {
	listener, err := auth.StartCallbackListener(state)
	if err != nil {
		return "", fmt.Errorf("remoteauth: start callback listener: %w", err)
	}
	defer func() { _ = listener.Close() }()

	if err := a.Opener.Open(authURL); err != nil {
		return "", fmt.Errorf("remoteauth: open browser: %w", err)
	}

	result, err := listener.Wait(ctx)
	if err != nil {
		return "", fmt.Errorf("remoteauth: wait for callback: %w", err)
	}
	return result.Code, nil
}
