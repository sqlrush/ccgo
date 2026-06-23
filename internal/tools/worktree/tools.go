package worktreetools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const (
	MetadataWorktreeOriginalCWD = "ccgo.worktree.original_cwd"
	MetadataWorktreePath        = "ccgo.worktree.path"
)

var validSegment = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

type enterWorktreeInput struct {
	Name string `json:"name,omitempty"`
}

type exitWorktreeInput struct {
	Action         string `json:"action"`
	DiscardChanges bool   `json:"discard_changes,omitempty"`
}

func validateWorktreeSlug(name string) error {
	if len(name) > 64 {
		return fmt.Errorf("name must be at most 64 characters")
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" {
			return fmt.Errorf("name must not have empty path segments")
		}
		if !validSegment.MatchString(segment) {
			return fmt.Errorf("name segment %q contains invalid characters (allowed: letters, digits, dots, underscores, dashes)", segment)
		}
	}
	return nil
}

func gitRoot(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// NewEnterWorktreeTool returns the EnterWorktree tool that creates an isolated
// git worktree and returns the new working directory path in StructuredContent.
func NewEnterWorktreeTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "EnterWorktree",
			Description: "Create and enter an isolated git worktree for the current session.",
			SearchHint:  "enter worktree isolated branch",
			ReadOnly:    false,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Optional slug for the worktree directory name (letters, digits, dots, underscores, dashes; max 64 chars).",
					},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Creates an isolated git worktree and switches the session working directory to it. Fails if already in a worktree. Use ExitWorktree to leave.", nil
		},
		PermissionFunc: func(_ tool.Context, _ json.RawMessage) (contracts.PermissionDecision, error) {
			return contracts.PermissionDecision{
				Behavior:       contracts.PermissionAllow,
				DecisionReason: "EnterWorktree always allowed",
			}, nil
		},
		ValidateFunc: func(_ tool.Context, raw json.RawMessage) error {
			var input enterWorktreeInput
			if err := json.Unmarshal(raw, &input); err != nil {
				return fmt.Errorf("invalid input: %w", err)
			}
			if input.Name != "" {
				return validateWorktreeSlug(input.Name)
			}
			return nil
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			if ctx.Metadata != nil {
				if existing, ok := ctx.Metadata[MetadataWorktreePath].(string); ok && existing != "" {
					return contracts.ToolResult{}, fmt.Errorf("already in a worktree at %s; call ExitWorktree first", existing)
				}
			}
			var input enterWorktreeInput
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, fmt.Errorf("invalid input: %w", err)
			}
			cwd := ctx.WorkingDirectory
			root, err := gitRoot(cwd)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			name := input.Name
			if name == "" {
				name = string(contracts.NewID())
			}
			sessionSlug := "session"
			if ctx.SessionID != "" {
				sessionSlug = string(ctx.SessionID)
			}
			worktreesDir := filepath.Join(filepath.Dir(root), ".ccgo-worktrees")
			if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
				return contracts.ToolResult{}, fmt.Errorf("creating worktrees directory: %w", err)
			}
			worktreePath := filepath.Join(worktreesDir, sessionSlug+"-"+name)
			addCmd := exec.CommandContext(ctx.Context, "git", "-C", root, "worktree", "add", "--detach", worktreePath, "HEAD")
			if out, err := addCmd.CombinedOutput(); err != nil {
				return contracts.ToolResult{}, fmt.Errorf("git worktree add failed: %w\n%s", err, strings.TrimSpace(string(out)))
			}
			branchCmd := exec.CommandContext(ctx.Context, "git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
			branchOut, _ := branchCmd.Output()
			branch := strings.TrimSpace(string(branchOut))
			msg := fmt.Sprintf("Created worktree at %s (branch: %s). Working directory switched.", worktreePath, branch)
			return contracts.ToolResult{
				Content: msg,
				StructuredContent: map[string]any{
					"worktree_path":   worktreePath,
					"worktree_branch": branch,
					"message":         msg,
					"original_cwd":    cwd,
				},
			}, nil
		},
		ReadOnlyFunc:    func(json.RawMessage) bool { return false },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

