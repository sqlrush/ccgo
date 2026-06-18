package config

import (
	"strings"

	"ccgo/internal/contracts"
)

const (
	CustomizationSurfaceSkills = "skills"
	CustomizationSurfaceAgents = "agents"
	CustomizationSurfaceHooks  = "hooks"
	CustomizationSurfaceMCP    = "mcp"
)

func IsRestrictedToPluginOnly(settings contracts.Settings, surface string) bool {
	surface = strings.TrimSpace(surface)
	if surface == "" {
		return false
	}
	switch policy := settings.StrictPluginOnlyCustomization.(type) {
	case bool:
		return policy
	case []string:
		for _, item := range policy {
			if strings.TrimSpace(item) == surface {
				return true
			}
		}
	case []any:
		for _, item := range policy {
			if value, ok := item.(string); ok && strings.TrimSpace(value) == surface {
				return true
			}
		}
	}
	return false
}

func IsAdminTrustedCustomizationSource(source string) bool {
	switch strings.TrimSpace(source) {
	case "plugin", "policySettings", "built-in", "builtin", "bundled":
		return true
	default:
		return false
	}
}
