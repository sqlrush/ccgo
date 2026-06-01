package tui

type ScreenLifecycle struct {
	AlternateScreen bool
	CursorHidden    bool
}

func (l *ScreenLifecycle) EnterAlternate() string {
	if l.AlternateScreen && l.CursorHidden {
		return ""
	}
	seq := ""
	if !l.AlternateScreen {
		seq += EnterAlternateScreen + ClearScreen + HomeCursor
	}
	if !l.CursorHidden {
		seq += HideCursor
	}
	l.AlternateScreen = true
	l.CursorHidden = true
	return seq
}

func (l *ScreenLifecycle) ExitAlternate() string {
	if !l.AlternateScreen && !l.CursorHidden {
		return ""
	}
	seq := ""
	if l.CursorHidden {
		seq += ShowCursor
	}
	if l.AlternateScreen {
		seq += ExitAlternateScreen
	}
	l.CursorHidden = false
	l.AlternateScreen = false
	return seq
}

func (l *ScreenLifecycle) Reset() string {
	return l.ExitAlternate()
}

func (l *ScreenLifecycle) ShowCursor() string {
	l.CursorHidden = false
	return ShowCursor
}

func (l *ScreenLifecycle) HideCursor() string {
	l.CursorHidden = true
	return HideCursor
}
