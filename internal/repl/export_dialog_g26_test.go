package repl

// G26: OVL-52 ExportDialog overlay state tests.
// Covers:
//   - Dialog opens on /export (no arg).
//   - Esc dismisses.
//   - Rune input + Backspace update buf.
//   - Enter submits "export:<name>".
//   - handleOverlaySubmit writes the file via writeExport.
//   - Render is non-empty.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/tui"
)

func TestExportDialogOverlayEscDismisses(t *testing.T) {
	o := newExportDialogOverlay("")
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if !res.Dismissed {
		t.Fatalf("Esc should dismiss, got %+v", res)
	}
}

func TestExportDialogOverlayRuneInput(t *testing.T) {
	o := newExportDialogOverlay("")
	o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'm'})
	o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'y'})
	o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: '-'})
	o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'f'})
	if o.Buf() != "my-f" {
		t.Fatalf("buf = %q want my-f", o.Buf())
	}
}

func TestExportDialogOverlayBackspace(t *testing.T) {
	o := newExportDialogOverlay("abc")
	o.ApplyKey(tui.Key{Type: tui.KeyBackspace})
	if o.Buf() != "ab" {
		t.Fatalf("buf after backspace = %q want ab", o.Buf())
	}
}

func TestExportDialogOverlayEnterSubmits(t *testing.T) {
	o := newExportDialogOverlay("")
	o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'r'})
	o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'e'})
	o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'p'})
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "export:rep" {
		t.Fatalf("submit = %q want export:rep", res.Submit)
	}
}

func TestExportDialogOverlayEnterEmptySubmitsEmpty(t *testing.T) {
	o := newExportDialogOverlay("")
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	// Empty input → "export:" (loop will auto-generate filename).
	if res.Submit != "export:" {
		t.Fatalf("submit = %q want export:", res.Submit)
	}
}

func TestExportDialogOverlayRenderNonEmpty(t *testing.T) {
	o := newExportDialogOverlay("")
	lines := o.Render(80, 24)
	if len(lines) == 0 {
		t.Fatal("Render should be non-empty")
	}
}

func TestExportDialogOverlayDefaultNamePreFilled(t *testing.T) {
	o := newExportDialogOverlay("session-2026")
	if o.Buf() != "session-2026" {
		t.Fatalf("buf = %q want session-2026 (pre-filled)", o.Buf())
	}
}

func TestLoopExportDialogSubmitWritesFile(t *testing.T) {
	// Verify that handleOverlaySubmit("export:<name>") writes the transcript.
	cwd := t.TempDir()
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetCWD(cwd)

	// Submit the export dialog result.
	handled := l.handleOverlaySubmit("export:my-session")
	if !handled {
		t.Fatal("handleOverlaySubmit should handle export: prefix")
	}

	// The file should exist.
	dest := filepath.Join(cwd, "my-session.txt")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("export file missing: %v (writeExport should have created it)", err)
	}
}

func TestLoopExportDialogSubmitEmptyAutoGenerates(t *testing.T) {
	// "export:" with empty name should auto-generate a claude-export-* file.
	cwd := t.TempDir()
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetCWD(cwd)

	l.handleOverlaySubmit("export:")

	// A file should have been created (auto-generated name).
	entries, err := os.ReadDir(cwd)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "claude-export-") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("auto-generated export file not found in cwd")
	}
}
