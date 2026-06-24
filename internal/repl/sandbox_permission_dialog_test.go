package repl

// OVL-45: SandboxPermissionRequest overlay tests.

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

func TestSandboxPermissionDialogRenderContainsViolationAndOptions(t *testing.T) {
	violation := "process tried to write outside sandbox root"
	d := NewSandboxPermissionDialog(violation)
	lines := d.Render(80, 24)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, violation) {
		t.Fatalf("Render missing violation text %q:\n%s", violation, joined)
	}
	if !strings.Contains(joined, "Allow") {
		t.Fatalf("Render missing 'Allow':\n%s", joined)
	}
	if !strings.Contains(joined, "Deny") {
		t.Fatalf("Render missing 'Deny':\n%s", joined)
	}
	if !strings.Contains(joined, "Sandbox Violation") {
		t.Fatalf("Render missing title 'Sandbox Violation':\n%s", joined)
	}
}

func TestSandboxPermissionDialogDefaultCursorIsAllow(t *testing.T) {
	d := NewSandboxPermissionDialog("violation")
	if d.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0 (Allow)", d.cursor)
	}
}

func TestSandboxPermissionDialogDefaultEnterAllows(t *testing.T) {
	d := NewSandboxPermissionDialog("violation")
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "sandbox:allow" {
		t.Fatalf("default Enter = %q, want sandbox:allow", res.Submit)
	}
}

func TestSandboxPermissionDialogNavigateToDenyAndConfirm(t *testing.T) {
	d := NewSandboxPermissionDialog("violation")
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor → 1 (Deny)
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "sandbox:deny" {
		t.Fatalf("navigate+Enter = %q, want sandbox:deny", res.Submit)
	}
}

func TestSandboxPermissionDialogEscDenies(t *testing.T) {
	d := NewSandboxPermissionDialog("violation")
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if res.Submit != "sandbox:deny" {
		t.Fatalf("Esc = %q, want sandbox:deny", res.Submit)
	}
}

func TestSandboxPermissionDialogTabTogglesCursor(t *testing.T) {
	d := NewSandboxPermissionDialog("violation")
	d.ApplyKey(tui.Key{Type: tui.KeyTab})
	if d.cursor != 1 {
		t.Fatalf("after Tab cursor = %d, want 1 (Deny)", d.cursor)
	}
	d.ApplyKey(tui.Key{Type: tui.KeyTab})
	if d.cursor != 0 {
		t.Fatalf("after double Tab cursor = %d, want 0 (Allow)", d.cursor)
	}
}

func TestSandboxPermissionDialogCursorClampsUp(t *testing.T) {
	d := NewSandboxPermissionDialog("v")
	d.ApplyKey(tui.Key{Type: tui.KeyUp}) // at 0, should not go negative
	if d.cursor != 0 {
		t.Fatalf("cursor clamped below 0: %d", d.cursor)
	}
}

func TestSandboxPermissionDialogCursorClampsDown(t *testing.T) {
	d := NewSandboxPermissionDialog("v")
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // to 1
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // should clamp at 1
	if d.cursor != 1 {
		t.Fatalf("cursor clamped above max: %d", d.cursor)
	}
}
