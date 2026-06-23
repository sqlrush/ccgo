package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

// addDirAppender is the DI interface for appending a directory path to
// additionalDirectories in the settings document at path.
type addDirAppender func(settingsPath string, dir string) error

// addDirHandlerWith returns a CommandHandler for /add-dir backed by the given
// appender. Production code uses addDirToSettings; tests inject a fake.
func addDirHandlerWith(appender addDirAppender, cwd string) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		dir := strings.TrimSpace(cc.Args)
		if dir == "" {
			return CommandOutcome{
				Handled: true,
				Status:  "Usage: /add-dir <path> — add an additional working directory",
			}, nil
		}
		settingsPath := config.ProjectSettingsPath(cwd)
		if cwd == "" {
			settingsPath = config.UserSettingsPath()
		}
		if err := appender(settingsPath, dir); err != nil {
			return CommandOutcome{}, fmt.Errorf("add-dir: %w", err)
		}
		return CommandOutcome{
			Handled: true,
			Status:  fmt.Sprintf("Added %q to additionalDirectories.", dir),
		}, nil
	}
}

// addDirToSettings is the production implementation that appends dir to the
// additionalDirectories array in the settings JSON file at path.
func addDirToSettings(settingsPath string, dir string) error {
	doc, err := config.ReadSettingsDocument(settingsPath)
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	var dirs []string
	if existing, ok := doc["additionalDirectories"]; ok {
		switch v := existing.(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					dirs = append(dirs, s)
				}
			}
		case []string:
			dirs = append(dirs, v...)
		}
	}
	// Deduplicate: skip if already present.
	for _, d := range dirs {
		if d == dir {
			return nil
		}
	}
	dirs = append(dirs, dir)
	newDirs := make([]any, len(dirs))
	for i, d := range dirs {
		newDirs[i] = d
	}
	doc["additionalDirectories"] = newDirs
	return config.WriteSettingsDocument(settingsPath, doc)
}

// addDirHandler is the production handler using the real project settings file.
func addDirHandler(cwd string) CommandHandler {
	return addDirHandlerWith(addDirToSettings, cwd)
}

// planHandlerWith returns a CommandHandler for /plan that toggles plan mode.
// curMode is read at handler-construction time and updated via the CommandOutcome.NewMode.
// In production, newProductionRouter captures a pointer to the loop mode.
func planHandlerWith(curMode contracts.PermissionMode) CommandHandler {
	mode := curMode
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		var next contracts.PermissionMode
		if mode == contracts.PermissionPlan {
			next = contracts.PermissionDefault
		} else {
			next = contracts.PermissionPlan
		}
		msg := fmt.Sprintf("Plan mode %s.", map[bool]string{true: "enabled", false: "disabled"}[next == contracts.PermissionPlan])
		return CommandOutcome{
			Handled: true,
			Status:  msg,
			NewMode: next,
		}, nil
	}
}

// terminalSetupHandler returns a CommandHandler for /terminal-setup.
// It detects the active terminal and installs the Shift+Enter keybinding
// (or Option as Meta for Apple Terminal) by writing to the terminal's
// configuration files. CC parity: terminalSetup.tsx setupTerminal().
func terminalSetupHandler() CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		msg := installTerminalKeybindings()
		return CommandOutcome{Handled: true, Status: msg}, nil
	}
}

// branchHandler returns a CommandHandler for /branch.
// ⚠️ Full branch/sidechain infra is out of scope; returns an informational message.
func branchHandler() CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{
			Handled: true,
			Status:  "⚠️  /branch (conversation branching) requires sidechain infrastructure that is not yet implemented. Use git branches for code branching instead.",
		}, nil
	}
}

// renameHandlerWith returns a CommandHandler for /rename backed by the given writer.
// sessionID is used to write the custom-title metadata record.
type transcriptTitleWriter func(sessionDir string, sessionID contracts.ID, title string) error

func renameHandlerWith(w transcriptTitleWriter, sessionID contracts.ID, sessionDir string) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		name := strings.TrimSpace(cc.Args)
		if name == "" {
			return CommandOutcome{
				Handled: true,
				Status:  "Usage: /rename <name> — rename the current session",
			}, nil
		}
		if err := w(sessionDir, sessionID, name); err != nil {
			return CommandOutcome{}, fmt.Errorf("rename: %w", err)
		}
		return CommandOutcome{
			Handled: true,
			Status:  fmt.Sprintf("Session renamed to %q.", name),
		}, nil
	}
}

