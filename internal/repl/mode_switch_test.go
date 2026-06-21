package repl

import (
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

func TestCycleMode(t *testing.T) {
	seq := []contracts.PermissionMode{
		contracts.PermissionDefault,
		contracts.PermissionAcceptEdits,
		contracts.PermissionPlan,
		contracts.PermissionBypassPermissions,
		contracts.PermissionDefault,
	}
	cur := contracts.PermissionDefault
	for i := 1; i < len(seq); i++ {
		cur = cycleMode(cur)
		if cur != seq[i] {
			t.Fatalf("cycle step %d = %q want %q", i, cur, seq[i])
		}
	}
}

func TestModeIndicatorPlan(t *testing.T) {
	got := modeIndicator(contracts.PermissionPlan, false, tui.VimMode(""))
	if !strings.Contains(strings.ToLower(got), "plan") {
		t.Fatalf("indicator = %q should mention plan", got)
	}
}

func TestModeIndicatorDefaultEmptyNoVim(t *testing.T) {
	if got := modeIndicator(contracts.PermissionDefault, false, tui.VimMode("")); got != "" {
		t.Fatalf("default mode w/o vim should be empty, got %q", got)
	}
}

func TestModeIndicatorAcceptEdits(t *testing.T) {
	got := modeIndicator(contracts.PermissionAcceptEdits, false, tui.VimMode(""))
	if !strings.Contains(strings.ToLower(got), "accept") {
		t.Fatalf("indicator = %q should mention accept", got)
	}
}

func TestModeIndicatorBypass(t *testing.T) {
	got := modeIndicator(contracts.PermissionBypassPermissions, false, tui.VimMode(""))
	if !strings.Contains(strings.ToLower(got), "bypass") {
		t.Fatalf("indicator = %q should mention bypass", got)
	}
}

func TestModeIndicatorVimInsert(t *testing.T) {
	got := modeIndicator(contracts.PermissionDefault, true, tui.VimInsert)
	if !strings.Contains(got, "INSERT") {
		t.Fatalf("vim insert indicator = %q should contain INSERT", got)
	}
}

func TestModeIndicatorVimInsertWithMode(t *testing.T) {
	got := modeIndicator(contracts.PermissionPlan, true, tui.VimInsert)
	if !strings.Contains(strings.ToLower(got), "plan") {
		t.Fatalf("indicator = %q should mention plan", got)
	}
	if !strings.Contains(got, "INSERT") {
		t.Fatalf("indicator = %q should contain INSERT", got)
	}
}

func TestModeIndicatorVimNormalNoInsert(t *testing.T) {
	got := modeIndicator(contracts.PermissionDefault, true, tui.VimNormal)
	if strings.Contains(got, "INSERT") {
		t.Fatalf("vim normal indicator = %q should not contain INSERT", got)
	}
}
