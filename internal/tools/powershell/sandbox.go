package powershelltools

import (
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/sandbox"
	"ccgo/internal/tool"
)

// sandboxPolicyFromPowerShellContext extracts sandbox policy from a
// tool.Context.Metadata in the same way as the Bash tool helper.
// CC parity: src/utils/Shell.ts — shouldUseSandbox + isSandboxedPowerShell.
func sandboxPolicyFromPowerShellContext(ctx tool.Context) sandbox.Policy {
	if ctx.Metadata == nil {
		return sandbox.Policy{}
	}
	switch s := ctx.Metadata[tool.MetadataSettingsKey].(type) {
	case contracts.Settings:
		return sandbox.PolicyFromSettings(s)
	case *contracts.Settings:
		if s != nil {
			return sandbox.PolicyFromSettings(*s)
		}
	}
	return sandbox.Policy{}
}

// sandboxedPowerShellCommand returns the (name, args) needed to run a
// PowerShell command under the sandbox. It mirrors CC's "sandboxed PowerShell"
// approach (Shell.ts:256):
//
//   - Build the raw pwsh invocation as a shell string.
//   - Pass /bin/sh as the inner shell to the sandbox wrapper (so that
//     sandbox-exec / re-exec confinement wraps the whole pwsh call).
//   - Return (sandboxExec, args) or fall back to raw /bin/sh if sandbox is
//     unavailable and FailIfUnavailable=false.
//
// SECURITY: sandbox decision uses p.ShouldSandbox (no ExcludedCommands
// for PowerShell — CC does not apply the excluded-command list to PowerShell).
// Anti-footgun: dangerouslyDisableSandbox only bypasses when AllowUnsandboxed=true.
//
// SBX-34: callPowerShell and startBackgroundPowerShell call this function.
func sandboxedPowerShellCommand(psCommand string, p sandbox.Policy, dangerouslyDisableSandbox bool) (string, []string) {
	name, ok := powerShellExecutable()
	if !ok {
		// No PowerShell — return a shell one-liner that reports the absence.
		return "/bin/sh", []string{"-c", "echo 'PowerShell executable not found' >&2; exit 1"}
	}

	// Decide whether to sandbox (CC mirrors shouldUseSandbox.ts for PowerShell:
	// ExcludedCommands are NOT applied to PowerShell tool).
	if !p.ShouldSandbox(dangerouslyDisableSandbox) {
		// Sandbox off or legitimately bypassed — run pwsh directly.
		return name, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", psCommand}
	}

	if !sandbox.SupportedForPolicy(p) {
		if p.FailIfUnavailable {
			// fail-closed: return a /bin/sh command that exits with error.
			return "/bin/sh", []string{"-c", "echo 'sandbox required but unavailable' >&2; exit 1"}
		}
		// Best-effort degraded mode — run unsandboxed.
		return name, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", psCommand}
	}

	// Build the full pwsh invocation as a shell-safe string.
	// CC uses base64-encoded commands to survive quoting layers; for simplicity
	// we use single-quote escaping on POSIX (the only platforms where sandbox
	// is supported). The shell string is: pwsh -NoLogo -NoProfile -NonInteractive
	// -Command '<psCommand escaped>'.
	escaped := strings.ReplaceAll(psCommand, "'", "'\\''")
	shellCmd := fmt.Sprintf("%s -NoLogo -NoProfile -NonInteractive -Command '%s'", name, escaped)

	// Wrap /bin/sh as the inner shell (CC's sandboxBinShell approach).
	wName, wArgs, err := sandbox.Wrap("/bin/sh", []string{"-c", shellCmd}, p)
	if err != nil {
		if p.FailIfUnavailable {
			return "/bin/sh", []string{"-c", "echo 'sandbox required but unavailable' >&2; exit 1"}
		}
		// Wrap failed — degrade gracefully.
		return name, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", psCommand}
	}
	return wName, wArgs
}
