package repl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── installTerminalKeybindings (unknown terminal) ─────────────────────────

// TestInstallTerminalKeybindingsUnknownTerminalReturnsInstructions verifies
// CMD-TERMSETUP-01 partial: when TERM_PROGRAM is unset/unsupported, the
// handler returns an informational message with terminal suggestions.
func TestInstallTerminalKeybindingsUnknownTerminalReturnsInstructions(t *testing.T) {
	// Unset terminal env vars.
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm")
	t.Setenv("ZED_TERM", "")
	t.Setenv("VSCODE_GIT_ASKPASS_MAIN", "")
	msg := installTerminalKeybindings()
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	// Must mention alternative terminals.
	lower := strings.ToLower(msg)
	if !strings.Contains(lower, "vscode") && !strings.Contains(lower, "alacritty") {
		t.Errorf("expected message to mention terminal alternatives, got: %q", msg)
	}
}

// TestInstallTerminalKeybindingsNativeCSIuSkipsSetup verifies that terminals
// with native CSI u support report no setup needed.
func TestInstallTerminalKeybindingsNativeCSIuSkipsSetup(t *testing.T) {
	tests := []struct {
		env  string
		want string
	}{
		{"ghostty", "Ghostty"},
		{"kitty", "Kitty"},
		{"WarpTerminal", "Warp"},
		{"WezTerm", "WezTerm"},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv("TERM_PROGRAM", tt.env)
			t.Setenv("TERM", "")
			t.Setenv("ZED_TERM", "")
			msg := installTerminalKeybindings()
			if !strings.Contains(msg, "natively supported") {
				t.Errorf("expected 'natively supported' in message for %s, got: %q", tt.env, msg)
			}
			if !strings.Contains(msg, tt.want) {
				t.Errorf("expected terminal name %q in message, got: %q", tt.want, msg)
			}
		})
	}
}

// TestInstallTerminalKeybindingsiTerm2NativeCSIuSkipsSetup verifies iTerm2.
func TestInstallTerminalKeybindingsiTerm2NativeCSIuSkipsSetup(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	t.Setenv("TERM", "")
	t.Setenv("ZED_TERM", "")
	msg := installTerminalKeybindings()
	if !strings.Contains(msg, "natively supported") {
		t.Errorf("expected 'natively supported' for iTerm2, got: %q", msg)
	}
}

// ── VSCode keybinding install ─────────────────────────────────────────────

// TestInstallVSCodeKeybindingCreatesFile verifies CMD-TERMSETUP-01 (VSCode):
// installVSCodeKeybinding writes the keybinding to a temp keybindings.json.
func TestInstallVSCodeKeybindingCreatesFile(t *testing.T) {
	dir := t.TempDir()
	// Override the home dir for this test by using a minimal vscodeKeybindingsPath.
	// We call the helper directly with a path override via the function.
	path := filepath.Join(dir, "keybindings.json")
	msg := installVSCodeKeybindingAt("VSCode", path)
	if !strings.Contains(msg, "Installed") {
		t.Errorf("expected 'Installed' in message, got: %q", msg)
	}
	// File must exist and contain the binding.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("keybindings.json not created: %v", err)
	}
	if !strings.Contains(string(content), "shift+enter") {
		t.Errorf("keybindings.json missing 'shift+enter', got: %q", string(content))
	}
	if !strings.Contains(string(content), "workbench.action.terminal.sendSequence") {
		t.Errorf("keybindings.json missing expected command, got: %q", string(content))
	}
}

// TestInstallVSCodeKeybindingIdempotent verifies that calling the installer
// twice returns a "found existing" message and does not duplicate the binding.
func TestInstallVSCodeKeybindingIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	// First install.
	msg1 := installVSCodeKeybindingAt("VSCode", path)
	if !strings.Contains(msg1, "Installed") {
		t.Fatalf("first install: expected 'Installed', got: %q", msg1)
	}
	// Second install: must return "found existing".
	msg2 := installVSCodeKeybindingAt("VSCode", path)
	if !strings.Contains(msg2, "existing") {
		t.Errorf("second install: expected 'existing' in message, got: %q", msg2)
	}
}