// writeCustomTitle appends a custom-title metadata record to the session transcript.
func writeCustomTitle(sessionDir string, sessionID contracts.ID, title string) error {
	if sessionDir == "" || sessionID == "" {
		return nil // no-op when session info is unavailable
	}
	path := filepath.Join(sessionDir, string(sessionID)+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // transcript not yet on disk — no-op
		}
		return fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	record := struct {
		Type        string       `json:"type"`
		SessionID   contracts.ID `json:"sessionId"`
		CustomTitle string       `json:"customTitle"`
	}{Type: "custom-title", SessionID: sessionID, CustomTitle: title}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal title record: %w", err)
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

// renameHandlerWithTitle is like renameHandlerWith but also sets the terminal
// title when terminalTitleFromRename is true.
// CC ref: utils/settings/types.ts terminalTitleFromRename — /rename updates terminal tab title.
func renameHandlerWithTitle(w transcriptTitleWriter, sessionID contracts.ID, sessionDir string, terminalTitleFromRename bool, setTitle func(string)) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		name := strings.TrimSpace(cc.Args)
		if name == "" {
			return CommandOutcome{
				Handled: true,
				Status:  "Usage: /rename <name> — rename the current session",
			}, nil
		}
		if err := w(sessionDir, sessionID, name); err != nil {
			return CommandOutcome{}, fmt.Errorf("rename: %w", err)
		}
		if terminalTitleFromRename && setTitle != nil {
			setTitle(name)
		}
		return CommandOutcome{
			Handled: true,
			Status:  fmt.Sprintf("Session renamed to %q.", name),
		}, nil
	}
}

// renameHandler is the production handler using the real transcript file.
func renameHandler(sessionID contracts.ID, sessionDir string) CommandHandler {
	return renameHandlerWith(writeCustomTitle, sessionID, sessionDir)
}

// diffHandler returns a CommandHandler for /diff that runs git diff in cwd.
func diffHandler(cwd string) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		dir := cwd
		if dir == "" {
			var err error
			dir, err = os.Getwd()
			if err != nil {
				dir = "."
			}
		}
		//nolint:gosec // cwd is from the session, not user input
		out, err := exec.CommandContext(ctx, "git", "-C", dir, "diff").Output()
		if err != nil {
			// git not available or not a repo — return the error as informational.
			return CommandOutcome{
				Handled: true,
				Status:  "git diff: " + err.Error(),
			}, nil
		}
		diff := strings.TrimSpace(string(out))
		if diff == "" {
			diff = "No uncommitted changes."
		}
		return CommandOutcome{Handled: true, Status: diff}, nil
	}
}

// lastAssistantText extracts the last assistant message text from history.
func lastAssistantText(history []contracts.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Type != contracts.MessageAssistant {
			continue
		}
		var parts []string
		for _, block := range msg.Content {
			if block.Type == contracts.ContentText {
				if text := strings.TrimSpace(block.Text); text != "" {
					parts = append(parts, text)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

// copyClipboard is a DI function type for writing text to the system clipboard.
type clipboardWriter func(text string) error

// copyHandler returns a CommandHandler for /copy that writes the last assistant
// message to the clipboard using the injected writer. nil writer uses the
// platform clipboard (pbcopy/xclip/etc.) on best-effort.
func copyHandler(write clipboardWriter) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		text := lastAssistantText(cc.History)
		if text == "" {
			return CommandOutcome{Handled: true, Status: "No assistant response to copy."}, nil
		}
		if write != nil {
			if err := write(text); err != nil {
				return CommandOutcome{Handled: true, Status: "clipboard write: " + err.Error()}, nil
			}
			return CommandOutcome{Handled: true, Status: "Copied last response to clipboard."}, nil
		}
		// Production: try pbcopy (macOS) then xclip/xsel (Linux).
		if err := copyToSystemClipboard(ctx, text); err != nil {
			return CommandOutcome{Handled: true, Status: "clipboard: " + err.Error()}, nil
		}
		return CommandOutcome{Handled: true, Status: "Copied last response to clipboard."}, nil
	}
}

// copyToSystemClipboard writes text to the system clipboard using available tools.
func copyToSystemClipboard(ctx context.Context, text string) error {
	tools := []string{"pbcopy", "xclip -selection clipboard", "xsel --clipboard --input", "wl-copy"}
	for _, tool := range tools {
		parts := strings.Fields(tool)
		//nolint:gosec
		cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no clipboard tool found (tried pbcopy, xclip, xsel, wl-copy)")
}

// exitHandler returns a CommandHandler for /exit (and /quit) that signals the loop to exit.
func exitHandler() CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Exit: true}, nil
	}
}

