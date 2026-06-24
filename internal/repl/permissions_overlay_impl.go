package repl

// G24: /permissions interactive TUI overlay (Overlay implementation).
//
// This file provides the PermissionsOverlayImpl which wraps the pure-data
// PermissionsOverlayState (permissions_overlay.go) with the Overlay interface
// (key handling + render). Rendering is MANUAL — the TUI layer is deferred.
//
// State transitions:
//   - Esc                → Dismiss
//   - Tab / Shift+Tab    → cycle tabs (Rules / Recent-Denials / Workspace)
//   - ↑ / ↓             → navigate rule cursor (Rules tab)
//   - 'a'               → enter add-allow-rule mode (Rules tab)
//   - 'd'               → delete selected rule; calls remover; submit "perm:remove:..."
//   - Enter (add mode)  → confirm new rule; calls adder; submit "perm:add:..."
//   - Backspace (add)   → delete last char of typed rule
//   - Esc (add mode)    → cancel add mode
//
// CC ref: src/components/permissions/rules/PermissionRuleList.tsx

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/tui"
)

// permRuleRemover removes an existing rule. Matches the permissionsRemover type
// but without the path argument (overlay holds the path in its handler).
type permRuleRemover func(behavior, rule string) error

// permRuleAdder adds a new rule. Called with behavior + rule string.
type permRuleAdder func(behavior, rule string) error

// PermissionsOverlayImpl is the interactive /permissions TUI overlay.
type PermissionsOverlayImpl struct {
	state      PermissionsOverlayState
	ruleCursor int
	// addMode: when true, user is typing a new rule.
	addMode    bool
	addBuf     string // partial rule being typed
	adder      permRuleAdder
	remover    permRuleRemover
}

// newPermissionsOverlayImpl creates the overlay from options.
func newPermissionsOverlayImpl(opts permissionsOverlayOptions) *PermissionsOverlayImpl {
	return &PermissionsOverlayImpl{
		state: newPermissionsOverlayState(opts),
	}
}

// newPermissionsOverlayImplWithRemover creates the overlay with a rule remover seam.
func newPermissionsOverlayImplWithRemover(opts permissionsOverlayOptions, remover permRuleRemover) *PermissionsOverlayImpl {
	return &PermissionsOverlayImpl{
		state:   newPermissionsOverlayState(opts),
		remover: remover,
	}
}

// State returns the current immutable state snapshot.
func (o *PermissionsOverlayImpl) State() PermissionsOverlayState { return o.state }

// IsAddingRule reports whether the overlay is in add-rule mode.
func (o *PermissionsOverlayImpl) IsAddingRule() bool { return o.addMode }

// RuleCursor returns the current rule-list cursor position (Rules tab).
func (o *PermissionsOverlayImpl) RuleCursor() int { return o.ruleCursor }

// ApplyKey processes a key event. Implements Overlay.
func (o *PermissionsOverlayImpl) ApplyKey(key tui.Key) (OverlayResult, bool) {
	// --- add-rule mode ---
	if o.addMode {
		return o.applyKeyAddMode(key)
	}

	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true

	case tui.KeyTab:
		next := (o.state.ActiveTab + 1) % len(o.state.Tabs)
		o.state = o.state.WithActiveTab(next)
		o.ruleCursor = 0
		return OverlayResult{}, true

	case tui.KeyShiftTab:
		prev := (o.state.ActiveTab - 1 + len(o.state.Tabs)) % len(o.state.Tabs)
		o.state = o.state.WithActiveTab(prev)
		o.ruleCursor = 0
		return OverlayResult{}, true

	case tui.KeyDown:
		if o.state.ActiveTab == 0 {
			rules := o.activeAllowRules()
			if o.ruleCursor < len(rules)-1 {
				o.ruleCursor++
			}
		}
		return OverlayResult{}, true

	case tui.KeyUp:
		if o.ruleCursor > 0 {
			o.ruleCursor--
		}
		return OverlayResult{}, true

	case tui.KeyRune:
		switch key.Rune {
		case 'a':
			if o.state.ActiveTab == 0 {
				o.addMode = true
				o.addBuf = ""
			}
			return OverlayResult{}, true
		case 'd':
			return o.deleteSelectedRule()
		}

	default:
	}
	return OverlayResult{}, false
}

// applyKeyAddMode handles key events while the user is typing a new rule.
func (o *PermissionsOverlayImpl) applyKeyAddMode(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		o.addMode = false
		o.addBuf = ""
		return OverlayResult{}, true

	case tui.KeyBackspace:
		if len(o.addBuf) > 0 {
			runes := []rune(o.addBuf)
			o.addBuf = string(runes[:len(runes)-1])
		}
		return OverlayResult{}, true

	case tui.KeyEnter:
		rule := strings.TrimSpace(o.addBuf)
		o.addMode = false
		o.addBuf = ""
		if rule == "" {
			return OverlayResult{}, true
		}
		if o.adder != nil {
			_ = o.adder("allow", rule)
		}
		o.state = o.state.WithRuleAdded("allow", rule)
		return OverlayResult{Submit: fmt.Sprintf("perm:add:allow:%s", rule)}, true

	case tui.KeyRune:
		o.addBuf += string(key.Rune)
		return OverlayResult{}, true

	default:
		return OverlayResult{}, false
	}
}

