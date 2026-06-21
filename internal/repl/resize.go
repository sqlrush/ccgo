//go:build !windows

package repl

import (
	"context"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

// resizeEvent carries a new terminal size produced by a SIGWINCH.
type resizeEvent struct {
	Width  int
	Height int
}

// startResizeListener installs a SIGWINCH handler that posts the current
// terminal size to out. It is a no-op for non-tty terminals (pipes never
// resize) and returns as soon as the goroutine is started. The goroutine
// stops when ctx is cancelled.
func startResizeListener(ctx context.Context, t Terminal, out chan<- resizeEvent) {
	if !t.IsTTY() {
		return
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, unix.SIGWINCH)
	go func() {
		defer signal.Stop(sig)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sig:
				w, h, err := t.Size()
				if err != nil || w <= 0 || h <= 0 {
					continue
				}
				select {
				case out <- resizeEvent{Width: w, Height: h}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
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