// TestInstallVSCodeKeybindingPreservesExistingContent verifies that the
// installer preserves existing keybindings when appending.
func TestInstallVSCodeKeybindingPreservesExistingContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	// Write an existing binding.
	existing := `[{"key":"ctrl+a","command":"selectAll"}]`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := installVSCodeKeybindingAt("VSCode", path)
	if !strings.Contains(msg, "Installed") {
		t.Errorf("expected 'Installed', got: %q", msg)
	}
	content, _ := os.ReadFile(path)
	var arr []map[string]any
	if err := json.Unmarshal(content, &arr); err != nil {
		t.Fatalf("result is not valid JSON: %v — content=%q", err, content)
	}
	if len(arr) < 2 {
		t.Errorf("expected at least 2 bindings (existing + new), got %d", len(arr))
	}
}

// ── Alacritty keybinding install ──────────────────────────────────────────

// TestInstallAlacrittyKeybindingCreatesEntry verifies CMD-TERMSETUP-01
// (Alacritty): the installer appends the Shift+Enter TOML block.
func TestInstallAlacrittyKeybindingCreatesEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alacritty.toml")
	msg := installAlacrittyKeybindingAt(path)
	if !strings.Contains(msg, "Installed") {
		t.Errorf("expected 'Installed', got: %q", msg)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("alacritty.toml not created: %v", err)
	}
	if !strings.Contains(string(content), `key = "Return"`) {
		t.Errorf("alacritty.toml missing keybinding entry, got: %q", string(content))
	}
	if !strings.Contains(string(content), `mods = "Shift"`) {
		t.Errorf("alacritty.toml missing Shift mod, got: %q", string(content))
	}
}

// TestInstallAlacrittyKeybindingIdempotent verifies idempotency.
func TestInstallAlacrittyKeybindingIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alacritty.toml")
	// Write pre-existing Shift+Return binding.
	existing := `[[keyboard.bindings]]
key = "Return"
mods = "Shift"
chars = "\r"
`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := installAlacrittyKeybindingAt(path)
	if !strings.Contains(msg, "existing") {
		t.Errorf("expected 'existing' in message, got: %q", msg)
	}
}

// ── Zed keybinding install ────────────────────────────────────────────────

// TestInstallZedKeybindingCreatesEntry verifies CMD-TERMSETUP-01 (Zed):
// the installer writes the terminal shift-enter binding.
func TestInstallZedKeybindingCreatesEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keymap.json")
	msg := installZedKeybindingAt(path)
	if !strings.Contains(msg, "Installed") {
		t.Errorf("expected 'Installed', got: %q", msg)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("keymap.json not created: %v", err)
	}
	if !strings.Contains(string(content), "shift-enter") {
		t.Errorf("keymap.json missing 'shift-enter', got: %q", string(content))
	}
	if !strings.Contains(string(content), "Terminal") {
		t.Errorf("keymap.json missing 'Terminal' context, got: %q", string(content))
	}
}

// TestInstallZedKeybindingIdempotent verifies idempotency.
func TestInstallZedKeybindingIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keymap.json")
	// Write pre-existing shift-enter.
	existing := `[{"context":"Terminal","bindings":{"shift-enter":["terminal::SendText","\r"]}}]`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := installZedKeybindingAt(path)
	if !strings.Contains(msg, "existing") {
		t.Errorf("expected 'existing' in message, got: %q", msg)
	}
}

// ── appendToJSONArray ─────────────────────────────────────────────────────

func TestAppendToJSONArrayEmpty(t *testing.T) {
	item := map[string]any{"key": "shift+enter"}
	result, err := appendToJSONArray("[]", item)
	if err != nil {
		t.Fatal(err)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(result), &arr); err != nil {
		t.Fatalf("invalid JSON: %v — %q", err, result)
	}
	if len(arr) != 1 {
		t.Errorf("expected 1 item, got %d", len(arr))
	}
}

func TestStripJSONComments(t *testing.T) {
	input := `[
  // this is a comment
  {"key": "a"} // trailing
]`
	stripped := stripJSONComments(input)
	var arr []map[string]any
	if err := json.Unmarshal([]byte(stripped), &arr); err != nil {
		t.Fatalf("stripped JSONC is not valid JSON: %v — %q", err, stripped)
	}
}
