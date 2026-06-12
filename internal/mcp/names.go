package mcp

import (
	"strings"

	"ccgo/internal/contracts"
)

const (
	ToolNamePrefix       = "mcp"
	ClaudeAIServerPrefix = "claude.ai "
)

type ToolNameInfo struct {
	ServerName string
	ToolName   string
}

func NormalizeNameForMCP(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	normalized := b.String()
	if strings.HasPrefix(name, ClaudeAIServerPrefix) {
		normalized = collapseUnderscores(normalized)
		normalized = strings.Trim(normalized, "_")
	}
	return normalized
}

func MCPPrefix(serverName string) string {
	return ToolNamePrefix + "__" + NormalizeNameForMCP(serverName) + "__"
}

func BuildToolName(serverName string, toolName string) string {
	return MCPPrefix(serverName) + NormalizeNameForMCP(toolName)
}

func InfoFromToolName(toolName string) (ToolNameInfo, bool) {
	parts := strings.Split(toolName, "__")
	if len(parts) < 2 || parts[0] != ToolNamePrefix || parts[1] == "" {
		return ToolNameInfo{}, false
	}
	info := ToolNameInfo{ServerName: parts[1]}
	if len(parts) > 2 {
		info.ToolName = strings.Join(parts[2:], "__")
	}
	return info, true
}

func DisplayName(fullName string, serverName string) string {
	return strings.TrimPrefix(fullName, MCPPrefix(serverName))
}

func ExtractToolDisplayName(userFacingName string) string {
	withoutSuffix := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(userFacingName), "(MCP)"))
	if idx := strings.Index(withoutSuffix, " - "); idx >= 0 {
		return strings.TrimSpace(withoutSuffix[idx+3:])
	}
	return withoutSuffix
}

func ToolNameForPermissionCheck(definition contracts.ToolDefinition) string {
	if definition.MCP == nil {
		return definition.Name
	}
	return BuildToolName(definition.MCP.ServerName, definition.MCP.ToolName)
}

func collapseUnderscores(value string) string {
	var b strings.Builder
	previousUnderscore := false
	for _, r := range value {
		if r == '_' {
			if previousUnderscore {
				continue
			}
			previousUnderscore = true
		} else {
			previousUnderscore = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
