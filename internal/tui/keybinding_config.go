package tui

import (
	"fmt"
	"strings"
	"unicode"
)

type BindingSpec struct {
	Key    string
	Action Action
}

func KeymapFromSpecs(base Keymap, specs []BindingSpec) (Keymap, error) {
	out := Keymap{Bindings: map[KeyType]Action{}, ChordBindings: map[string]Action{}}
	if base.Bindings == nil {
		base = DefaultKeymap()
	}
	for key, action := range base.Bindings {
		out.Bindings[key] = action
	}
	for chord, action := range base.ChordBindings {
		out.ChordBindings[chord] = action
	}
	for _, spec := range specs {
		chord, err := ParseKeyChord(spec.Key)
		if err != nil {
			return Keymap{}, err
		}
		action, err := ParseActionName(string(spec.Action))
		if err != nil {
			return Keymap{}, err
		}
		if len(chord) > 1 {
			key := encodeChordKey(chord)
			if action == ActionNone {
				delete(out.ChordBindings, key)
				continue
			}
			out.ChordBindings[key] = action
			continue
		}
		key := chord[0]
		if action == ActionNone {
			delete(out.Bindings, key)
			continue
		}
		out.Bindings[key] = action
	}
	return out, nil
}

func ParseKeyChord(raw string) ([]KeyType, error) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty keybinding")
	}
	chord := make([]KeyType, 0, len(parts))
	for _, part := range parts {
		key, err := ParseKeyName(part)
		if err != nil {
			return nil, err
		}
		chord = append(chord, key)
	}
	return chord, nil
}

