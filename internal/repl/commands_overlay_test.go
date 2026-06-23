package repl

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestApplyCommandOutcomeSetsOverlay: when a CommandOutcome carries an Overlay,
// applyCommandOutcome must set l.activeOverlay.
func TestApplyCommandOutcomeSetsOverlay(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	ol := newListOverlay("test", []listItem{{Label: "a", Submit: "a:1"}})
	l.applyCommandOutcome(CommandOutcome{Handled: true, Overlay: ol})
	if l.activeOverlay == nil {
		t.Fatal("applyCommandOutcome must set l.activeOverlay when Overlay is non-nil")
	}
}

// TestCommandRouterOpensOverlayViaLoop: feed a command whose handler returns
// an Overlay; the loop must set activeOverlay before the next key.
func TestCommandRouterOpensOverlayViaLoop(t *testing.T) {
	// /pick + Enter; then Esc (dismiss overlay); then double-EOF to exit.
	ft := NewFakeTerminal("/pick\r\x1b\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	ol := newListOverlay("test", []listItem{{Label: "x", Submit: "x:1"}})
	router := NewCommandRouter()
	router.Register("pick", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Overlay: ol}, nil
	})
	l.onCommand = func(input string) (CommandOutcome, bool) {
		out, err := router.Dispatch(context.Background(), input, CommandContext{Screen: &l.screen})
		if err != nil {
			return CommandOutcome{}, false
		}
		return out, out.Handled
	}
	l.StartTurn = func(string) { t.Fatal("model must not be called") }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The overlay rendered its header before being dismissed.
	if !strings.Contains(ft.Out.String(), "test") {
		t.Fatalf("overlay header not found in output: %q", ft.Out.String())
	}
}
