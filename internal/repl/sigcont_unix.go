//go:build !windows

package repl

// REPL-56: SIGCONT signal handling.
// When the process is resumed after being suspended (Ctrl+Z / SIGTSTP then
// `fg`), the kernel delivers SIGCONT. The TUI must force a full redraw so the
// terminal is in a consistent state after returning from the stopped state.
//
// Implementation: install a SIGCONT handler that posts a resizeEvent (same
// channel used by SIGWINCH). The event loop already calls Render() on every
// resize event, so a resize post-SIGCONT triggers a full repaint — exactly
// mirroring how CC handles it.
//
// CC ref: src/ink/ink.tsx:960 ("SIGCONT" listener → forceUpdate).

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// startSIGCONTListener installs a SIGCONT handler that posts the current
// terminal size to out, triggering a full redraw. It is a no-op for non-TTY
// terminals (pipes do not need repainting). The goroutine exits when ctx is
// cancelled.
func startSIGCONTListener(ctx context.Context, t Terminal, out chan<- resizeEvent) {
	if !t.IsTTY() {
		return
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGCONT)
	go func() {
		defer signal.Stop(sig)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sig:
				w, h, err := t.Size()
				if err != nil || w <= 0 || h <= 0 {
					// Terminal size unavailable; send a minimal non-zero event
					// so the loop still redraws.
					select {
					case out <- resizeEvent{Width: 80, Height: 24}:
					case <-ctx.Done():
						return
					}
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
