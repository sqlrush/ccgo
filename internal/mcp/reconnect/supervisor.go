package reconnect

import (
	"context"
	"fmt"
	"time"

	"ccgo/internal/mcp"
)

const (
	DefaultMaxAttempts    = 5
	DefaultInitialBackoff = time.Second
	DefaultMaxBackoff     = 30 * time.Second
)

type ConnectFunc func(ctx context.Context) error

type Options struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Sleep          func(context.Context, time.Duration) error
	OnAttempt      func(attempt int, err error)
}

func (o Options) withDefaults() Options {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = DefaultMaxAttempts
	}
	if o.InitialBackoff <= 0 {
		o.InitialBackoff = DefaultInitialBackoff
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = DefaultMaxBackoff
	}
	if o.Sleep == nil {
		o.Sleep = sleepWithContext
	}
	return o
}

// Run connects with exponential backoff. It returns nil on the first success,
// the last error when attempts are exhausted, or ctx.Err() on cancellation.
func Run(ctx context.Context, connect ConnectFunc, opts Options) error {
	opts = opts.withDefaults()
	var lastErr error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := connect(ctx)
		if opts.OnAttempt != nil {
			opts.OnAttempt(attempt, err)
		}
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == opts.MaxAttempts {
			break
		}
		backoff := backoffForAttempt(attempt, opts.InitialBackoff, opts.MaxBackoff)
		if sleepErr := opts.Sleep(ctx, backoff); sleepErr != nil {
			return sleepErr
		}
	}
	return fmt.Errorf("mcp reconnect exhausted %d attempts: %w", opts.MaxAttempts, lastErr)
}

func backoffForAttempt(attempt int, initial, max time.Duration) time.Duration {
	d := initial
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= max {
			return max
		}
	}
	if d > max {
		return max
	}
	return d
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ShouldReconnect reports whether a transport should be auto-reconnected.
// Local transports (stdio/sdk) restart differently and are excluded
// (matches CC useManageMCPConnections.ts).
func ShouldReconnect(transport string) bool {
	switch transport {
	case mcp.TransportHTTP, mcp.TransportSSE, mcp.TransportWS,
		mcp.TransportSSEIDE, mcp.TransportWSIDE, mcp.TransportClaudeAIProxy:
		return true
	default:
		return false
	}
}
