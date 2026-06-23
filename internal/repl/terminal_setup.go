package repl

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// installTerminalKeybindings installs the Shift+Enter keybinding for the
// detected terminal. Returns a human-readable result message.
//
// Supported terminals (CC parity — terminalSetup.tsx):
//   - Apple Terminal  (macOS only): enables Option as Meta key via PlistBuddy
//   - VSCode / Cursor / Windsurf: modifies keybindings.json
//   - Alacritty:                  appends to alacritty.toml
//   - Zed:                        modifies ~/.config/zed/keymap.json
//
// Terminals that natively support CSI u / Kitty protocol (Ghostty, Kitty,
// iTerm2, WezTerm, Warp) do not need setup — we report that.
//
// The TERM_PROGRAM env variable is the primary detection signal (same as CC).
func installTerminalKeybindings() string {
	terminal := terminalFromEnv()

	// Terminals with native CSI u / Kitty protocol — no setup needed.
	nativeCsiu := map[string]string{
		"ghostty":      "Ghostty",
		"kitty":        "Kitty",
		"iTerm.app":    "iTerm2",
		"WezTerm":      "WezTerm",
		"WarpTerminal": "Warp",
	}
	if displayName, ok := nativeCsiu[terminal]; ok {
		return fmt.Sprintf("Shift+Enter is natively supported in %s.\n\nNo configuration needed. Just use Shift+Enter to add newlines.", displayName)
	}

	switch terminal {
	case "Apple_Terminal":
		if runtime.GOOS != "darwin" {
			return "Apple Terminal setup is only supported on macOS."
		}
		return installAppleTerminalOptionMeta()
	case "vscode":
		return installVSCodeKeybinding("VSCode")
	case "cursor":
		return installVSCodeKeybinding("Cursor")
	case "windsurf":
		return installVSCodeKeybinding("Windsurf")
	case "alacritty":
		return installAlacrittyKeybinding()
	case "zed":
		return installZedKeybinding()
	default:
		platformTerminals := ""
		if runtime.GOOS == "darwin" {
			platformTerminals = "   • macOS: Apple Terminal\n"
		}
		return fmt.Sprintf(`Terminal setup cannot be run from %s.

This command configures a convenient Shift+Enter shortcut for multi-line prompts.
Note: You can already use backslash (\) + return to add newlines.

To set up the shortcut (optional):
1. Exit tmux/screen temporarily
2. Run /terminal-setup directly in one of these terminals:
%s   • IDE: VSCode, Cursor, Windsurf, Zed
   • Other: Alacritty
3. Return to tmux/screen - settings will persist

Note: iTerm2, WezTerm, Ghostty, Kitty, and Warp support Shift+Enter natively.`,
			terminalDisplayName(terminal), platformTerminals)
	}
}

// terminalFromEnv returns the CC-style terminal name from environment variables.
// Primary signal: TERM_PROGRAM; secondary: VSCODE_INJECTION and similar.
func terminalFromEnv() string {
	termProgram := os.Getenv("TERM_PROGRAM")
	switch termProgram {
	case "iTerm.app", "WezTerm", "WarpTerminal", "Apple_Terminal", "alacritty":
		return termProgram
	case "vscode":
		// Distinguish VSCode / Cursor / Windsurf by inspecting PATH or known env vars.
		if isWindsurf() {
			return "windsurf"
		}
		if isCursor() {
			return "cursor"
		}
		return "vscode"
	}
	// Ghostty uses TERM=xterm-ghostty
	if os.Getenv("TERM") == "xterm-ghostty" {
		return "ghostty"
	}
	// Kitty uses TERM=xterm-kitty
	if os.Getenv("TERM") == "xterm-kitty" {
		return "kitty"
	}
	// Zed sets ZED_TERM or similar
	if os.Getenv("ZED_TERM") != "" || strings.Contains(os.Getenv("TERM_PROGRAM_VERSION"), "zed") {
		return "zed"
	}
	return termProgram
}

func terminalDisplayName(t string) string {
	if t == "" {
		return "your current terminal"
	}
	return t
}

func isCursor() bool {
	askpass := os.Getenv("VSCODE_GIT_ASKPASS_MAIN")
	return strings.Contains(askpass, ".cursor-server") || strings.Contains(os.Getenv("PATH"), ".cursor-server")
}

