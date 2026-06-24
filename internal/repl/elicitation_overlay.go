package repl

// G24: MCP-34/35 elicitation interactive overlay.
//
// elicitationOverlay implements Overlay for MCP elicitation/create requests.
// The overlay shows the server's message and three action buttons:
//   [Accept] [Decline] [Cancel]
//
// State transitions:
//   - ↑/↓     → navigate action cursor
//   - Enter   → submit "elicitation:<action>"
//   - Esc     → submit "elicitation:cancel"
//
// The replElicitationPrompt bridges between the blocking mcp.ElicitationPrompt
// function type and the overlay. In production the REPL loop is responsible for
// showing the overlay and resolving the result; the seam here allows unit tests
// to drive the prompt synchronously via a callback.
//
// CC ref: src/components/mcp/ElicitationDialog.tsx

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/mcp"
	"ccgo/internal/tui"
)

// elicitationActions is the ordered list of action labels in the overlay.
var elicitationActions = []string{"accept", "decline", "cancel"}

// elicitationOverlay is the interactive MCP elicitation dialog overlay.
type elicitationOverlay struct {
	req    mcp.ElicitationRequest
	cursor int // 0=accept, 1=decline, 2=cancel
}

// newElicitationOverlay creates an overlay for the given elicitation request.
func newElicitationOverlay(req mcp.ElicitationRequest) *elicitationOverlay {
	return &elicitationOverlay{req: req}
}

// Cursor returns the current cursor position.
func (o *elicitationOverlay) Cursor() int { return o.cursor }

// ApplyKey processes a key event. Implements Overlay.
func (o *elicitationOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Submit: "elicitation:cancel"}, true

	case tui.KeyDown:
		if o.cursor < len(elicitationActions)-1 {
			o.cursor++
		}
		return OverlayResult{}, true

	case tui.KeyUp:
		if o.cursor > 0 {
			o.cursor--
		}
		return OverlayResult{}, true

	case tui.KeyEnter:
		action := elicitationActions[o.cursor]
		return OverlayResult{Submit: "elicitation:" + action}, true

	default:
		return OverlayResult{}, false
	}
}

// Render returns display lines for the elicitation overlay. Implements Overlay.
func (o *elicitationOverlay) Render(width, height int) []string {
	lines := []string{"MCP Server Request:"}
	// Wrap message text.
	for _, part := range splitLines(o.req.Message, width) {
		lines = append(lines, "  "+part)
	}
	lines = append(lines, "")
	// Show schema fields if present.
	if len(o.req.RequestedSchema) > 0 {
		lines = append(lines, "Fields:")
		for k := range o.req.RequestedSchema {
			lines = append(lines, fmt.Sprintf("  %s", k))
		}
		lines = append(lines, "")
	}
	// Action buttons.
	for i, action := range elicitationActions {
		marker := "  "
		if i == o.cursor {
			marker = "> "
		}
		lines = append(lines, marker+strings.Title(action)) //nolint:staticcheck
	}
	lines = append(lines, "(↑↓ navigate, Enter to choose, Esc=cancel)")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

// splitLines splits s into chunks of at most width runes (approximately).
func splitLines(s string, width int) []string {
	if width <= 0 || width > 200 {
		return []string{s}
	}
	runes := []rune(s)
	var out []string
	for len(runes) > 0 {
		end := width
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[:end]))
		runes = runes[end:]
	}
	return out
}

// elicitationUICallback is the seam that drives the overlay during unit tests.
// In production the loop handles this by opening the overlay and reading the result.
type elicitationUICallback func(ov *elicitationOverlay) string // returns action string

// newReplElicitationPrompt returns an mcp.ElicitationPrompt that delegates to
// the provided UICallback seam. In production, replace the callback with a
// channel-based implementation wired to the REPL loop's overlay mechanism.
func newReplElicitationPrompt(ui elicitationUICallback) mcp.ElicitationPrompt {
	return func(ctx context.Context, req mcp.ElicitationRequest) (string, map[string]any, error) {
		if ui == nil {
			return "cancel", nil, nil
		}
		ov := newElicitationOverlay(req)
		action := ui(ov)
		if action == "" {
			action = "cancel"
		}
		return action, nil, nil
	}
}

// loopElicitationPrompt creates an mcp.ElicitationPrompt that shows the
// elicitation overlay in the REPL loop and waits for the user to choose.
// The reply channel is used to receive the action from handleOverlaySubmit.
// This is the production seam; replyCh receives the "elicitation:<action>"
// submit token from the overlay.
func loopElicitationPrompt(showOverlay func(*elicitationOverlay) <-chan string) mcp.ElicitationPrompt {
	return func(ctx context.Context, req mcp.ElicitationRequest) (string, map[string]any, error) {
		if showOverlay == nil {
			return "cancel", nil, nil
		}
		ov := newElicitationOverlay(req)
		replyCh := showOverlay(ov)
		select {
		case <-ctx.Done():
			return "cancel", nil, ctx.Err()
		case raw := <-replyCh:
			// raw = "elicitation:<action>"
			action := strings.TrimPrefix(raw, "elicitation:")
			if action == "" {
				action = "cancel"
			}
			return action, nil, nil
		}
	}
}
