package config

import "fmt"

// MCPApprovalAction is the decision from a MCPServerApprovalDialog submission.
// CC ref: src/services/mcpServerApproval.tsx.
type MCPApprovalAction string

const (
	// MCPApprovalYesAll approves all current and future project MCP servers.
	MCPApprovalYesAll MCPApprovalAction = "yes_all"
	// MCPApprovalYes approves a single server for this session.
	MCPApprovalYes MCPApprovalAction = "yes"
	// MCPApprovalNo declines a single server.
	MCPApprovalNo MCPApprovalAction = "no"
)

// ApplyMCPApproval persists a project-scope MCP trust decision to the local
// settings file at path (typically .claude/settings.local.json).
//
//   - MCPApprovalYesAll: sets enableAllProjectMcpServers=true
//   - MCPApprovalYes:    appends serverName to enabledMcpjsonServers
//   - MCPApprovalNo:     appends serverName to disabledMcpjsonServers
//
// CC ref: src/services/mcpServerApproval.tsx (saveCurrentProjectConfig calls).
func ApplyMCPApproval(path string, action MCPApprovalAction, serverName string) error {
	if path == "" {
		return fmt.Errorf("settings path is required")
	}
	doc, err := ReadSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("read settings %s: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	updated := copyMap(doc)
	switch action {
	case MCPApprovalYesAll:
		updated["enableAllProjectMcpServers"] = true
	case MCPApprovalYes:
		updated["enabledMcpjsonServers"] = appendUniqueString(stringSliceFromDoc(doc, "enabledMcpjsonServers"), serverName)
	case MCPApprovalNo:
		updated["disabledMcpjsonServers"] = appendUniqueString(stringSliceFromDoc(doc, "disabledMcpjsonServers"), serverName)
	default:
		return fmt.Errorf("unknown MCP approval action %q", action)
	}
	if err := WriteSettingsDocument(path, updated); err != nil {
		return fmt.Errorf("write settings %s: %w", path, err)
	}
	return nil
}

// copyMap creates a shallow copy of a map[string]any.
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// stringSliceFromDoc extracts a []string from a document key, returning nil
// when the key is absent or has wrong type.
func stringSliceFromDoc(doc map[string]any, key string) []string {
	raw, ok := doc[key]
	if !ok {
		return nil
	}
	slice, _ := raw.([]any)
	out := make([]string, 0, len(slice))
	for _, item := range slice {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ResetMCPProjectChoices removes the three project-level MCP approval fields
// from the local settings file at path:
//
//   - enableAllProjectMcpServers
//   - enabledMcpjsonServers
//   - disabledMcpjsonServers
//
// Other fields are preserved. If the file does not exist, the call succeeds
// without writing anything.
//
// CC ref: src/main.tsx:3953 (mcpResetProjectChoicesHandler).
func ResetMCPProjectChoices(path string) error {
	if path == "" {
		return fmt.Errorf("settings path is required")
	}
	doc, err := ReadSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("read settings %s: %w", path, err)
	}
	if doc == nil {
		return nil
	}
	// Build a new map without the three approval keys (immutable pattern).
	const (
		keyAll      = "enableAllProjectMcpServers"
		keyEnabled  = "enabledMcpjsonServers"
		keyDisabled = "disabledMcpjsonServers"
	)
	_, hadAll := doc[keyAll]
	_, hadEnabled := doc[keyEnabled]
	_, hadDisabled := doc[keyDisabled]
	if !hadAll && !hadEnabled && !hadDisabled {
		// Nothing to remove; skip the write.
		return nil
	}
	updated := make(map[string]any, len(doc))
	for k, v := range doc {
		if k == keyAll || k == keyEnabled || k == keyDisabled {
			continue
		}
		updated[k] = v
	}
	if err := WriteSettingsDocument(path, updated); err != nil {
		return fmt.Errorf("write settings %s: %w", path, err)
	}
	return nil
}

// appendUniqueString appends value to slice only if not already present.
func appendUniqueString(slice []string, value string) []string {
	for _, s := range slice {
		if s == value {
			return slice
		}
	}
	// Return a new slice (immutable pattern).
	out := make([]string, len(slice)+1)
	copy(out, slice)
	out[len(slice)] = value
	return out
}
