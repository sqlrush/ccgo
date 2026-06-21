package repl

// NewThemePicker builds an overlay to choose a theme. Enter submits
// "theme:<name>" which the REPL loop applies and persists.
// Note: the actual theme list (light/dark/custom) is a Task-13 wiring seam —
// callers inject the prepared slice; this constructor only shapes the overlay.
func NewThemePicker(themes []string) *listOverlay {
	items := make([]listItem, 0, len(themes))
	for _, name := range themes {
		items = append(items, listItem{Label: name, Submit: "theme:" + name})
	}
	return newListOverlay("Select theme (esc to cancel)", items)
}