func ParseKeyName(raw string) (KeyType, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.ReplaceAll(name, "_", "-")
	name = expandDelimitedShortKeyModifierAlias(name)
	compact := strings.NewReplacer("-", "", "+", "").Replace(name)
	compact = expandCompactShortKeyModifierAlias(compact)
	compact = expandCompactModifierArrowAlias(compact)
	switch name {
	case "enter", "return", "ctrl+j", "ctrl-j", "control+j", "control-j", "ctrl+m", "ctrl-m", "control+m", "control-m":
		return KeyEnter, nil
	case "shift+enter", "shift-enter", "shift+return", "shift-return":
		return KeyShiftEnter, nil
	case "esc", "escape", "ctrl+[", "ctrl-[", "control+[", "control-[":
		return KeyEsc, nil
	case "alt+b", "alt-b", "meta+b", "meta-b", "option+b", "option-b", "cmd+b", "cmd-b", "command+b", "command-b", "super+b", "super-b":
		return KeyAltB, nil
	case "alt+d", "alt-d", "meta+d", "meta-d", "option+d", "option-d", "cmd+d", "cmd-d", "command+d", "command-d", "super+d", "super-d":
		return KeyAltD, nil
	case "alt+f", "alt-f", "meta+f", "meta-f", "option+f", "option-f", "cmd+f", "cmd-f", "command+f", "command-f", "super+f", "super-f":
		return KeyAltF, nil
	case "alt+y", "alt-y", "meta+y", "meta-y", "option+y", "option-y", "cmd+y", "cmd-y", "command+y", "command-y", "super+y", "super-y":
		return KeyAltY, nil
	case "alt+backspace", "alt-backspace", "meta+backspace", "meta-backspace", "option+backspace", "option-backspace", "cmd+backspace", "cmd-backspace", "command+backspace", "command-backspace", "super+backspace", "super-backspace":
		return KeyAltBS, nil
	case "backspace", "bs", "ctrl+?", "ctrl-?", "control+?", "control-?", "ctrl+h", "ctrl-h", "control+h", "control-h":
		return KeyBackspace, nil
	case "delete", "del":
		return KeyDelete, nil
	case "left", "arrow-left", "arrowleft":
		return KeyLeft, nil
	case "right", "arrow-right", "arrowright":
		return KeyRight, nil
	case "alt+left", "alt-left", "meta+left", "meta-left", "option+left", "option-left", "cmd+left", "cmd-left", "command+left", "command-left", "super+left", "super-left", "alt+arrow-left", "alt-arrow-left", "meta+arrow-left", "meta-arrow-left", "option+arrow-left", "option-arrow-left", "cmd+arrow-left", "cmd-arrow-left", "command+arrow-left", "command-arrow-left", "super+arrow-left", "super-arrow-left":
		return KeyAltLeft, nil
	case "alt+right", "alt-right", "meta+right", "meta-right", "option+right", "option-right", "cmd+right", "cmd-right", "command+right", "command-right", "super+right", "super-right", "alt+arrow-right", "alt-arrow-right", "meta+arrow-right", "meta-arrow-right", "option+arrow-right", "option-arrow-right", "cmd+arrow-right", "cmd-arrow-right", "command+arrow-right", "command-arrow-right", "super+arrow-right", "super-arrow-right":
		return KeyAltRight, nil
	case "ctrl+left", "ctrl-left", "control+left", "control-left", "ctrl+arrow-left", "ctrl-arrow-left", "control+arrow-left", "control-arrow-left":
		return KeyCtrlLeft, nil
	case "ctrl+right", "ctrl-right", "control+right", "control-right", "ctrl+arrow-right", "ctrl-arrow-right", "control+arrow-right", "control-arrow-right":
		return KeyCtrlRight, nil
	case "up", "arrow-up", "arrowup":
		return KeyUp, nil
	case "down", "arrow-down", "arrowdown":
		return KeyDown, nil
	case "pageup", "page-up", "pgup", "pg-up", "prior":
		return KeyPageUp, nil
	case "pagedown", "page-down", "pgdn", "pg-dn", "pgdown", "pg-down", "next":
		return KeyPageDown, nil
	case "home":
		return KeyHome, nil
	case "end":
		return KeyEnd, nil
	case "tab", "ctrl+i", "ctrl-i", "control+i", "control-i":
		return KeyTab, nil
	case "shift+tab", "shift-tab", "backtab", "back-tab", "btab":
		return KeyShiftTab, nil
	case "ctrl+a", "ctrl-a", "control+a", "control-a":
		return KeyCtrlA, nil
	case "ctrl+b", "ctrl-b", "control+b", "control-b":
		return KeyCtrlB, nil
	case "ctrl+c", "ctrl-c", "control+c", "control-c":
		return KeyCtrlC, nil
	case "ctrl+d", "ctrl-d", "control+d", "control-d":
		return KeyCtrlD, nil
	case "ctrl+e", "ctrl-e", "control+e", "control-e":
		return KeyCtrlE, nil
	case "ctrl+f", "ctrl-f", "control+f", "control-f":
		return KeyCtrlF, nil
	case "ctrl+g", "ctrl-g", "control+g", "control-g":
		return KeyCtrlG, nil
	case "ctrl+k", "ctrl-k", "control+k", "control-k":
		return KeyCtrlK, nil
	case "ctrl+l", "ctrl-l", "control+l", "control-l":
		return KeyCtrlL, nil
	case "ctrl+n", "ctrl-n", "control+n", "control-n":
		return KeyCtrlN, nil
	case "ctrl+o", "ctrl-o", "control+o", "control-o":
		return KeyCtrlO, nil
	case "ctrl+p", "ctrl-p", "control+p", "control-p":
		return KeyCtrlP, nil
	case "ctrl+q", "ctrl-q", "control+q", "control-q":
		return KeyCtrlQ, nil
	case "ctrl+r", "ctrl-r", "control+r", "control-r":
		return KeyCtrlR, nil
	case "ctrl+s", "ctrl-s", "control+s", "control-s":
		return KeyCtrlS, nil
	case "ctrl+t", "ctrl-t", "control+t", "control-t":
		return KeyCtrlT, nil
	case "ctrl+u", "ctrl-u", "control+u", "control-u":
		return KeyCtrlU, nil
	case "ctrl+v", "ctrl-v", "control+v", "control-v":
		return KeyCtrlV, nil
	case "ctrl+w", "ctrl-w", "control+w", "control-w":
		return KeyCtrlW, nil
	case "ctrl+x", "ctrl-x", "control+x", "control-x":
		return KeyCtrlX, nil
	case "ctrl+y", "ctrl-y", "control+y", "control-y":
		return KeyCtrlY, nil
	case "ctrl+z", "ctrl-z", "control+z", "control-z":
		return KeyCtrlZ, nil
	case "paste", "bracketed-paste":
		return KeyPaste, nil
	case "image-hint", "image":
		return KeyImageHint, nil
	case "mouse":
		return KeyMouse, nil
	case "focus-in", "focusin":
		return KeyFocusIn, nil
	case "focus-out", "focusout", "blur":
		return KeyFocusOut, nil
	}
	switch compact {
	case "ctrlj", "controlj", "ctrlm", "controlm":
		return KeyEnter, nil
	case "ctrl[", "control[":
		return KeyEsc, nil
	case "shiftenter", "shiftreturn":
		return KeyShiftEnter, nil
	case "shifttab", "backtab", "btab":
		return KeyShiftTab, nil
	case "altb", "metab", "optionb", "cmdb", "commandb", "superb":
		return KeyAltB, nil
	case "altd", "metad", "optiond", "cmdd", "commandd", "superd":
		return KeyAltD, nil
	case "altf", "metaf", "optionf", "cmdf", "commandf", "superf":
		return KeyAltF, nil
	case "alty", "metay", "optiony", "cmdy", "commandy", "supery":
		return KeyAltY, nil
	case "altbackspace", "metabackspace", "optionbackspace", "cmdbackspace", "commandbackspace", "superbackspace":
		return KeyAltBS, nil
	case "ctrl?", "control?", "ctrlh", "controlh":
		return KeyBackspace, nil
	case "ctrli", "controli":
		return KeyTab, nil
	case "altleft", "metaleft", "optionleft", "cmdleft", "commandleft", "superleft":
		return KeyAltLeft, nil
	case "altright", "metaright", "optionright", "cmdright", "commandright", "superright":
		return KeyAltRight, nil
	case "ctrlleft", "controlleft":
		return KeyCtrlLeft, nil
	case "ctrlright", "controlright":
		return KeyCtrlRight, nil
	case "ctrla", "controla":
		return KeyCtrlA, nil
	case "ctrlb", "controlb":
		return KeyCtrlB, nil
	case "ctrlc", "controlc":
		return KeyCtrlC, nil
	case "ctrld", "controld":
		return KeyCtrlD, nil
	case "ctrle", "controle":
		return KeyCtrlE, nil
	case "ctrlf", "controlf":
		return KeyCtrlF, nil
	case "ctrlg", "controlg":
		return KeyCtrlG, nil
	case "ctrlk", "controlk":
		return KeyCtrlK, nil
	case "ctrll", "controll":
		return KeyCtrlL, nil
	case "ctrln", "controln":
		return KeyCtrlN, nil
	case "ctrlo", "controlo":
		return KeyCtrlO, nil
	case "ctrlp", "controlp":
		return KeyCtrlP, nil
	case "ctrlq", "controlq":
		return KeyCtrlQ, nil
	case "ctrlr", "controlr":
		return KeyCtrlR, nil
	case "ctrls", "controls":
		return KeyCtrlS, nil
	case "ctrlt", "controlt":
		return KeyCtrlT, nil
	case "ctrlu", "controlu":
		return KeyCtrlU, nil
	case "ctrlv", "controlv":
		return KeyCtrlV, nil
	case "ctrlw", "controlw":
		return KeyCtrlW, nil
	case "ctrlx", "controlx":
		return KeyCtrlX, nil
	case "ctrly", "controly":
		return KeyCtrlY, nil
	case "ctrlz", "controlz":
		return KeyCtrlZ, nil
	default:
		return KeyUnknown, fmt.Errorf("unknown key %q", raw)
	}
}

