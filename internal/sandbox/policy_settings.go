package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"ccgo/internal/contracts"
)

// PolicyFromSettings builds an immutable Policy from merged settings.
// It is a convenience wrapper around PolicyFromSettingsAt using the current
// working directory as both originalCwd and currentCwd.
//
// Defaults match CC (sandbox-adapter.ts:474-485):
//   - Enabled: false (must be explicitly enabled)
//   - AllowUnsandboxed: true  (allowUnsandboxedCommands ?? true)
//   - FailIfUnavailable: false (failIfUnavailable ?? false)
//   - AllowNetwork: false
//
// Unknown or wrong-typed values are silently ignored; the function never
// returns an error so that a bad settings value cannot prevent startup.
func PolicyFromSettings(s contracts.Settings) Policy {
	cwd, _ := os.Getwd()
	return PolicyFromSettingsAt(s, cwd, cwd)
}

// PolicyFromSettingsAt builds a Policy with cwd-dependent security injections.
// originalCwd is where Claude Code started; currentCwd is the active directory.
// Using explicit parameters rather than reading os.Getwd() makes this testable
// without filesystem side effects.
//
// Security injections (CC sandbox-adapter.ts:222-300):
//   - DenyWrite: all settings.json / settings.local.json paths (SBX-40)
//   - DenyWrite: .claude/skills in originalCwd (and currentCwd if different) (SBX-41)
//   - DenyWrite: existing bare-repo files (HEAD/objects/refs/hooks/config) in cwd (SBX-42)
//   - AllowWrite: permissions.additionalDirectories (SBX-45)
//   - AllowWrite: FileEdit allow permission rules (SBX-46)
//   - DenyWrite: FileEdit deny permission rules (SBX-46)
//   - DenyRead: FileRead deny permission rules (SBX-47)
func PolicyFromSettingsAt(s contracts.Settings, originalCwd, currentCwd string) Policy {
	// CC defaults: allowUnsandboxedCommands ?? true, autoAllowBashIfSandboxed ?? true
	p := Policy{
		AllowUnsandboxed:         true,
		AutoAllowBashIfSandboxed: true, // SBX-35: default true (sandbox-adapter.ts:471)
	}

	box := s.Sandbox
	if box == nil {
		return p
	}

	if v, ok := boolAt(box, "enabled"); ok {
		p.Enabled = v
	}
	if v, ok := boolAt(box, "failIfUnavailable"); ok {
		p.FailIfUnavailable = v
	}
	if v, ok := boolAt(box, "allowUnsandboxedCommands"); ok {
		p.AllowUnsandboxed = v
	}
	if v, ok := boolAt(box, "allowNetworkAccess"); ok {
		p.AllowNetwork = v
	}
	if v, ok := boolAt(box, "autoAllowBashIfSandboxed"); ok {
		p.AutoAllowBashIfSandboxed = v
	}
	if fs, ok := box["filesystem"].(map[string]any); ok {
		p.AllowWrite = stringsAt(fs, "allowWrite")
		p.DenyWrite = stringsAt(fs, "denyWrite")
		p.DenyRead = stringsAt(fs, "denyRead")
		p.AllowRead = stringsAt(fs, "allowRead")
	}

	// F10-C01: excludedCommands
	p.ExcludedCommands = stringsAt(box, "excludedCommands")

	// F10-C03: enabledPlatforms
	p.EnabledPlatforms = stringsAt(box, "enabledPlatforms")

	// F10-C04: fine-grained network fields
	if net, ok := box["network"].(map[string]any); ok {
		p.AllowedDomains = stringsAt(net, "allowedDomains")
		p.DeniedDomains = stringsAt(net, "deniedDomains")
		p.AllowUnixSockets = stringsAt(net, "allowUnixSockets")
	}

	// ── Security injections (F10-C02) ──────────────────────────────────────

	// SBX-40: Deny writes to settings.json to prevent sandbox escape
	p.DenyWrite = append(p.DenyWrite, settingsFilePaths(originalCwd)...)
	if currentCwd != originalCwd {
		p.DenyWrite = append(p.DenyWrite, settingsFilePaths(currentCwd)...)
	}

	// SBX-41: Deny writes to .claude/skills (same privilege as commands/agents)
	p.DenyWrite = append(p.DenyWrite, filepath.Join(originalCwd, ".claude", "skills"))
	if currentCwd != originalCwd {
		p.DenyWrite = append(p.DenyWrite, filepath.Join(currentCwd, ".claude", "skills"))
	}

	// SBX-42: Bare-repo detection — deny writes to existing bare-repo files
	for _, dir := range uniqueDirs(originalCwd, currentCwd) {
		for _, name := range bareRepoFileNames {
			path := filepath.Join(dir, name)
			if fileExists(path) {
				p.DenyWrite = append(p.DenyWrite, path)
			}
		}
	}

	// SBX-45: permissions.additionalDirectories → AllowWrite
	if s.Permissions != nil {
		for _, dir := range s.Permissions.AdditionalDirectories {
			if dir != "" {
				p.AllowWrite = append(p.AllowWrite, dir)
			}
		}

		// SBX-46/47: Permission rules → filesystem policy
		for _, rule := range s.Permissions.Allow {
			if tool, content := parsePermissionRule(rule); tool == "Edit" && content != "" {
				p.AllowWrite = append(p.AllowWrite, resolveRulePath(content))
			}
		}
		for _, rule := range s.Permissions.Deny {
			tool, content := parsePermissionRule(rule)
			switch {
			case tool == "Edit" && content != "":
				p.DenyWrite = append(p.DenyWrite, resolveRulePath(content))
			case tool == "Read" && content != "":
				p.DenyRead = append(p.DenyRead, resolveRulePath(content))
			}
		}
	}

	return p
}

