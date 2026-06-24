package plantools

import (
	"testing"

	"ccgo/internal/contracts"
)

// TestPlanFilePathFromSettings_CustomDir verifies that a non-empty plansDir
// takes precedence over sessionPath.
// CFG-43: CC ref: utils/settings/types.ts plansDirectory.
func TestPlanFilePathFromSettings_CustomDir(t *testing.T) {
	got := PlanFilePathFromSettings("session/path", "/custom/plans", contracts.ID("sess1"))
	want := "/custom/plans/sess1.plan.md"
	if got != want {
		t.Errorf("PlanFilePathFromSettings with custom dir: got %q, want %q", got, want)
	}
}

// TestPlanFilePathFromSettings_FallsBackToSessionPath verifies that an empty
// plansDir falls back to sessionPath.
func TestPlanFilePathFromSettings_FallsBackToSessionPath(t *testing.T) {
	got := PlanFilePathFromSettings("session/path", "", contracts.ID("sess1"))
	want := "session/path/sess1.plan.md"
	if got != want {
		t.Errorf("PlanFilePathFromSettings fallback: got %q, want %q", got, want)
	}
}

// TestPlanFilePathFromSettings_BothEmpty verifies that both empty fields produce
// a path relative to "." (current directory).
func TestPlanFilePathFromSettings_BothEmpty(t *testing.T) {
	got := PlanFilePathFromSettings("", "", contracts.ID("s"))
	want := "s.plan.md"
	// filepath.Join(".", "s.plan.md") on all platforms.
	if got != want {
		t.Errorf("PlanFilePathFromSettings both empty: got %q, want %q", got, want)
	}
}
