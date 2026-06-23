package repl

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

// TestAddDirHandlerNoArg verifies /add-dir with no argument returns usage text.
func TestAddDirHandlerNoArg(t *testing.T) {
	var appends []string
	fakeAppender := func(path string, dir string) error {
		appends = append(appends, dir)
		return nil
	}
	h := addDirHandlerWith(fakeAppender, "")
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !strings.Contains(out.Status, "Usage") && !strings.Contains(out.Status, "usage") && !strings.Contains(out.Status, "path") {
		t.Fatalf("expected usage hint in status, got %q", out.Status)
	}
	if len(appends) != 0 {
		t.Fatal("should not have called appender for empty arg")
	}
}

// TestAddDirHandlerWithPath verifies /add-dir <path> appends to additionalDirectories.
func TestAddDirHandlerWithPath(t *testing.T) {
	var appends []string
	fakeAppender := func(path string, dir string) error {
		appends = append(appends, dir)
		return nil
	}
	h := addDirHandlerWith(fakeAppender, "/tmp/cwd")
	out, err := h(context.Background(), CommandContext{Args: "/tmp/extra"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if len(appends) == 0 || appends[0] != "/tmp/extra" {
		t.Fatalf("expected /tmp/extra appended, got %v", appends)
	}
}

// TestPlanHandlerTogglesToPlanMode verifies /plan switches to plan mode.
func TestPlanHandlerTogglesToPlanMode(t *testing.T) {
	h := planHandlerWith(contracts.PermissionDefault)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if out.NewMode != contracts.PermissionPlan {
		t.Fatalf("expected NewMode=plan, got %q", out.NewMode)
	}
}

// TestPlanHandlerFromPlanModeRestores verifies /plan in plan mode returns to default.
func TestPlanHandlerFromPlanModeRestores(t *testing.T) {
	h := planHandlerWith(contracts.PermissionPlan)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if out.NewMode != contracts.PermissionDefault {
		t.Fatalf("expected NewMode=default, got %q", out.NewMode)
	}
}

// TestTerminalSetupHandlerReturnsInstructions verifies /terminal-setup returns setup text.
func TestTerminalSetupHandlerReturnsInstructions(t *testing.T) {
	h := terminalSetupHandler()
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty status with setup instructions")
	}
}

// TestExitHandlerSetsExitFlag verifies /exit returns Exit=true.
func TestExitHandlerSetsExitFlag(t *testing.T) {
	h := exitHandler()
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !out.Exit {
		t.Fatal("expected Exit=true")
	}
}

// TestDiffHandlerRunsGitDiff verifies /diff produces output (or an error message).
func TestDiffHandlerRunsGitDiff(t *testing.T) {
	h := diffHandler("/Users/sqlrush/ccgo")
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	// Result is either the diff output or a "no changes" message — non-empty status.
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty diff status")
	}
}

