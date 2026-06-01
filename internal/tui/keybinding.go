package tui

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
	Bindings map[KeyType]Action
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

func (k Keymap) Resolve(key Key) Action {
	if key.Type == KeyRune {
		return ActionInsertRune
	}
	if k.Bindings == nil {
		k = DefaultKeymap()
	}
	if action, ok := k.Bindings[key.Type]; ok {
		return action
	}
	return ActionNone
}
