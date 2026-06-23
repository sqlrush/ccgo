package sandbox

import "strings"

// Policy is the OS-agnostic sandbox configuration for a single command.
// It is immutable: builders return new copies; ShouldSandbox is pure.
//
// Default (zero-value Policy): Enabled=false, AllowUnsandboxed=false.
// When Enabled is false, ShouldSandbox always returns false — sandbox is OFF.
// This matches CC behaviour: sandboxing must be opted-in via settings; the
// zero-value does NOT sandbox silently (there is no platform available by
// default). To enforce confinement, callers must set Enabled=true.
type Policy struct {
	Enabled           bool
	FailIfUnavailable bool
	AllowUnsandboxed  bool
	AllowWrite        []string
	DenyWrite         []string
	DenyRead          []string
	AllowRead         []string
	AllowNetwork      bool

	// ExcludedCommands is a user-facing convenience list (not a security boundary —
	// see CC NOTE in shouldUseSandbox.ts). When a command or any subcommand of a
	// compound expression matches one of these patterns the sandbox is skipped.
	// Pattern forms: "git" (prefix match), "git:*" (prefix alias), "git *" (wildcard).
	ExcludedCommands []string

	// Network fine-grained control fields (F10-C04).
	// On macOS the seatbelt profile supports per-domain/socket rules; these fields
	// hold the policy-layer intent for profile generation and diagnostics.
	// NOTE: Linux landlock does not support network, so these are macOS-only.
	AllowedDomains  []string // per-domain allow-list for network requests
	DeniedDomains   []string // per-domain deny-list (macOS only)
	AllowUnixSockets []string // allowed Unix socket paths (macOS only)

	// EnabledPlatforms restricts on which platforms the sandbox is active.
	// Empty list means "all supported platforms". Matches CC sandbox.enabledPlatforms.
	EnabledPlatforms []string
}

// ShouldSandbox decides whether this command must be confined.
// It does NOT check ExcludedCommands — use ShouldSandboxCommand for that.
//
// Decision rule (CC parity — mirrors shouldUseSandbox.ts:130-153):
//
//	sandbox UNLESS (!Enabled) OR (dangerouslyDisableSandbox && AllowUnsandboxed)
//
// SECURITY — anti-footgun: the dangerouslyDisableSandbox flag bypasses
// confinement ONLY when the policy explicitly permits unsandboxed commands
// (AllowUnsandboxed=true). A stray flag alone does NOT silently disable the
// sandbox. This preserves the CC invariant: operator policy controls whether
// the flag is effective.
func (p Policy) ShouldSandbox(dangerouslyDisableSandbox bool) bool {
	if !p.Enabled {
		return false
	}
	if dangerouslyDisableSandbox && p.AllowUnsandboxed {
		return false
	}
	return true
}

// ShouldSandboxCommand is the full decision including ExcludedCommands analysis.
// It mirrors CC's shouldUseSandbox.ts:shouldUseSandbox which calls containsExcludedCommand.
// SECURITY NOTE: ExcludedCommands is a user convenience feature, not a security boundary.
func (p Policy) ShouldSandboxCommand(command string, dangerouslyDisableSandbox bool) bool {
	if !p.ShouldSandbox(dangerouslyDisableSandbox) {
		return false
	}
	if command == "" {
		return false
	}
	if len(p.ExcludedCommands) > 0 && containsExcludedCommand(command, p.ExcludedCommands) {
		return false
	}
	return true
}

// containsExcludedCommand splits a compound command and checks each segment
// against the excluded patterns (with env-var + safe-wrapper stripping).
// Mirrors CC's containsExcludedCommand in shouldUseSandbox.ts:21-117.
func containsExcludedCommand(command string, patterns []string) bool {
	subcommands := splitCompoundCommand(command)
	for _, sub := range subcommands {
		trimmed := strings.TrimSpace(sub)
		if matchesAnyPattern(trimmed, patterns) {
			return true
		}
	}
	return false
}

// matchesAnyPattern iterates the fixed-point stripped candidates and checks each
// against the pattern list. Mirrors CC's fixed-point candidate expansion.
func matchesAnyPattern(cmd string, patterns []string) bool {
	// Build candidate set via fixed-point iteration (env-strip + wrapper-strip).
	candidates := []string{cmd}
	seen := map[string]bool{cmd: true}
	start := 0
	for start < len(candidates) {
		end := len(candidates)
		for i := start; i < end; i++ {
			c := candidates[i]
			if e := stripLeadingEnvVars(c); !seen[e] {
				candidates = append(candidates, e)
				seen[e] = true
			}
			if w := stripSafeWrapper(c); !seen[w] {
				candidates = append(candidates, w)
				seen[w] = true
			}
		}
		start = end
	}
	for _, cand := range candidates {
		for _, pat := range patterns {
			if matchPattern(pat, cand) {
				return true
			}
		}
	}
	return false
}

