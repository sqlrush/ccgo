package repl

import "ccgo/internal/tui"

// OverlayResult is the outcome of feeding a key to an overlay.
type OverlayResult struct {
	// Submit, when non-empty, is text to inject into the prompt/turn pipeline
	// after the overlay closes (e.g. a selected "/command").
	Submit string
	// Dismissed signals the overlay should close with no submission.
	Dismissed bool
}

// Overlay is a modal view rendered above the transcript. It owns its own key
// handling; the loop closes it when ApplyKey reports Submit or Dismissed.
type Overlay interface {
	ApplyKey(key tui.Key) (result OverlayResult, handled bool)
	Render(width, height int) []string
}
