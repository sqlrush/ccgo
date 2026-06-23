package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
)

// TestPolicyFromSettingsExcludedCommands verifies that sandbox.excludedCommands
// in settings maps to Policy.ExcludedCommands.
func TestPolicyFromSettingsExcludedCommands(t *testing.T) {
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled":          true,
			"excludedCommands": []any{"git", "make:*"},
		},
	}
	p := PolicyFromSettings(s)
	if len(p.ExcludedCommands) != 2 {
		t.Fatalf("expected 2 excludedCommands, got %v", p.ExcludedCommands)
	}
	if p.ExcludedCommands[0] != "git" || p.ExcludedCommands[1] != "make:*" {
		t.Fatalf("wrong excludedCommands: %v", p.ExcludedCommands)
	}
}

// TestPolicyFromSettingsSettingsJsonDenyWrite verifies SBX-40:
// PolicyFromSettings injects DenyWrite entries for settings.json paths.
func TestPolicyFromSettingsSettingsJsonDenyWrite(t *testing.T) {
	dir := t.TempDir()
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled": true,
		},
	}
	p := PolicyFromSettingsAt(s, dir, dir)
	// Must include the project-level settings.json path
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	if !containsPath(p.DenyWrite, settingsPath) {
		t.Fatalf("SBX-40: DenyWrite must contain settings.json %q; got %v", settingsPath, p.DenyWrite)
	}
	settingsLocalPath := filepath.Join(dir, ".claude", "settings.local.json")
	if !containsPath(p.DenyWrite, settingsLocalPath) {
		t.Fatalf("SBX-40: DenyWrite must contain settings.local.json %q; got %v", settingsLocalPath, p.DenyWrite)
	}
}

// TestPolicyFromSettingsSkillsDenyWrite verifies SBX-41:
// PolicyFromSettings injects DenyWrite for the .claude/skills directory.
func TestPolicyFromSettingsSkillsDenyWrite(t *testing.T) {
	dir := t.TempDir()
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled": true,
		},
	}
	p := PolicyFromSettingsAt(s, dir, dir)
	skillsPath := filepath.Join(dir, ".claude", "skills")
	if !containsPath(p.DenyWrite, skillsPath) {
		t.Fatalf("SBX-41: DenyWrite must contain .claude/skills %q; got %v", skillsPath, p.DenyWrite)
	}
}

// TestPolicyFromSettingsBareRepoDetection verifies SBX-42:
// When cwd contains bare git repo files (HEAD, objects/, refs/), they are
// injected into DenyWrite.
func TestPolicyFromSettingsBareRepoDetection(t *testing.T) {
	dir := t.TempDir()
	// Plant bare repo files
	if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := contracts.Settings{
		Sandbox: map[string]any{"enabled": true},
	}
	p := PolicyFromSettingsAt(s, dir, dir)
	// HEAD and objects and refs exist → DenyWrite must contain them
	if !containsPath(p.DenyWrite, filepath.Join(dir, "HEAD")) {
		t.Fatalf("SBX-42: HEAD in cwd must be DenyWrite; got %v", p.DenyWrite)
	}
	if !containsPath(p.DenyWrite, filepath.Join(dir, "objects")) {
		t.Fatalf("SBX-42: objects/ in cwd must be DenyWrite; got %v", p.DenyWrite)
	}
}

// TestPolicyFromSettingsAdditionalDirectories verifies SBX-45:
// permissions.additionalDirectories injects into AllowWrite.
func TestPolicyFromSettingsAdditionalDirectories(t *testing.T) {
	dir := t.TempDir()
	s := contracts.Settings{
		Permissions: &contracts.PermissionsSetting{
			AdditionalDirectories: []string{"/data", "/workspace"},
		},
		Sandbox: map[string]any{"enabled": true},
	}
	p := PolicyFromSettingsAt(s, dir, dir)
	if !containsPath(p.AllowWrite, "/data") {
		t.Fatalf("SBX-45: /data must be in AllowWrite; got %v", p.AllowWrite)
	}
	if !containsPath(p.AllowWrite, "/workspace") {
		t.Fatalf("SBX-45: /workspace must be in AllowWrite; got %v", p.AllowWrite)
	}
}

