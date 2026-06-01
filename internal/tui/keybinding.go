package tui

import "strings"

type Action string

const (
	ActionNone             Action = ""
	ActionInsertRune       Action = "insert_rune"
	ActionSubmitPrompt     Action = "submit_prompt"
	ActionCancel           Action = "cancel"
	ActionInterrupt        Action = "interrupt"
	ActionMoveLeft         Action = "move_left"
	ActionMoveRight        Action = "move_right"
	ActionMoveStart        Action = "move_start"
	ActionMoveEnd          Action = "move_end"
	ActionDeleteBackward   Action = "delete_backward"
	ActionDeleteForward    Action = "delete_forward"
	ActionHistoryPrevious  Action = "history_previous"
	ActionHistoryNext      Action = "history_next"
	ActionScrollUp         Action = "scroll_up"
	ActionScrollDown       Action = "scroll_down"
	ActionPageUp           Action = "page_up"
	ActionPageDown         Action = "page_down"
	ActionFocusNext        Action = "focus_next"
	ActionFocusPrevious    Action = "focus_previous"
	ActionConfirmSelection Action = "confirm_selection"
	ActionReverseSearch    Action = "reverse_search"
)

type Keymap struct {
	Bindings      map[KeyType]Action
	ChordBindings map[string]Action
	PendingChord  []KeyType
}

func DefaultKeymap() Keymap {
	return Keymap{Bindings: map[KeyType]Action{
		KeyEnter:     ActionSubmitPrompt,
		KeyEsc:       ActionCancel,
		KeyCtrlC:     ActionInterrupt,
		KeyLeft:      ActionMoveLeft,
		KeyRight:     ActionMoveRight,
		KeyHome:      ActionMoveStart,
		KeyCtrlA:     ActionMoveStart,
		KeyEnd:       ActionMoveEnd,
		KeyCtrlE:     ActionMoveEnd,
		KeyBackspace: ActionDeleteBackward,
		KeyDelete:    ActionDeleteForward,
		KeyUp:        ActionHistoryPrevious,
		KeyDown:      ActionHistoryNext,
		KeyPageUp:    ActionPageUp,
		KeyPageDown:  ActionPageDown,
		KeyTab:       ActionFocusNext,
		KeyShiftTab:  ActionFocusPrevious,
		KeyCtrlR:     ActionReverseSearch,
	}}
}

func (k *Keymap) Resolve(key Key) Action {
	if key.Type == KeyRune {
		k.PendingChord = nil
		return ActionInsertRune
	}
	if k.Bindings == nil {
		defaults := DefaultKeymap()
		k.Bindings = defaults.Bindings
	}
	testChord := append(append([]KeyType(nil), k.PendingChord...), key.Type)
	if k.hasLongerChord(testChord) {
		k.PendingChord = testChord
		return ActionNone
	}
	if action, ok := k.exactChord(testChord); ok {
		k.PendingChord = nil
		return action
	}
	if len(k.PendingChord) > 0 {
		k.PendingChord = nil
		return ActionNone
	}
	if action, ok := k.Bindings[key.Type]; ok {
		return action
	}
	return ActionNone
}

func (k Keymap) hasLongerChord(prefix []KeyType) bool {
	for raw := range k.ChordBindings {
		chord := decodeChordKey(raw)
		if len(chord) <= len(prefix) || !chordPrefix(prefix, chord) {
			continue
		}
		return true
	}
	return false
}

func (k Keymap) exactChord(chord []KeyType) (Action, bool) {
	if len(chord) <= 1 {
		return ActionNone, false
	}
	action, ok := k.ChordBindings[encodeChordKey(chord)]
	return action, ok
}

func encodeChordKey(chord []KeyType) string {
	if len(chord) == 0 {
		return ""
	}
	out := string(chord[0])
	for _, step := range chord[1:] {
		out += " " + string(step)
	}
	return out
}

func decodeChordKey(raw string) []KeyType {
	if raw == "" {
		return nil
	}
	parts := strings.Fields(raw)
	chord := make([]KeyType, 0, len(parts))
	for _, part := range parts {
		chord = append(chord, KeyType(part))
	}
	return chord
}

func chordPrefix(prefix []KeyType, chord []KeyType) bool {
	if len(prefix) > len(chord) {
		return false
	}
	for i := range prefix {
		if prefix[i] != chord[i] {
			return false
		}
	}
	return true
}
