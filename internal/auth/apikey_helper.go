package auth

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultAPIKeyHelperTTL = 5 * time.Minute
const apiKeyHelperTimeout = 10 * time.Minute

// APIKeyHelperResolver runs a user-configured shell command whose stdout is an
// API key, caching the result for a TTL (mirrors CC's apiKeyHelper in
// utils/auth.ts:_executeApiKeyHelper with execa({shell:true,timeout:600000})).
type APIKeyHelperResolver struct {
	Command string
	TTL     time.Duration
	Now     func() time.Time
	run     func(ctx context.Context, command string) (string, error)

	mu        sync.Mutex
	cached    string
	cachedAt  time.Time
	hasCached bool
}

// NewAPIKeyHelperResolver builds a resolver for the given shell command using
// the platform shell. TTL defaults to 5 minutes (CC's DEFAULT_API_KEY_HELPER_TTL)
// and can be overridden by the CLAUDE_CODE_API_KEY_HELPER_TTL_MS environment variable.
func NewAPIKeyHelperResolver(command string) *APIKeyHelperResolver {
	return &APIKeyHelperResolver{
		Command: command,
		TTL:     apiKeyHelperTTLFromEnv(),
		Now:     time.Now,
		run:     runShellCommand,
	}
}

// apiKeyHelperTTLFromEnv reads CLAUDE_CODE_API_KEY_HELPER_TTL_MS; on parse
// failure or absence it returns the 5-minute default.
func apiKeyHelperTTLFromEnv() time.Duration {
	if v := os.Getenv("CLAUDE_CODE_API_KEY_HELPER_TTL_MS"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultAPIKeyHelperTTL
}

// Resolve returns the API key, running the helper command if the cache is cold
// or has expired. The output is trimmed of surrounding whitespace before being
// returned (and cached). Errors from the helper are wrapped but the raw output
// is never included in error messages (it may contain a secret).
func (r *APIKeyHelperResolver) Resolve(ctx context.Context) (string, error) {
	if strings.TrimSpace(r.Command) == "" {
		return "", fmt.Errorf("auth: apiKeyHelper command is empty")
	}
	now := r.nowTime()

	r.mu.Lock()
	if r.hasCached && now.Sub(r.cachedAt) < r.ttlDuration() {
		key := r.cached
		r.mu.Unlock()
		return key, nil
	}
	r.mu.Unlock()

	tctx, cancel := context.WithTimeout(ctx, apiKeyHelperTimeout)
	defer cancel()

	out, err := r.run(tctx, r.Command)
	if err != nil {
		// Never include command output in the error — it may contain the key.
		return "", fmt.Errorf("auth: apiKeyHelper %q failed: %w", r.Command, err)
	}
	key := strings.TrimSpace(out)
	if key == "" {
		return "", fmt.Errorf("auth: apiKeyHelper %q returned no value", r.Command)
	}

	r.mu.Lock()
	r.cached, r.cachedAt, r.hasCached = key, now, true
	r.mu.Unlock()
	return key, nil
}

func (r *APIKeyHelperResolver) nowTime() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *APIKeyHelperResolver) ttlDuration() time.Duration {
	if r.TTL > 0 {
		return r.TTL
	}
	return defaultAPIKeyHelperTTL
}

// runShellCommand runs the command through the platform shell, matching CC's
// execa({shell:true}) behaviour. The 10-minute context timeout is enforced by
// the caller (Resolve).
func runShellCommand(ctx context.Context, command string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command) //nolint:gosec
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec
	}
	out, err := cmd.Output()
	return string(out), err
}