// TestRenameHandlerWithName verifies /rename <name> produces a confirmation.
func TestRenameHandlerWithName(t *testing.T) {
	h := renameHandler("sess_test", "/tmp")
	out, err := h(context.Background(), CommandContext{Args: "my-session"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !strings.Contains(out.Status, "my-session") && !strings.Contains(out.Status, "renamed") && !strings.Contains(out.Status, "Renamed") {
		t.Fatalf("expected session name in status, got %q", out.Status)
	}
}

// TestRenameHandlerNoArg verifies /rename with no argument returns usage.
func TestRenameHandlerNoArg(t *testing.T) {
	h := renameHandler("sess_test", "/tmp")
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !strings.Contains(out.Status, "Usage") && !strings.Contains(out.Status, "usage") {
		t.Fatalf("expected usage hint, got %q", out.Status)
	}
}

// TestStatsHandlerTextOutput verifies /stats returns text output.
func TestStatsHandlerTextOutput(t *testing.T) {
	h := statsHandler()
	out, err := h(context.Background(), CommandContext{History: []contracts.Message{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty stats output")
	}
}

// TestKeybindingsHandlerTextOutput verifies /keybindings returns path info.
func TestKeybindingsHandlerTextOutput(t *testing.T) {
	h := keybindingsHandler("/tmp/fake-home")
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !strings.Contains(out.Status, "keybindings") {
		t.Fatalf("expected 'keybindings' in output, got %q", out.Status)
	}
}

// TestReloadPluginsHandlerTextOutput verifies /reload-plugins returns a message.
func TestReloadPluginsHandlerTextOutput(t *testing.T) {
	h := reloadPluginsHandler("/tmp")
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty reload output")
	}
}

// TestCopyHandlerTextOutput verifies /copy returns a message (clipboard not available in tests).
func TestCopyHandlerTextOutput(t *testing.T) {
	h := copyHandler(nil)
	out, err := h(context.Background(), CommandContext{History: []contracts.Message{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty copy output")
	}
}

// TestFastHandlerReturnsMessage verifies /fast returns a mode info message.
func TestFastHandlerReturnsMessage(t *testing.T) {
	h := fastHandler()
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty fast output")
	}
}

// TestTagHandlerNoArg verifies /tag with no argument returns usage.
func TestTagHandlerNoArg(t *testing.T) {
	h := tagHandler("sess_test", "/tmp")
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !strings.Contains(out.Status, "Usage") && !strings.Contains(out.Status, "usage") {
		t.Fatalf("expected usage hint, got %q", out.Status)
	}
}

// TestColorHandlerNoArg verifies /color with no argument returns usage.
func TestColorHandlerNoArg(t *testing.T) {
	h := colorHandler()
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty color output")
	}
}

// TestStatusLineHandlerReturnsMessage verifies /statusline returns info.
func TestStatusLineHandlerReturnsMessage(t *testing.T) {
	h := statusLineHandler()
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty statusline output")
	}
}

// TestBranchHandlerReturnsMessage verifies /branch returns info (⚠️ no sidechain infra).
func TestBranchHandlerReturnsMessage(t *testing.T) {
	h := branchHandler()
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty branch output")
	}
}

// TestTasksHandlerReturnsMessage verifies /tasks returns info (⚠️ no infra).
func TestTasksHandlerReturnsMessage(t *testing.T) {
	h := tasksHandler()
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty tasks output")
	}
}

// TestLastAssistantTextExtractsLastMessage verifies that lastAssistantText
// returns the text from the last assistant message.
func TestLastAssistantTextExtractsLastMessage(t *testing.T) {
	history := []contracts.Message{
		{
			Type: contracts.MessageUser,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentText, Text: "hello"},
			},
		},
		{
			Type: contracts.MessageAssistant,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentText, Text: "first response"},
			},
		},
		{
			Type: contracts.MessageUser,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentText, Text: "follow-up"},
			},
		},
		{
			Type: contracts.MessageAssistant,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentText, Text: "last response"},
			},
		},
	}
	got := lastAssistantText(history)
	if got != "last response" {
		t.Fatalf("expected 'last response', got %q", got)
	}
}

// TestLastAssistantTextEmptyOnNoAssistant verifies empty string on missing assistant.
func TestLastAssistantTextEmptyOnNoAssistant(t *testing.T) {
	history := []contracts.Message{
		{
			Type: contracts.MessageUser,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentText, Text: "hello"},
			},
		},
	}
	got := lastAssistantText(history)
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

// TestCopyHandlerCopiesLastAssistantText verifies that copyHandler passes the
// last assistant message text to the injected writer.
func TestCopyHandlerCopiesLastAssistantText(t *testing.T) {
	var captured string
	writer := func(text string) error {
		captured = text
		return nil
	}
	h := copyHandler(writer)
	history := []contracts.Message{
		{
			Type: contracts.MessageAssistant,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentText, Text: "assistant said this"},
			},
		},
	}
	out, err := h(context.Background(), CommandContext{History: history})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if captured != "assistant said this" {
		t.Fatalf("expected captured text %q, got %q", "assistant said this", captured)
	}
}

// TestCopyHandlerMissingClipboardToolGraceful verifies that a writer error is
// handled gracefully — Handled=true with a status mentioning "clipboard".
func TestCopyHandlerMissingClipboardToolGraceful(t *testing.T) {
	writer := func(text string) error {
		return fmt.Errorf("all tools failed")
	}
	h := copyHandler(writer)
	history := []contracts.Message{
		{
			Type: contracts.MessageAssistant,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentText, Text: "some text"},
			},
		},
	}
	out, err := h(context.Background(), CommandContext{History: history})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !out.Handled {
		t.Fatalf("expected Handled=true, got false")
	}
	if !strings.Contains(strings.ToLower(out.Status), "clipboard") {
		t.Fatalf("expected 'clipboard' in status, got %q", out.Status)
	}
}

// TestFastHandlerWithSetterCallsHaiku verifies that fastHandlerWith calls the
// setter with the Haiku model ID.
func TestFastHandlerWithSetterCallsHaiku(t *testing.T) {
	var called string
	setter := func(model string) error {
		called = model
		return nil
	}
	h := fastHandlerWith(setter)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if called != haikuModel {
		t.Fatalf("expected setter called with %q, got %q", haikuModel, called)
	}
}

