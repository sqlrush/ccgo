package reconnect

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunSucceedsAfterRetries(t *testing.T) {
	attempts := 0
	var slept []time.Duration
	err := Run(context.Background(), func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("transient")
		}
		return nil
	}, Options{
		MaxAttempts:    5,
		InitialBackoff: time.Second,
		MaxBackoff:     30 * time.Second,
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d want 3", attempts)
	}
	// Backoff before attempt 2 = 1s, before attempt 3 = 2s.
	if len(slept) != 2 || slept[0] != time.Second || slept[1] != 2*time.Second {
		t.Fatalf("backoffs = %v want [1s 2s]", slept)
	}
}

func TestRunExhausts(t *testing.T) {
	attempts := 0
	err := Run(context.Background(), func(ctx context.Context) error {
		attempts++
		return errors.New("nope")
	}, Options{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
		Sleep: func(context.Context, time.Duration) error { return nil }})
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d want 3", attempts)
	}
}

func TestRunCapsBackoff(t *testing.T) {
	var slept []time.Duration
	_ = Run(context.Background(), func(ctx context.Context) error { return errors.New("x") },
		Options{MaxAttempts: 6, InitialBackoff: time.Second, MaxBackoff: 4 * time.Second,
			Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }})
	// 1,2,4,4,4 (cap at 4s).
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second, 4 * time.Second}
	if len(slept) != len(want) {
		t.Fatalf("slept = %v want %v", slept, want)
	}
	for i := range want {
		if slept[i] != want[i] {
			t.Fatalf("slept[%d] = %v want %v", i, slept[i], want[i])
		}
	}
}

func TestRunAbortsOnContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, func(context.Context) error { return errors.New("x") },
		Options{MaxAttempts: 5, InitialBackoff: time.Second, MaxBackoff: time.Second,
			Sleep: func(c context.Context, _ time.Duration) error { return c.Err() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v want context.Canceled", err)
	}
}

func TestShouldReconnect(t *testing.T) {
	for _, transport := range []string{"http", "sse", "ws"} {
		if !ShouldReconnect(transport) {
			t.Fatalf("%q should reconnect", transport)
		}
	}
	for _, transport := range []string{"stdio", "sdk", ""} {
		if ShouldReconnect(transport) {
			t.Fatalf("%q should NOT reconnect", transport)
		}
	}
}