func isWindsurf() bool {
	askpass := os.Getenv("VSCODE_GIT_ASKPASS_MAIN")
	return strings.Contains(askpass, ".windsurf-server") || strings.Contains(os.Getenv("PATH"), ".windsurf-server")
}

// ── Apple Terminal ─────────────────────────────────────────────────────────

// installAppleTerminalOptionMeta uses PlistBuddy to enable "Use Option as Meta
// key" and disable the audio bell in the default Terminal.app profile.
// CC parity: enableOptionAsMetaForTerminal in terminalSetup.tsx.
func installAppleTerminalOptionMeta() string {
	plistPath := appleTerminalPlistPath()

	// Read default and startup profile names.
	defaultProfile, err := runOutput("defaults", "read", "com.apple.Terminal", "Default Window Settings")
	if err != nil || strings.TrimSpace(defaultProfile) == "" {
		return "Failed to read default Terminal.app profile. Terminal.app may not be running."
	}
	startupProfile, _ := runOutput("defaults", "read", "com.apple.Terminal", "Startup Window Settings")

	profiles := []string{strings.TrimSpace(defaultProfile)}
	if sp := strings.TrimSpace(startupProfile); sp != "" && sp != profiles[0] {
		profiles = append(profiles, sp)
	}

	updated := false
	for _, profile := range profiles {
		ok1 := plistBuddySet(plistPath, profile, "useOptionAsMetaKey", "bool", "true")
		ok2 := plistBuddySet(plistPath, profile, "Bell", "bool", "false")
		if ok1 || ok2 {
			updated = true
		}
	}
	if !updated {
		return "Failed to update Terminal.app settings. Check that Terminal.app is your active terminal."
	}

	// Flush preferences cache.
	_ = runSilentNoErr("killall", "cfprefsd")

	return `Configured Terminal.app settings:
- Enabled "Use Option as Meta key"
- Switched to visual bell
Option+Enter will now enter a newline.
You must restart Terminal.app for changes to take effect.`
}

func appleTerminalPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Preferences", "com.apple.Terminal.plist")
}

// plistBuddySet adds or sets a PlistBuddy property. Returns true on success.
func plistBuddySet(plist, profile, key, typ, value string) bool {
	prop := fmt.Sprintf(":'Window Settings':'%s':%s", profile, key)
	// Try Add first; if it fails (already exists), try Set.
	addCmd := fmt.Sprintf("Add %s %s %s", prop, typ, value)
	if err := runSilentNoErr("/usr/libexec/PlistBuddy", "-c", addCmd, plist); err == nil {
		return true
	}
	setCmd := fmt.Sprintf("Set %s %s", prop, value)
	return runSilentNoErr("/usr/libexec/PlistBuddy", "-c", setCmd, plist) == nil
}

// ── VSCode / Cursor / Windsurf ─────────────────────────────────────────────

// vscodeKeybinding is the JSON object we inject into keybindings.json.
type vscodeKeybinding struct {
	Key     string `json:"key"`
	Command string `json:"command"`
	Args    struct {
		Text string `json:"text"`
	} `json:"args"`
	When string `json:"when"`
}

// installVSCodeKeybinding appends the Shift+Enter binding to the editor's
// keybindings.json. CC parity: installBindingsForVSCodeTerminal.
func installVSCodeKeybinding(editor string) string {
	keybindingsPath, err := vscodeKeybindingsPath(editor)
	if err != nil {
		return fmt.Sprintf("Cannot determine %s keybindings path: %v", editor, err)
	}
	return installVSCodeKeybindingAt(editor, keybindingsPath)
}