// bareRepoFileNames are the files git uses to identify a bare repository.
// Planting these in cwd can trick git into treating it as a bare repo and
// loading an attacker-controlled config (core.fsmonitor, etc.) — sandbox escape.
// CC ref: sandbox-adapter.ts:268.
var bareRepoFileNames = []string{"HEAD", "objects", "refs", "hooks", "config"}

// settingsFilePaths returns the settings.json and settings.local.json paths for dir.
func settingsFilePaths(dir string) []string {
	base := filepath.Join(dir, ".claude")
	return []string{
		filepath.Join(base, "settings.json"),
		filepath.Join(base, "settings.local.json"),
	}
}

// fileExists returns true if path exists (any type).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// uniqueDirs returns a deduplicated slice of directories.
func uniqueDirs(dirs ...string) []string {
	seen := make(map[string]bool, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// parsePermissionRule parses a permission rule string like "Edit(/path/*)".
// Returns ("Edit", "/path/*") or ("", "") on parse failure.
func parsePermissionRule(rule string) (tool, content string) {
	idx := strings.IndexByte(rule, '(')
	if idx < 0 {
		return rule, ""
	}
	t := rule[:idx]
	if !strings.HasSuffix(rule, ")") {
		return t, ""
	}
	c := rule[idx+1 : len(rule)-1]
	return t, c
}

// resolveRulePath resolves CC permission-rule path conventions:
//   - "//path" → "/path" (absolute escape for compat)
//   - "/path"  → as-is (absolute path)
//   - everything else → as-is
func resolveRulePath(p string) string {
	if strings.HasPrefix(p, "//") {
		return p[1:]
	}
	return p
}

// SupportedForPolicy reports whether the sandbox enforcement backend is
// available AND the current platform is permitted by Policy.EnabledPlatforms.
// This is the gate that callers should use instead of bare Supported().
//
// SBX-37: when enabledPlatforms is set, only those platforms may use the sandbox.
func SupportedForPolicy(p Policy) bool {
	if !Supported() {
		return false
	}
	if len(p.EnabledPlatforms) == 0 {
		return true // no restriction
	}
	current := goPlatformName()
	for _, pl := range p.EnabledPlatforms {
		if pl == current {
			return true
		}
	}
	return false
}

// goPlatformName returns the CC-style platform name for the current OS.
// CC platforms: "macos", "linux", "windows", "wsl". We use GOOS for the Go side.
func goPlatformName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "linux":
		return "linux"
	case "windows":
		return "windows"
	default:
		return runtime.GOOS
	}
}

// UnavailableReason returns a human-readable explanation when the sandbox is
// enabled but cannot run, or an empty string when it's fine or not enabled.
// This mirrors CC's getSandboxUnavailableReason (sandbox-adapter.ts:562).
//
// SBX-39: surface a useful message instead of silently ignoring sandbox.enabled.
func UnavailableReason(p Policy) string {
	if !p.Enabled {
		return "" // Not enabled — no noise.
	}
	if !Supported() {
		return fmt.Sprintf("sandbox.enabled is set but %s is not supported (requires macOS or Linux)", runtime.GOOS)
	}
	if len(p.EnabledPlatforms) > 0 {
		current := goPlatformName()
		found := false
		for _, pl := range p.EnabledPlatforms {
			if pl == current {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("sandbox.enabled is set but %s is not in sandbox.enabledPlatforms %v", current, p.EnabledPlatforms)
		}
	}
	return ""
}

// boolAt extracts a bool from a map[string]any, returning (false, false) if
// the key is absent or the value is not a bool.
func boolAt(m map[string]any, key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

// stringsAt extracts a []string from a []any in a map[string]any.
// Empty strings are dropped. Returns nil (not []) when nothing is found.
func stringsAt(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
