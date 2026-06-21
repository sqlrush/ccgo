package auth

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCallbackListenerSuccess(t *testing.T) {
	l, err := StartCallbackListener("st-123")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Close()

	if l.Port() <= 0 {
		t.Fatalf("port = %d", l.Port())
	}
	if want := "http://localhost:"; !strings.HasPrefix(l.RedirectURI(), want) ||
		!strings.HasSuffix(l.RedirectURI(), "/callback") {
		t.Fatalf("redirect = %q", l.RedirectURI())
	}

	// Simulate the IdP redirect hitting the loopback callback.
	go func() {
		url := l.RedirectURI() + "?code=AUTH_CODE&state=st-123"
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := l.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait err: %v", err)
	}
	if res.Code != "AUTH_CODE" {
		t.Fatalf("code = %q want AUTH_CODE", res.Code)
	}
}

func TestCallbackListenerStateMismatch(t *testing.T) {
	l, err := StartCallbackListener("good-state")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Close()

	// Make the request in a goroutine to avoid blocking the test,
	// but capture the response for immediate assertion once it arrives.
	respChan := make(chan *http.Response, 1)
	go func() {
		resp, err := http.Get(l.RedirectURI() + "?code=X&state=WRONG")
		if err == nil {
			respChan <- resp
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = l.Wait(ctx)
	if err == nil {
		t.Fatal("expected error on state mismatch")
	}
	// The error MUST NOT leak the bad state value back.
	if strings.Contains(err.Error(), "WRONG") {
		t.Fatalf("error leaked attacker-controlled state: %v", err)
	}

	// The HTTP response is the synchronization point: we only receive
	// on respChan once the handler has responded. Deterministically
	// assert the response status is 400 (Bad Request) for state mismatch.
	select {
	case resp := <-respChan:
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("callback status = %d want %d", resp.StatusCode, http.StatusBadRequest)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for callback response")
	}
}

func TestCallbackListenerIdPError(t *testing.T) {
	l, err := StartCallbackListener("s")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Close()
	go func() {
		resp, e := http.Get(l.RedirectURI() + "?error=access_denied&error_description=nope&state=s")
		if e == nil {
			resp.Body.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := l.Wait(ctx); err == nil {
		t.Fatal("expected error when IdP returns error=")
	}
}

func TestCallbackListenerContextCancel(t *testing.T) {
	l, err := StartCallbackListener("s")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := l.Wait(ctx); err == nil {
		t.Fatal("expected ctx deadline error")
	}
}
