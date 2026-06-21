package repl

import (
	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// listItem is one selectable row in a listOverlay.
type listItem struct {
	Label  string
	Submit string
}

// listOverlay is a reusable cursor-driven list overlay (help/theme/memory).
// It satisfies the Overlay interface.
type listOverlay struct {
	title  string
	items  []listItem
	cursor int
}

// newListOverlay constructs a listOverlay from a title and item slice.
// The items slice is defensively copied so the caller's backing array is
// never shared or mutated.
func newListOverlay(title string, items []listItem) *listOverlay {
	copied := make([]listItem, len(items))
	copy(copied, items)
	return &listOverlay{title: title, items: copied}
}

// ApplyKey handles key events for the overlay. It satisfies the Overlay
// interface. Returns (result, true) when the key is consumed.
func (o *listOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true
	case tui.KeyUp:
		if o.cursor > 0 {
			o.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if o.cursor < len(o.items)-1 {
			o.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		if len(o.items) == 0 {
			return OverlayResult{Dismissed: true}, true
		}
		return OverlayResult{Submit: o.items[o.cursor].Submit}, true
	default:
		return OverlayResult{}, false
	}
}

// Render returns display lines for the overlay, capped to height rows.
func (o *listOverlay) Render(width, height int) []string {
	lines := []string{o.title}
	max := height - 2
	if max < 1 {
		max = 1
	}
	for i, it := range o.items {
		if i >= max {
			break
		}
		marker := "  "
		if i == o.cursor {
			marker = "> "
		}
		lines = append(lines, marker+it.Label)
	}
	return lines
}

// NewHelpScreen builds the HelpV2 overlay listing the visible commands.
// Hidden commands are excluded, consistent with the slash menu.
// The overlay is informational: selecting a row submits the command text so
// the loop can insert it into the prompt.
// Note: the actual command list is injected by the caller (Task-13 wiring seam).
func NewHelpScreen(commands []contracts.Command) *listOverlay {
	items := make([]listItem, 0, len(commands))
	for _, c := range commands {
		if c.Hidden {
			continue
		}
		label := "/" + c.Name
		if c.Description != "" {
			label += " — " + c.Description
		}
		items = append(items, listItem{Label: label, Submit: "/" + c.Name})
	}
	return newListOverlay("Help — commands (esc to close)", items)
}
