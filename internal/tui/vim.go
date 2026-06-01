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
			return ScreenEvent{}, true
		}
		return ScreenEvent{}, false
	}
	switch key.Type {
	case KeyEsc:
		return ScreenEvent{}, true
	case KeyRune:
		return s.applyVimNormalRune(key.Rune), true
	}
	return ScreenEvent{}, false
}

func (s *REPLScreen) applyVimNormalRune(r rune) ScreenEvent {
	switch r {
	case 'i':
		s.VimMode = VimInsert
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
	case '0':
		s.Prompt.Apply(Key{Type: KeyHome})
	case '$':
		s.Prompt.Apply(Key{Type: KeyEnd})
	case 'x':
		s.Prompt.Apply(Key{Type: KeyDelete})
	}
	return ScreenEvent{}
}
