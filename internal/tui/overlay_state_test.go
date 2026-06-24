package tui

import (
	"testing"
)

// --- CFG-49: SurveyOverlayState ---

// TestSurveyOverlayState_ZeroRate verifies that when rate=0 ShouldTrigger returns false.
// CFG-49: feedbackSurveyRate=0 → surveys are never shown.
func TestSurveyOverlayState_ZeroRate(t *testing.T) {
	s := NewSurveyOverlayState(0)
	if s.ShouldTrigger(0.0) {
		t.Error("expected ShouldTrigger=false when rate=0, sample=0")
	}
	if s.ShouldTrigger(0.5) {
		t.Error("expected ShouldTrigger=false when rate=0, sample=0.5")
	}
}

// TestSurveyOverlayState_FullRate verifies that when rate=1 ShouldTrigger returns true for any sample.
// CFG-49.
func TestSurveyOverlayState_FullRate(t *testing.T) {
	s := NewSurveyOverlayState(1.0)
	if !s.ShouldTrigger(0.0) {
		t.Error("expected ShouldTrigger=true when rate=1, sample=0")
	}
	if !s.ShouldTrigger(0.999) {
		t.Error("expected ShouldTrigger=true when rate=1, sample=0.999")
	}
}

// TestSurveyOverlayState_PartialRate verifies that ShouldTrigger uses strict-less comparison.
// CFG-49.
func TestSurveyOverlayState_PartialRate(t *testing.T) {
	s := NewSurveyOverlayState(0.5)
	if !s.ShouldTrigger(0.49) {
		t.Error("expected true when sample < rate")
	}
	if s.ShouldTrigger(0.5) {
		t.Error("expected false when sample == rate")
	}
	if s.ShouldTrigger(0.51) {
		t.Error("expected false when sample > rate")
	}
}

// TestSurveyOverlayState_Trigger verifies that Trigger sets Visible=true.
// CFG-49.
func TestSurveyOverlayState_Trigger(t *testing.T) {
	s := NewSurveyOverlayState(0.5)
	opts := []string{"👍 Great", "👎 Poor", "Dismiss"}
	triggered := s.Trigger(SurveyKindFeedback, opts)
	if !triggered.Visible {
		t.Error("expected Visible=true after Trigger")
	}
	if triggered.Kind != SurveyKindFeedback {
		t.Errorf("expected Kind=feedback, got %q", triggered.Kind)
	}
	if len(triggered.Options) != len(opts) {
		t.Errorf("expected %d options, got %d", len(opts), len(triggered.Options))
	}
	if triggered.Selected != -1 {
		t.Errorf("expected Selected=-1 after trigger, got %d", triggered.Selected)
	}
}

// TestSurveyOverlayState_Dismiss verifies that Dismiss sets Visible=false.
// CFG-49.
func TestSurveyOverlayState_Dismiss(t *testing.T) {
	s := NewSurveyOverlayState(1.0)
	triggered := s.Trigger(SurveyKindFeedback, []string{"A", "B"})
	if !triggered.Visible {
		t.Fatal("prerequisite: expected visible after Trigger")
	}
	dismissed := triggered.Dismiss()
	if dismissed.Visible {
		t.Error("expected Visible=false after Dismiss")
	}
	// Rate must be preserved.
	if dismissed.Rate != s.Rate {
		t.Errorf("expected Rate=%v after Dismiss, got %v", s.Rate, dismissed.Rate)
	}
}

// TestSurveyOverlayState_Select verifies option selection.
// CFG-49.
func TestSurveyOverlayState_Select(t *testing.T) {
	s := NewSurveyOverlayState(1.0).Trigger(SurveyKindFeedback, []string{"A", "B", "C"})
	selected := s.Select(1)
	if selected.Selected != 1 {
		t.Errorf("expected Selected=1, got %d", selected.Selected)
	}
	// Out-of-range → -1.
	outOfRange := s.Select(99)
	if outOfRange.Selected != -1 {
		t.Errorf("expected Selected=-1 for out-of-range, got %d", outOfRange.Selected)
	}
}

// TestSurveyOverlayState_ToDialog_NotVisible verifies that ToDialog returns false when not visible.
// CFG-49.
func TestSurveyOverlayState_ToDialog_NotVisible(t *testing.T) {
	s := NewSurveyOverlayState(0.5)
	_, ok := s.ToDialog()
	if ok {
		t.Error("expected ToDialog ok=false when not visible")
	}
}

