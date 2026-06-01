package tui

type VimMode string

const (
	VimInsert VimMode = "insert"
	VimNormal VimMode = "normal"
)

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
			s.VimPendingOperator = 0
			return ScreenEvent{}, true
		}
		return ScreenEvent{}, false
	}
	switch key.Type {
	case KeyEsc:
		s.VimPendingOperator = 0
		return ScreenEvent{}, true
	case KeyRune:
		return s.applyVimNormalRune(key.Rune), true
	}
	return ScreenEvent{}, false
}

func (s *REPLScreen) applyVimNormalRune(r rune) ScreenEvent {
	if s.VimPendingOperator != 0 {
		return s.applyVimOperator(r)
	}
	switch r {
	case 'i':
		s.VimMode = VimInsert
	case 'd', 'c':
		s.VimPendingOperator = r
	case 'a':
		s.Prompt.Apply(Key{Type: KeyRight})
		s.VimMode = VimInsert
	case 'I':
		s.Prompt.Apply(Key{Type: KeyHome})
		s.VimMode = VimInsert
	case 'A':
		s.Prompt.Apply(Key{Type: KeyEnd})
		s.VimMode = VimInsert
	case 'h':
		s.Prompt.Apply(Key{Type: KeyLeft})
	case 'l':
		s.Prompt.Apply(Key{Type: KeyRight})
	case 'w':
		s.Prompt.moveWordForward()
	case 'b':
		s.Prompt.moveWordBackward()
	case 'e':
		s.Prompt.moveWordEnd()
	case '0':
		s.Prompt.Apply(Key{Type: KeyHome})
	case '$':
		s.Prompt.Apply(Key{Type: KeyEnd})
	case 'x':
		s.Prompt.Apply(Key{Type: KeyDelete})
	case 'X':
		s.Prompt.Apply(Key{Type: KeyBackspace})
	case 'D':
		s.Prompt.deleteToEnd()
	case 'C':
		s.Prompt.deleteToEnd()
		s.VimMode = VimInsert
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyVimOperator(r rune) ScreenEvent {
	operator := s.VimPendingOperator
	s.VimPendingOperator = 0
	change := operator == 'c'
	switch r {
	case 'd', 'c':
		if r == operator {
			s.Prompt.deleteAll()
			if change {
				s.VimMode = VimInsert
			}
		}
	case 'w':
		s.Prompt.deleteWordForward()
		if change {
			s.VimMode = VimInsert
		}
	case '$':
		s.Prompt.deleteToEnd()
		if change {
			s.VimMode = VimInsert
		}
	case 'b':
		s.Prompt.deleteWordBackward()
		if change {
			s.VimMode = VimInsert
		}
	}
	return ScreenEvent{}
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

func isWordRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}
