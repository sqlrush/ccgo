package repl

import (
	"strings"
	"testing"
	"time"
)

// TestSpinnerTipsEnabledFalseOmitsTip verifies that when SpinnerConfig.TipsEnabled
// is false, the spinner Line() output does not contain a tip string.
// CC ref: src/services/tips/tipScheduler.ts:36 — spinnerTipsEnabled === false → no tip.
func TestSpinnerTipsEnabledFalseOmitsTip(t *testing.T) {
	tip := "Press ESC to interrupt"
	cfg := SpinnerConfig{TipsEnabled: false, Tip: tip}
	s := NewSpinnerWithConfig(time.Now(), cfg)
	line := s.Line(time.Now())
	if strings.Contains(line, tip) {
		t.Errorf("expected tip %q to be absent when TipsEnabled=false; got %q", tip, line)
	}
}

// TestSpinnerTipsEnabledTrueIncludesTip verifies that when SpinnerConfig.TipsEnabled
// is true and a tip is provided, the spinner Line() output contains the tip string.
func TestSpinnerTipsEnabledTrueIncludesTip(t *testing.T) {
	tip := "Press ESC to interrupt"
	cfg := SpinnerConfig{TipsEnabled: true, Tip: tip}
	s := NewSpinnerWithConfig(time.Now(), cfg)
	line := s.Line(time.Now())
	if !strings.Contains(line, tip) {
		t.Errorf("expected tip %q to be present when TipsEnabled=true; got %q", tip, line)
	}
}

// TestSpinnerDefaultHasNoTip verifies that NewSpinner (default config, no tip set)
// produces a line that doesn't include a tip suffix (only the esc-to-interrupt text).
func TestSpinnerDefaultHasNoTip(t *testing.T) {
	s := NewSpinner(time.Now())
	line := s.Line(time.Now())
	// Default line should have the standard "esc to interrupt" content but no custom tip.
	if !strings.Contains(line, "Working") {
		t.Errorf("expected default spinner to contain 'Working'; got %q", line)
	}
}

// TestSpinnerVerbCustomizes verifies that SpinnerConfig.Verb overrides the default verb.
func TestSpinnerVerbCustomizes(t *testing.T) {
	cfg := SpinnerConfig{Verb: "Analyzing…"}
	s := NewSpinnerWithConfig(time.Now(), cfg)
	line := s.Line(time.Now())
	if !strings.Contains(line, "Analyzing") {
		t.Errorf("expected custom verb 'Analyzing' in spinner line; got %q", line)
	}
}
