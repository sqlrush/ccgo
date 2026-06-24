package repl

// REPL-30: ESC double-press → MessageSelector overlay.
// CC ref: src/components/PromptInput/PromptInput.tsx:1254 — double ESC on
// empty input opens MessageSelector.

import (
	"fmt"
	"strings"

	"ccgo/internal/tui"
)

// MessageSelectorOverlay lets the user scroll conversation history and select
// a message to rewind to. Opened by double-pressing ESC on an empty prompt.
//
// Navigation: Up/Down arrows move the cursor.
// Confirm: Enter submits "rewind:<selectedText>".
// Dismiss: Esc closes the overlay without action.
type MessageSelectorOverlay struct {
	entries []string // display strings for each selectable message
	cursor  int      // index of the highlighted entry
}

// NewMessageSelectorOverlay constructs a MessageSelectorOverlay from a slice of
// text entries (typically the history messages rendered as strings).
func NewMessageSelectorOverlay(entries []string) *MessageSelectorOverlay {
	copied := make([]string, len(entries))
	copy(copied, entries)
	return &MessageSelectorOverlay{entries: copied}
}

// ApplyKey implements Overlay.
func (m *MessageSelectorOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true
	case tui.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		if len(m.entries) == 0 {
			return OverlayResult{Dismissed: true}, true
		}
		selected := m.entries[m.cursor]
		return OverlayResult{Submit: fmt.Sprintf("rewind:%s", selected)}, true
	default:
		return OverlayResult{}, false
	}
}

// Render implements Overlay. It returns lines formatted to fit within width.
func (m *MessageSelectorOverlay) Render(width, _ int) []string {
	lines := []string{
		"Select a message to rewind to:",
		"",
	}
	if len(m.entries) == 0 {
		lines = append(lines, "  (no messages)")
		return lines
	}
	for i, entry := range m.entries {
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}
		// Truncate to fit width accounting for prefix.
		display := truncateToWidth(entry, width-len(prefix))
		// Replace newlines so entries stay on one line each.
		display = strings.ReplaceAll(display, "\n", " ")
		lines = append(lines, prefix+display)
	}
	return lines
}