// deleteSelectedRule removes the currently selected rule and calls the remover.
func (o *PermissionsOverlayImpl) deleteSelectedRule() (OverlayResult, bool) {
	rules := o.activeAllowRules()
	if len(rules) == 0 || o.ruleCursor >= len(rules) {
		return OverlayResult{}, true
	}
	rule := rules[o.ruleCursor]
	behavior := "allow"
	if o.remover != nil {
		_ = o.remover(behavior, rule)
	}
	o.state = o.state.WithRuleRemoved(behavior, rule)
	if o.ruleCursor >= len(o.state.AllowRules) && o.ruleCursor > 0 {
		o.ruleCursor--
	}
	return OverlayResult{Submit: fmt.Sprintf("perm:remove:%s:%s", behavior, rule)}, true
}

// activeAllowRules returns the combined (persisted + pending) allow rules.
func (o *PermissionsOverlayImpl) activeAllowRules() []string {
	all := make([]string, 0, len(o.state.AllowRules)+len(o.state.PendingAllowRules))
	all = append(all, o.state.AllowRules...)
	all = append(all, o.state.PendingAllowRules...)
	return all
}

// Render returns display lines for the overlay. Implements Overlay.
func (o *PermissionsOverlayImpl) Render(width, height int) []string {
	lines := []string{o.renderTabBar()}

	switch o.state.ActiveTab {
	case 0: // Rules
		lines = append(lines, "Allow rules:")
		for i, r := range o.state.AllowRules {
			marker := "  "
			if i == o.ruleCursor {
				marker = "> "
			}
			lines = append(lines, marker+r)
		}
		for _, r := range o.state.PendingAllowRules {
			lines = append(lines, "  +"+r+" (pending)")
		}
		if len(o.state.DenyRules) > 0 {
			lines = append(lines, "Deny rules:")
			for _, r := range o.state.DenyRules {
				lines = append(lines, "  "+r)
			}
		}
		if o.addMode {
			lines = append(lines, "Add rule: "+o.addBuf+"_")
		} else {
			lines = append(lines, "(a)dd rule  (d)elete  Tab=next tab  Esc=close")
		}
	case 1: // Recent-Denials
		if len(o.state.RecentDenials) == 0 {
			lines = append(lines, "No recent denials.")
		} else {
			for _, d := range o.state.RecentDenials {
				lines = append(lines, fmt.Sprintf("  %s: %s", d.ToolName, d.Description))
			}
		}
	case 2: // Workspace
		if len(o.state.WorkspaceDirs) == 0 {
			lines = append(lines, "No workspace directories configured.")
		} else {
			for _, dir := range o.state.WorkspaceDirs {
				lines = append(lines, "  "+dir)
			}
		}
	}
	// Trim to height.
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

// renderTabBar returns the tab-bar header line.
func (o *PermissionsOverlayImpl) renderTabBar() string {
	parts := make([]string, len(o.state.Tabs))
	for i, tab := range o.state.Tabs {
		if i == o.state.ActiveTab {
			parts[i] = "[" + tab + "]"
		} else {
			parts[i] = " " + tab + " "
		}
	}
	return strings.Join(parts, " | ")
}

// permissionsOverlayHandlerWith returns a CommandHandler for /permissions that:
//   - No-arg or "list": opens the PermissionsOverlayImpl.
//   - With subcommand (allow/deny/ask/remove): performs the text-based mutation.
//
// PERM-PERSIST-06 wiring: the overlay is returned in CommandOutcome.Overlay so
// the loop opens it as an active overlay.
func permissionsOverlayHandlerWith(path string, add permissionsAdder, remove permissionsRemover) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		args := strings.TrimSpace(cc.Args)

		// No-arg or explicit "list" → open interactive overlay.
		if args == "" || args == "list" {
			opts, err := loadPermissionsOverlayOptions(path)
			if err != nil {
				// Fall back to text on error.
				return listPermissions(path)
			}
			ov := newPermissionsOverlayImpl(opts)
			ov.adder = func(behavior, rule string) error {
				return add(path, behavior, rule)
			}
			ov.remover = func(behavior, rule string) error {
				return remove(path, behavior, rule)
			}
			return CommandOutcome{Handled: true, Overlay: ov}, nil
		}

		// Delegate subcommands to the existing text handler.
		return permissionsHandlerWith(path, add, remove)(ctx, cc)
	}
}

// loadPermissionsOverlayOptions reads current rules from the settings file at
// path and returns a permissionsOverlayOptions struct.
func loadPermissionsOverlayOptions(path string) (permissionsOverlayOptions, error) {
	// permissionsHandlerWith already has the logic to read the document;
	// reuse that by reading the file ourselves.
	out, err := listPermissions(path)
	if err != nil && path != "" {
		return permissionsOverlayOptions{}, err
	}
	// Parse out.Status to extract rules is fragile; instead read via config directly.
	// Use the same ReadSettingsDocument path as permissionsHandler.
	opts := permissionsOverlayOptions{}
	_ = out // status text not used here
	// The overlay will display rules loaded from the state; we load them live.
	// For simplicity, opts starts empty — the user can still add/remove rules
	// via the overlay which calls the adder/remover immediately.
	// Full population from disk is handled by loadPermissionsOverlayOptionsFull.
	opts2, err2 := loadPermissionsOverlayOptionsFull(path)
	if err2 != nil {
		return opts, nil // return empty on file-read error (first run)
	}
	return opts2, nil
}

// loadPermissionsOverlayOptionsFull reads the settings doc and populates opts.
func loadPermissionsOverlayOptionsFull(path string) (permissionsOverlayOptions, error) {
	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		return permissionsOverlayOptions{}, err
	}
	perms, _ := doc["permissions"].(map[string]any)
	return permissionsOverlayOptions{
		AllowRules: rulesFromDoc(perms, "allow"),
		DenyRules:  rulesFromDoc(perms, "deny"),
		AskRules:   rulesFromDoc(perms, "ask"),
	}, nil
}
