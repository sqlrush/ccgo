package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

// CallbackResult is the validated content of the OAuth redirect.
type CallbackResult struct {
	Code  string
	State string
}

// CallbackListener serves the loopback OAuth redirect on an ephemeral port.
type CallbackListener struct {
	expectedState string
	listener      net.Listener
	server        *http.Server
	resultCh      chan CallbackResult
	errCh         chan error
	once          sync.Once
}

const callbackPath = "/callback"

// StartCallbackListener binds 127.0.0.1 on an OS-assigned port and begins
// serving. expectedState must be the PKCE state generated for this login.
func StartCallbackListener(expectedState string) (*CallbackListener, error) {
	if strings.TrimSpace(expectedState) == "" {
		return nil, errors.New("auth: callback listener requires a non-empty state")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("auth: bind callback listener: %w", err)
	}
	l := &CallbackListener{
		expectedState: expectedState,
		listener:      ln,
		resultCh:      make(chan CallbackResult, 1),
		errCh:         make(chan error, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, l.handle)
	l.server = &http.Server{Handler: mux}
	go func() { _ = l.server.Serve(ln) }()
	return l, nil
}

// Port returns the OS-assigned callback port.
func (l *CallbackListener) Port() int {
	return l.listener.Addr().(*net.TCPAddr).Port
}

// RedirectURI is the exact redirect_uri to register in the authorize request
// AND replay in the token exchange. Uses host "localhost" to match CC's
// format: http://localhost:<port>/callback (see oauth.go:115).
func (l *CallbackListener) RedirectURI() string {
	return fmt.Sprintf("http://localhost:%d%s", l.Port(), callbackPath)
}

// handle validates the redirect request and pushes the first result/error.
func (l *CallbackListener) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// IdP-reported error takes precedence (do not echo description back raw).
	if e := q.Get("error"); e != "" {
		writeCallbackPage(w, http.StatusBadRequest, "Login failed. You can close this window.")
		l.fail(fmt.Errorf("auth: authorization error %q", sanitizeErrorCode(e)))
		return
	}
	state := q.Get("state")
	if state != l.expectedState {
		writeCallbackPage(w, http.StatusBadRequest, "Invalid request. You can close this window.")
		// Do NOT include the received state in the error (CSRF / log hygiene).
		l.fail(errors.New("auth: callback state mismatch"))
		return
	}
	code := q.Get("code")
	if code == "" {
		writeCallbackPage(w, http.StatusBadRequest, "Missing authorization code. You can close this window.")
		l.fail(errors.New("auth: callback missing authorization code"))
		return
	}
	writeCallbackPage(w, http.StatusOK, "Login successful. You can close this window and return to the terminal.")
	l.succeed(CallbackResult{Code: code, State: state})
}

func (l *CallbackListener) succeed(res CallbackResult) {
	l.once.Do(func() { l.resultCh <- res })
}

func (l *CallbackListener) fail(err error) {
	l.once.Do(func() { l.errCh <- err })
}

// Wait blocks until the callback fires, an error occurs, or ctx is done.
func (l *CallbackListener) Wait(ctx context.Context) (CallbackResult, error) {
	select {
	case res := <-l.resultCh:
		return res, nil
	case err := <-l.errCh:
		return CallbackResult{}, err
	case <-ctx.Done():
		return CallbackResult{}, ctx.Err()
	}
}

// Close shuts the HTTP server and releases the port.
func (l *CallbackListener) Close() error {
	return l.server.Close()
}

func writeCallbackPage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	// message is one of our own constant strings, never attacker input.
	_, _ = w.Write([]byte("<!doctype html><html><body><p>" + message + "</p></body></html>"))
}

// sanitizeErrorCode keeps only the OAuth error code charset; never reflects
// arbitrary IdP text into our error string.
func sanitizeErrorCode(s string) string {
	const max = 64
	clean := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			clean = append(clean, r)
		}
		if len(clean) >= max {
			break
		}
	}
	return string(clean)
}
