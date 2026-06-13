package mcp

import (
	"context"
	"path/filepath"

	"ccgo/internal/contracts"
)

type ConfiguredToolSetOptions struct {
	UserSettings    contracts.Settings
	ProjectSettings contracts.Settings
	LocalSettings   contracts.Settings
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
	user, err := LoadSettingsServers(options.UserSettings, ScopeUser, options.ParseOptions)
	if err != nil {
		return ConfiguredToolSetResult{}, err
	}
	project, err := LoadSettingsServers(options.ProjectSettings, ScopeProject, options.ParseOptions)
	if err != nil {
		return ConfiguredToolSetResult{}, err
	}
	local, err := LoadSettingsServers(options.LocalSettings, ScopeLocal, options.ParseOptions)
	if err != nil {
		return ConfiguredToolSetResult{}, err
	}

	loadErrors := append([]ValidationError{}, user.Errors...)
	loadErrors = append(loadErrors, project.Errors...)
	loadErrors = append(loadErrors, local.Errors...)

	projectServers := project.Servers
	if options.CWD != "" {
		projectChain, err := LoadProjectConfigChain(options.CWD, options.ParseOptions)
		if err != nil {
			return ConfiguredToolSetResult{}, err
		}
		loadErrors = append(loadErrors, projectChain.Errors...)
		projectServers = MergeServers(projectServers, projectChain.Servers)
	}

	manual := MergeManualConfigSources(ManualConfigSources{
		User:    user.Servers,
		Project: projectServers,
		Local:   local.Servers,
		Plugin:  options.PluginServers,
		Policy:  PolicyFromSettings(mergeMCPPolicySettings(options.UserSettings, options.ProjectSettings, options.LocalSettings)),
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
