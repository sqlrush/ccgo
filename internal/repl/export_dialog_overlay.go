package repl

// G26: OVL-52 ExportDialog overlay state layer.
//
// exportDialogOverlay prompts the user for a filename before writing the
// export. When the user presses Enter the overlay submits "export:<filename>",
// which handleOverlaySubmit routes back to exportHandler via a direct write.
// Esc dismisses without export.
//
// CC ref: src/components/ExportDialog.tsx — single input field for filename.

import (
	"fmt"
	"strings"

	"ccgo/internal/tui"
)

// exportDialogOverlay is the state layer for the /export filename-prompt dialog.
// The loop opens it when /export is invoked without an argument.
type exportDialogOverlay struct {
	// buf holds the user's in-progress filename input.
	buf string
	// defaultName is pre-filled when the dialog opens (empty string = no pre-fill).
	defaultName string
}

// newExportDialogOverlay creates an ExportDialog overlay.
// defaultName, when non-empty, is pre-filled in the input buffer.
func newExportDialogOverlay(defaultName string) *exportDialogOverlay {
	return &exportDialogOverlay{
		buf:         defaultName,
		defaultName: defaultName,
	}
}

// ApplyKey handles key input for the export filename prompt. Implements Overlay.
//
//   - Esc       → dismissed (no export)
//   - Enter     → submit "export:<filename>" (empty input uses defaultName or ""
//     which the caller treats as auto-generate)
//   - Backspace → delete last rune from buf
//   - Rune      → append to buf
func (o *exportDialogOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true

	case tui.KeyEnter:
		name := strings.TrimSpace(o.buf)
		return OverlayResult{Submit: fmt.Sprintf("export:%s", name)}, true

	case tui.KeyBackspace:
		if len(o.buf) > 0 {
			runes := []rune(o.buf)
			o.buf = string(runes[:len(runes)-1])
		}
		return OverlayResult{}, true

	case tui.KeyRune:
		o.buf += string(key.Rune)
		return OverlayResult{}, true

	default:
		return OverlayResult{}, false
	}
}

// Render returns the display lines for the export dialog. Implements Overlay.
func (o *exportDialogOverlay) Render(width, _ int) []string {
	prompt := "Export filename (without .txt): " + o.buf + "|"
	header := "Export (Enter to save, Esc to cancel)"
	if width > 0 && len(prompt) > width {
		prompt = prompt[:width]
	}
	return []string{header, prompt}
}

// Buf returns the current input buffer (used in tests to inspect state).
func (o *exportDialogOverlay) Buf() string { return o.buf }
