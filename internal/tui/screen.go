package tui

import "time"

const DoublePressTimeout = 800 * time.Millisecond

type ScreenEventType string

const (
	ScreenEventNone             ScreenEventType = ""
	ScreenEventPromptSubmitted  ScreenEventType = "prompt_submitted"
	ScreenEventDialogAction     ScreenEventType = "dialog_action"
	ScreenEventCancelled        ScreenEventType = "cancelled"
	ScreenEventInterrupted      ScreenEventType = "interrupted"
	ScreenEventExitPending      ScreenEventType = "exit_pending"
	ScreenEventExit             ScreenEventType = "exit"
	ScreenEventRedraw           ScreenEventType = "redraw"
	ScreenEventToggleTranscript ScreenEventType = "toggle_transcript"
	ScreenEventToggleTodos      ScreenEventType = "toggle_todos"
	ScreenEventExternalEditor   ScreenEventType = "external_editor"
	ScreenEventStashPrompt      ScreenEventType = "stash_prompt"
	ScreenEventKillAgents       ScreenEventType = "kill_agents"
	ScreenEventReverseSearch    ScreenEventType = "reverse_search"
	ScreenEventReverseSelected  ScreenEventType = "reverse_search_selected"
	ScreenEventFocusIn          ScreenEventType = "focus_in"
	ScreenEventFocusOut         ScreenEventType = "focus_out"
	ScreenEventViewportSelected ScreenEventType = "viewport_selected"
)

type ScreenEvent struct {
	Type       ScreenEventType
	Value      string
	DialogID   string
	DialogKind DialogKind
}

type REPLScreen struct {
	Width                int
	Height               int
	Messages             []Message
	Status               string
	Prompt               PromptState
	Dialog               *Dialog
	Keymap               Keymap
	Viewport             Viewport
	VimEnabled           bool
	VimMode              VimMode
	VimPendingOperator   rune
	VimPendingCharMotion rune
	VimPendingTextObject rune
	VimPendingG          bool
	VimPendingIndent     rune
	VimLastCharMotion    rune
	VimLastCharTarget    rune
	VimRepeatingChar     bool
	VimCount             int
	VimPendingCount      int
	VimPendingReplace    bool
	VimInsertedText      string
	VimLastChange        vimRecordedChange
	VimReplayingChange   bool
	VimRegister          string
	VimRegisterLinewise  bool
	VimUndoStack         []vimPromptSnapshot
	Focused              bool
	ReverseSearch        ReverseSearchState
	SelectedViewportLine int
	ExitPendingKey       KeyType
	ExitPendingAt        time.Time
	Now                  func() time.Time
}

func NewREPLScreen(width int, height int, history []string) REPLScreen {
	prompt := NewPromptState(history)
	prompt.EnablePasteReferences()
	screen := REPLScreen{
		Width:                width,
		Height:               height,
		Prompt:               prompt,
		Keymap:               DefaultKeymap(),
		Focused:              true,
		SelectedViewportLine: -1,
	}
	screen.rebuildViewport()
	return screen
}

func (s *REPLScreen) SetMessages(messages []Message) {
	s.Messages = append([]Message(nil), messages...)
	s.SelectedViewportLine = -1
	s.rebuildViewport()
}

func (s *REPLScreen) AppendMessage(message Message) {
	s.Messages = append(s.Messages, message)
	s.rebuildViewport()
	s.Viewport.ScrollToBottom()
}