// NewExitWorktreeTool returns the ExitWorktree tool that exits the current
// worktree and optionally removes it.
func NewExitWorktreeTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "ExitWorktree",
			Description: "Exit the current git worktree and optionally remove it.",
			SearchHint:  "exit worktree leave cleanup",
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"action"},
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []any{"keep", "remove"},
						"description": "keep: leave the worktree on disk. remove: delete the worktree.",
					},
					"discard_changes": map[string]any{
						"type":        "boolean",
						"description": "When true with action=remove, discard uncommitted changes and unpushed commits.",
					},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Exits the current git worktree and restores the original working directory. Use action=keep to leave the worktree on disk, or action=remove to delete it. Uncommitted changes or unpushed commits block removal unless discard_changes=true.", nil
		},
		PermissionFunc: func(_ tool.Context, _ json.RawMessage) (contracts.PermissionDecision, error) {
			return contracts.PermissionDecision{
				Behavior:       contracts.PermissionAllow,
				DecisionReason: "ExitWorktree always allowed",
			}, nil
		},
		ValidateFunc: func(ctx tool.Context, raw json.RawMessage) error {
			var input exitWorktreeInput
			if err := json.Unmarshal(raw, &input); err != nil {
				return fmt.Errorf("invalid input: %w", err)
			}
			if input.Action == "" {
				return fmt.Errorf("action is required (keep or remove)")
			}
			if input.Action != "keep" && input.Action != "remove" {
				return fmt.Errorf("action must be keep or remove, got %q", input.Action)
			}
			if ctx.Metadata == nil {
				return fmt.Errorf("not in a worktree")
			}
			path, _ := ctx.Metadata[MetadataWorktreePath].(string)
			if path == "" {
				return fmt.Errorf("not in a worktree")
			}
			return nil
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			var input exitWorktreeInput
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, fmt.Errorf("invalid input: %w", err)
			}
			worktreePath, _ := ctx.Metadata[MetadataWorktreePath].(string)
			originalCWD, _ := ctx.Metadata[MetadataWorktreeOriginalCWD].(string)
			if originalCWD == "" {
				originalCWD = ctx.WorkingDirectory
			}
			if input.Action == "remove" {
				if err := checkWorktreeClean(ctx, worktreePath, input.DiscardChanges); err != nil {
					return contracts.ToolResult{}, err
				}
				root, err := gitRoot(worktreePath)
				if err != nil {
					return contracts.ToolResult{}, err
				}
				removeCmd := exec.CommandContext(ctx.Context, "git", "-C", root, "worktree", "remove", "--force", worktreePath)
				if out, err := removeCmd.CombinedOutput(); err != nil {
					return contracts.ToolResult{}, fmt.Errorf("git worktree remove failed: %w\n%s", err, strings.TrimSpace(string(out)))
				}
			}
			msg := fmt.Sprintf("Exited worktree %s (action=%s). Working directory restored to %s.", worktreePath, input.Action, originalCWD)
			return contracts.ToolResult{
				Content: msg,
				StructuredContent: map[string]any{
					"original_cwd":  originalCWD,
					"worktree_path": worktreePath,
					"action":        input.Action,
					"message":       msg,
				},
			}, nil
		},
		ReadOnlyFunc:    func(json.RawMessage) bool { return false },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
		DestructiveFunc: func(raw json.RawMessage) bool {
			var input exitWorktreeInput
			if err := json.Unmarshal(raw, &input); err != nil {
				return false
			}
			return input.Action == "remove"
		},
	}
}

func checkWorktreeClean(ctx tool.Context, worktreePath string, discardChanges bool) error {
	if discardChanges {
		return nil
	}
	statusCmd := exec.CommandContext(ctx.Context, "git", "-C", worktreePath, "status", "--porcelain")
	statusOut, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("checking worktree status: %w", err)
	}
	var issues []string
	if len(strings.TrimSpace(string(statusOut))) > 0 {
		for _, line := range strings.Split(strings.TrimSpace(string(statusOut)), "\n") {
			if strings.TrimSpace(line) != "" {
				issues = append(issues, "  uncommitted: "+strings.TrimSpace(line))
			}
		}
	}
	revCmd := exec.CommandContext(ctx.Context, "git", "-C", worktreePath, "rev-list", "--count", "@{upstream}..HEAD")
	revOut, revErr := revCmd.Output()
	if revErr == nil {
		count := strings.TrimSpace(string(revOut))
		if count != "" && count != "0" {
			issues = append(issues, fmt.Sprintf("  %s unpushed commit(s)", count))
		}
	}
	if len(issues) > 0 {
		return fmt.Errorf("worktree has unsaved changes; use discard_changes=true to force removal:\n%s", strings.Join(issues, "\n"))
	}
	return nil
}
