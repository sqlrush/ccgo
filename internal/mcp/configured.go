package mcp

import (
	"context"
	"path/filepath"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
)

type ConfiguredToolSetOptions struct {
	UserSettings    contracts.Settings
	ProjectSettings contracts.Settings
	LocalSettings   contracts.Settings
	PolicySettings  contracts.Settings
	PluginServers   map[string]contracts.MCPServer
	CWD             string
	ParseOptions    ParseOptions
	ToolOptions     ServerToolOptions
}

type ConfiguredToolSetResult struct {
	Servers    map[string]contracts.MCPServer
	LoadErrors []ValidationError
	Blocked    []string
	ToolSets   MultiServerToolSet
}

func BuildConfiguredToolSets(ctx context.Context, options ConfiguredToolSetOptions) (ConfiguredToolSetResult, error) {
	mcpLocked := config.IsRestrictedToPluginOnly(options.PolicySettings, config.CustomizationSurfaceMCP)
	var user LoadResult
	var err error
	if !mcpLocked {
		user, err = LoadSettingsServers(options.UserSettings, ScopeUser, options.ParseOptions)
	}
	if err != nil {
		return ConfiguredToolSetResult{}, err
	}
	var project LoadResult
	if !mcpLocked {
		project, err = LoadSettingsServers(options.ProjectSettings, ScopeProject, options.ParseOptions)
	}
	if err != nil {
		return ConfiguredToolSetResult{}, err
	}
	var local LoadResult
	if !mcpLocked {
		local, err = LoadSettingsServers(options.LocalSettings, ScopeLocal, options.ParseOptions)
	}
	if err != nil {
		return ConfiguredToolSetResult{}, err
	}

	loadErrors := append([]ValidationError{}, user.Errors...)
	loadErrors = append(loadErrors, project.Errors...)
	loadErrors = append(loadErrors, local.Errors...)

	projectServers := project.Servers
	if options.CWD != "" && !mcpLocked {
		projectChain, err := LoadProjectConfigChain(options.CWD, options.ParseOptions)
		if err != nil {
			return ConfiguredToolSetResult{}, err
		}
		loadErrors = append(loadErrors, projectChain.Errors...)
		// MCP-24: apply project-scope trust filter before merging .mcp.json
		// servers. Only connect servers that the user has explicitly approved
		// (via MCPServerApprovalDialog) unless enableAllProjectMcpServers=true.
		// Servers from settings.json project/user/local scopes are not filtered
		// here — only .mcp.json chain servers are subject to approval.
		// CC ref: src/services/mcpServerApproval.tsx.
		projectChain.Servers = filterProjectMCPServers(projectChain.Servers, options.LocalSettings)
		projectServers = MergeServers(projectServers, projectChain.Servers)
	}

	policySettings := options.PolicySettings
	if !mcpLocked {
		policySettings = mergeMCPPolicySettings(options.UserSettings, options.ProjectSettings, options.LocalSettings, options.PolicySettings)
	}
	manual := MergeManualConfigSources(ManualConfigSources{
		User:    user.Servers,
		Project: projectServers,
		Local:   local.Servers,
		Plugin:  options.PluginServers,
		Policy:  PolicyFromSettings(policySettings),
	})
	toolOptions := options.ToolOptions
	if len(toolOptions.ClientRoots) == 0 && options.CWD != "" {
		root, err := FileRoot(options.CWD, filepath.Base(options.CWD))
		if err != nil {
			return ConfiguredToolSetResult{}, err
		}
		toolOptions.ClientRoots = []Root{root}
	}
	toolsets := BuildServerToolSets(ctx, manual.Servers, toolOptions)
	return ConfiguredToolSetResult{
		Servers:    manual.Servers,
		LoadErrors: loadErrors,
		Blocked:    manual.Blocked,
		ToolSets:   toolsets,
	}, nil
}

// filterProjectMCPServers applies the enabledMcpjsonServers / enableAllProjectMcpServers
// trust filter to project-scope (.mcp.json) servers.
//
// Behaviour (mirrors CC src/services/mcpServerApproval.tsx):
//   - enableAllProjectMcpServers=true  → allow all servers (no filter)
//   - enabledMcpjsonServers=[…]        → allow only the listed server names
//   - both absent / empty              → block all .mcp.json servers (not yet approved)
//   - a server listed in disabledMcpjsonServers → always blocked
//
// CC ref: src/services/mcpServerApproval.tsx (isMCPServerTrusted).
func filterProjectMCPServers(servers map[string]contracts.MCPServer, local contracts.Settings) map[string]contracts.MCPServer {
	if len(servers) == 0 {
		return servers
	}
	// Build disabled set.
	disabled := make(map[string]bool, len(local.DisabledMCPJSONServers))
	for _, name := range local.DisabledMCPJSONServers {
		disabled[name] = true
	}
	// If enableAllProjectMcpServers=true, only honour the disabled list.
	if local.EnableAllProjectMCPServers != nil && *local.EnableAllProjectMCPServers {
		if len(disabled) == 0 {
			return servers
		}
		out := make(map[string]contracts.MCPServer, len(servers))
		for name, srv := range servers {
			if !disabled[name] {
				out[name] = srv
			}
		}
		return out
	}
	// Use the explicit allow-list.
	enabled := make(map[string]bool, len(local.EnabledMCPJSONServers))
	for _, name := range local.EnabledMCPJSONServers {
		enabled[name] = true
	}
	out := make(map[string]contracts.MCPServer, len(servers))
	for name, srv := range servers {
		if enabled[name] && !disabled[name] {
			out[name] = srv
		}
	}
	return out
}

func mergeMCPPolicySettings(settings ...contracts.Settings) contracts.Settings {
	var out contracts.Settings
	for _, setting := range settings {
		if setting.AllowedMCPServers != nil && out.AllowedMCPServers == nil {
			out.AllowedMCPServers = []contracts.MCPServerPolicyEntry{}
		}
		out.AllowedMCPServers = append(out.AllowedMCPServers, setting.AllowedMCPServers...)
		out.DeniedMCPServers = append(out.DeniedMCPServers, setting.DeniedMCPServers...)
	}
	return out
}
