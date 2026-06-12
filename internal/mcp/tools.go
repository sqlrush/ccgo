package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

type RemoteTool struct {
	Name        string
	Description string
	InputSchema contracts.JSONSchema
	ReadOnly    bool
}

type Client interface {
	ListTools(ctx context.Context, serverName string) ([]RemoteTool, error)
	CallTool(ctx context.Context, serverName string, toolName string, input json.RawMessage) (any, error)
}

type ToolBuildOptions struct {
	ServerName     string
	Client         Client
	ResultStoreDir string
	MaxResultChars int
}

func BuildTools(ctx context.Context, options ToolBuildOptions) ([]tool.Tool, error) {
	if options.Client == nil {
		return nil, fmt.Errorf("mcp client is nil")
	}
	remoteTools, err := options.Client.ListTools(ctx, options.ServerName)
	if err != nil {
		return nil, err
	}
	tools := make([]tool.Tool, 0, len(remoteTools))
	for _, remote := range remoteTools {
		if remote.Name == "" {
			continue
		}
		tools = append(tools, buildTool(options, remote))
	}
	return tools, nil
}

func buildTool(options ToolBuildOptions, remote RemoteTool) tool.Tool {
	fullName := BuildToolName(options.ServerName, remote.Name)
	schema := remote.InputSchema
	if schema == nil {
		schema = contracts.JSONSchema{"type": "object"}
	}
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               fullName,
			Description:        remote.Description,
			InputSchema:        schema,
			ReadOnly:           remote.ReadOnly,
			ConcurrencySafe:    remote.ReadOnly,
			MaxResultSizeChars: options.MaxResultChars,
			MCP: &contracts.MCPToolRef{
				ServerName: options.ServerName,
				ToolName:   remote.Name,
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			result, err := options.Client.CallTool(ctx.Context, options.ServerName, remote.Name, raw)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			return ProcessToolResult(result, ResultOptions{
				ServerName:     options.ServerName,
				ToolName:       remote.Name,
				MaxChars:       options.MaxResultChars,
				ResultStoreDir: options.ResultStoreDir,
			})
		},
	}
}