func expandDelimitedShortKeyModifierAlias(name string) string {
	for _, alias := range []struct {
		from string
		to   string
	}{
		{from: "c+", to: "ctrl+"},
		{from: "c-", to: "ctrl-"},
		{from: "m+", to: "meta+"},
		{from: "m-", to: "meta-"},
		{from: "a+", to: "alt+"},
		{from: "a-", to: "alt-"},
		{from: "opt+", to: "option+"},
		{from: "opt-", to: "option-"},
		{from: "s+", to: "shift+"},
		{from: "s-", to: "shift-"},
	} {
		if strings.HasPrefix(name, alias.from) {
			return alias.to + strings.TrimPrefix(name, alias.from)
		}
	}
	return name
}

func expandCompactShortKeyModifierAlias(compact string) string {
	if suffix, ok := strings.CutPrefix(compact, "opt"); ok && compactAltKeySuffix(suffix) {
		return "option" + compactNavigationKeySuffix(suffix)
	}
	if suffix, ok := strings.CutPrefix(compact, "c"); ok && compactCtrlKeySuffix(suffix) {
		return "ctrl" + compactNavigationKeySuffix(suffix)
	}
	if suffix, ok := strings.CutPrefix(compact, "m"); ok && compactAltKeySuffix(suffix) {
		return "meta" + compactNavigationKeySuffix(suffix)
	}
	if suffix, ok := strings.CutPrefix(compact, "a"); ok && compactAltKeySuffix(suffix) {
		return "alt" + compactNavigationKeySuffix(suffix)
	}
	if suffix, ok := strings.CutPrefix(compact, "s"); ok && compactShiftKeySuffix(suffix) {
		return "shift" + suffix
	}
	return compact
}