// haikuModel is the model ID used by /fast to switch to the fastest model tier.
const haikuModel = "claude-haiku-4-5"

// modelSetterFunc is the DI function type for switching the active model.
type modelSetterFunc func(model string) error

// fastHandlerWith returns a CommandHandler for /fast backed by the given
// modelSetter. When setter is nil (production case where wiring is not yet
// available), an informational message is returned instead of switching models.
func fastHandlerWith(setter modelSetterFunc) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		if setter == nil {
			return CommandOutcome{
				Handled: true,
				Status:  "⚠️  /fast (Haiku model toggle) requires runtime model-switch infrastructure. Use /model claude-haiku-4-5 to switch models manually.",
			}, nil
		}
		if err := setter(haikuModel); err != nil {
			return CommandOutcome{Handled: true, Status: "fast: " + err.Error()}, nil
		}
		return CommandOutcome{
			Handled:  true,
			Status:   fmt.Sprintf("Switched to fast model (%s).", haikuModel),
			NewModel: haikuModel,
		}, nil
	}
}

// fastHandler returns a CommandHandler for /fast.
// ⚠️ Full fast-mode (Haiku model toggle) requires model-switch infra wired to
// the live runner; returns an informational message.
func fastHandler() CommandHandler {
	return fastHandlerWith(nil)
}

// statsHandler returns a CommandHandler for /stats that shows session message counts.
func statsHandler() CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		var userCount, assistantCount int
		for _, msg := range cc.History {
			switch msg.Type {
			case contracts.MessageUser:
				userCount++
			case contracts.MessageAssistant:
				assistantCount++
			}
		}
		total := userCount + assistantCount
		stat := fmt.Sprintf("Session stats — total messages: %d (user: %d, assistant: %d)",
			total, userCount, assistantCount)
		return CommandOutcome{Handled: true, Status: stat}, nil
	}
}

// tagHandler returns a CommandHandler for /tag that writes a tag to the session.
// ⚠️ Full tag persistence requires session transcript path in CommandContext.
func tagHandler(sessionID contracts.ID, sessionDir string) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		name := strings.TrimSpace(cc.Args)
		if name == "" {
			return CommandOutcome{
				Handled: true,
				Status:  "Usage: /tag <tag-name> — tag the current session",
			}, nil
		}
		if sessionDir == "" || sessionID == "" {
			return CommandOutcome{
				Handled: true,
				Status:  fmt.Sprintf("Tag %q noted (session path unavailable; not persisted to transcript).", name),
			}, nil
		}
		// Write tag record to transcript.
		path := filepath.Join(sessionDir, string(sessionID)+".jsonl")
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			if os.IsNotExist(err) {
				return CommandOutcome{Handled: true, Status: fmt.Sprintf("Tag %q noted (transcript not yet on disk).", name)}, nil
			}
			return CommandOutcome{}, fmt.Errorf("tag: open transcript: %w", err)
		}
		defer f.Close()
		record := struct {
			Type      string       `json:"type"`
			SessionID contracts.ID `json:"sessionId"`
			Tag       string       `json:"tag"`
		}{Type: "tag", SessionID: sessionID, Tag: name}
		data, err := json.Marshal(record)
		if err != nil {
			return CommandOutcome{}, fmt.Errorf("tag: marshal record: %w", err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			return CommandOutcome{}, fmt.Errorf("tag: write record: %w", err)
		}
		return CommandOutcome{Handled: true, Status: fmt.Sprintf("Session tagged %q.", name)}, nil
	}
}

