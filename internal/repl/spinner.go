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
	frames []string
	verb   string
	start  time.Time
}

func NewSpinner(now time.Time) Spinner {
	return Spinner{frames: spinnerFrames, verb: "Working…", start: now}
}

// Line returns the status string at the given wall-clock time, e.g.
// "⠹ Working… (3s · esc to interrupt)".
func (s Spinner) Line(now time.Time) string {
	elapsed := now.Sub(s.start)
	if elapsed < 0 {
		elapsed = 0
	}
	idx := int(elapsed/spinnerInterval) % len(s.frames)
	secs := int(elapsed / time.Second)
	return fmt.Sprintf("%s %s (%ds · esc to interrupt)", s.frames[idx], s.verb, secs)
}
