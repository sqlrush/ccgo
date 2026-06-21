package repl

// NewMemorySelector builds an overlay to pick a memory file to edit. Enter
// submits "memory:<path>" which the REPL loop opens in the user's editor.
// Note: discovering user/project/nested memory file paths is a Task-13 wiring
// seam — callers inject the prepared slice; this constructor only shapes the
// overlay.
func NewMemorySelector(files []string) *listOverlay {
	items := make([]listItem, 0, len(files))
	for _, path := range files {
		items = append(items, listItem{Label: path, Submit: "memory:" + path})
	}
	return newListOverlay("Edit memory file (esc to cancel)", items)
}
