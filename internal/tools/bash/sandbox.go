package bashtools

import (
	"fmt"
	"os"

	"ccgo/internal/contracts"
	"ccgo/internal/sandbox"
	"ccgo/internal/sandbox/netproxy"
	"ccgo/internal/tool"
)

// defaultShell returns the raw shell name that shellCommand uses, without a
// command string. This is the "unwrapped" executable that tests compare against.
func defaultShell() string {
	name, _ := shellCommand("")
	return name
}

// sandboxPolicyFromContext extracts sandbox policy from tool.Context by reading
// the merged contracts.Settings stored under tool.MetadataSettingsKey (the same
// convention used by skill/tools.go and tools/task/worktree.go).
func sandboxPolicyFromContext(ctx tool.Context) sandbox.Policy {
	return sandbox.PolicyFromSettings(settingsFromBashMetadata(ctx.Metadata))
}

// settingsFromBashMetadata reads contracts.Settings from a metadata map,
// mirroring the pattern in internal/tools/task/worktree.go:taskSettingsFromMetadata.
func settingsFromBashMetadata(metadata map[string]any) contracts.Settings {
	if metadata == nil {
		return contracts.Settings{}
	}
	switch s := metadata[tool.MetadataSettingsKey].(type) {
	case contracts.Settings:
		return s
	case *contracts.Settings:
		if s != nil {
			return *s
		}
	}
	return contracts.Settings{}
}

// sandboxedShellCommand returns the (name, args) to execute command, confined
// according to p and the dangerouslyDisableSandbox flag.
//
// SECURITY: confinement is decided by p.ShouldSandboxCommand (which includes
// ExcludedCommands analysis), never by the flag alone. The dangerous flag only
// bypasses when the policy explicitly allows it (AllowUnsandboxed=true). A
// stray flag cannot silently disable an enabled sandbox.
//
// When sandbox is required but unavailable:
//   - FailIfUnavailable=true  → returns failClosedCommand (exits non-zero; fail-closed)
//   - FailIfUnavailable=false → returns the raw shell (degraded/best-effort mode)
func sandboxedShellCommand(command string, p sandbox.Policy, dangerous bool) (string, []string) {
	name, args := shellCommand(command)
	if !p.ShouldSandboxCommand(command, dangerous) {
		// Sandbox is off (disabled, legitimately bypassed, or excluded command): run as-is.
		return name, args
	}
	if !sandbox.SupportedForPolicy(p) {
		// Sandbox required but platform has no enforcement backend (or not in enabledPlatforms).
		if p.FailIfUnavailable {
			return failClosedCommand(p)
		}
		// Best-effort degraded mode: run unconfined with no silent failure.
		return name, args
	}
	wName, wArgs, err := sandbox.Wrap(name, args, p)
	if err != nil {
		// Wrap failed (e.g. ErrUnsupported or config error).
		if p.FailIfUnavailable {
			return failClosedCommand(p)
		}
		return name, args
	}
	return wName, wArgs
}

// shellArgsFor returns the args portion of shellCommand for a given command
// string. This extracts the flag+command portion without the executable name.
func shellArgsFor(command string) []string {
	_, args := shellCommand(command)
	return args
}

// failClosedCommand returns a shell invocation that exits non-zero with a clear
// error message, used when sandbox enforcement is required but unavailable.
// This ensures a misconfigured or unsupported sandbox never silently runs an
// unconfined command.
func failClosedCommand(_ sandbox.Policy) (string, []string) {
	name, _ := shellCommand("")
	return name, shellArgsFor("echo 'sandbox required but unavailable' >&2; exit 1")
}

// scrubBareGitRepoFiles delegates to sandbox.ScrubBareGitRepoFiles for
// post-command cleanup of bare-repo stubs planted by the sandbox (SBX-43).
func scrubBareGitRepoFiles(dir string) {
	sandbox.ScrubBareGitRepoFiles(dir)
}

// startProxyForPolicy starts a domain-filtering HTTP proxy when the Policy has
// per-domain network rules (AllowedDomains or DeniedDomains is non-empty) and
// the sandbox is active.
//
// SBX-48: the proxy is the enforcement layer for per-domain network filtering.
// macOS seatbelt and Linux landlock cannot express DNS-level rules at the kernel
// level; the proxy fills that gap for HTTP/HTTPS traffic.
//
// Returns:
//   - envVars: the proxy env vars to inject into the sandboxed command's Env.
//     If no proxy is needed, returns nil (caller should use os.Environ()).
//   - stop: must be called (via defer) after the command completes.
//   - err: non-nil only on proxy listen failure; callers should log and continue
//     unfiltered (best-effort) unless p.FailIfUnavailable.
//
// The returned envVars are meant to be appended to os.Environ() so the
// sandboxed process routes all HTTP/HTTPS traffic through the local filter.
func startProxyForPolicy(p sandbox.Policy) (envVars []string, stop func(), err error) {
	noop := func() {}
	if !netproxy.NeedsProxy(p.AllowedDomains, p.DeniedDomains) {
		return nil, noop, nil
	}
	_, vars, stopFn, startErr := netproxy.StartForSandbox(p.AllowedDomains, p.DeniedDomains)
	if startErr != nil {
		return nil, noop, fmt.Errorf("sandbox: start domain-filtering proxy: %w", startErr)
	}
	return vars, stopFn, nil
}

// envWithProxyVars returns a copy of the current process environment with the
// given proxy vars appended. If proxyVars is nil or empty, it returns
// os.Environ() unchanged. The result is safe to assign to exec.Cmd.Env.
func envWithProxyVars(proxyVars []string) []string {
	base := os.Environ()
	if len(proxyVars) == 0 {
		return base
	}
	merged := make([]string, len(base), len(base)+len(proxyVars))
	copy(merged, base)
	return append(merged, proxyVars...)
}
