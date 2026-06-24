package repl

import (
	"fmt"

	"ccgo/internal/tui"
)

// surveyDialogOverlay wraps a tui.SurveyOverlayState and satisfies the Overlay
// interface. It renders as a simple numbered-option dialog and dismisses on
// Enter (records selection) or Escape (dismisses without selection).
//
// CFG-49: feedback survey overlay shown at the configured feedbackSurveyRate.
// CC ref: src/utils/settings/types.ts feedbackSurveyRate;
//         src/hooks/useSkillImprovementSurvey.ts.
type surveyDialogOverlay struct {
	// state points back into loop.surveyState so mutations propagate.
	state *tui.SurveyOverlayState
}

// ApplyKey handles key input for the survey overlay. Immutable transitions are
// written back to *state so the loop sees updated values.
func (o *surveyDialogOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	s := *o.state
	switch key.Type {
	case tui.KeyEsc:
		*o.state = s.Dismiss()
		return OverlayResult{Dismissed: true}, true
	case tui.KeyUp:
		if s.Selected > 0 {
			*o.state = s.Select(s.Selected - 1)
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		max := len(s.Options) - 1
		if max < 0 {
			max = 0
		}
		if s.Selected < max {
			*o.state = s.Select(s.Selected + 1)
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		sel := s.Selected
		if sel < 0 {
			sel = 0
		}
		*o.state = s.Dismiss()
		return OverlayResult{Submit: fmt.Sprintf("survey:dismiss:%d", sel)}, true
	default:
		return OverlayResult{}, false
	}
}

// Render produces a simple text representation of the survey dialog.
func (o *surveyDialogOverlay) Render(width, _ int) []string {
	s := *o.state
	d, ok := s.ToDialog()
	if !ok {
		return nil
	}
	titlePad := width - len(d.Title) - 4
	if titlePad < 0 {
		titlePad = 0
	}
	borderPad := width - 2
	if borderPad < 0 {
		borderPad = 0
	}
	lines := []string{
		"┌─ " + d.Title + " " + repeatRune('─', titlePad) + "┐",
		"│ " + d.Body,
		"│",
	}
	for i, action := range d.Actions {
		cursor := "  "
		if i == d.Focused {
			cursor = "▶ "
		}
		lines = append(lines, "│ "+cursor+action)
	}
	lines = append(lines, "└"+repeatRune('─', borderPad)+"┘")
	return lines
}

func repeatRune(r rune, n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}

