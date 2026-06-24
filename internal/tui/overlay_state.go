package tui

import "math/rand"

// SurveyOverlayKind distinguishes survey overlay types.
type SurveyOverlayKind string

const (
	SurveyKindFeedback SurveyOverlayKind = "feedback"
	SurveyKindSkill    SurveyOverlayKind = "skill"
)

// SurveyOverlayState holds the state for the feedback survey overlay (CFG-49).
// CC ref: src/utils/settings/types.ts feedbackSurveyRate;
//         src/hooks/useSkillImprovementSurvey.ts;
//         src/services/analytics/config.ts isFeedbackSurveyDisabled.
type SurveyOverlayState struct {
	// Visible reports whether the survey overlay is currently shown.
	Visible bool

	// Kind is the type of survey (feedback, skill, etc.).
	Kind SurveyOverlayKind

	// Options are the selectable responses shown in the overlay.
	Options []string

	// Selected is the chosen option index (-1 = none).
	Selected int

	// Rate is the configured feedbackSurveyRate (0.0–1.0).
	// 0 means the survey is disabled; 1 means always shown.
	Rate float64
}

// NewSurveyOverlayState creates a SurveyOverlayState from the configured rate.
// CFG-49: when feedbackSurveyRate=0 surveys are never shown.
func NewSurveyOverlayState(rate float64) SurveyOverlayState {
	return SurveyOverlayState{
		Rate:     rate,
		Selected: -1,
	}
}

// ShouldTrigger reports whether a survey should be shown, based on the
// configured rate and the provided random sample value (0.0–1.0).
// Callers pass rand.Float64() in production; tests inject deterministic values.
// CFG-49: feedbackSurveyRate=0 → never; 1.0 → always.
func (s SurveyOverlayState) ShouldTrigger(sample float64) bool {
	if s.Rate <= 0 {
		return false
	}
	return sample < s.Rate
}

// Trigger transitions the overlay to visible with the given kind and options.
// Returns a new SurveyOverlayState (immutable).
func (s SurveyOverlayState) Trigger(kind SurveyOverlayKind, options []string) SurveyOverlayState {
	return SurveyOverlayState{
		Visible:  true,
		Kind:     kind,
		Options:  append([]string(nil), options...),
		Selected: -1,
		Rate:     s.Rate,
	}
}

// MaybeTrigger calls Trigger if ShouldTrigger(rand.Float64()) is true.
// Returns a new state and whether the overlay was triggered.
func (s SurveyOverlayState) MaybeTrigger(kind SurveyOverlayKind, options []string) (SurveyOverlayState, bool) {
	if !s.ShouldTrigger(rand.Float64()) { //nolint:gosec — non-cryptographic rate sampling
		return s, false
	}
	return s.Trigger(kind, options), true
}

// Dismiss hides the overlay. Returns a new SurveyOverlayState (immutable).
func (s SurveyOverlayState) Dismiss() SurveyOverlayState {
	return SurveyOverlayState{
		Visible:  false,
		Kind:     s.Kind,
		Options:  s.Options,
		Selected: s.Selected,
		Rate:     s.Rate,
	}
}

// Select records the chosen option index. Returns a new SurveyOverlayState (immutable).
func (s SurveyOverlayState) Select(index int) SurveyOverlayState {
	if index < 0 || (len(s.Options) > 0 && index >= len(s.Options)) {
		index = -1
	}
	return SurveyOverlayState{
		Visible:  s.Visible,
		Kind:     s.Kind,
		Options:  s.Options,
		Selected: index,
		Rate:     s.Rate,
	}
}

// ToDialog converts the overlay state to a Dialog for rendering.
// Returns (zero Dialog, false) when the overlay is not visible.
func (s SurveyOverlayState) ToDialog() (Dialog, bool) {
	if !s.Visible {
		return Dialog{}, false
	}
	title := "Feedback Survey"
	if s.Kind == SurveyKindSkill {
		title = "Skill Survey"
	}
	actions := s.Options
	if len(actions) == 0 {
		actions = []string{"Dismiss"}
	}
	focused := s.Selected
	if focused < 0 {
		focused = 0
	}
	return Dialog{
		Title:   title,
		Body:    "How would you rate your experience?",
		Actions: append([]string(nil), actions...),
		Focused: focused,
		ID:      "survey-" + string(s.Kind),
		Kind:    DialogSurvey,
	}, true
}

// PromptSuggestionState holds the state for the prompt suggestion feature (CFG-50).
// CC ref: src/utils/settings/types.ts promptSuggestionEnabled;
//         src/state/AppStateStore.ts promptSuggestionEnabled;
//         src/components/PromptInput/usePromptInputPlaceholder.ts.
type PromptSuggestionState struct {
	// Enabled reports whether prompt suggestions are active.
	// Wired from settings.promptSuggestionEnabled.
	Enabled bool

	// Suggestions holds the current candidate suggestions (may be empty).
	Suggestions []string

	// ActiveIndex is the currently displayed suggestion (-1 = none).
	ActiveIndex int
}

// NewPromptSuggestionState creates state from the settings flag.
// CFG-50: promptSuggestionEnabled=false → suggestions are never shown.
func NewPromptSuggestionState(enabled bool) PromptSuggestionState {
	return PromptSuggestionState{Enabled: enabled, ActiveIndex: -1}
}

// WithSuggestions returns a new state with the provided suggestion list.
// When Enabled is false the suggestions are cleared. Immutable.
func (p PromptSuggestionState) WithSuggestions(suggestions []string) PromptSuggestionState {
	if !p.Enabled {
		return PromptSuggestionState{Enabled: false, ActiveIndex: -1}
	}
	idx := p.ActiveIndex
	if idx >= len(suggestions) {
		idx = -1
	}
	return PromptSuggestionState{
		Enabled:     true,
		Suggestions: append([]string(nil), suggestions...),
		ActiveIndex: idx,
	}
}

// Dismiss clears the current suggestion list. Immutable.
func (p PromptSuggestionState) Dismiss() PromptSuggestionState {
	return PromptSuggestionState{Enabled: p.Enabled, ActiveIndex: -1}
}

// Active returns the currently displayed suggestion string, or "" if none.
func (p PromptSuggestionState) Active() string {
	if !p.Enabled || p.ActiveIndex < 0 || p.ActiveIndex >= len(p.Suggestions) {
		return ""
	}
	return p.Suggestions[p.ActiveIndex]
}

// Next advances to the next suggestion (cycling). Returns new state. Immutable.
func (p PromptSuggestionState) Next() PromptSuggestionState {
	if !p.Enabled || len(p.Suggestions) == 0 {
		return p
	}
	next := (p.ActiveIndex + 1) % len(p.Suggestions)
	return PromptSuggestionState{
		Enabled:     p.Enabled,
		Suggestions: p.Suggestions,
		ActiveIndex: next,
	}
}

// OverlayState groups the two overlay states for the TUI session.
// CFG-49 (survey) + CFG-50 (prompt suggestion).
type OverlayState struct {
	Survey           SurveyOverlayState
	PromptSuggestion PromptSuggestionState
}

// NewOverlayState creates OverlayState from settings flags.
func NewOverlayState(surveyRate float64, promptSuggestionEnabled bool) OverlayState {
	return OverlayState{
		Survey:           NewSurveyOverlayState(surveyRate),
		PromptSuggestion: NewPromptSuggestionState(promptSuggestionEnabled),
	}
}
