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
	compact := strings.NewReplacer("-", "", "+", "").Replace(name)
	switch name {
	case "enter", "return", "ctrl+m", "ctrl-m", "control+m", "control-m":
		return KeyEnter, nil
	case "shift+enter", "shift-enter", "shift+return", "shift-return":
		return KeyShiftEnter, nil
	case "esc", "escape":
		return KeyEsc, nil
	case "alt+b", "alt-b", "meta+b", "meta-b", "option+b", "option-b":
		return KeyAltB, nil
	case "alt+d", "alt-d", "meta+d", "meta-d", "option+d", "option-d":
		return KeyAltD, nil
	case "alt+f", "alt-f", "meta+f", "meta-f", "option+f", "option-f":
		return KeyAltF, nil
	case "alt+y", "alt-y", "meta+y", "meta-y", "option+y", "option-y":
		return KeyAltY, nil
	case "alt+backspace", "alt-backspace", "meta+backspace", "meta-backspace", "option+backspace", "option-backspace":
		return KeyAltBS, nil
	case "backspace", "bs", "ctrl+h", "ctrl-h", "control+h", "control-h":
		return KeyBackspace, nil
	case "delete", "del":
		return KeyDelete, nil
	case "left":
		return KeyLeft, nil
	case "right":
		return KeyRight, nil
	case "alt+left", "alt-left", "meta+left", "meta-left", "option+left", "option-left":
		return KeyAltLeft, nil
	case "alt+right", "alt-right", "meta+right", "meta-right", "option+right", "option-right":
		return KeyAltRight, nil
	case "ctrl+left", "ctrl-left", "control+left", "control-left":
		return KeyCtrlLeft, nil
	case "ctrl+right", "ctrl-right", "control+right", "control-right":
		return KeyCtrlRight, nil
	case "up":
		return KeyUp, nil
	case "down":
		return KeyDown, nil
	case "pageup", "page-up":
		return KeyPageUp, nil
	case "pagedown", "page-down":
		return KeyPageDown, nil
	case "home":
		return KeyHome, nil
	case "end":
		return KeyEnd, nil
	case "tab", "ctrl+i", "ctrl-i", "control+i", "control-i":
		return KeyTab, nil
	case "shift+tab", "shift-tab":
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
	case "ctrl+r", "ctrl-r", "control+r", "control-r":
		return KeyCtrlR, nil
	case "ctrl+s", "ctrl-s", "control+s", "control-s":
		return KeyCtrlS, nil
	case "ctrl+t", "ctrl-t", "control+t", "control-t":
		return KeyCtrlT, nil
	case "ctrl+u", "ctrl-u", "control+u", "control-u":
		return KeyCtrlU, nil
	case "ctrl+w", "ctrl-w", "control+w", "control-w":
		return KeyCtrlW, nil
	case "ctrl+x", "ctrl-x", "control+x", "control-x":
		return KeyCtrlX, nil
	case "ctrl+y", "ctrl-y", "control+y", "control-y":
		return KeyCtrlY, nil
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
	case "ctrlm", "controlm":
		return KeyEnter, nil
	case "shiftenter", "shiftreturn":
		return KeyShiftEnter, nil
	case "shifttab":
		return KeyShiftTab, nil
	case "altb", "metab", "optionb":
		return KeyAltB, nil
	case "altd", "metad", "optiond":
		return KeyAltD, nil
	case "altf", "metaf", "optionf":
		return KeyAltF, nil
	case "alty", "metay", "optiony":
		return KeyAltY, nil
	case "altbackspace", "metabackspace", "optionbackspace":
		return KeyAltBS, nil
	case "ctrlh", "controlh":
		return KeyBackspace, nil
	case "ctrli", "controli":
		return KeyTab, nil
	case "altleft", "metaleft", "optionleft":
		return KeyAltLeft, nil
	case "altright", "metaright", "optionright":
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
	case "ctrlr", "controlr":
		return KeyCtrlR, nil
	case "ctrls", "controls":
		return KeyCtrlS, nil
	case "ctrlt", "controlt":
		return KeyCtrlT, nil
	case "ctrlu", "controlu":
		return KeyCtrlU, nil
	case "ctrlw", "controlw":
		return KeyCtrlW, nil
	case "ctrlx", "controlx":
		return KeyCtrlX, nil
	case "ctrly", "controly":
		return KeyCtrlY, nil
	default:
		return KeyUnknown, fmt.Errorf("unknown key %q", raw)
	}
}

func ParseActionName(raw string) (Action, error) {
	name := normalizeActionName(raw)
	switch name {
	case "", "none", "noop", "no_op", "null", "unbind", "unbound":
		return ActionNone, nil
	case "submit", "submit_prompt":
		return ActionSubmitPrompt, nil
	case "newline", "insert_newline":
		return ActionInsertNewline, nil
	case "delete_word_back", "delete_word_backward":
		return ActionDeleteWordBack, nil
	case "delete_word_fwd", "delete_word_forward":
		return ActionDeleteWordFwd, nil
	case "history_prev", "history_previous", "previous_history":
		return ActionHistoryPrevious, nil
	case "history_next", "next_history":
		return ActionHistoryNext, nil
	case "pageup", "page_up":
		return ActionPageUp, nil
	case "pagedown", "page_down":
		return ActionPageDown, nil
	case "scroll_top", "scroll_to_top", "top":
		return ActionScrollToTop, nil
	case "scroll_bottom", "scroll_to_bottom", "bottom":
		return ActionScrollToBottom, nil
	case "confirm", "confirm_selection":
		return ActionConfirmSelection, nil
	case "reverse_search", "history_search", "search_history":
		return ActionReverseSearch, nil
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