// TestSurveyOverlayState_ToDialog_Visible verifies the Dialog produced by a visible overlay.
// CFG-49.
func TestSurveyOverlayState_ToDialog_Visible(t *testing.T) {
	opts := []string{"Great", "OK", "Poor"}
	s := NewSurveyOverlayState(1.0).Trigger(SurveyKindFeedback, opts)
	d, ok := s.ToDialog()
	if !ok {
		t.Fatal("expected ToDialog ok=true when visible")
	}
	if d.Kind != DialogSurvey {
		t.Errorf("expected Kind=survey, got %q", d.Kind)
	}
	if len(d.Actions) != len(opts) {
		t.Errorf("expected %d actions, got %d", len(opts), len(d.Actions))
	}
}

// TestSurveyOverlayState_Immutability verifies Trigger/Dismiss/Select return new states.
// CFG-49.
func TestSurveyOverlayState_Immutability(t *testing.T) {
	base := NewSurveyOverlayState(1.0)
	triggered := base.Trigger(SurveyKindFeedback, []string{"A"})
	if base.Visible {
		t.Error("Trigger must not mutate base state")
	}
	dismissed := triggered.Dismiss()
	if !triggered.Visible {
		t.Error("Dismiss must not mutate triggered state")
	}
	_ = dismissed
}

// --- CFG-50: PromptSuggestionState ---

// TestPromptSuggestionState_DisabledNeverShows verifies disabled state shows nothing.
// CFG-50: promptSuggestionEnabled=false → no suggestions.
func TestPromptSuggestionState_DisabledNeverShows(t *testing.T) {
	p := NewPromptSuggestionState(false)
	p2 := p.WithSuggestions([]string{"hello world", "write tests"})
	if p2.Active() != "" {
		t.Errorf("expected empty Active() when disabled, got %q", p2.Active())
	}
	if len(p2.Suggestions) != 0 {
		t.Errorf("expected no suggestions when disabled, got %v", p2.Suggestions)
	}
}

// TestPromptSuggestionState_EnabledShowsSuggestions verifies enabled state serves suggestions.
// CFG-50.
func TestPromptSuggestionState_EnabledShowsSuggestions(t *testing.T) {
	p := NewPromptSuggestionState(true)
	p2 := p.WithSuggestions([]string{"hello world", "write tests"})
	if p2.Active() != "" {
		t.Error("expected empty Active() before index is advanced")
	}
	p3 := p2.Next()
	if p3.Active() != "hello world" {
		t.Errorf("expected 'hello world', got %q", p3.Active())
	}
	p4 := p3.Next()
	if p4.Active() != "write tests" {
		t.Errorf("expected 'write tests', got %q", p4.Active())
	}
	// Cycles back.
	p5 := p4.Next()
	if p5.Active() != "hello world" {
		t.Errorf("expected wrap to 'hello world', got %q", p5.Active())
	}
}

// TestPromptSuggestionState_Dismiss verifies Dismiss clears suggestions.
// CFG-50.
func TestPromptSuggestionState_Dismiss(t *testing.T) {
	p := NewPromptSuggestionState(true).WithSuggestions([]string{"a", "b"}).Next()
	dismissed := p.Dismiss()
	if dismissed.Active() != "" {
		t.Errorf("expected empty Active() after Dismiss, got %q", dismissed.Active())
	}
	if dismissed.ActiveIndex != -1 {
		t.Errorf("expected ActiveIndex=-1 after Dismiss, got %d", dismissed.ActiveIndex)
	}
}

// TestPromptSuggestionState_Immutability verifies WithSuggestions/Next/Dismiss are immutable.
// CFG-50.
func TestPromptSuggestionState_Immutability(t *testing.T) {
	base := NewPromptSuggestionState(true)
	withSugg := base.WithSuggestions([]string{"a", "b"})
	if len(base.Suggestions) != 0 {
		t.Error("WithSuggestions must not mutate base state")
	}
	afterNext := withSugg.Next()
	if withSugg.ActiveIndex != -1 {
		t.Error("Next must not mutate withSugg state")
	}
	_ = afterNext
}

// TestOverlayState_NewFromSettings verifies OverlayState is constructed from rate + flag.
// CFG-49 + CFG-50.
func TestOverlayState_NewFromSettings(t *testing.T) {
	o := NewOverlayState(0.25, true)
	if o.Survey.Rate != 0.25 {
		t.Errorf("expected Survey.Rate=0.25, got %v", o.Survey.Rate)
	}
	if !o.PromptSuggestion.Enabled {
		t.Error("expected PromptSuggestion.Enabled=true")
	}

	o2 := NewOverlayState(0, false)
	if o2.Survey.ShouldTrigger(0) {
		t.Error("expected survey disabled when rate=0")
	}
	if o2.PromptSuggestion.Enabled {
		t.Error("expected PromptSuggestion.Enabled=false")
	}
}