func expandCompactModifierArrowAlias(compact string) string {
	for _, prefix := range []string{"ctrl", "control", "alt", "meta", "option", "cmd", "command", "super"} {
		if suffix, ok := strings.CutPrefix(compact, prefix); ok {
			navigation := compactNavigationKeySuffix(suffix)
			if navigation != suffix {
				return prefix + navigation
			}
		}
	}
	return compact
}

func compactNavigationKeySuffix(suffix string) string {
	switch suffix {
	case "arrowleft":
		return "left"
	case "arrowright":
		return "right"
	default:
		return suffix
	}
}

func compactCtrlKeySuffix(suffix string) bool {
	switch suffix {
	case "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z", "[", "?", "left", "right", "arrowleft", "arrowright":
		return true
	default:
		return false
	}
}

func compactAltKeySuffix(suffix string) bool {
	switch suffix {
	case "b", "d", "f", "y", "backspace", "left", "right", "arrowleft", "arrowright":
		return true
	default:
		return false
	}
}

func compactShiftKeySuffix(suffix string) bool {
	switch suffix {
	case "enter", "return", "tab":
		return true
	default:
		return false
	}
}

func ParseActionName(raw string) (Action, error) {
	name := normalizeActionName(raw)
	switch name {
	case "", "none", "noop", "no_op", "null", "unbind", "unbound":
		return ActionNone, nil
	case "cancel", "cancel_prompt", "dismiss", "close", "escape", "abort":
		return ActionCancel, nil
	case "interrupt", "interrupt_request", "stop", "stop_generation", "stop_response", "cancel_generation", "cancel_response":
		return ActionInterrupt, nil
	case "exit", "quit", "eof", "close_app", "exit_app":
		return ActionExit, nil
	case "redraw", "clear", "clear_screen", "redraw_screen", "refresh", "refresh_screen":
		return ActionRedraw, nil
	case "toggle_transcript", "transcript", "show_transcript", "toggle_history":
		return ActionToggleTranscript, nil
	case "toggle_todos", "toggle_todo", "todos", "toggle_tasks", "toggle_task_list":
		return ActionToggleTodos, nil
	case "external_editor", "external_edit", "open_editor", "open_external_editor", "editor", "edit_in_editor":
		return ActionExternalEditor, nil
	case "stash_prompt", "stash", "stash_input", "stash_current_prompt":
		return ActionStashPrompt, nil
	case "kill_agents", "kill_agent", "stop_agents", "cancel_agents":
		return ActionKillAgents, nil
	case "submit", "submit_prompt", "send", "send_message", "submit_message":
		return ActionSubmitPrompt, nil
	case "newline", "new_line", "line_break", "insert_newline":
		return ActionInsertNewline, nil
	case "left", "cursor_left", "backward", "move_backward":
		return ActionMoveLeft, nil
	case "right", "cursor_right", "forward", "move_forward":
		return ActionMoveRight, nil
	case "word_left", "word_backward", "backward_word", "move_word_backward":
		return ActionMoveWordLeft, nil
	case "word_right", "word_forward", "forward_word", "move_word_forward":
		return ActionMoveWordRight, nil
	case "home", "start", "line_start", "beginning", "move_to_start":
		return ActionMoveStart, nil
	case "end", "line_end", "move_to_end":
		return ActionMoveEnd, nil
	case "backspace", "delete_back", "delete_backward", "delete_previous", "delete_previous_char":
		return ActionDeleteBackward, nil
	case "delete", "del", "delete_forward", "delete_next", "delete_next_char":
		return ActionDeleteForward, nil
	case "delete_start", "delete_line_start", "delete_to_beginning", "delete_to_line_start":
		return ActionDeleteToStart, nil
	case "kill_line", "delete_line_end", "delete_to_line_end":
		return ActionDeleteToEnd, nil
	case "delete_word_back", "delete_word_backward", "delete_word_left", "kill_word_backward", "backward_kill_word":
		return ActionDeleteWordBack, nil
	case "delete_word_fwd", "delete_word_forward", "delete_word_right", "kill_word", "kill_word_forward":
		return ActionDeleteWordFwd, nil
	case "yank", "paste_yank", "paste_kill_ring":
		return ActionYank, nil
	case "yank_pop", "yank_previous", "rotate_yank", "paste_previous_yank":
		return ActionYankPop, nil
	case "history_prev", "history_previous", "previous_history":
		return ActionHistoryPrevious, nil
	case "history_next", "next_history":
		return ActionHistoryNext, nil
	case "scroll_up", "line_up", "scroll_line_up":
		return ActionScrollUp, nil
	case "scroll_down", "line_down", "scroll_line_down":
		return ActionScrollDown, nil
	case "pageup", "page_up":
		return ActionPageUp, nil
	case "pagedown", "page_down":
		return ActionPageDown, nil
	case "half_page_up", "half_up":
		return ActionHalfPageUp, nil
	case "half_page_down", "half_down":
		return ActionHalfPageDown, nil
	case "scroll_top", "scroll_to_top", "top":
		return ActionScrollToTop, nil
	case "scroll_bottom", "scroll_to_bottom", "bottom":
		return ActionScrollToBottom, nil
	case "confirm", "confirm_selection", "accept", "accept_selection", "select", "select_item":
		return ActionConfirmSelection, nil
	case "reverse_search", "history_search", "search_history", "search":
		return ActionReverseSearch, nil
	case "focus_next", "next_focus", "focus_forward", "tab_next":
		return ActionFocusNext, nil
	case "focus_previous", "focus_prev", "previous_focus", "prev_focus", "focus_backward", "tab_previous":
		return ActionFocusPrevious, nil
	}
	action := Action(name)
	if !IsKnownAction(action) {
		return ActionNone, fmt.Errorf("unknown action %q", raw)
	}
	return action, nil
}

