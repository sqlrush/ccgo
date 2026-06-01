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
	out := Keymap{Bindings: map[KeyType]Action{}}
	if base.Bindings == nil {
		base = DefaultKeymap()
	}
	for key, action := range base.Bindings {
		out.Bindings[key] = action
	}
	for _, spec := range specs {
		key, err := ParseKeyName(spec.Key)
		if err != nil {
			return Keymap{}, err
		}
		if !IsKnownAction(spec.Action) {
			return Keymap{}, fmt.Errorf("unknown action %q", spec.Action)
		}
		if spec.Action == ActionNone {
			delete(out.Bindings, key)
			continue
		}
		out.Bindings[key] = spec.Action
	}
	return out, nil
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
	case "ctrl+c", "ctrl-c", "control+c", "control-c":
		return KeyCtrlC, nil
	case "ctrl+e", "ctrl-e", "control+e", "control-e":
		return KeyCtrlE, nil
	case "ctrl+r", "ctrl-r", "control+r", "control-r":
		return KeyCtrlR, nil
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
		ActionHistoryPrevious,
		ActionHistoryNext,
		ActionScrollUp,
		ActionScrollDown,
		ActionPageUp,
		ActionPageDown,
		ActionFocusNext,
		ActionFocusPrevious,
		ActionConfirmSelection,
		ActionReverseSearch:
		return true
	default:
		return false
	}
}
