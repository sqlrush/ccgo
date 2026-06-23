package plantools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// TestExitPlanModePermissionMessageIncludesPlan verifies that when the plan
// file exists, CheckPermissions includes the plan content in its Message so
// the interactive permission dialog shows the plan for review (TOOL-PLAN-02).
func TestExitPlanModePermissionMessageIncludesPlan(t *testing.T) {
	dir := t.TempDir()
	plan := "Step 1: do X\nStep 2: do Y"
	if err := WritePlan(dir, "s-msg", plan); err != nil {
		t.Fatalf("WritePlan err: %v", err)
	}

	toolImpl := NewExitPlanModeTool()
	ctx := tool.Context{
		Context:   context.Background(),
		SessionID: "s-msg",
		Metadata:  map[string]any{tool.MetadataSessionPathKey: dir},
	}

	dec, err := toolImpl.CheckPermissions(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CheckPermissions err: %v", err)
	}
	if dec.Behavior != contracts.PermissionAsk {
		t.Fatalf("behavior = %q want ask", dec.Behavior)
	}
	if !strings.Contains(dec.Message, "Step 1: do X") {
		t.Fatalf("permission message %q does not contain plan content", dec.Message)
	}
}

// TestExitPlanModePermissionMessageNoPlanFile verifies that when no plan file
// exists the permission dialog still works (graceful empty-plan case).
func TestExitPlanModePermissionMessageNoPlanFile(t *testing.T) {
	toolImpl := NewExitPlanModeTool()
	ctx := tool.Context{
		Context:   context.Background(),
		SessionID: "s-noplan",
		Metadata:  map[string]any{tool.MetadataSessionPathKey: t.TempDir()},
	}

	dec, err := toolImpl.CheckPermissions(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CheckPermissions err: %v", err)
	}
	if dec.Behavior != contracts.PermissionAsk {
		t.Fatalf("behavior = %q want ask", dec.Behavior)
	}
	// No plan → message still exists (just the prompt header).
	if dec.Message == "" {
		t.Fatal("message should not be empty even without plan file")
	}
}
