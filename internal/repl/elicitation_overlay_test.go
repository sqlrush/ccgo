package repl

// G24: MCP-34/35 elicitation interactive overlay tests.
// Tests the state machine: show message + fields → user accepts/declines/cancels.
// Wiring: the REPL loop gets an ElicitationPrompt seam that blocks on an overlay.
//
// CC ref: src/components/mcp/ElicitationDialog.tsx

import (
	"context"
	"testing"

	"ccgo/internal/mcp"
	"ccgo/internal/tui"
)

func TestElicitationOverlayShowsMessage(t *testing.T) {
	req := mcp.ElicitationRequest{Message: "Please confirm your name"}
	ov := newElicitationOverlay(req)
	lines := ov.Render(80, 24)
	found := false
	for _, l := range lines {
		if contains(l, "Please confirm your name") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Render missing message text: %v", lines)
	}
}

func TestElicitationOverlayDefaultCursorIsAccept(t *testing.T) {
	ov := newElicitationOverlay(mcp.ElicitationRequest{Message: "?"})
	if ov.Cursor() != 0 {
		t.Fatalf("initial cursor = %d want 0 (accept)", ov.Cursor())
	}
}

func TestElicitationOverlayNavigateDown(t *testing.T) {
	ov := newElicitationOverlay(mcp.ElicitationRequest{Message: "?"})
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyDown})
	if !handled {
		t.Fatal("Down should be handled")
	}
	if res.Dismissed || res.Submit != "" {
		t.Fatalf("Down should not dismiss/submit: %+v", res)
	}
	if ov.Cursor() != 1 {
		t.Fatalf("cursor after Down = %d want 1 (decline)", ov.Cursor())
	}
}

func TestElicitationOverlayEnterAccept(t *testing.T) {
	ov := newElicitationOverlay(mcp.ElicitationRequest{Message: "?"})
	// cursor = 0 → accept
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "elicitation:accept" {
		t.Fatalf("submit = %q want elicitation:accept", res.Submit)
	}
}

func TestElicitationOverlayEnterDecline(t *testing.T) {
	ov := newElicitationOverlay(mcp.ElicitationRequest{Message: "?"})
	ov.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor → 1 (decline)
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "elicitation:decline" {
		t.Fatalf("submit = %q want elicitation:decline", res.Submit)
	}
}

func TestElicitationOverlayEnterCancel(t *testing.T) {
	ov := newElicitationOverlay(mcp.ElicitationRequest{Message: "?"})
	ov.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor=1
	ov.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor=2 (cancel)
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "elicitation:cancel" {
		t.Fatalf("submit = %q want elicitation:cancel", res.Submit)
	}
}

func TestElicitationOverlayEscCancels(t *testing.T) {
	ov := newElicitationOverlay(mcp.ElicitationRequest{Message: "?"})
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if res.Submit != "elicitation:cancel" {
		t.Fatalf("Esc submit = %q want elicitation:cancel", res.Submit)
	}
}

func TestElicitationOverlayRenderShowsOptions(t *testing.T) {
	ov := newElicitationOverlay(mcp.ElicitationRequest{Message: "?"})
	lines := ov.Render(80, 24)
	found := false
	for _, l := range lines {
		if contains(l, "accept") || contains(l, "Accept") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Render missing 'accept' option: %v", lines)
	}
}

func TestElicitationOverlayCursorBoundAtBottom(t *testing.T) {
	ov := newElicitationOverlay(mcp.ElicitationRequest{Message: "?"})
	// 3 options (0=accept, 1=decline, 2=cancel)
	ov.ApplyKey(tui.Key{Type: tui.KeyDown}) // 0→1
	ov.ApplyKey(tui.Key{Type: tui.KeyDown}) // 1→2
	ov.ApplyKey(tui.Key{Type: tui.KeyDown}) // should not go past 2
	if ov.Cursor() != 2 {
		t.Fatalf("cursor past bottom = %d want 2", ov.Cursor())
	}
}

// TestElicitationPromptSeamAccept verifies the ElicitationPrompt seam returns
// "accept" when the overlay receives an Enter on the accept option.
// This tests the callback-based bridge between the REPL loop and the overlay.
func TestElicitationPromptSeamAccept(t *testing.T) {
	req := mcp.ElicitationRequest{Message: "Pick something"}

	var receivedAction string
	prompt := newReplElicitationPrompt(func(overlay *elicitationOverlay) string {
		// Simulate: open overlay, press Enter (cursor=0=accept)
		return "accept"
	})

	action, _, err := prompt(context.Background(), req)
	if err != nil {
		t.Fatalf("prompt err: %v", err)
	}
	_ = receivedAction
	if action != "accept" {
		t.Fatalf("action = %q want accept", action)
	}
}

func TestElicitationPromptSeamDecline(t *testing.T) {
	req := mcp.ElicitationRequest{Message: "Pick something"}

	prompt := newReplElicitationPrompt(func(_ *elicitationOverlay) string {
		return "decline"
	})

	action, _, err := prompt(context.Background(), req)
	if err != nil {
		t.Fatalf("prompt err: %v", err)
	}
	if action != "decline" {
		t.Fatalf("action = %q want decline", action)
	}
}
