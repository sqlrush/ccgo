package tui

import "ccgo/internal/session"

type VimMode string

const (
	VimInsert VimMode = "insert"
	VimNormal VimMode = "normal"
)

type vimPromptSnapshot struct {
	Text           string
	Cursor         int
	PastedContents map[int]session.PastedContent
	NextPastedID   int
}

func (s *REPLScreen) SetVimEnabled(enabled bool) {
	s.VimEnabled = enabled
	if s.VimMode == "" {
		s.VimMode = VimInsert
	}
}

func (s *REPLScreen) applyVimKey(key Key) (ScreenEvent, bool) {
	if !s.VimEnabled {
		return ScreenEvent{}, false
	}
	if s.VimMode == "" {
		s.VimMode = VimInsert
	}
	if s.VimMode == VimInsert {
		if key.Type == KeyEsc {
			s.VimMode = VimNormal
			s.clearVimPending()
			return ScreenEvent{}, true
		}
		return ScreenEvent{}, false
	}
	switch key.Type {
	case KeyEsc:
		s.clearVimPending()
		return ScreenEvent{}, true
	case KeyRune:
		return s.applyVimNormalRune(key.Rune), true
	}
	return ScreenEvent{}, false
}

func (s *REPLScreen) applyVimNormalRune(r rune) ScreenEvent {
	if s.VimPendingReplace {
		return s.applyVimReplace(r)
	}
	if s.VimPendingOperator != 0 {
		return s.applyVimOperator(r)
	}
	if isVimCountRune(r) {
		if r == '0' && s.VimCount == 0 {
			s.Prompt.Apply(Key{Type: KeyHome})
			return ScreenEvent{}
		}
		s.VimCount = s.VimCount*10 + int(r-'0')
		return ScreenEvent{}
	}
	count := s.takeVimCount()
	switch r {
	case 'i':
		s.VimMode = VimInsert
		s.clearVimPending()
	case 'u':
		s.undoVimPrompt()
	case 'r':
		s.VimPendingReplace = true
		s.VimPendingCount = count
	case 'd', 'c':
		s.VimPendingOperator = r
		s.VimPendingCount = count
	case 'a':
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyRight}) })
		s.VimMode = VimInsert
	case 'I':
		s.Prompt.Apply(Key{Type: KeyHome})
		s.VimMode = VimInsert
	case 'A':
		s.Prompt.Apply(Key{Type: KeyEnd})
		s.VimMode = VimInsert
	case 'h':
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyLeft}) })
	case 'l':
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyRight}) })
	case 'w':
		applyN(count, func() { s.Prompt.moveWordForward() })
	case 'b':
		applyN(count, func() { s.Prompt.moveWordBackward() })
	case 'e':
		applyN(count, func() { s.Prompt.moveWordEnd() })
	case '$':
		s.Prompt.Apply(Key{Type: KeyEnd})
	case 'x':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyDelete}) })
	case 'X':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyBackspace}) })
	case 'D':
		s.recordVimUndo()
		s.Prompt.deleteToEnd()
	case 'C':
		s.recordVimUndo()
		s.Prompt.deleteToEnd()
		s.VimMode = VimInsert
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyVimOperator(r rune) ScreenEvent {
	if isVimCountRune(r) && (r != '0' || s.VimCount > 0) {
		s.VimCount = s.VimCount*10 + int(r-'0')
		return ScreenEvent{}
	}
	operator := s.VimPendingOperator
	count := s.takeVimOperatorCount()
	s.clearVimPending()
	change := operator == 'c'
	switch r {
	case 'd', 'c':
		if r == operator {
			s.recordVimUndo()
			s.Prompt.deleteAll()
			if change {
				s.VimMode = VimInsert
			}
		}
	case 'w':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.deleteWordForward() })
		if change {
			s.VimMode = VimInsert
		}
	case '$':
		s.recordVimUndo()
		s.Prompt.deleteToEnd()
		if change {
			s.VimMode = VimInsert
		}
	case '0':
		s.recordVimUndo()
		s.Prompt.deleteToStart()
		if change {
			s.VimMode = VimInsert
		}
	case 'b':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.deleteWordBackward() })
		if change {
			s.VimMode = VimInsert
		}
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyVimReplace(r rune) ScreenEvent {
	count := s.takeVimOperatorCount()
	s.clearVimPending()
	s.recordVimUndo()
	s.Prompt.replaceRunes(count, r)
	return ScreenEvent{}
}

func (s *REPLScreen) takeVimCount() int {
	if s.VimCount <= 0 {
		return 1
	}
	count := s.VimCount
	s.VimCount = 0
	return count
}

func (s *REPLScreen) takeVimOperatorCount() int {
	count := s.VimPendingCount
	if count <= 0 {
		count = 1
	}
	if s.VimCount > 0 {
		count *= s.VimCount
	}
	return count
}

