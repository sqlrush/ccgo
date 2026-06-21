package repl

import (
	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

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
