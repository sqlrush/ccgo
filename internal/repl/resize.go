package repl

// resizeEvent carries a new terminal size produced by a SIGWINCH.
type resizeEvent struct {
	Width  int
	Height int
}

// applyResize updates the screen and cached dimensions. Non-positive sizes
// (e.g. a transient zero from a detaching tty) are ignored.
func (l *Loop) applyResize(ev resizeEvent) {
	if ev.Width <= 0 || ev.Height <= 0 {
		return
	}
	l.width = ev.Width
	l.height = ev.Height
	l.screen.Resize(ev.Width, ev.Height)
}
