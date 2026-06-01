package tui

import (
	"fmt"
	"strings"
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
		if !IsKnownAction(spec.Action) {
			return Keymap{}, fmt.Errorf("unknown action %q", spec.Action)
		}
		if len(chord) > 1 {
			key := encodeChordKey(chord)
			if spec.Action == ActionNone {
				delete(out.ChordBindings, key)
				continue
			}
			out.ChordBindings[key] = spec.Action
			continue
		}
		key := chord[0]
		if spec.Action == ActionNone {
			delete(out.Bindings, key)
			continue
		}
		out.Bindings[key] = spec.Action
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
	switch name {
	case "enter", "return":
		return KeyEnter, nil
	case "esc", "escape":
		return KeyEsc, nil
	case "backspace", "bs":
		return KeyBackspace, nil
	case "delete", "del":
		return KeyDelete, nil
	case "left":
		return KeyLeft, nil
	case "right":
		return KeyRight, nil
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
	case "tab":
		return KeyTab, nil
	case "shift+tab", "shift-tab":
		return KeyShiftTab, nil
	case "ctrl+a", "ctrl-a", "control+a", "control-a":
		return KeyCtrlA, nil
	case "ctrl+b", "ctrl-b", "control+b", "control-b":
		return KeyCtrlB, nil
	case "ctrl+c", "ctrl-c", "control+c", "control-c":
		return KeyCtrlC, nil
	case "ctrl+e", "ctrl-e", "control+e", "control-e":
		return KeyCtrlE, nil
	case "ctrl+f", "ctrl-f", "control+f", "control-f":
		return KeyCtrlF, nil
	case "ctrl+k", "ctrl-k", "control+k", "control-k":
		return KeyCtrlK, nil
	case "ctrl+r", "ctrl-r", "control+r", "control-r":
		return KeyCtrlR, nil
	case "ctrl+u", "ctrl-u", "control+u", "control-u":
		return KeyCtrlU, nil
	case "ctrl+w", "ctrl-w", "control+w", "control-w":
		return KeyCtrlW, nil
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
	default:
		return KeyUnknown, fmt.Errorf("unknown key %q", raw)
	}
}

func IsKnownAction(action Action) bool {
	switch action {
	case ActionNone,
		ActionInsertRune,
		ActionSubmitPrompt,
		ActionCancel,
		ActionInterrupt,
		ActionMoveLeft,
		ActionMoveRight,
		ActionMoveStart,
		ActionMoveEnd,
		ActionDeleteBackward,
		ActionDeleteForward,
		ActionDeleteToStart,
		ActionDeleteToEnd,
		ActionDeleteWordBack,
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
