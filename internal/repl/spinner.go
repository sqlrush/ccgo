package repl

import (
	"fmt"
	"time"
)

const spinnerInterval = 100 * time.Millisecond

// spinnerFrames is a Braille-dot animation; ASCII-safe in any UTF-8 terminal.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner renders an animated in-turn progress line. It is a value type; the
// Line method is pure (frame derived from elapsed time) so tests are stable.
type Spinner struct {
	frames        []string
	verb          string
	start         time.Time
	thinkingStart time.Time
}

func NewSpinner(now time.Time) Spinner {
	return Spinner{frames: spinnerFrames, verb: "Working…", start: now}
}

// WithThinkingMode returns a new Spinner (immutable) with thinking mode active.
// The verb changes to "Thinking…" and elapsed time is measured from now.
func (s Spinner) WithThinkingMode(now time.Time) Spinner {
	return Spinner{
		frames:        s.frames,
		verb:          "Thinking…",
		start:         s.start,
		thinkingStart: now,
	}
}

// Line returns the status string at the given wall-clock time.
// Normal mode: "⠹ Working… (3s · esc to interrupt)"
// Thinking mode: "⠹ Thinking… (3s)"
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
	return fmt.Sprintf("%s %s (%ds · esc to interrupt)", s.frames[idx], s.verb, secs)
}