// TestPolicyFromSettingsFileEditAllowRule verifies SBX-46:
// FileEdit allow permission rule → AllowWrite injection.
func TestPolicyFromSettingsFileEditAllowRule(t *testing.T) {
	dir := t.TempDir()
	s := contracts.Settings{
		Permissions: &contracts.PermissionsSetting{
			Allow: []string{"Edit(/workspace/*)"},
		},
		Sandbox: map[string]any{"enabled": true},
	}
	p := PolicyFromSettingsAt(s, dir, dir)
	if !containsPath(p.AllowWrite, "/workspace/*") {
		t.Fatalf("SBX-46: Edit allow rule must inject /workspace/* into AllowWrite; got %v", p.AllowWrite)
	}
}

// TestPolicyFromSettingsFileEditDenyRule verifies SBX-46:
// FileEdit deny permission rule → DenyWrite injection.
func TestPolicyFromSettingsFileEditDenyRule(t *testing.T) {
	dir := t.TempDir()
	s := contracts.Settings{
		Permissions: &contracts.PermissionsSetting{
			Deny: []string{"Edit(/etc/*)"},
		},
		Sandbox: map[string]any{"enabled": true},
	}
	p := PolicyFromSettingsAt(s, dir, dir)
	if !containsPath(p.DenyWrite, "/etc/*") {
		t.Fatalf("SBX-46: Edit deny rule must inject /etc/* into DenyWrite; got %v", p.DenyWrite)
	}
}

// TestPolicyFromSettingsFileReadDenyRule verifies SBX-47:
// FileRead deny permission rule → DenyRead injection.
func TestPolicyFromSettingsFileReadDenyRule(t *testing.T) {
	dir := t.TempDir()
	s := contracts.Settings{
		Permissions: &contracts.PermissionsSetting{
			Deny: []string{"Read(/secrets/*)"},
		},
		Sandbox: map[string]any{"enabled": true},
	}
	p := PolicyFromSettingsAt(s, dir, dir)
	if !containsPath(p.DenyRead, "/secrets/*") {
		t.Fatalf("SBX-47: Read deny rule must inject /secrets/* into DenyRead; got %v", p.DenyRead)
	}
}

// TestPolicyFromSettingsEnabledPlatforms verifies SBX-37:
// sandbox.enabledPlatforms field is read into Policy.EnabledPlatforms.
func TestPolicyFromSettingsEnabledPlatforms(t *testing.T) {
	dir := t.TempDir()
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled":          true,
			"enabledPlatforms": []any{"linux"},
		},
	}
	p := PolicyFromSettingsAt(s, dir, dir)
	if len(p.EnabledPlatforms) != 1 || p.EnabledPlatforms[0] != "linux" {
		t.Fatalf("SBX-37: EnabledPlatforms must be [linux]; got %v", p.EnabledPlatforms)
	}
}

// TestPolicyFromSettingsNetworkDomains verifies SBX-48:
// sandbox.network.allowedDomains/deniedDomains map into Policy fields.
func TestPolicyFromSettingsNetworkDomains(t *testing.T) {
	dir := t.TempDir()
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled": true,
			"network": map[string]any{
				"allowedDomains": []any{"api.github.com"},
				"deniedDomains":  []any{"evil.com"},
			},
		},
	}
	p := PolicyFromSettingsAt(s, dir, dir)
	if !containsPath(p.AllowedDomains, "api.github.com") {
		t.Fatalf("SBX-48: AllowedDomains must contain api.github.com; got %v", p.AllowedDomains)
	}
	if !containsPath(p.DeniedDomains, "evil.com") {
		t.Fatalf("SBX-48: DeniedDomains must contain evil.com; got %v", p.DeniedDomains)
	}
}

// TestPolicyFromSettingsAllowUnixSockets verifies SBX-49:
// sandbox.network.allowUnixSockets maps into Policy.AllowUnixSockets.
func TestPolicyFromSettingsAllowUnixSockets(t *testing.T) {
	dir := t.TempDir()
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled": true,
			"network": map[string]any{
				"allowUnixSockets": []any{"/var/run/docker.sock"},
			},
		},
	}
	p := PolicyFromSettingsAt(s, dir, dir)
	if !containsPath(p.AllowUnixSockets, "/var/run/docker.sock") {
		t.Fatalf("SBX-49: AllowUnixSockets must contain /var/run/docker.sock; got %v", p.AllowUnixSockets)
	}
}