func (s *REPLScreen) ApplyKey(key Key) ScreenEvent {
	switch key.Type {
	case KeyFocusIn:
		s.Focused = true
		return ScreenEvent{Type: ScreenEventFocusIn}
	case KeyFocusOut:
		s.Focused = false
		return ScreenEvent{Type: ScreenEventFocusOut}
	}
	if key.Type == KeyMouse {
		return s.applyMouse(key)
	}
	if s.ReverseSearch.Active {
		return s.applyReverseSearchKey(key)
	}
	if s.Dialog == nil {
		if event, handled := s.applyVimKey(key); handled {
			return event
		}
	}
	action := s.Keymap.Resolve(key)
	if action == ActionRedraw {
		return ScreenEvent{Type: ScreenEventRedraw}
	}
	if action == ActionToggleTranscript {
		return ScreenEvent{Type: ScreenEventToggleTranscript}
	}
	if action == ActionToggleTodos {
		return ScreenEvent{Type: ScreenEventToggleTodos}
	}
	if action == ActionExternalEditor {
		return ScreenEvent{Type: ScreenEventExternalEditor}
	}
	if action == ActionStashPrompt {
		return ScreenEvent{Type: ScreenEventStashPrompt}
	}
	if action == ActionKillAgents {
		return ScreenEvent{Type: ScreenEventKillAgents}
	}
	if action == ActionInterrupt {
		if s.Dialog != nil {
			return s.applyDoublePressExit(KeyCtrlC)
		}
		return s.applyInterrupt()
	}
	if action == ActionExit {
		if s.Dialog != nil || s.Prompt.Text == "" {
			return s.applyDoublePressExit(KeyCtrlD)
		}
		result := s.Prompt.Apply(Key{Type: KeyCtrlD})
		if result.Cancelled {
			return ScreenEvent{Type: ScreenEventCancelled}
		}
		return ScreenEvent{}
	}
	if s.Dialog != nil {
		return s.applyDialogAction(action)
	}
	switch action {
	case ActionScrollUp:
		s.Viewport.Scroll(-1)
	case ActionScrollDown:
		s.Viewport.Scroll(1)
	case ActionPageUp:
		s.Viewport.Page(-1)
	case ActionPageDown:
		s.Viewport.Page(1)
	case ActionHalfPageUp:
		s.Viewport.HalfPage(-1)
	case ActionHalfPageDown:
		s.Viewport.HalfPage(1)
	case ActionScrollToTop:
		s.Viewport.ScrollToTop()
	case ActionScrollToBottom:
		s.Viewport.ScrollToBottom()
	case ActionReverseSearch:
		s.OpenReverseSearch("")
		return ScreenEvent{Type: ScreenEventReverseSearch}
	case ActionCancel:
		return ScreenEvent{Type: ScreenEventCancelled}
	default:
		result := s.Prompt.Apply(keyForAction(action, key))
		switch {
		case result.Submitted != "":
			return ScreenEvent{Type: ScreenEventPromptSubmitted, Value: result.Submitted}
		case result.Cancelled:
			return ScreenEvent{Type: ScreenEventCancelled}
		case result.Interrupted:
			return ScreenEvent{Type: ScreenEventInterrupted}
		}
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyInterrupt() ScreenEvent {
	now := s.now()
	if s.ExitPendingKey == KeyCtrlC && !s.ExitPendingAt.IsZero() && now.Sub(s.ExitPendingAt) <= DoublePressTimeout {
		s.ExitPendingKey = ""
		s.ExitPendingAt = time.Time{}
		return ScreenEvent{Type: ScreenEventExit}
	}
	s.ExitPendingKey = KeyCtrlC
	s.ExitPendingAt = now
	return ScreenEvent{Type: ScreenEventInterrupted}
}

func (s *REPLScreen) applyDoublePressExit(key KeyType) ScreenEvent {
	now := s.now()
	if s.ExitPendingKey == key && !s.ExitPendingAt.IsZero() && now.Sub(s.ExitPendingAt) <= DoublePressTimeout {
		s.ExitPendingKey = ""
		s.ExitPendingAt = time.Time{}
		return ScreenEvent{Type: ScreenEventExit}
	}
	s.ExitPendingKey = key
	s.ExitPendingAt = now
	return ScreenEvent{Type: ScreenEventExitPending, Value: exitKeyDisplay(key)}
}

func (s *REPLScreen) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func exitKeyDisplay(key KeyType) string {
	switch key {
	case KeyCtrlC:
		return "Ctrl-C"
	case KeyCtrlD:
		return "Ctrl-D"
	default:
		return string(key)
	}
}

func (s *REPLScreen) OpenReverseSearch(query string) {
	s.ReverseSearch = NewReverseSearchState(s.Prompt.History, query)
}

func (s *REPLScreen) applyReverseSearchKey(key Key) ScreenEvent {
	sharedKillRing.trackKey(key.Type)
	switch key.Type {
	case KeyEsc, KeyCtrlC:
		s.ReverseSearch = ReverseSearchState{}
		return ScreenEvent{Type: ScreenEventCancelled}
	case KeyEnter:
		if selected, ok := s.ReverseSearch.Current(); ok {
			s.Prompt.Text = selected
			s.Prompt.Cursor = len([]rune(selected))
			s.Prompt.resetHistoryCursor()
			s.ReverseSearch = ReverseSearchState{}
			return ScreenEvent{Type: ScreenEventReverseSelected, Value: selected}
		}
		s.ReverseSearch = ReverseSearchState{}
	case KeyUp, KeyCtrlP:
		s.ReverseSearch.Move(-1)
	case KeyDown, KeyCtrlN:
		s.ReverseSearch.Move(1)
	case KeyBackspace:
		s.ReverseSearch.Backspace(s.Prompt.History)
	case KeyDelete, KeyCtrlD:
		s.ReverseSearch.DeleteForward(s.Prompt.History)
	case KeyLeft, KeyCtrlB:
		s.ReverseSearch.MoveCursor(-1)
	case KeyRight, KeyCtrlF:
		s.ReverseSearch.MoveCursor(1)
	case KeyAltB, KeyAltLeft, KeyCtrlLeft:
		s.ReverseSearch.MoveWordBackward()
	case KeyAltF, KeyAltRight, KeyCtrlRight:
		s.ReverseSearch.MoveWordForward()
	case KeyHome, KeyCtrlA:
		s.ReverseSearch.MoveStart()
	case KeyEnd, KeyCtrlE:
		s.ReverseSearch.MoveEnd()
	case KeyCtrlK:
		s.ReverseSearch.DeleteToEnd(s.Prompt.History)
	case KeyCtrlU:
		s.ReverseSearch.DeleteToStart(s.Prompt.History)
	case KeyCtrlW:
		s.ReverseSearch.DeleteWordBackward(s.Prompt.History)
	case KeyAltBS:
		s.ReverseSearch.DeleteWordBackward(s.Prompt.History)
	case KeyAltD:
		s.ReverseSearch.DeleteWordForward(s.Prompt.History)
	case KeyCtrlY:
		s.ReverseSearch.YankLastKill(s.Prompt.History)
	case KeyAltY:
		s.ReverseSearch.YankPop(s.Prompt.History)
	case KeyRune:
		s.ReverseSearch.AppendRune(s.Prompt.History, key.Rune)
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyMouse(key Key) ScreenEvent {
	if key.MouseRelease {
		return ScreenEvent{}
	}
	if s.Dialog != nil && isPrimaryMouseAction(key.MouseButton) {
		if index, ok := s.dialogActionAtMouse(key.MouseX, key.MouseY); ok {
			dialogID := s.Dialog.ID
			dialogKind := s.Dialog.Kind
			value := s.Dialog.Actions[index]
			s.Dialog.Focused = index
			s.Dialog = nil
			return ScreenEvent{Type: ScreenEventDialogAction, Value: value, DialogID: dialogID, DialogKind: dialogKind}
		}
	}
	if s.Dialog == nil && isPrimaryMouseAction(key.MouseButton) {
		if line, ok := s.selectViewportAtMouse(key.MouseX, key.MouseY); ok {
			return ScreenEvent{Type: ScreenEventViewportSelected, Value: line}
		}
	}
	if delta := mouseWheelDelta(key.MouseButton); delta != 0 {
		s.Viewport.Scroll(delta)
	}
	return ScreenEvent{}
}

const (
	sgrMouseBaseMask  = 3
	sgrMouseWheelMask = 64
)

func isPrimaryMouseAction(button int) bool {
	return button&sgrMouseWheelMask == 0 && button&sgrMouseBaseMask == 0
}

func mouseWheelDelta(button int) int {
	if button&sgrMouseWheelMask == 0 {
		return 0
	}
	switch button & sgrMouseBaseMask {
	case 0:
		return -1
	case 1:
		return 1
	default:
		return 0
	}
}

func (s *REPLScreen) selectViewportAtMouse(x int, y int) (string, bool) {
	if x <= 0 || y <= 0 {
		return "", false
	}
	bodyHeight := s.Height - 2
	if bodyHeight < 0 {
		bodyHeight = 0
	}
	if y > bodyHeight {
		return "", false
	}
	visible := s.Viewport.Visible()
	index := y - 1
	if index < 0 || index >= len(visible) {
		return "", false
	}
	s.SelectedViewportLine = s.Viewport.Offset + index
	return visible[index], true
}

func (s *REPLScreen) dialogActionAtMouse(x int, y int) (int, bool) {
	if s.Dialog == nil || len(s.Dialog.Actions) == 0 || x <= 0 || y <= 0 {
		return 0, false
	}
	dialogLines := RenderDialog(*s.Dialog, s.Width)
	if len(dialogLines) < 3 {
		return 0, false
	}
	bodyHeight := s.Height - 2
	if bodyHeight < 0 {
		bodyHeight = 0
	}
	actionIndex := len(dialogLines) - 2
	visibleStart := 0
	if len(dialogLines) > bodyHeight {
		visibleStart = len(dialogLines) - bodyHeight
	}
	if actionIndex < visibleStart {
		return 0, false
	}
	actionRow := actionIndex - visibleStart + 1
	if y != actionRow {
		return 0, false
	}
	col := 3
	for i, action := range s.Dialog.Actions {
		width := len([]rune(action)) + 2
		if x >= col && x < col+width {
			return i, true
		}
		col += width
		if i < len(s.Dialog.Actions)-1 {
			col++
		}
	}
	return 0, false
}

func (s *REPLScreen) Frame() Frame {
	var reverseSearch *ReverseSearchState
	if s.ReverseSearch.Active {
		state := s.ReverseSearch
		reverseSearch = &state
	}
	return Frame{
		Width:         s.Width,
		Height:        s.Height,
		BodyLines:     s.Viewport.Visible(),
		Status:        s.Status,
		Prompt:        s.Prompt,
		Dialog:        s.Dialog,
		ReverseSearch: reverseSearch,
		ShowCursor:    s.Dialog == nil,
	}
}

func (s *REPLScreen) Render() string {
	return NewRenderer(s.Width, s.Height).Render(s.Frame())
}

func (s *REPLScreen) Resize(width int, height int) {
	if width > 0 {
		s.Width = width
	}
	if height > 0 {
		s.Height = height
	}
	s.rebuildViewportPreservingScroll()
}

func (s *REPLScreen) rebuildViewport() {
	bodyHeight := s.Height - 2
	if bodyHeight < 0 {
		bodyHeight = 0
	}
	lines := RenderMessages(s.Messages, s.Width)
	s.Viewport = NewViewport(lines, bodyHeight)
}

func (s *REPLScreen) rebuildViewportPreservingScroll() {
	previous := s.Viewport
	bodyHeight := s.Height - 2
	if bodyHeight < 0 {
		bodyHeight = 0
	}
	atBottom := previous.Offset >= maxViewportOffset(previous)
	lines := RenderMessages(s.Messages, s.Width)
	s.Viewport = Viewport{Lines: lines, Height: bodyHeight, Offset: previous.Offset}
	if atBottom {
		s.Viewport.ScrollToBottom()
		return
	}
	s.Viewport.clamp()
}

func maxViewportOffset(viewport Viewport) int {
	if viewport.Height <= 0 {
		return 0
	}
	maxOffset := len(viewport.Lines) - viewport.Height
	if maxOffset < 0 {
		return 0
	}
	return maxOffset
}

func (s *REPLScreen) applyDialogAction(action Action) ScreenEvent {
	if s.Dialog == nil {
		return ScreenEvent{}
	}
	dialogID := s.Dialog.ID
	dialogKind := s.Dialog.Kind
	switch action {
	case ActionFocusNext, ActionMoveRight, ActionHistoryNext:
		if len(s.Dialog.Actions) > 0 {
			s.Dialog.Focused = (s.Dialog.Focused + 1) % len(s.Dialog.Actions)
		}
	case ActionFocusPrevious, ActionMoveLeft, ActionHistoryPrevious:
		if len(s.Dialog.Actions) > 0 {
			s.Dialog.Focused--
			if s.Dialog.Focused < 0 {
				s.Dialog.Focused = len(s.Dialog.Actions) - 1
			}
		}
	case ActionSubmitPrompt, ActionConfirmSelection:
		if len(s.Dialog.Actions) == 0 {
			return ScreenEvent{Type: ScreenEventDialogAction, DialogID: dialogID, DialogKind: dialogKind}
		}
		value := s.Dialog.Actions[s.Dialog.Focused]
		s.Dialog = nil
		return ScreenEvent{Type: ScreenEventDialogAction, Value: value, DialogID: dialogID, DialogKind: dialogKind}
	case ActionCancel, ActionInterrupt:
		s.Dialog = nil
		return ScreenEvent{Type: ScreenEventCancelled, DialogID: dialogID, DialogKind: dialogKind}
	}
	return ScreenEvent{}
}

func keyForAction(action Action, key Key) Key {
	switch action {
	case ActionInsertRune:
		return key
	case ActionInsertNewline:
		return Key{Type: KeyShiftEnter}
	case ActionSubmitPrompt:
		return Key{Type: KeyEnter}
	case ActionMoveLeft:
		return Key{Type: KeyLeft}
	case ActionMoveRight:
		return Key{Type: KeyRight}
	case ActionMoveWordLeft:
		return Key{Type: KeyAltB}
	case ActionMoveWordRight:
		return Key{Type: KeyAltF}
	case ActionMoveStart:
		return Key{Type: KeyHome}
	case ActionMoveEnd:
		return Key{Type: KeyEnd}
	case ActionDeleteBackward:
		return Key{Type: KeyBackspace}
	case ActionDeleteForward:
		return Key{Type: KeyDelete}
	case ActionDeleteToStart:
		return Key{Type: KeyCtrlU}
	case ActionDeleteToEnd:
		return Key{Type: KeyCtrlK}
	case ActionDeleteWordBack:
		return Key{Type: KeyCtrlW}
	case ActionDeleteWordFwd:
		return Key{Type: KeyAltD}
	case ActionYank:
		return Key{Type: KeyCtrlY}
	case ActionYankPop:
		return Key{Type: KeyAltY}
	case ActionHistoryPrevious:
		return Key{Type: KeyUp}
	case ActionHistoryNext:
		return Key{Type: KeyDown}
	case ActionCancel:
		return Key{Type: KeyEsc}
	case ActionInterrupt:
		return Key{Type: KeyCtrlC}
	case ActionExit:
		return Key{Type: KeyCtrlD}
	default:
		return key
	}
}
