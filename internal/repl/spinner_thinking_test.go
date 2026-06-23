package repl

import (
	"strings"
	"testing"
	"time"
)

// TestSpinnerDefaultVerb verifies that a new spinner has a verb containing "Working".
func TestSpinnerDefaultVerb(t *testing.T) {
	s := NewSpinner(time.Now())
	if !strings.Contains(s.verb, "Working") {
		t.Fatalf("expected verb to contain 'Working', got %q", s.verb)
	}
}

// TestSpinnerThinkingModeVerb verifies that WithThinkingMode changes the verb
// to contain "Thinking".
func TestSpinnerThinkingModeVerb(t *testing.T) {
	now := time.Now()
	s := NewSpinner(now).WithThinkingMode(now)
	if !strings.Contains(s.verb, "Thinking") {
		t.Fatalf("expected verb to contain 'Thinking', got %q", s.verb)
	}
}

// TestSpinnerThinkingModeShowsElapsed verifies that Line in thinking mode shows
// the elapsed thinking time in seconds (not total elapsed from start).
func TestSpinnerThinkingModeShowsElapsed(t *testing.T) {
	start := time.Unix(1000, 0)
	thinkingStart := start.Add(1 * time.Second) // thinking began 1s after turn start
	s := NewSpinner(start).WithThinkingMode(thinkingStart)

	// 3 seconds after thinking started.
	now := thinkingStart.Add(3 * time.Second)
	line := s.Line(now)

	if !strings.Contains(line, "Thinking") {
		t.Fatalf("line %q should contain 'Thinking'", line)
	}
	if !strings.Contains(line, "3s") {
		t.Fatalf("line %q should contain thinking elapsed '3s'", line)
	}
	// Normal spinner shows "esc to interrupt" — thinking mode should NOT show it.
	if strings.Contains(line, "esc to interrupt") {
		t.Fatalf("thinking mode line %q should not contain 'esc to interrupt'", line)
	}
}

// TestSpinnerThinkingModeIsImmutable verifies that WithThinkingMode returns a
// new Spinner and does not mutate the original.
func TestSpinnerThinkingModeIsImmutable(t *testing.T) {
	now := time.Now()
	original := NewSpinner(now)
	thinking := original.WithThinkingMode(now)

	if original.verb == thinking.verb {
		t.Fatalf("WithThinkingMode should change verb: original=%q thinking=%q", original.verb, thinking.verb)
	}
	if !original.thinkingStart.IsZero() {
		t.Fatal("original spinner should not have thinkingStart set")
	}
	if thinking.thinkingStart.IsZero() {
		t.Fatal("thinking spinner should have thinkingStart set")
	}
}