// TestSupportedForEnabledPlatforms verifies SBX-37:
// SupportedForPolicy returns false when current platform is not in EnabledPlatforms.
func TestSupportedForEnabledPlatforms(t *testing.T) {
	// Platform that can't match either darwin or linux
	p := Policy{
		Enabled:          true,
		EnabledPlatforms: []string{"nonexistent-os"},
	}
	if SupportedForPolicy(p) {
		t.Fatal("SBX-37: SupportedForPolicy must return false when platform not in EnabledPlatforms")
	}
}

// TestSupportedForPolicyNoRestriction verifies that empty EnabledPlatforms
// uses the base Supported() result.
func TestSupportedForPolicyNoRestriction(t *testing.T) {
	p := Policy{Enabled: true} // no EnabledPlatforms restriction
	got := SupportedForPolicy(p)
	want := Supported()
	if got != want {
		t.Fatalf("SupportedForPolicy with empty EnabledPlatforms: got %v want %v", got, want)
	}
}

// TestUnavailableReason verifies SBX-39:
// UnavailableReason returns a non-empty string when sandbox is enabled but unsupported.
func TestUnavailableReason(t *testing.T) {
	// Policy enabled but platform not in EnabledPlatforms
	p := Policy{
		Enabled:          true,
		EnabledPlatforms: []string{"nonexistent-os"},
	}
	reason := UnavailableReason(p)
	if reason == "" {
		t.Fatal("SBX-39: UnavailableReason must return non-empty when platform not in enabledPlatforms")
	}
}

// TestUnavailableReasonWhenDisabled verifies SBX-39:
// UnavailableReason returns empty when sandbox is not enabled (no noise).
func TestUnavailableReasonWhenDisabled(t *testing.T) {
	p := Policy{Enabled: false}
	reason := UnavailableReason(p)
	if reason != "" {
		t.Fatalf("SBX-39: UnavailableReason with disabled sandbox must return empty; got %q", reason)
	}
}

// TestAutoAllowBashIfSandboxedDefaultTrue verifies SBX-35:
// PolicyFromSettings defaults AutoAllowBashIfSandboxed to true (mirrors CC:471).
func TestAutoAllowBashIfSandboxedDefaultTrue(t *testing.T) {
	s := contracts.Settings{
		Sandbox: map[string]any{"enabled": true},
	}
	p := PolicyFromSettings(s)
	if !p.AutoAllowBashIfSandboxed {
		t.Error("SBX-35: AutoAllowBashIfSandboxed must default to true when not specified in settings")
	}
}

// TestAutoAllowBashIfSandboxedExplicitFalse verifies SBX-35:
// Setting autoAllowBashIfSandboxed=false in settings propagates to Policy.
func TestAutoAllowBashIfSandboxedExplicitFalse(t *testing.T) {
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled":                  true,
			"autoAllowBashIfSandboxed": false,
		},
	}
	p := PolicyFromSettings(s)
	if p.AutoAllowBashIfSandboxed {
		t.Error("SBX-35: AutoAllowBashIfSandboxed must be false when settings.sandbox.autoAllowBashIfSandboxed=false")
	}
}

// TestAutoAllowBashIfSandboxedExplicitTrue verifies SBX-35:
// Setting autoAllowBashIfSandboxed=true in settings propagates to Policy.
func TestAutoAllowBashIfSandboxedExplicitTrue(t *testing.T) {
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled":                  true,
			"autoAllowBashIfSandboxed": true,
		},
	}
	p := PolicyFromSettings(s)
	if !p.AutoAllowBashIfSandboxed {
		t.Error("SBX-35: AutoAllowBashIfSandboxed must be true when settings.sandbox.autoAllowBashIfSandboxed=true")
	}
}

// TestAutoAllowBashIfSandboxedZeroValuePolicy verifies SBX-35:
// A zero-value Policy (no settings) has AutoAllowBashIfSandboxed=false (not
// the default — the default only applies via PolicyFromSettings).
func TestAutoAllowBashIfSandboxedZeroValuePolicy(t *testing.T) {
	var p Policy
	if p.AutoAllowBashIfSandboxed {
		t.Error("SBX-35: zero-value Policy.AutoAllowBashIfSandboxed must be false")
	}
}

// containsPath is a test helper that checks whether paths contains target.
func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}
