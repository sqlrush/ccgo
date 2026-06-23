package repl

import (
	"fmt"
	"strings"

	"ccgo/internal/fuzzy"
	"ccgo/internal/tui"
)

const quickOpenMaxFiles = 10_000
const quickOpenVisibleResults = 8

// QuickOpenOverlay is a fuzzy file picker overlay (OVL-05 / @-mention trigger).
// The user types a query; the list is filtered in real-time using the fuzzy
// package. Pressing Enter inserts the selected path into the prompt with an
// "@" prefix; Esc dismisses.
//
// State transitions:
//
//	query change  → refilter, reset cursor
//	↑ / ↓        → move cursor
//	Enter         → Submit = "quickopen:<path>"  (loop injects "@<path> " into prompt)
//	Esc           → Dismissed
//	Tab           → Submit = "quickopen-insert:<path>"  (plain path, no @)
type QuickOpenOverlay struct {
	// all holds the full file list for the working directory.
	all []string
	// filtered holds the current fuzzy-ranked results.
	filtered []string
	query    string
	cursor   int
}

// NewQuickOpenOverlay constructs the overlay by walking root for project files.
// root is typically the REPL's working directory.
func NewQuickOpenOverlay(root string) *QuickOpenOverlay {
	all := fuzzy.WalkFiles(root, fuzzy.WalkOptions{MaxFiles: quickOpenMaxFiles})
	o := &QuickOpenOverlay{all: all}
	o.refilter()
	return o
}

// newQuickOpenOverlayWithFiles is a test constructor that accepts an explicit
// file list instead of walking the filesystem.
func newQuickOpenOverlayWithFiles(files []string) *QuickOpenOverlay {
	copied := append([]string(nil), files...)
	o := &QuickOpenOverlay{all: copied}
	o.refilter()
	return o
}

func (o *QuickOpenOverlay) refilter() {
	if o.query == "" {
		o.filtered = append([]string(nil), o.all...)
	} else {
		o.filtered = fuzzy.Values(o.all, o.query)
	}
	if o.cursor >= len(o.filtered) {
		o.cursor = len(o.filtered) - 1
	}
	if o.cursor < 0 {
		o.cursor = 0
	}
}

// ApplyKey handles keyboard input for the overlay.
func (o *QuickOpenOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true
	case tui.KeyUp:
		if o.cursor > 0 {
			o.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if o.cursor < len(o.filtered)-1 {
			o.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		if len(o.filtered) == 0 {
			return OverlayResult{Dismissed: true}, true
		}
		return OverlayResult{Submit: "quickopen:" + o.filtered[o.cursor]}, true
	case tui.KeyTab:
		if len(o.filtered) == 0 {
			return OverlayResult{Dismissed: true}, true
		}
		return OverlayResult{Submit: "quickopen-insert:" + o.filtered[o.cursor]}, true
	case tui.KeyBackspace:
		if len(o.query) > 0 {
			runes := []rune(o.query)
			o.query = string(runes[:len(runes)-1])
			o.refilter()
		}
		return OverlayResult{}, true
	case tui.KeyRune:
		o.query += string(key.Rune)
		o.refilter()
		return OverlayResult{}, true
	default:
		return OverlayResult{}, false
	}
}

// Render returns display lines for the overlay capped to width/height.
func (o *QuickOpenOverlay) Render(width, height int) []string {
	header := fmt.Sprintf("Quick Open (%s):", o.query)
	if o.query == "" {
		header = "Quick Open — type to search files:"
	}
	lines := []string{header}
	max := height - 3 // reserve header + footer hint
	if max < 1 {
		max = 1
	}
	if len(o.filtered) == 0 {
		if o.query != "" {
			lines = append(lines, "  (no matching files)")
		} else {
			lines = append(lines, "  (no files found)")
		}
	} else {
		for i, f := range o.filtered {
			if i >= max {
				break
			}
			marker := "  "
			if i == o.cursor {
				marker = "> "
			}
			lines = append(lines, truncateToWidth(marker+f, width))
		}
	}
	hint := "Enter=@mention  Tab=insert path  Esc=cancel"
	lines = append(lines, truncateToWidth(hint, width))
	return lines
}

// SelectedPath returns the currently highlighted path or "".
func (o *QuickOpenOverlay) SelectedPath() string {
	if o.cursor < 0 || o.cursor >= len(o.filtered) {
		return ""
	}
	return o.filtered[o.cursor]
}

// Query returns the current search query (for testing).
func (o *QuickOpenOverlay) Query() string { return o.query }

// handleQuickOpenSubmit processes the "quickopen:" and "quickopen-insert:"
// submit strings emitted by QuickOpenOverlay. It injects text into the prompt
// and returns true to prevent forwarding to the model.
//
// Routing:
//
//	"quickopen:<path>"        → inserts "@<path> " into prompt (mention)
//	"quickopen-insert:<path>" → inserts "<path> " into prompt (plain path)
func handleQuickOpenSubmit(prompt *string, submit string) bool {
	if rest, ok := strings.CutPrefix(submit, "quickopen:"); ok {
		*prompt = "@" + rest + " "
		return true
	}
	if rest, ok := strings.CutPrefix(submit, "quickopen-insert:"); ok {
		*prompt = rest + " "
		return true
	}
	return false
}