// TestFastHandlerNilSetterReturnsMessage verifies that nil setter returns
// an informational message with Handled=true.
func TestFastHandlerNilSetterReturnsMessage(t *testing.T) {
	h := fastHandlerWith(nil)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty status message")
	}
}

// TestCommandOutcomeNewModeWiresIntoLoop verifies that CommandOutcome.NewMode
// causes the loop to update its mode and status bar without falling through to the model.
func TestCommandOutcomeNewModeWiresIntoLoop(t *testing.T) {
	ft := NewFakeTerminal("/plan\r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)

	var modeChanges []contracts.PermissionMode
	l.onModeChange = func(m contracts.PermissionMode) { modeChanges = append(modeChanges, m) }

	router := NewCommandRouter()
	router.Register("plan", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, NewMode: contracts.PermissionPlan}, nil
	})
	l.onCommand = func(input string) (CommandOutcome, bool) {
		out, err := router.Dispatch(context.Background(), input, CommandContext{Screen: &l.screen})
		if err != nil {
			return CommandOutcome{}, false
		}
		return out, out.Handled
	}

	hit := 0
	l.StartTurn = func(string) { hit++ }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hit != 0 {
		t.Fatalf("/plan must not hit the model; StartTurn called %d times", hit)
	}
	if len(modeChanges) == 0 || modeChanges[0] != contracts.PermissionPlan {
		t.Fatalf("expected plan mode change; got %v", modeChanges)
	}
}

// TestCommandOutcomeExitExitsLoop verifies CommandOutcome.Exit=true causes the loop to exit.
func TestCommandOutcomeExitExitsLoop(t *testing.T) {
	ft := NewFakeTerminal("/exit\r", 80, 24) // no need for extra EOF since exit fires first
	l := NewLoop(ft, nil)

	router := NewCommandRouter()
	router.Register("exit", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Exit: true}, nil
	})
	l.onCommand = func(input string) (CommandOutcome, bool) {
		out, err := router.Dispatch(context.Background(), input, CommandContext{Screen: &l.screen})
		if err != nil {
			return CommandOutcome{}, false
		}
		return out, out.Handled
	}

	l.StartTurn = func(string) {}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
}


// TestBranchHandlerWithNoTranscriptReturnsWarning verifies that /branch when
// there is no session transcript (nil forker) returns an informational message.
func TestBranchHandlerWithNilForkerReturnsInfo(t *testing.T) {
	h := branchHandlerWith(nil, "")
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty status")
	}
}

// TestBranchHandlerWithForkerSucceeds verifies that /branch calls the forker
// and returns a success message containing the fork session ID.
func TestBranchHandlerWithForkerSucceeds(t *testing.T) {
	called := false
	var gotTitle string
	fakeForker := func(title string) (sessionForkerResult, error) {
		called = true
		gotTitle = title
		return sessionForkerResult{SessionID: "fork-1234", Title: "My Branch (Branch)"}, nil
	}
	h := branchHandlerWith(fakeForker, "")
	out, err := h(context.Background(), CommandContext{Args: "My Branch"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !called {
		t.Fatal("forker was not called")
	}
	if gotTitle != "My Branch" {
		t.Fatalf("forker called with %q, want %q", gotTitle, "My Branch")
	}
	if !strings.Contains(out.Status, "fork-1234") && !strings.Contains(out.Status, "Branch") {
		t.Fatalf("status must mention fork session or branch; got %q", out.Status)
	}
}

// TestBranchHandlerWithForkerError verifies that /branch returns a user-facing
// error message when the forker fails.
func TestBranchHandlerWithForkerError(t *testing.T) {
	fakeForker := func(title string) (sessionForkerResult, error) {
		return sessionForkerResult{}, fmt.Errorf("disk full")
	}
	h := branchHandlerWith(fakeForker, "")
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !strings.Contains(out.Status, "disk full") {
		t.Fatalf("expected error in status, got %q", out.Status)
	}
}

// TestBranchHandlerBackwardCompatibility verifies that the zero-arg branchHandler()
// still returns Handled=true (backward-compat; no session context → info message).
func TestBranchHandlerBackwardCompatibility(t *testing.T) {
	h := branchHandler()
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if strings.TrimSpace(out.Status) == "" {
		t.Fatal("expected non-empty status")
	}
}
