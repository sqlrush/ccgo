package tui

type ScreenLifecycle struct {
	AlternateScreen bool
	CursorHidden    bool
}

func (l *ScreenLifecycle) EnterAlternate() string {
	l.AlternateScreen = true
	l.CursorHidden = true
	return EnterAlternateScreen + HideCursor + ClearScreen + HomeCursor
}

func (l *ScreenLifecycle) ExitAlternate() string {
	l.CursorHidden = false
	l.AlternateScreen = false
	return ShowCursor + ExitAlternateScreen
}

func (l *ScreenLifecycle) ShowCursor() string {
	l.CursorHidden = false
	return ShowCursor
}

func (l *ScreenLifecycle) HideCursor() string {
	l.CursorHidden = true
	return HideCursor
}
