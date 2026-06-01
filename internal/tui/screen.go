package tui

type ScreenEventType string

const (
	ScreenEventNone            ScreenEventType = ""
	ScreenEventPromptSubmitted ScreenEventType = "prompt_submitted"
	ScreenEventDialogAction    ScreenEventType = "dialog_action"
	ScreenEventCancelled       ScreenEventType = "cancelled"
	ScreenEventInterrupted     ScreenEventType = "interrupted"
	ScreenEventReverseSearch   ScreenEventType = "reverse_search"
	ScreenEventReverseSelected ScreenEventType = "reverse_search_selected"
	ScreenEventFocusIn         ScreenEventType = "focus_in"
	ScreenEventFocusOut        ScreenEventType = "focus_out"
)

type ScreenEvent struct {
	Type       ScreenEventType
	Value      string
	DialogID   string
	DialogKind DialogKind
}

type REPLScreen struct {
	Width         int
	Height        int
	Messages      []Message
	Status        string
	Prompt        PromptState
	Dialog        *Dialog
	Keymap        Keymap
	Viewport      Viewport
	VimEnabled    bool
	VimMode       VimMode
	Focused       bool
	ReverseSearch ReverseSearchState
}

func NewREPLScreen(width int, height int, history []string) REPLScreen {
	prompt := NewPromptState(history)
	prompt.EnablePasteReferences()
	screen := REPLScreen{
		Width:   width,
		Height:  height,
		Prompt:  prompt,
		Keymap:  DefaultKeymap(),
		Focused: true,
	}
	screen.rebuildViewport()
	return screen
}

func (s *REPLScreen) SetMessages(messages []Message) {
	s.Messages = append([]Message(nil), messages...)
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
	case ActionReverseSearch:
		s.OpenReverseSearch("")
		return ScreenEvent{Type: ScreenEventReverseSearch}
	case ActionCancel:
		return ScreenEvent{Type: ScreenEventCancelled}
	case ActionInterrupt:
		return ScreenEvent{Type: ScreenEventInterrupted}
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

func (s *REPLScreen) OpenReverseSearch(query string) {
	s.ReverseSearch = NewReverseSearchState(s.Prompt.History, query)
}

func (s *REPLScreen) applyReverseSearchKey(key Key) ScreenEvent {
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
	case KeyUp:
		s.ReverseSearch.Move(-1)
	case KeyDown:
		s.ReverseSearch.Move(1)
	case KeyBackspace:
		s.ReverseSearch.Backspace(s.Prompt.History)
	case KeyRune:
		s.ReverseSearch.AppendRune(s.Prompt.History, key.Rune)
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyMouse(key Key) ScreenEvent {
	if key.MouseRelease {
		return ScreenEvent{}
	}
	switch key.MouseButton {
	case 64:
		s.Viewport.Scroll(-1)
	case 65:
		s.Viewport.Scroll(1)
	}
	return ScreenEvent{}
}

func (s *REPLScreen) Frame() Frame {
	return Frame{
		Width:      s.Width,
		Height:     s.Height,
		BodyLines:  s.Viewport.Visible(),
		Status:     s.Status,
		Prompt:     s.Prompt,
		Dialog:     s.Dialog,
		ShowCursor: s.Dialog == nil,
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
	case ActionSubmitPrompt:
		return Key{Type: KeyEnter}
	case ActionMoveLeft:
		return Key{Type: KeyLeft}
	case ActionMoveRight:
		return Key{Type: KeyRight}
	case ActionMoveStart:
		return Key{Type: KeyHome}
	case ActionMoveEnd:
		return Key{Type: KeyEnd}
	case ActionDeleteBackward:
		return Key{Type: KeyBackspace}
	case ActionDeleteForward:
		return Key{Type: KeyDelete}
	case ActionHistoryPrevious:
		return Key{Type: KeyUp}
	case ActionHistoryNext:
		return Key{Type: KeyDown}
	case ActionCancel:
		return Key{Type: KeyEsc}
	case ActionInterrupt:
		return Key{Type: KeyCtrlC}
	default:
		return key
	}
}