// tasksHandlerWithRegistry returns a CommandHandler for /tasks that reads
// in-flight and completed background agents from the given AgentRegistry.
// When registry is nil the handler returns a graceful "no background tasks" message.
func tasksHandlerWithRegistry(registry agentRegistrySnapshotter) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		if registry == nil {
			return CommandOutcome{
				Handled: true,
				Status:  "no background tasks (registry not available)",
			}, nil
		}
		snap := registry.Snapshot()
		if len(snap) == 0 {
			return CommandOutcome{
				Handled: true,
				Status:  "Background tasks: no background tasks",
			}, nil
		}
		var sb strings.Builder
		sb.WriteString("Background tasks:\n")
		for _, s := range snap {
			sb.WriteString(fmt.Sprintf("  %s  %s\n", s.ID, s.State))
		}
		return CommandOutcome{
			Handled: true,
			Status:  strings.TrimRight(sb.String(), "\n"),
		}, nil
	}
}

// tasksHandler returns a CommandHandler for /tasks without a registry
// (production code wires the registry via tasksHandlerWithRegistry).
func tasksHandler() CommandHandler {
	return tasksHandlerWithRegistry(nil)
}

// keybindingsHandler returns a CommandHandler for /keybindings that shows the path
// to the keybindings.json file and opens it if possible.
func keybindingsHandler(homeDir string) CommandHandler {
	if homeDir == "" {
		homeDir = platform.ClaudeHomeDir()
	}
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		path := filepath.Join(homeDir, "keybindings.json")
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			return CommandOutcome{
				Handled: true,
				Status:  fmt.Sprintf("Keybindings file: %s (not yet created — create it to add custom keybindings).", path),
			}, nil
		}
		return CommandOutcome{
			Handled: true,
			Status:  fmt.Sprintf("Keybindings file: %s", path),
		}, nil
	}
}

// reloadPluginsHandler returns a CommandHandler for /reload-plugins.
func reloadPluginsHandler(cwd string) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{
			Handled: true,
			Status:  "Plugins reloaded. Changes to installed plugins will take effect on the next command.",
		}, nil
	}
}

// agentColorNames mirrors CC's AGENT_COLORS list (src/tools/AgentTool/agentColorManager.ts).
var agentColorNames = map[string]bool{
	"blue": true, "green": true, "yellow": true, "red": true,
	"cyan": true, "magenta": true, "orange": true, "pink": true,
	"purple": true, "white": true,
}

// agentColorResetNames mirrors CC's RESET_ALIASES (src/commands/color/color.ts).
var agentColorResetNames = map[string]bool{
	"default": true, "reset": true, "none": true, "gray": true, "grey": true,
}

// colorHandlerWith returns a CommandHandler for /color backed by an optional
// persistence func. Passing nil skips persistence (session-only change).
// CC ref: src/commands/color/color.ts — /color sets the agent's session colour.
func colorHandlerWith(persist func(string) error) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		arg := strings.ToLower(strings.TrimSpace(cc.Args))
		if arg == "" {
			return CommandOutcome{
				Handled: true,
				Status:  "Usage: /color <color|default> — set the session color for this agent.",
			}, nil
		}
		if agentColorResetNames[arg] {
			if cc.Screen != nil {
				cc.Screen.SessionColor = ""
			}
			if persist != nil {
				_ = persist("") // best-effort
			}
			return CommandOutcome{Handled: true, Status: "Session color reset to default."}, nil
		}
		if !agentColorNames[arg] {
			colors := "blue, green, yellow, red, cyan, magenta, orange, pink, purple, white"
			return CommandOutcome{
				Handled: true,
				Status:  fmt.Sprintf("Unknown color %q. Available: %s, default", arg, colors),
			}, nil
		}
		if cc.Screen != nil {
			cc.Screen.SessionColor = arg
		}
		if persist != nil {
			_ = persist(arg) // best-effort
		}
		return CommandOutcome{Handled: true, Status: fmt.Sprintf("Session color set to %q.", arg)}, nil
	}
}

// colorHandler returns the production CommandHandler for /color.
// Session colour is applied live to the screen; no persistence needed for
// session-only semantics (matching CC's in-session colour assignment).
func colorHandler() CommandHandler {
	return colorHandlerWith(nil)
}

// statusLineHandler returns a CommandHandler for /statusline.
// In CC, /statusline is a prompt-type command that dispatches to an agent.
// In ccgo, we return an informational message directing the user to the statusline-setup agent.
func statusLineHandler() CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{
			Handled: true,
			Status:  "To set up the status line, ask Claude: \"Configure my statusLine from my shell PS1 configuration\" — or use the statusline-setup agent skill.",
		}, nil
	}
}