// installVSCodeKeybindingAt is the testable core: editor is the human-readable
// name and keybindingsPath is the target file path.
func installVSCodeKeybindingAt(editor, keybindingsPath string) string {
	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(keybindingsPath), 0o755); err != nil {
		return fmt.Sprintf("Cannot create %s user directory: %v", editor, err)
	}

	// Read existing content (or start with empty array).
	existing := "[]"
	fileExists := false
	if content, err := os.ReadFile(keybindingsPath); err == nil {
		existing = string(content)
		fileExists = true
	}

	// Backup if file already exists.
	if fileExists {
		backupPath := fmt.Sprintf("%s.%x.bak", keybindingsPath, rand.Int31())
		if err := copyFile(keybindingsPath, backupPath); err != nil {
			return fmt.Sprintf("Error backing up existing %s terminal keybindings. Bailing out.\nSee %s", editor, keybindingsPath)
		}
	}

	// Check if binding already exists.
	if strings.Contains(existing, "shift+enter") && strings.Contains(existing, "workbench.action.terminal.sendSequence") {
		return fmt.Sprintf("Found existing %s terminal Shift+Enter key binding. Remove it to continue.\nSee %s", editor, keybindingsPath)
	}

	// Build new binding.
	binding := vscodeKeybinding{
		Key:     "shift+enter",
		Command: "workbench.action.terminal.sendSequence",
		When:    "terminalFocus",
	}
	binding.Args.Text = "\r"

	updated, err := appendToJSONArray(existing, binding)
	if err != nil {
		return fmt.Sprintf("Failed to update %s keybindings: %v", editor, err)
	}

	if err := os.WriteFile(keybindingsPath, []byte(updated), 0o644); err != nil {
		return fmt.Sprintf("Failed to write %s keybindings: %v", editor, err)
	}
	return fmt.Sprintf("Installed %s terminal Shift+Enter key binding\nSee %s", editor, keybindingsPath)
}

// vscodeKeybindingsPath returns the path to the editor's keybindings.json.
func vscodeKeybindingsPath(editor string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	editorDir := editor // "VSCode", "Cursor", "Windsurf"
	if editor == "VSCode" {
		editorDir = "Code"
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", editorDir, "User", "keybindings.json"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", errors.New("APPDATA not set")
		}
		return filepath.Join(appData, editorDir, "User", "keybindings.json"), nil
	default: // linux and others
		return filepath.Join(home, ".config", editorDir, "User", "keybindings.json"), nil
	}
}

// ── Alacritty ─────────────────────────────────────────────────────────────

const alacrittyKeybindingBlock = `
[[keyboard.bindings]]
key = "Return"
mods = "Shift"
chars = "\r"
`

// installAlacrittyKeybinding appends the Shift+Enter binding to alacritty.toml.
// CC parity: installBindingsForAlacritty.
func installAlacrittyKeybinding() string {
	configPath, exists := alacrittyConfigPath()
	if configPath == "" {
		return "No valid config path found for Alacritty."
	}
	return installAlacrittyKeybindingAt(configPath, exists)
}

// installAlacrittyKeybindingAt is the testable core.
// configPath is the target file; exists indicates whether it already exists.
func installAlacrittyKeybindingAt(configPath string, exists ...bool) string {
	// Allow calling with just path (tests write a pre-existing file themselves).
	fileExists := false
	if _, err := os.Stat(configPath); err == nil {
		fileExists = true
	}
	// Caller can override the existence check (for future use).
	if len(exists) > 0 {
		fileExists = exists[0]
	}

	var existing string
	if fileExists {
		content, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Sprintf("Cannot read Alacritty config: %v", err)
		}
		existing = string(content)
		// Check if already configured.
		if strings.Contains(existing, `mods = "Shift"`) && strings.Contains(existing, `key = "Return"`) {
			return fmt.Sprintf("Found existing Alacritty Shift+Enter key binding. Remove it to continue.\nSee %s", configPath)
		}
		// Backup.
		backupPath := fmt.Sprintf("%s.%x.bak", configPath, rand.Int31())
		if err := copyFile(configPath, backupPath); err != nil {
			return fmt.Sprintf("Error backing up existing Alacritty config. Bailing out.\nSee %s", configPath)
		}
	} else {
		// Ensure directory exists.
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			return fmt.Sprintf("Cannot create Alacritty config directory: %v", err)
		}
	}

	updated := existing
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		updated += "\n"
	}
	updated += alacrittyKeybindingBlock

	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return fmt.Sprintf("Failed to write Alacritty config: %v", err)
	}
	return fmt.Sprintf("Installed Alacritty Shift+Enter key binding\nYou may need to restart Alacritty for changes to take effect\nSee %s", configPath)
}

