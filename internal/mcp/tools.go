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

type RemoteResource struct {
	URI         string
	Name        string
	Description string
	MimeType    string
}

type ResourceContent struct {
	URI      string
	MimeType string
	Text     string
	Blob     string
}

type Client interface {
	ListTools(ctx context.Context, serverName string) ([]RemoteTool, error)
	CallTool(ctx context.Context, serverName string, toolName string, input json.RawMessage) (any, error)
	ListResources(ctx context.Context, serverName string) ([]RemoteResource, error)
	ReadResource(ctx context.Context, serverName string, uri string) ([]ResourceContent, error)
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

func BuildResourceTools(options ToolBuildOptions) []tool.Tool {
	return []tool.Tool{
		buildListResourcesTool(options),
		buildReadResourceTool(options),
	}
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

func buildListResourcesTool(options ToolBuildOptions) tool.Tool {
	name := BuildToolName(options.ServerName, "list_resources")
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            name,
			Description:     "List MCP resources for " + options.ServerName,
			InputSchema:     contracts.JSONSchema{"type": "object"},
			ReadOnly:        true,
			ConcurrencySafe: true,
			MCP: &contracts.MCPToolRef{
				ServerName: options.ServerName,
				ToolName:   "list_resources",
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			if options.Client == nil {
				return contracts.ToolResult{}, fmt.Errorf("mcp client is nil")
			}
			resources, err := options.Client.ListResources(ctx.Context, options.ServerName)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			return ProcessToolResult(map[string]any{"structuredContent": resourcesToStructuredContent(resources)}, ResultOptions{
				ServerName:     options.ServerName,
				ToolName:       "list_resources",
				MaxChars:       options.MaxResultChars,
				ResultStoreDir: options.ResultStoreDir,
			})
		},
	}
}

func buildReadResourceTool(options ToolBuildOptions) tool.Tool {
	name := BuildToolName(options.ServerName, "read_resource")
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        name,
			Description: "Read an MCP resource from " + options.ServerName,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []string{"uri"},
				"properties": map[string]any{
					"uri": map[string]any{"type": "string"},
				},
			},
			ReadOnly:        true,
			ConcurrencySafe: true,
			MCP: &contracts.MCPToolRef{
				ServerName: options.ServerName,
				ToolName:   "read_resource",
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			if options.Client == nil {
				return contracts.ToolResult{}, fmt.Errorf("mcp client is nil")
			}
			var input struct {
				URI string `json:"uri"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, err
			}
			if input.URI == "" {
				return contracts.ToolResult{}, fmt.Errorf("uri is required")
			}
			contents, err := options.Client.ReadResource(ctx.Context, options.ServerName, input.URI)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			return ProcessToolResult(map[string]any{"content": resourceContentsToMCPContent(contents)}, ResultOptions{
				ServerName:     options.ServerName,
				ToolName:       "read_resource",
				MaxChars:       options.MaxResultChars,
				ResultStoreDir: options.ResultStoreDir,
			})
		},
	}
}

func resourcesToStructuredContent(resources []RemoteResource) []map[string]any {
	out := make([]map[string]any, 0, len(resources))
	for _, resource := range resources {
		out = append(out, map[string]any{
			"uri":         resource.URI,
			"name":        resource.Name,
			"description": resource.Description,
			"mimeType":    resource.MimeType,
		})
	}
	return out
}

func resourceContentsToMCPContent(contents []ResourceContent) []any {
	out := make([]any, 0, len(contents))
	for _, content := range contents {
		resource := map[string]any{
			"uri":      content.URI,
			"mimeType": content.MimeType,
		}
		if content.Text != "" {
			resource["text"] = content.Text
		}
		if content.Blob != "" {
			resource["blob"] = content.Blob
		}
		out = append(out, map[string]any{
			"type":     "resource",
			"resource": resource,
		})
	}
	return out
}