// matchPattern checks whether cand matches the excluded-command pattern pat.
//
// Pattern forms (CC parity — bashPermissions.ts:bashPermissionRule):
//
//	"git:*"   → prefix alias, matches "git" or "git <anything>"
//	"git *"   → glob/wildcard,  matches any string matching the glob
//	"git"     → prefix match,   matches "git" or "git <anything>"
func matchPattern(pat, cand string) bool {
	// Prefix alias form: "bazel:*" → prefix "bazel"
	if strings.HasSuffix(pat, ":*") {
		prefix := pat[:len(pat)-2]
		return cand == prefix || strings.HasPrefix(cand, prefix+" ")
	}
	// Wildcard form: contains * (but not the :* suffix handled above)
	if strings.ContainsRune(pat, '*') {
		return matchWildcard(pat, cand)
	}
	// Plain prefix match: "git" matches "git" or "git status"
	return cand == pat || strings.HasPrefix(cand, pat+" ")
}

// matchWildcard is a minimal glob matcher supporting only *.
func matchWildcard(pattern, s string) bool {
	parts := strings.SplitN(pattern, "*", 2)
	if len(parts) == 1 {
		return pattern == s
	}
	prefix, suffix := parts[0], parts[1]
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	rest := s[len(prefix):]
	if suffix == "" {
		return true
	}
	return strings.HasSuffix(rest, suffix) && len(rest) >= len(suffix)
}

// splitCompoundCommand splits a shell command string by &&, ||, |, and ;
// without a full shell parser. This is intentionally conservative — it may
// produce extra segments for edge cases, which is safe (errs toward exclusion).
// Mirrors CC's splitCommand_DEPRECATED usage for this purpose.
func splitCompoundCommand(cmd string) []string {
	// Tokenise by &&, ||, |, ; in that order of precedence.
	// We do a single linear scan to avoid regex dependency.
	var parts []string
	start := 0
	i := 0
	for i < len(cmd) {
		switch {
		case i+1 < len(cmd) && cmd[i] == '&' && cmd[i+1] == '&':
			parts = append(parts, cmd[start:i])
			i += 2
			start = i
		case i+1 < len(cmd) && cmd[i] == '|' && cmd[i+1] == '|':
			parts = append(parts, cmd[start:i])
			i += 2
			start = i
		case cmd[i] == '|':
			parts = append(parts, cmd[start:i])
			i++
			start = i
		case cmd[i] == ';':
			parts = append(parts, cmd[start:i])
			i++
			start = i
		default:
			i++
		}
	}
	parts = append(parts, cmd[start:])
	return parts
}

// stripLeadingEnvVars removes leading KEY=value env-var assignments.
// Mirrors CC's stripAllLeadingEnvVars (bashPermissions.ts).
func stripLeadingEnvVars(cmd string) string {
	for {
		// Find first token
		rest := strings.TrimLeft(cmd, " \t")
		idx := strings.IndexAny(rest, " \t")
		if idx < 0 {
			return cmd
		}
		tok := rest[:idx]
		if isEnvVarAssignment(tok) {
			cmd = strings.TrimLeft(rest[idx:], " \t")
			continue
		}
		return rest
	}
}

// isEnvVarAssignment returns true if tok looks like NAME=value.
func isEnvVarAssignment(tok string) bool {
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return false
	}
	name := tok[:eq]
	for _, c := range name {
		if !isIdentRune(c) {
			return false
		}
	}
	return true
}

func isIdentRune(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// safeWrappers mirrors CC's stripSafeWrappers (bashPermissions.ts):
// command prefixes that are "safe" meta-commands and don't change the
// semantic identity of the underlying command.
var safeWrappers = []string{
	"timeout ", "env ", "nice ", "time ", "ionice ", "strace ",
	"sudo ", "doas ", "nohup ", "setsid ",
}

// stripSafeWrapper removes at most one leading safe-wrapper prefix.
// Fixed-point iteration is done by the caller (matchesAnyPattern).
func stripSafeWrapper(cmd string) string {
	trimmed := strings.TrimLeft(cmd, " \t")
	for _, w := range safeWrappers {
		if strings.HasPrefix(trimmed, w) {
			after := strings.TrimLeft(trimmed[len(w):], " \t")
			// If wrapper takes a numeric arg (like "timeout 30"), skip it.
			if w == "timeout " {
				after = skipNumericArg(after)
			}
			return after
		}
	}
	return cmd
}

// skipNumericArg skips a leading numeric token (e.g. "30 make" → "make").
func skipNumericArg(cmd string) string {
	end := 0
	for end < len(cmd) && cmd[end] != ' ' && cmd[end] != '\t' {
		end++
	}
	tok := cmd[:end]
	if isNumeric(tok) {
		return strings.TrimLeft(cmd[end:], " \t")
	}
	return cmd
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
