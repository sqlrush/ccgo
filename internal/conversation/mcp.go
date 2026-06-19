package conversation

import (
	"context"

	"ccgo/internal/mcp"
	"ccgo/internal/tool"
)

func (r Runner) withConfiguredMCPTools(ctx context.Context) (Runner, func() error, error) {
	if r.MCP == nil {
		return r, nil, nil
	}

	options := mcp.ConfiguredToolSetOptions{
		UserSettings:    r.MCP.UserSettings,
		ProjectSettings: r.MCP.ProjectSettings,
		LocalSettings:   r.MCP.LocalSettings,
		PolicySettings:  r.MCP.PolicySettings,
		PluginServers:   r.MCP.PluginServers,
		CWD:             r.MCP.CWD,
		ParseOptions:    r.MCP.ParseOptions,
		ToolOptions:     r.MCP.ToolOptions,
	}
	if options.CWD == "" {
		options.CWD = r.WorkingDirectory
	}

	configured, err := mcp.BuildConfiguredToolSets(ctx, options)
	if err != nil {
		return r, nil, err
	}
	closeMCP := configured.ToolSets.Close
	if len(configured.ToolSets.Servers) > 0 {
		resetDeferredToolTokenCountCache()
		closeMCP = func() error {
			err := configured.ToolSets.Close()
			resetDeferredToolTokenCountCache()
			return err
		}
	}
	if len(configured.ToolSets.Tools) == 0 {
		return r, closeMCP, nil
	}

	registry, err := mergeToolRegistry(r.Tools.Registry, configured.ToolSets.Tools)
	if err != nil {
		_ = closeMCP()
		return r, nil, err
	}
	r.Tools.Registry = registry
	return r, closeMCP, nil
}

func mergeToolRegistry(base *tool.Registry, extra []tool.Tool) (*tool.Registry, error) {
	if len(extra) == 0 {
		return base, nil
	}
	tools := registryTools(base)
	tools = append(tools, extra...)
	return tool.NewRegistry(tools...)
}

func registryTools(registry *tool.Registry) []tool.Tool {
	if registry == nil {
		return nil
	}
	names := registry.Names()
	tools := make([]tool.Tool, 0, len(names))
	for _, name := range names {
		t, ok := registry.Lookup(name)
		if !ok {
			continue
		}
		tools = append(tools, t)
	}
	return tools
}
