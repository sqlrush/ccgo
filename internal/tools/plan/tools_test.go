package plantools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestEnterPlanModeAllowsAndSignalsMode(t *testing.T) {
	toolImpl := NewEnterPlanModeTool()
	ctx := tool.Context{Context: context.Background()}
	dec, err := toolImpl.CheckPermissions(ctx, json.RawMessage(`{}`))
	if err != nil || dec.Behavior != contracts.PermissionAllow {
		t.Fatalf("CheckPermissions = %v, %v", dec.Behavior, err)
	}
	res, err := toolImpl.Call(ctx, json.RawMessage(`{}`), tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	if got, _ := res.StructuredContent["permission_mode"].(string); got != string(contracts.PermissionPlan) {
		t.Fatalf("permission_mode = %q want %q", got, contracts.PermissionPlan)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "plan mode") {
		t.Fatalf("Call content = %q", content)
	}
}

func TestExitPlanModeAsksThenApproves(t *testing.T) {
	dir := t.TempDir()
	if err := WritePlan(dir, "s1", "1. do the thing"); err != nil {
		t.Fatalf("WritePlan err: %v", err)
	}
	toolImpl := NewExitPlanModeTool()
	ctx := tool.Context{
		Context:   context.Background(),
		SessionID: "s1",
		Metadata:  map[string]any{tool.MetadataSessionPathKey: dir},
	}
	dec, err := toolImpl.CheckPermissions(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CheckPermissions err: %v", err)
	}
	if dec.Behavior != contracts.PermissionAsk {
		t.Fatalf("ExitPlanMode behavior = %q want ask", dec.Behavior)
	}
	// After approval the executor calls Call; it must echo the plan.
	res, err := toolImpl.Call(ctx, json.RawMessage(`{}`), tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "approved your plan") || !strings.Contains(content, "do the thing") {
		t.Fatalf("Call content = %q", content)
	}
}

func TestPlanRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sessionID := contracts.ID("sess-abc")
	plan := "Step 1: do X\nStep 2: do Y"
	if err := WritePlan(dir, sessionID, plan); err != nil {
		t.Fatalf("WritePlan err: %v", err)
	}
	got, err := ReadPlan(dir, sessionID)
	if err != nil {
		t.Fatalf("ReadPlan err: %v", err)
	}
	if got != plan {
		t.Fatalf("round-trip: got %q want %q", got, plan)
	}
}

func TestExitPlanModeSignalsRestoreMode(t *testing.T) {
	dir := t.TempDir()
	if err := WritePlan(dir, "s2", "my plan"); err != nil {
		t.Fatalf("WritePlan err: %v", err)
	}
	toolImpl := NewExitPlanModeTool()
	ctx := tool.Context{
		Context:   context.Background(),
		SessionID: "s2",
		Metadata:  map[string]any{tool.MetadataSessionPathKey: dir},
	}
	res, err := toolImpl.Call(ctx, json.RawMessage(`{}`), tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	if res.StructuredContent["restore_mode"] != true {
		t.Fatalf("restore_mode not set in StructuredContent: %v", res.StructuredContent)
	}
	if res.StructuredContent["type"] != "exit_plan_mode" {
		t.Fatalf("type not set in StructuredContent: %v", res.StructuredContent)
	}
}
