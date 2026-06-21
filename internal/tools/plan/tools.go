package plantools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// PlanFilePath returns where the active plan markdown is stored for a session.
// CC reads the plan from disk in ExitPlanMode (ExitPlanModeV2Tool.ts:246).
func PlanFilePath(sessionPath string, sessionID contracts.ID) string {
	dir := strings.TrimSpace(sessionPath)
	if dir == "" {
		dir = "."
	}
	name := string(sessionID)
	if name == "" {
		name = "plan"
	}
	return filepath.Join(dir, name+".plan.md")
}

// WritePlan persists the plan markdown for a session to disk.
func WritePlan(sessionPath string, sessionID contracts.ID, plan string) error {
	path := PlanFilePath(sessionPath, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create plan dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(plan), 0o600); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}
	return nil
}

// ReadPlan reads the persisted plan markdown for a session from disk.
// Returns empty string (no error) when no plan file exists yet.
func ReadPlan(sessionPath string, sessionID contracts.ID) (string, error) {
	data, err := os.ReadFile(PlanFilePath(sessionPath, sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read plan: %w", err)
	}
	return string(data), nil
}

// NewEnterPlanModeTool returns a Tool that signals entry into plan mode.
// CheckPermissions returns Allow (no user gate needed to enter plan mode).
// Call emits permission_mode=plan in StructuredContent — the runner applies
// the mode switch (Phase 2); this tool merely signals the intent.
func NewEnterPlanModeTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:                "EnterPlanMode",
			Description:         "Requests permission to enter plan mode for complex tasks requiring exploration and design.",
			ReadOnly:            true,
			RequiresInteraction: true,
			InputSchema:         contracts.JSONSchema{"type": "object", "properties": map[string]any{}},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Enters plan mode. In plan mode you focus on exploring and designing; DO NOT write or edit any files. Write the plan to disk, then call ExitPlanMode to request approval to start coding.", nil
		},
		PermissionFunc: func(_ tool.Context, _ json.RawMessage) (contracts.PermissionDecision, error) {
			return contracts.PermissionDecision{
				Behavior:       contracts.PermissionAllow,
				DecisionReason: "entering plan mode",
			}, nil
		},
		CallFunc: func(_ tool.Context, _ json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{
				Content: "Entered plan mode. Focus on exploring and designing. DO NOT write or edit any files yet. Write your plan, then call ExitPlanMode.",
				StructuredContent: map[string]any{
					"type":            "enter_plan_mode",
					"permission_mode": string(contracts.PermissionPlan),
				},
			}, nil
		},
		ReadOnlyFunc:    func(_ json.RawMessage) bool { return true },
		ConcurrencyFunc: func(_ json.RawMessage) bool { return false },
	}
}

// NewExitPlanModeTool returns a Tool that requests exit from plan mode.
// CheckPermissions returns Ask with message "Exit plan mode?" so the
// executor's Asker runs the approval ceremony (Phase 2 supplies the rich
// plan-preview dialog).
// Call reads the plan from disk and returns the approval message; it signals
// restore_mode=true in StructuredContent so the runner can restore PrePlanMode.
// The mode switch application and UI indicator are Phase 2's responsibility.
func NewExitPlanModeTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:                "ExitPlanMode",
			Description:         "Prompts the user to review and approve your plan, then exits plan mode to start coding.",
			RequiresInteraction: true,
			InputSchema:         contracts.JSONSchema{"type": "object", "properties": map[string]any{}},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Requests approval to exit plan mode and begin coding. This tool does NOT take the plan as a parameter — it reads the plan you wrote to disk. Only call it when you have finished planning.", nil
		},
		PermissionFunc: func(_ tool.Context, _ json.RawMessage) (contracts.PermissionDecision, error) {
			// Ask routes through Executor.Asker; Phase 2 renders the plan preview.
			return contracts.PermissionDecision{
				Behavior: contracts.PermissionAsk,
				Message:  "Exit plan mode?",
			}, nil
		},
		CallFunc: func(ctx tool.Context, _ json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			sessionPath, _ := ctx.Metadata[tool.MetadataSessionPathKey].(string)
			plan, err := ReadPlan(sessionPath, ctx.SessionID)
			if err != nil {
				return contracts.ToolResult{}, fmt.Errorf("exit plan mode: %w", err)
			}
			content := "User has approved your plan. You can now start coding."
			if strings.TrimSpace(plan) != "" {
				content += "\n\nApproved plan:\n" + plan
			}
			return contracts.ToolResult{
				Content: content,
				StructuredContent: map[string]any{
					"type":         "exit_plan_mode",
					"restore_mode": true, // runner restores PrePlanMode
					"plan":         plan,
				},
			}, nil
		},
		ReadOnlyFunc:    func(_ json.RawMessage) bool { return false },
		ConcurrencyFunc: func(_ json.RawMessage) bool { return false },
	}
}
