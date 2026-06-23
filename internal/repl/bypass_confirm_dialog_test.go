package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

// TestBypassConfirmDialogDefaultDeclines: the default cursor (0) is "No, go
// back", so Enter without navigating must return "bypass:decline".
func TestBypassConfirmDialogDefaultDeclines(t *testing.T) {
	d := NewBypassConfirmDialog()
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "bypass:decline" {
		t.Fatalf("default Enter = %q want bypass:decline", res.Submit)
	}
}

// TestBypassConfirmDialogAcceptAfterNavigate: moving cursor to 1 then Enter
// must return "bypass:accept".
func TestBypassConfirmDialogAcceptAfterNavigate(t *testing.T) {
	d := NewBypassConfirmDialog()
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor → 1 (Yes, I accept)
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "bypass:accept" {
		t.Fatalf("navigate+Enter = %q want bypass:accept", res.Submit)
	}
}

// TestBypassConfirmDialogEscDeclines: Esc must always return "bypass:decline".
func TestBypassConfirmDialogEscDeclines(t *testing.T) {
	d := NewBypassConfirmDialog()
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if res.Submit != "bypass:decline" {
		t.Fatalf("Esc = %q want bypass:decline", res.Submit)
	}
}

// TestBypassConfirmDialogTabTogglesCursor: Tab toggles between 0 and 1.
func TestBypassConfirmDialogTabTogglesCursor(t *testing.T) {
	d := NewBypassConfirmDialog()
	if d.cursor != 0 {
		t.Fatalf("initial cursor = %d want 0", d.cursor)
	}
	d.ApplyKey(tui.Key{Type: tui.KeyTab})
	if d.cursor != 1 {
		t.Fatalf("after Tab cursor = %d want 1", d.cursor)
	}
	d.ApplyKey(tui.Key{Type: tui.KeyTab})
	if d.cursor != 0 {
		t.Fatalf("after double Tab cursor = %d want 0", d.cursor)
	}
}

// TestBypassConfirmDialogRenderContainsWarning: Render must contain warning text.
func TestBypassConfirmDialogRenderContainsWarning(t *testing.T) {
	d := NewBypassConfirmDialog()
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(strings.ToLower(out), "bypass") {
		t.Fatalf("Render missing 'bypass': %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "warning") && !strings.Contains(strings.ToLower(out), "WARNING") && !strings.Contains(out, "WARNING") {
		t.Fatalf("Render missing warning text: %q", out)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// AutoModeOptInDialog tests
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAutoModeOptInDialogDefaultAccepts(t *testing.T) {
	d := NewAutoModeOptInDialog()
	// cursor 0 = "Yes, enable auto mode"
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "auto:accept" {
		t.Fatalf("default Enter = %q want auto:accept", res.Submit)
	}
}

func TestAutoModeOptInDialogDeclineAfterNavigate(t *testing.T) {
	d := NewAutoModeOptInDialog()
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor → 1 (No, go back)
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "auto:decline" {
		t.Fatalf("navigate+Enter = %q want auto:decline", res.Submit)
	}
}

func TestAutoModeOptInDialogEscDeclines(t *testing.T) {
	d := NewAutoModeOptInDialog()
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if res.Submit != "auto:decline" {
		t.Fatalf("Esc = %q want auto:decline", res.Submit)
	}
}

func TestAutoModeOptInDialogRenderContainsInfo(t *testing.T) {
	d := NewAutoModeOptInDialog()
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(strings.ToLower(out), "auto") {
		t.Fatalf("Render missing 'auto': %q", out)
	}
}
