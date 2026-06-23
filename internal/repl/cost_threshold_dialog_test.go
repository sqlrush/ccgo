package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// CostThresholdDialog
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestCostThresholdDialogEnterAcks(t *testing.T) {
	d := NewCostThresholdDialog(5.00)
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "cost:ack" {
		t.Fatalf("Enter = %q want cost:ack", res.Submit)
	}
}

func TestCostThresholdDialogEscAcks(t *testing.T) {
	d := NewCostThresholdDialog(7.50)
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if res.Submit != "cost:ack" {
		t.Fatalf("Esc = %q want cost:ack", res.Submit)
	}
}

func TestCostThresholdDialogRenderShowsAmount(t *testing.T) {
	d := NewCostThresholdDialog(5.12)
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "5.12") {
		t.Fatalf("Render missing amount: %q", out)
	}
}

func TestCostThresholdUnknownKeyNotHandled(t *testing.T) {
	d := NewCostThresholdDialog(5.0)
	_, handled := d.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'x'})
	if handled {
		t.Fatal("unknown key should not be handled")
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// TokenWarningOverlay
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestTokenWarningOverlayEnterAcks(t *testing.T) {
	o := NewTokenWarningOverlay(180000, 200000)
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "tokenwarn:ack" {
		t.Fatalf("Enter = %q want tokenwarn:ack", res.Submit)
	}
}

func TestTokenWarningOverlayRenderShowsPct(t *testing.T) {
	o := NewTokenWarningOverlay(160000, 200000)
	out := strings.Join(o.Render(80, 24), "\n")
	if !strings.Contains(out, "80%") {
		t.Fatalf("Render should show 80%%: %q", out)
	}
}

func TestTokenWarningOverlayZeroMax(t *testing.T) {
	// Should not panic with zero maxTokens.
	o := NewTokenWarningOverlay(0, 0)
	lines := o.Render(80, 24)
	if len(lines) == 0 {
		t.Fatal("Render should return lines even with zero maxTokens")
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// AutoCompactWarningOverlay
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAutoCompactWarningOverlayEnterAcks(t *testing.T) {
	o := NewAutoCompactWarningOverlay(195000, 200000)
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "compact:ack" {
		t.Fatalf("Enter = %q want compact:ack", res.Submit)
	}
}

func TestAutoCompactWarningOverlayRenderShowsWarning(t *testing.T) {
	o := NewAutoCompactWarningOverlay(195000, 200000)
	out := strings.Join(o.Render(80, 24), "\n")
	if !strings.Contains(strings.ToLower(out), "compact") {
		t.Fatalf("Render should mention compact: %q", out)
	}
}
