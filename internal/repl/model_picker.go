package repl

// NewModelPicker builds an overlay to choose an AI model. Enter submits
// "model:<name>" which the REPL loop can handle to switch the model.
func NewModelPicker(models []string) *listOverlay {
	items := make([]listItem, 0, len(models))
	for _, m := range models {
		items = append(items, listItem{Label: m, Submit: "model:" + m})
	}
	return newListOverlay("Select model (esc to cancel)", items)
}
