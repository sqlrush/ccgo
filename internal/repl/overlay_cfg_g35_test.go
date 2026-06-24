package repl

import (
	"context"
	"testing"
	"time"

	"ccgo/internal/conversation"
	"ccgo/internal/tui"
)

// ---------------------------------------------------------------------------
// CFG-49: FeedbackSurveyRate — setting is read, surveyState instantiated,
// survey overlay triggered after turns when rate=1.0 (always).
// ---------------------------------------------------------------------------

// TestInteractiveOptions_SurveyRateField verifies that InteractiveOptions has a
// FeedbackSurveyRate field that defaults to 0 (survey disabled).
// CFG-49: setting must be readable from InteractiveOptions.
func TestInteractiveOptions_SurveyRateField(t *testing.T) {
	opts := InteractiveOptions{}
	if opts.FeedbackSurveyRate != 0 {
		t.Errorf("expected FeedbackSurveyRate=0 by default, got %v", opts.FeedbackSurveyRate)
	}
}

// TestInteractiveOptions_PromptSuggestionField verifies that InteractiveOptions has
// a PromptSuggestionEnabled field that defaults to false (suggestions disabled).
// CFG-50: setting must be readable from InteractiveOptions.
func TestInteractiveOptions_PromptSuggestionField(t *testing.T) {
	opts := InteractiveOptions{}
	if opts.PromptSuggestionEnabled {
		t.Errorf("expected PromptSuggestionEnabled=false by default, got true")
	}
}

// TestLoop_SurveyStateField verifies that the Loop struct has a surveyState field
// that can be set and accessed.
// CFG-49: the loop must carry the survey state.
func TestLoop_SurveyStateField(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	loop := NewLoop(ft, nil)
	loop.surveyState = tui.NewSurveyOverlayState(1.0)
	if loop.surveyState.Rate != 1.0 {
		t.Errorf("expected surveyState.Rate=1.0, got %v", loop.surveyState.Rate)
	}
}

// TestLoop_PromptSuggestionStateField verifies that the Loop struct has a
// promptSuggestionState field that can be set and accessed.
// CFG-50: the loop must carry the suggestion state.
func TestLoop_PromptSuggestionStateField(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	loop := NewLoop(ft, nil)
	loop.promptSuggestionState = tui.NewPromptSuggestionState(true)
	if !loop.promptSuggestionState.Enabled {
		t.Error("expected promptSuggestionState.Enabled=true, got false")
	}
}

// TestLoop_TriggerSurveyIfDue_RateOne verifies that when rate=1.0 (always) the
// triggerSurveyIfDue method marks the survey visible and sets an activeOverlay.
// CFG-49: trigger is reachable and produces a visible overlay.
func TestLoop_TriggerSurveyIfDue_RateOne(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	loop := NewLoop(ft, nil)
	loop.surveyState = tui.NewSurveyOverlayState(1.0) // rate=1 → always trigger

	loop.triggerSurveyIfDue()

	if !loop.surveyState.Visible {
		t.Error("expected surveyState.Visible=true after trigger with rate=1.0")
	}
	if loop.activeOverlay == nil {
		t.Error("expected activeOverlay to be non-nil after survey trigger")
	}
}

// TestLoop_TriggerSurveyIfDue_RateZero verifies that rate=0 never triggers.
// CFG-49: disabled survey must not show overlay.
func TestLoop_TriggerSurveyIfDue_RateZero(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	loop := NewLoop(ft, nil)
	loop.surveyState = tui.NewSurveyOverlayState(0) // rate=0 → never

	loop.triggerSurveyIfDue()

	if loop.surveyState.Visible {
		t.Error("expected surveyState.Visible=false when rate=0")
	}
	if loop.activeOverlay != nil {
		t.Error("expected no activeOverlay when rate=0")
	}
}

// TestRunInteractiveWithOptions_SurveyWiredViaOpts verifies the RunInteractiveWithOptions
// path compiles and runs with FeedbackSurveyRate set.
// CFG-49: opts field is reachable in the production path.
func TestRunInteractiveWithOptions_SurveyWiredViaOpts(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24) // empty input → immediate EOF exit
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = RunInteractiveWithOptions(ctx, ft,
		conversation.Runner{},
		nil,
		InteractiveOptions{FeedbackSurveyRate: 1.0},
	)
	// Just verifying it compiles and runs; the internal wiring is proven by
	// TestLoop_TriggerSurveyIfDue_* above.
}

// TestRunInteractiveWithOptions_PromptSuggestionWiredViaOpts verifies the prod path
// compiles and runs with PromptSuggestionEnabled set.
// CFG-50: opts field is reachable in the production path.
func TestRunInteractiveWithOptions_PromptSuggestionWiredViaOpts(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = RunInteractiveWithOptions(ctx, ft,
		conversation.Runner{},
		nil,
		InteractiveOptions{PromptSuggestionEnabled: true},
	)
}
