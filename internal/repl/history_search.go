package repl

import (
	"fmt"
	"strings"

	"ccgo/internal/fuzzy"
	"ccgo/internal/tui"
)

const historySearchVisibleResults = 8

// historyItem is a single entry in the HistorySearchOverlay's item list.
type historyItem struct {
	// display is the full prompt text (possibly multi-line).
	display string
	// firstLine is just the first line for list display.
	firstLine string
}

// HistorySearchOverlay is a fuzzy-searchable overlay over submitted prompt
// history (OVL-07). The user types a query; matching entries are shown in
// ranked order. Enter re-submits the selected entry into the prompt; Esc
// dismisses.
//
// State transitions:
//
//	query change → refilter (exact substring first, then subsequence)
//	↑ / ↓        → move cursor
//	Enter         → Submit = "historysearch:<first-line-of-selected>"
//	Esc           → Dismissed
type HistorySearchOverlay struct {
	all      []historyItem
	filtered []historyItem
	query    string
	cursor   int
}

// NewHistorySearchOverlay creates an overlay pre-loaded with the given history
// entries (newest-first order recommended). The entries slice is copied.
func NewHistorySearchOverlay(entries []string) *HistorySearchOverlay {
	items := make([]historyItem, 0, len(entries))
	for _, e := range entries {
		if e == "" {
			continue
		}
		nl := strings.IndexByte(e, '\n')
		first := e
		if nl >= 0 {
			first = e[:nl]
		}
		items = append(items, historyItem{display: e, firstLine: first})
	}
	o := &HistorySearchOverlay{all: items}
	o.refilter()
	return o
}

func (o *HistorySearchOverlay) refilter() {
	if o.query == "" {
		o.filtered = append([]historyItem(nil), o.all...)
		o.clampCursor()
		return
	}
	q := strings.ToLower(strings.TrimSpace(o.query))
	// Two passes: exact substring matches first, then fuzzy subsequence.
	var exact, subseq []historyItem
	for _, item := range o.all {
		lower := strings.ToLower(item.display)
		if strings.Contains(lower, q) {
			exact = append(exact, item)
		} else if fuzzy.Filter([]string{item.display}, o.query) != nil {
			// Use fuzzy.Filter to check subsequence match.
			if ms := fuzzy.Filter([]string{item.display}, o.query); len(ms) > 0 {
				subseq = append(subseq, item)
			}
		}
	}
	o.filtered = append(exact, subseq...)
	o.clampCursor()
}

func (o *HistorySearchOverlay) clampCursor() {
	if o.cursor >= len(o.filtered) {
		o.cursor = len(o.filtered) - 1
	}
	if o.cursor < 0 {
		o.cursor = 0
	}
}

// ApplyKey handles keyboard input for the history search overlay.
func (o *HistorySearchOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
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
		selected := o.filtered[o.cursor]
		return OverlayResult{Submit: "historysearch:" + selected.display}, true
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

// Render returns display lines for the overlay.
func (o *HistorySearchOverlay) Render(width, height int) []string {
	header := fmt.Sprintf("Search History (%s):", o.query)
	if o.query == "" {
		header = "Search History — type to filter:"
	}
	lines := []string{header}
	max := height - 3
	if max < 1 {
		max = 1
	}
	if len(o.filtered) == 0 {
		if o.query != "" {
			lines = append(lines, "  (no matching entries)")
		} else {
			lines = append(lines, "  (no history)")
		}
	} else {
		for i, item := range o.filtered {
			if i >= max {
				break
			}
			marker := "  "
			if i == o.cursor {
				marker = "> "
			}
			lines = append(lines, truncateToWidth(marker+item.firstLine, width))
		}
	}
	hint := "Enter=use  Esc=cancel"
	lines = append(lines, truncateToWidth(hint, width))
	return lines
}

// Query returns the current search query (for testing).
func (o *HistorySearchOverlay) Query() string { return o.query }

// SelectedDisplay returns the full display text of the highlighted entry, or "".
func (o *HistorySearchOverlay) SelectedDisplay() string {
	if o.cursor < 0 || o.cursor >= len(o.filtered) {
		return ""
	}
	return o.filtered[o.cursor].display
}

// handleHistorySearchSubmit processes the "historysearch:" prefix emitted by
// HistorySearchOverlay. It inserts the selected prompt into the prompt buffer
// and returns true to prevent forwarding to the model.
func handleHistorySearchSubmit(prompt *string, submit string) bool {
	if rest, ok := strings.CutPrefix(submit, "historysearch:"); ok {
		// Inject the selected prompt text into the prompt; use only the first line
		// so the user can review and edit before re-submitting.
		nl := strings.IndexByte(rest, '\n')
		if nl >= 0 {
			*prompt = rest[:nl]
		} else {
			*prompt = rest
		}
		return true
	}
	return false
}
