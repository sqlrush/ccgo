package repl

import (
	"fmt"
	"strings"
	"time"
)

const spinnerInterval = 100 * time.Millisecond

// spinnerFrames is a Braille-dot animation; ASCII-safe in any UTF-8 terminal.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerConfig carries render-affecting settings sourced from mergedSettings.
// CC ref: src/services/tips/tipScheduler.ts (spinnerTipsEnabled gate);
//
//	src/utils/settings/types.ts (spinnerVerbs / spinnerTipsOverride fields).
type SpinnerConfig struct {
	// TipsEnabled mirrors settings.SpinnerTipsEnabled.
	// When false, no tip text is appended to the spinner line.
	TipsEnabled bool
	// Tip is the current tip text selected by the tip scheduler.
	// Ignored when TipsEnabled is false.
	Tip string
	// Verb overrides the default "Working…" verb.
	// When empty, the default verb is used.
	Verb string
}

// Spinner renders an animated in-turn progress line. It is a value type; the
// Line method is pure (frame derived from elapsed time) so tests are stable.
type Spinner struct {
	frames        []string
	verb          string
	start         time.Time
	thinkingStart time.Time
	cfg           SpinnerConfig
}

func NewSpinner(now time.Time) Spinner {
	return NewSpinnerWithConfig(now, SpinnerConfig{})
}

// NewSpinnerWithConfig creates a Spinner that honours the given SpinnerConfig.
// CC ref: src/services/tips/tipScheduler.ts — spinnerTipsEnabled controls tip display.
func NewSpinnerWithConfig(now time.Time, cfg SpinnerConfig) Spinner {
	verb := cfg.Verb
	if verb == "" {
		verb = "Working…"
	}
	return Spinner{frames: spinnerFrames, verb: verb, start: now, cfg: cfg}
}

// WithThinkingMode returns a new Spinner (immutable) with thinking mode active.
// The verb changes to "Thinking…" and elapsed time is measured from now.
func (s Spinner) WithThinkingMode(now time.Time) Spinner {
	return Spinner{
		frames:        s.frames,
		verb:          "Thinking…",
		start:         s.start,
		thinkingStart: now,
		cfg:           s.cfg,
	}
}

// Line returns the status string at the given wall-clock time.
// Normal mode: "⠹ Working… (3s · esc to interrupt)"
// Thinking mode: "⠹ Thinking… (3s)"
// When cfg.TipsEnabled and cfg.Tip is non-empty, the tip is appended.
func (s Spinner) Line(now time.Time) string {
	elapsed := now.Sub(s.start)
	if elapsed < 0 {
		elapsed = 0
	}
	idx := int(elapsed/spinnerInterval) % len(s.frames)
	if !s.thinkingStart.IsZero() {
		thinkingElapsed := now.Sub(s.thinkingStart)
		if thinkingElapsed < 0 {
			thinkingElapsed = 0
		}
		secs := int(thinkingElapsed / time.Second)
		return fmt.Sprintf("%s %s (%ds)", s.frames[idx], s.verb, secs)
	}
	secs := int(elapsed / time.Second)
	base := fmt.Sprintf("%s %s (%ds · esc to interrupt)", s.frames[idx], s.verb, secs)
	// Append tip only when enabled and a tip string is set.
	// CC ref: tipScheduler.ts:36 — spinnerTipsEnabled === false → no tip.
	if s.cfg.TipsEnabled && strings.TrimSpace(s.cfg.Tip) != "" {
		return base + " · " + strings.TrimSpace(s.cfg.Tip)
	}
	return base
}
