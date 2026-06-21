package repl

import (
	"strings"
	"testing"
	"time"
)

func TestSpinnerLineDeterministic(t *testing.T) {
	start := time.Unix(1000, 0)
	s := NewSpinner(start)
	// 3.2s in: elapsed should read 3s; frame index = (3200ms/100ms) % len.
	line := s.Line(start.Add(3200 * time.Millisecond))
	if !strings.Contains(line, "3s") {
		t.Fatalf("line %q should contain elapsed 3s", line)
	}
	if !strings.Contains(line, "esc to interrupt") {
		t.Fatalf("line %q should mention esc to interrupt", line)
	}
	if !strings.Contains(line, s.verb) {
		t.Fatalf("line %q should contain verb %q", line, s.verb)
	}
}

func TestSpinnerFrameAdvances(t *testing.T) {
	start := time.Unix(0, 0)
	s := NewSpinner(start)
	a := strings.Fields(s.Line(start))[0]
	b := strings.Fields(s.Line(start.Add(100 * time.Millisecond)))[0]
	if a == b {
		t.Fatalf("frame did not advance: %q == %q", a, b)
	}
}
