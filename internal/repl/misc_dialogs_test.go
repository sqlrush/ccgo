package repl

import (
	"strings"
	"testing"
	"time"

	"ccgo/internal/tui"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ContextVisualizationOverlay (OVL-40)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestContextVisualizationOverlayEnterDismisses(t *testing.T) {
	o := NewContextVisualizationOverlay([]ContextGroup{
		{Label: "System", Tokens: 1000},
	}, 10000)
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "ctx:dismiss" {
		t.Fatalf("Enter = %q want ctx:dismiss", res.Submit)
	}
}

func TestContextVisualizationOverlayEscDismisses(t *testing.T) {
	o := NewContextVisualizationOverlay(nil, 0)
	res, _ := o.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if res.Submit != "ctx:dismiss" {
		t.Fatalf("Esc = %q want ctx:dismiss", res.Submit)
	}
}

func TestContextVisualizationOverlayRenderGroupLabels(t *testing.T) {
	o := NewContextVisualizationOverlay([]ContextGroup{
		{Label: "System", Tokens: 500},
		{Label: "User", Tokens: 300},
	}, 1000)
	out := strings.Join(o.Render(80, 24), "\n")
	if !strings.Contains(out, "System") {
		t.Fatalf("Render missing System label: %q", out)
	}
	if !strings.Contains(out, "User") {
		t.Fatalf("Render missing User label: %q", out)
	}
}

func TestContextVisualizationOverlayDefensiveCopy(t *testing.T) {
	groups := []ContextGroup{{Label: "A", Tokens: 100}}
	o := NewContextVisualizationOverlay(groups, 1000)
	groups[0].Label = "mutated"
	out := strings.Join(o.Render(80, 24), "\n")
	if strings.Contains(out, "mutated") {
		t.Fatal("overlay should not share backing slice")
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// MemoryUpdateNotice (OVL-41)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestMemoryUpdateNoticeContainsFilePath(t *testing.T) {
	notice := MemoryUpdateNotice("/home/user/.claude/CLAUDE.md")
	if !strings.Contains(notice, "/home/user/.claude/CLAUDE.md") {
		t.Fatalf("notice missing file path: %q", notice)
	}
	if !strings.Contains(strings.ToLower(notice), "memory") {
		t.Fatalf("notice missing 'memory': %q", notice)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// UpdateAvailableNotice (OVL-43)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestUpdateAvailableNoticeContainsVersions(t *testing.T) {
	notice := UpdateAvailableNotice("1.0.0", "1.1.0")
	if !strings.Contains(notice, "1.0.0") {
		t.Fatalf("notice missing current version: %q", notice)
	}
	if !strings.Contains(notice, "1.1.0") {
		t.Fatalf("notice missing new version: %q", notice)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// StatusNoticesOverlay (OVL-44)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestStatusNoticesOverlayEnterDismisses(t *testing.T) {
	o := NewStatusNoticesOverlay([]StatusNotice{{Level: "warn", Message: "rate limited"}})
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "notices:dismiss" {
		t.Fatalf("Enter = %q want notices:dismiss", res.Submit)
	}
}

func TestStatusNoticesOverlayRenderShowsMessages(t *testing.T) {
	o := NewStatusNoticesOverlay([]StatusNotice{
		{Level: "warn", Message: "rate limit warning"},
		{Level: "error", Message: "something failed"},
	})
	out := strings.Join(o.Render(80, 24), "\n")
	if !strings.Contains(out, "rate limit warning") {
		t.Fatalf("Render missing notice message: %q", out)
	}
	if !strings.Contains(out, "something failed") {
		t.Fatalf("Render missing error message: %q", out)
	}
}

func TestStatusNoticesOverlayDefensiveCopy(t *testing.T) {
	notices := []StatusNotice{{Level: "info", Message: "original"}}
	o := NewStatusNoticesOverlay(notices)
	notices[0].Message = "mutated"
	out := strings.Join(o.Render(80, 24), "\n")
	if strings.Contains(out, "mutated") {
		t.Fatal("overlay should not share backing slice")
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// IdleReturnDialog (OVL-50)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestIdleReturnDialogDefaultContinues(t *testing.T) {
	d := NewIdleReturnDialog(5 * time.Minute)
	// cursor 0 = Continue
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "idle:continue" {
		t.Fatalf("default Enter = %q want idle:continue", res.Submit)
	}
}

func TestIdleReturnDialogNavigateToExit(t *testing.T) {
	d := NewIdleReturnDialog(10 * time.Minute)
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor 0→1 (Exit)
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "idle:exit" {
		t.Fatalf("navigate+Enter = %q want idle:exit", res.Submit)
	}
}

func TestIdleReturnDialogEscContinues(t *testing.T) {
	d := NewIdleReturnDialog(time.Minute)
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if res.Submit != "idle:continue" {
		t.Fatalf("Esc = %q want idle:continue", res.Submit)
	}
}

func TestIdleReturnDialogRenderShowsDuration(t *testing.T) {
	d := NewIdleReturnDialog(5 * time.Minute)
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "5m") {
		t.Fatalf("Render should show duration: %q", out)
	}
}

func TestIdleReturnDialogTabTogglesCursor(t *testing.T) {
	d := NewIdleReturnDialog(time.Minute)
	d.ApplyKey(tui.Key{Type: tui.KeyTab})
	if d.cursor != 1 {
		t.Fatalf("after Tab cursor = %d want 1", d.cursor)
	}
	d.ApplyKey(tui.Key{Type: tui.KeyTab})
	if d.cursor != 0 {
		t.Fatalf("after double Tab cursor = %d want 0", d.cursor)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// WorktreeExitDialog (OVL-51)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestWorktreeExitDialogDefaultCancel(t *testing.T) {
	d := NewWorktreeExitDialog("/wt/path", "feat/x")
	// cursor 0 = cancel
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "worktree:cancel" {
		t.Fatalf("default Enter = %q want worktree:cancel", res.Submit)
	}
}

func TestWorktreeExitDialogSelectDiscard(t *testing.T) {
	d := NewWorktreeExitDialog("/wt", "b")
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor 0→1 = discard
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "worktree:discard" {
		t.Fatalf("discard = %q want worktree:discard", res.Submit)
	}
}

func TestWorktreeExitDialogSelectMerge(t *testing.T) {
	d := NewWorktreeExitDialog("/wt", "b")
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // 0→1
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // 1→2 = merge
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "worktree:merge" {
		t.Fatalf("merge = %q want worktree:merge", res.Submit)
	}
}

func TestWorktreeExitDialogEscCancels(t *testing.T) {
	d := NewWorktreeExitDialog("/wt", "b")
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if res.Submit != "worktree:cancel" {
		t.Fatalf("Esc = %q want worktree:cancel", res.Submit)
	}
}

func TestWorktreeExitDialogRenderShowsBranch(t *testing.T) {
	d := NewWorktreeExitDialog("/my/path", "feat/hello")
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "/my/path") {
		t.Fatalf("Render missing path: %q", out)
	}
	if !strings.Contains(out, "feat/hello") {
		t.Fatalf("Render missing branch: %q", out)
	}
}
