package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAPIKeyHelperResolveCaches(t *testing.T) {
	calls := 0
	r := &APIKeyHelperResolver{
		Command: "print-key",
		TTL:     time.Minute,
		Now:     func() time.Time { return time.Unix(1000, 0) },
		run: func(ctx context.Context, command string) (string, error) {
			calls++
			return "  sk-from-helper\n", nil
		},
	}
	key, err := r.Resolve(context.Background())
	if err != nil || key != "sk-from-helper" {
		t.Fatalf("Resolve = %q,%v want sk-from-helper,nil", key, err)
	}
	// Second call within TTL must hit the cache, not re-run.
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("helper ran %d times; expected cached (1)", calls)
	}
}

func TestAPIKeyHelperResolveExpiresCache(t *testing.T) {
	calls := 0
	now := time.Unix(0, 0)
	r := &APIKeyHelperResolver{
		Command: "print-key",
		TTL:     time.Minute,
		Now:     func() time.Time { return now },
		run:     func(ctx context.Context, command string) (string, error) { calls++; return "k", nil },
	}
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute) // past TTL
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("helper ran %d times; expected re-run after TTL (2)", calls)
	}
}

func TestAPIKeyHelperEmptyOutputIsError(t *testing.T) {
	r := &APIKeyHelperResolver{
		Command: "noop",
		Now:     time.Now,
		run:     func(ctx context.Context, command string) (string, error) { return "   \n", nil },
	}
	if _, err := r.Resolve(context.Background()); err == nil {
		t.Fatal("empty helper output must be an error")
	}
}

func TestAPIKeyHelperRunFailure(t *testing.T) {
	r := &APIKeyHelperResolver{
		Command: "boom",
		Now:     time.Now,
		run:     func(ctx context.Context, command string) (string, error) { return "", errors.New("exit 1") },
	}
	if _, err := r.Resolve(context.Background()); err == nil {
		t.Fatal("helper failure must propagate")
	}
}