// alacrittyConfigPath returns the preferred Alacritty config path and whether
// it already exists.
func alacrittyConfigPath() (path string, exists bool) {
	home, _ := os.UserHomeDir()

	var candidates []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "alacritty", "alacritty.toml"))
	}
	candidates = append(candidates, filepath.Join(home, ".config", "alacritty", "alacritty.toml"))
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			candidates = append(candidates, filepath.Join(appData, "alacritty", "alacritty.toml"))
		}
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, true
		}
	}
	if len(candidates) > 0 {
		return candidates[0], false
	}
	return "", false
}

// ── Zed ──────────────────────────────────────────────────────────────────

// installZedKeybinding appends the Shift+Enter binding to Zed's keymap.json.
// CC parity: installBindingsForZed.
func installZedKeybinding() string {
	home, _ := os.UserHomeDir()
	keymapPath := filepath.Join(home, ".config", "zed", "keymap.json")
	return installZedKeybindingAt(keymapPath)
}

// installZedKeybindingAt is the testable core.
func installZedKeybindingAt(keymapPath string) string {
	if err := os.MkdirAll(filepath.Dir(keymapPath), 0o755); err != nil {
		return fmt.Sprintf("Cannot create Zed config directory: %v", err)
	}

	existing := "[]"
	fileExists := false
	if content, err := os.ReadFile(keymapPath); err == nil {
		existing = string(content)
		fileExists = true
	}

	// Check if binding already installed.
	if strings.Contains(existing, "shift-enter") {
		return fmt.Sprintf("Found existing Zed Shift+Enter key binding. Remove it to continue.\nSee %s", keymapPath)
	}

	// Backup if file exists.
	if fileExists {
		backupPath := fmt.Sprintf("%s.%x.bak", keymapPath, rand.Int31())
		if err := copyFile(keymapPath, backupPath); err != nil {
			return fmt.Sprintf("Error backing up existing Zed keymap. Bailing out.\nSee %s", keymapPath)
		}
	}

	// Parse, add entry, and re-serialize.
	var keymap []map[string]any
	if err := json.Unmarshal([]byte(existing), &keymap); err != nil || keymap == nil {
		keymap = []map[string]any{}
	}
	keymap = append(keymap, map[string]any{
		"context":  "Terminal",
		"bindings": map[string]any{"shift-enter": []any{"terminal::SendText", "\r"}},
	})
	out, err := json.MarshalIndent(keymap, "", "  ")
	if err != nil {
		return fmt.Sprintf("Failed to serialize Zed keymap: %v", err)
	}

	if err := os.WriteFile(keymapPath, append(out, '\n'), 0o644); err != nil {
		return fmt.Sprintf("Failed to write Zed keymap: %v", err)
	}
	return fmt.Sprintf("Installed Zed Shift+Enter key binding\nSee %s", keymapPath)
}

// ── helpers ───────────────────────────────────────────────────────────────

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	return os.WriteFile(dst, content, 0o644)
}

// appendToJSONArray inserts item into a JSON array string and returns the
// updated string. Handles JSONC (comments) by stripping them first.
func appendToJSONArray(jsonStr string, item any) (string, error) {
	// Strip single-line (//) comments for parsing (JSONC tolerance).
	stripped := stripJSONComments(jsonStr)

	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(stripped), &arr); err != nil {
		// If we can't parse, start fresh.
		arr = []json.RawMessage{}
	}
	itemBytes, err := json.MarshalIndent(item, "  ", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal item: %w", err)
	}
	arr = append(arr, itemBytes)
	out, err := json.MarshalIndent(arr, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal array: %w", err)
	}
	return string(out) + "\n", nil
}

// stripJSONComments removes single-line // comments from a JSON string.
// This is a minimal JSONC stripper for keybindings.json compatibility.
func stripJSONComments(s string) string {
	var b strings.Builder
	inStr := false
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if escaped {
			b.WriteByte(ch)
			escaped = false
			continue
		}
		if inStr {
			if ch == '\\' {
				b.WriteByte(ch)
				escaped = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			b.WriteByte(ch)
			continue
		}
		if ch == '"' {
			inStr = true
			b.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(s) && s[i+1] == '/' {
			// Skip until end of line.
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// runOutput runs a command and returns its stdout output.
func runOutput(name string, args ...string) (string, error) {
	return execOutput(name, args)
}

// runSilentNoErr runs a command discarding output and returns any error.
func runSilentNoErr(name string, args ...string) error {
	return execSilent(name, args)
}