func normalizeActionName(raw string) string {
	var b strings.Builder
	lastSeparator := false
	previousLowerOrDigit := false
	for _, r := range strings.TrimSpace(raw) {
		switch r {
		case '-', '_', ' ', '\t', '\n', '.', '/', ':':
			if b.Len() > 0 && !lastSeparator {
				b.WriteByte('_')
				lastSeparator = true
			}
			previousLowerOrDigit = false
			continue
		}
		if unicode.IsUpper(r) {
			if b.Len() > 0 && !lastSeparator && previousLowerOrDigit {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		} else {
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
		lastSeparator = false
		previousLowerOrDigit = unicode.IsLower(r) || unicode.IsDigit(r)
	}
	return strings.Trim(b.String(), "_")
}

func IsKnownAction(action Action) bool {
	switch action {
	case ActionNone,
		ActionInsertRune,
		ActionInsertNewline,
		ActionSubmitPrompt,
		ActionCancel,
		ActionInterrupt,
		ActionExit,
		ActionRedraw,
		ActionToggleTranscript,
		ActionToggleTodos,
		ActionExternalEditor,
		ActionStashPrompt,
		ActionKillAgents,
		ActionMoveLeft,
		ActionMoveRight,
		ActionMoveWordLeft,
		ActionMoveWordRight,
		ActionMoveStart,
		ActionMoveEnd,
		ActionDeleteBackward,
		ActionDeleteForward,
		ActionDeleteToStart,
		ActionDeleteToEnd,
		ActionDeleteWordBack,
		ActionDeleteWordFwd,
		ActionYank,
		ActionYankPop,
		ActionHistoryPrevious,
		ActionHistoryNext,
		ActionScrollUp,
		ActionScrollDown,
		ActionPageUp,
		ActionPageDown,
		ActionHalfPageUp,
		ActionHalfPageDown,
		ActionScrollToTop,
		ActionScrollToBottom,
		ActionFocusNext,
		ActionFocusPrevious,
		ActionConfirmSelection,
		ActionReverseSearch:
		return true
	default:
		return false
	}
}
