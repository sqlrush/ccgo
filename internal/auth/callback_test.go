package auth

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
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

	var status atomic.Int32
	go func() {
		resp, err := http.Get(l.RedirectURI() + "?code=X&state=WRONG")
		if err == nil {
			status.Store(int32(resp.StatusCode))
			resp.Body.Close()
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
	// Give the goroutine a moment; the HTTP response should be a 4xx.
	time.Sleep(50 * time.Millisecond)
	if st := status.Load(); st != 0 && (st < 400 || st >= 500) {
		t.Fatalf("callback status = %d want 4xx", st)
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