func (s *REPLScreen) clearVimPending() {
	s.VimPendingOperator = 0
	s.VimPendingCount = 0
	s.VimCount = 0
	s.VimPendingReplace = false
}

func (s *REPLScreen) recordVimUndo() {
	snapshot := vimPromptSnapshot{
		Text:           s.Prompt.Text,
		Cursor:         s.Prompt.Cursor,
		PastedContents: clonePastedContents(s.Prompt.PastedContents),
		NextPastedID:   s.Prompt.NextPastedID,
	}
	if len(s.VimUndoStack) > 0 {
		last := s.VimUndoStack[len(s.VimUndoStack)-1]
		if last.Text == snapshot.Text && last.Cursor == snapshot.Cursor {
			return
		}
	}
	s.VimUndoStack = append(s.VimUndoStack, snapshot)
	if len(s.VimUndoStack) > 50 {
		s.VimUndoStack = append([]vimPromptSnapshot(nil), s.VimUndoStack[len(s.VimUndoStack)-50:]...)
	}
}

func (s *REPLScreen) undoVimPrompt() {
	if len(s.VimUndoStack) == 0 {
		return
	}
	last := s.VimUndoStack[len(s.VimUndoStack)-1]
	s.VimUndoStack = s.VimUndoStack[:len(s.VimUndoStack)-1]
	s.Prompt.Text = last.Text
	s.Prompt.Cursor = last.Cursor
	s.Prompt.replacePastedContents(last.PastedContents)
	if s.Prompt.UsePasteReferences && last.NextPastedID > 0 {
		s.Prompt.NextPastedID = last.NextPastedID
	}
	s.Prompt.resetHistoryCursor()
}

func isVimCountRune(r rune) bool {
	return r >= '0' && r <= '9'
}

func applyN(count int, fn func()) {
	if count <= 0 {
		count = 1
	}
	for i := 0; i < count; i++ {
		fn()
	}
}

func (p *PromptState) moveWordForward() {
	runes := []rune(p.Text)
	i := p.Cursor
	for i < len(runes) && !isWordRune(runes[i]) {
		i++
	}
	for i < len(runes) && isWordRune(runes[i]) {
		i++
	}
	for i < len(runes) && !isWordRune(runes[i]) {
		i++
	}
	p.Cursor = i
}

func (p *PromptState) moveWordBackward() {
	runes := []rune(p.Text)
	i := p.Cursor
	if i > len(runes) {
		i = len(runes)
	}
	for i > 0 && !isWordRune(runes[i-1]) {
		i--
	}
	for i > 0 && isWordRune(runes[i-1]) {
		i--
	}
	p.Cursor = i
}

func (p *PromptState) moveWordEnd() {
	runes := []rune(p.Text)
	i := p.Cursor
	if i < len(runes) && isWordRune(runes[i]) {
		i++
	}
	for i < len(runes) && !isWordRune(runes[i]) {
		i++
	}
	for i < len(runes) && isWordRune(runes[i]) {
		i++
	}
	if i > 0 {
		i--
	}
	p.Cursor = i
}

func (p *PromptState) deleteAll() {
	p.Text = ""
	p.Cursor = 0
	p.resetHistoryCursor()
	p.resetPastedContents()
}

func (p *PromptState) deleteToEnd() {
	runes := []rune(p.Text)
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor > len(runes) {
		p.Cursor = len(runes)
	}
	p.Text = string(runes[:p.Cursor])
	p.resetHistoryCursor()
}

func (p *PromptState) deleteToStart() {
	runes := []rune(p.Text)
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor > len(runes) {
		p.Cursor = len(runes)
	}
	p.Text = string(runes[p.Cursor:])
	p.Cursor = 0
	p.resetHistoryCursor()
}

func (p *PromptState) deleteWordForward() {
	start := p.Cursor
	p.moveWordForward()
	end := p.Cursor
	p.deleteRange(start, end)
}

func (p *PromptState) deleteWordBackward() {
	end := p.Cursor
	p.moveWordBackward()
	start := p.Cursor
	p.deleteRange(start, end)
}

func (p *PromptState) deleteRange(start int, end int) {
	runes := []rune(p.Text)
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if end < start {
		start, end = end, start
	}
	p.Text = string(append(runes[:start], runes[end:]...))
	p.Cursor = start
	p.resetHistoryCursor()
}

func (p *PromptState) replaceRunes(count int, r rune) {
	if count <= 0 {
		count = 1
	}
	runes := []rune(p.Text)
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor >= len(runes) {
		return
	}
	end := p.Cursor + count
	if end > len(runes) {
		end = len(runes)
	}
	for i := p.Cursor; i < end; i++ {
		runes[i] = r
	}
	p.Text = string(runes)
	p.resetHistoryCursor()
}

func isWordRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}
