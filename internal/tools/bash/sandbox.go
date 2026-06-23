package bashtools

import (
	"ccgo/internal/contracts"
	"ccgo/internal/sandbox"
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
