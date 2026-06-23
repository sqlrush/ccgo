package repl

import (
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/tool"
	"ccgo/internal/tui"
)

// questionOverlay is a modal Overlay that presents a single multiple-choice
// question to the user. It supports both single-select (Enter to confirm cursor
// position) and multi-select (Space to toggle, Enter to confirm all checked).
// Submitting encodes the answer as a JSON string so the loop's handleOverlaySubmit
// can route it to the question channel via the "question:" prefix.
type questionOverlay struct {
	q       tool.Question
	cursor  int
	checked []bool // only meaningful when q.MultiSelect is true
}

// newQuestionOverlay constructs a questionOverlay for a single question.
func newQuestionOverlay(q tool.Question) *questionOverlay {
	return &questionOverlay{
		q:       q,
		cursor:  0,
		checked: make([]bool, len(q.Options)),
	}
}

// ApplyKey handles key events. It satisfies the Overlay interface.
// Returns (result, true) when the key is consumed.
func (o *questionOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true
	case tui.KeyUp:
		if o.cursor > 0 {
			o.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if o.cursor < len(o.q.Options)-1 {
			o.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyRune:
		if key.Rune == ' ' && o.q.MultiSelect {
			o.checked[o.cursor] = !o.checked[o.cursor]
			return OverlayResult{}, true
		}
	case tui.KeyEnter:
		// Encode the answer as a JSON array of selected labels, prefixed with
		// "question:" so handleOverlaySubmit can recognise and route it.
		selected := o.selectedAnswers()
		encoded, _ := json.Marshal(selected)
		return OverlayResult{Submit: "question:" + string(encoded)}, true
	}
	return OverlayResult{}, false
}

// selectedAnswers returns the chosen option labels for the current state.
// For single-select this is always the cursor item; for multi-select it is
// every item with checked=true (preserving original order).
func (o *questionOverlay) selectedAnswers() []string {
	if !o.q.MultiSelect {
		if len(o.q.Options) == 0 {
			return nil
		}
		return []string{o.q.Options[o.cursor].Label}
	}
	var out []string
	for i, opt := range o.q.Options {
		if o.checked[i] {
			out = append(out, opt.Label)
		}
	}
	return out
}

// Render returns display lines for the overlay, capped to height rows.
// Line 0: question text with header; subsequent lines: options with marker.
func (o *questionOverlay) Render(width, height int) []string {
	header := fmt.Sprintf("[%s] %s", o.q.Header, o.q.Question)
	if o.q.MultiSelect {
		header += " (Space=toggle, Enter=confirm)"
	} else {
		header += " (↑↓=navigate, Enter=select)"
	}
	lines := []string{header}

	maxItems := height - 3
	if maxItems < 1 {
		maxItems = 1
	}
	for i, opt := range o.q.Options {
		if i >= maxItems {
			break
		}
		prefix := "  "
		if i == o.cursor {
			prefix = "> "
		}
		label := opt.Label
		if o.q.MultiSelect {
			checked := "[ ]"
			if o.checked[i] {
				checked = "[x]"
			}
			label = checked + " " + label
		}
		if opt.Description != "" {
			// Truncate description to available width.
			desc := opt.Description
			max := width - len(prefix) - len(label) - 3
			if max > 0 && len(desc) > max {
				desc = desc[:max] + "…"
			}
			label = label + " — " + desc
		}
		lines = append(lines, prefix+label)
		// Respect width.
		last := lines[len(lines)-1]
		if width > 0 && len(last) > width {
			lines[len(lines)-1] = last[:width]
		}
	}
	return lines
}

// questionAnswerSubmitPrefix is the prefix that questionOverlay.ApplyKey puts
// on its Submit string so the loop can recognise and route it.
const questionAnswerSubmitPrefix = "question:"

// decodeQuestionAnswer decodes the JSON-encoded selected labels from a
// "question:..." submit string. Returns nil if the prefix is absent.
func decodeQuestionAnswer(submit string) ([]string, bool) {
	after, ok := strings.CutPrefix(submit, questionAnswerSubmitPrefix)
	if !ok {
		return nil, false
	}
	var selected []string
	if err := json.Unmarshal([]byte(after), &selected); err != nil {
		return nil, false
	}
	return selected, true
}
