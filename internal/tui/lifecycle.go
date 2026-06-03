package tui

type ScreenLifecycle struct {
	AlternateScreen bool
	CursorHidden    bool
	MouseTracking   bool
	FocusEvents     bool
	BracketedPaste  bool
	ExtendedKeys    bool
}

type TerminalModeOptions struct {
	MouseTracking  bool
	FocusEvents    bool
	BracketedPaste bool
	ExtendedKeys   bool
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

func (l *ScreenLifecycle) EnterInteractive(options TerminalModeOptions) string {
	seq := l.EnterAlternate()
	seq += l.SetTerminalModes(options)
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

func (l *ScreenLifecycle) ExitInteractive() string {
	return l.DisableTerminalModes() + l.ExitAlternate()
}

func (l *ScreenLifecycle) Reset() string {
	return l.ExitInteractive()
}

func (l *ScreenLifecycle) EnableTerminalModes(options TerminalModeOptions) string {
	seq := ""
	if options.BracketedPaste && !l.BracketedPaste {
		seq += EnableBracketedPaste
		l.BracketedPaste = true
	}
	if options.FocusEvents && !l.FocusEvents {
		seq += EnableFocusEvents
		l.FocusEvents = true
	}
	if options.ExtendedKeys && !l.ExtendedKeys {
		seq += EnableExtendedKeys
		l.ExtendedKeys = true
	}
	if options.MouseTracking && !l.MouseTracking {
		seq += EnableMouseTracking
		l.MouseTracking = true
	}
	return seq
}

func (l *ScreenLifecycle) SetTerminalModes(options TerminalModeOptions) string {
	seq := ""
	if !options.MouseTracking && l.MouseTracking {
		seq += DisableMouseTracking
		l.MouseTracking = false
	}
	if !options.FocusEvents && l.FocusEvents {
		seq += DisableFocusEvents
		l.FocusEvents = false
	}
	if !options.ExtendedKeys && l.ExtendedKeys {
		seq += DisableExtendedKeys
		l.ExtendedKeys = false
	}
	if !options.BracketedPaste && l.BracketedPaste {
		seq += DisableBracketedPaste
		l.BracketedPaste = false
	}
	seq += l.EnableTerminalModes(options)
	return seq
}

func (l *ScreenLifecycle) DisableTerminalModes() string {
	seq := ""
	if l.MouseTracking {
		seq += DisableMouseTracking
		l.MouseTracking = false
	}
	if l.FocusEvents {
		seq += DisableFocusEvents
		l.FocusEvents = false
	}
	if l.ExtendedKeys {
		seq += DisableExtendedKeys
		l.ExtendedKeys = false
	}
	if l.BracketedPaste {
		seq += DisableBracketedPaste
		l.BracketedPaste = false
	}
	return seq
}

func (l *ScreenLifecycle) ReassertTerminalModes(options TerminalModeOptions) string {
	seq := ""
	if options.BracketedPaste && l.BracketedPaste {
		seq += EnableBracketedPaste
	}
	if options.FocusEvents && l.FocusEvents {
		seq += EnableFocusEvents
	}
	if options.ExtendedKeys && l.ExtendedKeys {
		seq += ReassertExtendedKeys
	}
	if options.MouseTracking && l.MouseTracking {
		seq += EnableMouseTracking
	}
	return seq
}

func (l *ScreenLifecycle) ReassertInteractive(options TerminalModeOptions) string {
	seq := ""
	if l.AlternateScreen {
		seq += EnterAlternateScreen + ClearScreen + HomeCursor
	}
	if l.CursorHidden {
		seq += HideCursor
	}
	seq += l.ReassertTerminalModes(options)
	return seq
}

func (l *ScreenLifecycle) ShowCursor() string {
	l.CursorHidden = false
	return ShowCursor
}

func (l *ScreenLifecycle) HideCursor() string {
	l.CursorHidden = true
	return HideCursor
}
