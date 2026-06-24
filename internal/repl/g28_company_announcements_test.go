package repl

import (
	"context"
	"strings"
	"testing"

	"ccgo/internal/conversation"
)

// CFG-48: settings.companyAnnouncements → displayed at REPL startup.
// Given: InteractiveOptions.CompanyAnnouncements = ["Hello Corp", "Stay secure"]
// When:  RunInteractiveWithOptions is called (exits immediately via EOF input)
// Then:  announcement lines appear in terminal output before the prompt.
func TestCompanyAnnouncementsDisplayedAtStartup(t *testing.T) {
	t.Parallel()

	// FakeTerminal with empty input → EOF → loop exits immediately.
	ft := NewFakeTerminal("", 80, 24)

	// Minimal runner: no client needed since the loop exits before a turn fires.
	runner := conversation.Runner{}

	opts := InteractiveOptions{
		CompanyAnnouncements: []string{"Hello Corp", "Stay secure"},
	}

	ctx := context.Background()
	// RunInteractiveWithOptions exits when the terminal returns EOF.
	// We ignore the error (may be nil or EOF-derived).
	_ = RunInteractiveWithOptions(ctx, ft, runner, nil, opts)

	out := ft.Out.String()
	if !strings.Contains(out, "Hello Corp") {
		t.Errorf("CFG-48: expected 'Hello Corp' in terminal output; got: %q", out)
	}
	if !strings.Contains(out, "Stay secure") {
		t.Errorf("CFG-48: expected 'Stay secure' in terminal output; got: %q", out)
	}
}

// CFG-48: empty CompanyAnnouncements → no announcement lines printed.
func TestCompanyAnnouncementsEmptyNoOutput(t *testing.T) {
	t.Parallel()

	ft := NewFakeTerminal("", 80, 24)
	runner := conversation.Runner{}
	opts := InteractiveOptions{
		CompanyAnnouncements: nil,
	}

	ctx := context.Background()
	_ = RunInteractiveWithOptions(ctx, ft, runner, nil, opts)

	out := ft.Out.String()
	if strings.Contains(out, "[Announcement]") {
		t.Errorf("CFG-48: expected NO '[Announcement]' prefix when announcements is nil; got: %q", out)
	}
}
