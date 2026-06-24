package repl

import (
	"encoding/json"
	"fmt"
	"net/url"

	"ccgo/internal/contracts"
	"ccgo/internal/native"
	"ccgo/internal/tool"
)

// toolSpecificDialogContent returns enriched dialog content for a permission
// ask request, matching the tool-specific UX from CC:
//   - Bash/PowerShell: show the full command text
//   - WebFetch: show the host/domain extracted from the URL
//   - Edit/Write: show a unified diff when old/new strings are available,
//     otherwise show the file path
//   - Other tools: fall through to the raw description
//
// PERM-TOOL-02: tool-specific permission dialog content.
// CC ref: src/components/permissions/BashPermissionRequest/BashPermissionRequest.tsx
//         src/components/permissions/FileEditPermissionRequest/FileEditPermissionRequest.tsx
func toolSpecificDialogContent(req tool.PermissionAskRequest) string {
	switch req.ToolName {
	case "Bash", "PowerShell":
		// Show the full command.
		if req.Description != "" {
			return fmt.Sprintf("Command: %s", req.Description)
		}
		return req.ToolName
	case "WebFetch":
		// Extract domain from description (usually the URL).
		if req.Description != "" {
			if u, err := url.Parse(req.Description); err == nil && u.Host != "" {
				return fmt.Sprintf("Host: %s\nURL: %s", u.Host, req.Description)
			}
		}
		// Fall back to raw description.
		if req.Description != "" {
			return req.Description
		}
		return "Fetch"
	case "Edit", "Write", "FileEdit", "FileWrite", "NotebookEdit", "SedEdit":
		// PERM-TOOL-02: render a unified diff when old/new content is available
		// in the Input map. This lets the user see what change they are approving.
		// CC ref: FileEditPermissionRequest shows the patch diff.
		if diff := editDiffFromInput(req); diff != "" {
			if req.Path != "" {
				return fmt.Sprintf("File: %s\n%s", req.Path, diff)
			}
			return diff
		}
		content := req.Path
		if content == "" {
			content = req.Description
		}
		if req.Path != "" && req.Description != "" && req.Path != req.Description {
			return fmt.Sprintf("File: %s\n%s", req.Path, req.Description)
		}
		return content
	default:
		if req.Description != "" {
			return req.Description
		}
		return req.ToolName
	}
}

// editDiffFromInput extracts old_string/new_string (or content for Write) from
// req.Input and returns a plain unified diff string for display in the
// permission dialog. Returns "" when not applicable.
func editDiffFromInput(req tool.PermissionAskRequest) string {
	if len(req.Input) == 0 {
		return ""
	}
	// Re-marshal the Input map to JSON so we can decode it into the typed struct.
	raw, err := json.Marshal(req.Input)
	if err != nil {
		return ""
	}
	var in editToolInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return ""
	}
	oldText := in.OldString
	newText := in.NewString
	if newText == "" && in.Content != "" {
		newText = in.Content
	}
	if oldText == "" && newText == "" {
		return ""
	}
	cd := native.BuildColorDiff(oldText, newText, native.ColorDiffOptions{
		Path:  in.FilePath,
		Color: false, // no ANSI in dialog content
	})
	if cd.Unified != "" {
		return cd.Unified
	}
	return ""
}

const (
	actionAllowOnce = "Allow once"
	actionDeny      = "Deny"
)

// scopeRequiredTools is the set of tools where a bare (unscoped) allow rule is
// dangerous: persisting "allow all Bash commands" or "allow all WebFetch hosts"
// is a least-privilege violation. For these tools we must have a concrete scope
// before writing any persistence rule.
var scopeRequiredTools = map[string]bool{
	"Bash":       true,
	"PowerShell": true,
	"WebFetch":   true,
	"WebSearch":  true,
}

// scopeRequired reports whether tool must have a concrete scope before we are
// allowed to write a persistence rule on its behalf.
func scopeRequired(toolName string) bool {
	return scopeRequiredTools[toolName]
}

// permActions is the action set for a tool's permission dialog plus the index
// of the persistence ("always") action.
type permActions struct {
	Actions     []string
	AlwaysIndex int
}

// permissionActions returns the per-tool dialog actions. All tools share the
// canonical Allow-once / Allow-always / Deny shape; the always-label text is
// tool-specific for parity with CC, but the persisted rule is uniform.
func permissionActions(req tool.PermissionAskRequest) permActions {
	always := alwaysLabel(req)
	return permActions{
		Actions:     []string{actionAllowOnce, always, actionDeny},
		AlwaysIndex: 1,
	}
}

func alwaysLabel(req tool.PermissionAskRequest) string {
	switch req.ToolName {
	case "Bash", "PowerShell":
		return "Allow always for this command"
	case "WebFetch":
		return "Allow always for this host"
	case "Edit", "Write", "FileEdit", "FileWrite", "NotebookEdit", "SedEdit", "Filesystem":
		return "Allow always for this session"
	default:
		return "Allow always for this tool"
	}
}

// decisionForAction maps a chosen action label to a PermissionDecision. The
// "always" action additionally carries a Suggestions update the loop persists,
// unless the tool is scope-required and no concrete scope could be derived — in
// that case the decision is allowed for this call only (no persistence rule is
// written, preventing a silent over-grant).
func decisionForAction(req tool.PermissionAskRequest, action string) contracts.PermissionDecision {
	switch action {
	case actionDeny:
		return contracts.PermissionDecision{Behavior: contracts.PermissionDeny}
	case actionAllowOnce:
		return contracts.PermissionDecision{Behavior: contracts.PermissionAllow}
	default:
		// Any non-deny, non-once action is the tool-specific "always" label.
		if scopeRequired(req.ToolName) && persistScope(req) == "" {
			// Cannot derive a scoped rule: persist nothing to avoid a broad
			// allow-all rule (e.g. bare "Bash" which grants every command).
			// The call is still allowed this turn.
			return contracts.PermissionDecision{
				Behavior: contracts.PermissionAllow,
				Message:  "Allowed for this call only (could not derive a rule to remember).",
			}
		}
		return contracts.PermissionDecision{
			Behavior:    contracts.PermissionAllow,
			Suggestions: []contracts.PermissionUpdate{persistUpdate(req)},
		}
	}
}

// persistUpdate builds the addRules update for an "always" choice. Rule content
// is the path/host scope when available; the rule defaults to the bare tool
// name when no narrower scope exists (matching CC's tool-level allow).
func persistUpdate(req tool.PermissionAskRequest) contracts.PermissionUpdate {
	rule := contracts.PermissionRuleValue{ToolName: req.ToolName}
	if scope := persistScope(req); scope != "" {
		rule.RuleContent = scope
	}
	return contracts.PermissionUpdate{
		Type:        "addRules",
		Destination: string(contracts.PermissionSourceLocalSettings),
		Behavior:    contracts.PermissionAllow,
		Rules:       []contracts.PermissionRuleValue{rule},
	}
}

func persistScope(req tool.PermissionAskRequest) string {
	// Prefer a rule suggested by the permission engine if present.
	if len(req.Decision.Suggestions) > 0 {
		for _, s := range req.Decision.Suggestions {
			if len(s.Rules) > 0 && s.Rules[0].RuleContent != "" {
				return s.Rules[0].RuleContent
			}
		}
	}
	return req.Path
}
