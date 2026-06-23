package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

type RemoteTool struct {
	Name         string
	Description  string
	InputSchema  contracts.JSONSchema
	OutputSchema contracts.JSONSchema
	ReadOnly     bool
	Destructive  bool
}

type RemoteResource struct {
	URI         string
	Name        string
	Description string
	MimeType    string
}

type RemoteResourceTemplate struct {
	URITemplate string
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

type RemotePrompt struct {
	Name        string
	Description string
	Arguments   []PromptArgument
}

type PromptArgument struct {
	Name        string
	Description string
	Required    bool
}

type PromptResult struct {
	Description string
	Messages    []PromptMessage
}

type PromptMessage struct {
	Role    string
	Content any
}

type Client interface {
	ListTools(ctx context.Context, serverName string) ([]RemoteTool, error)
	CallTool(ctx context.Context, serverName string, toolName string, input json.RawMessage) (any, error)
	ListResources(ctx context.Context, serverName string) ([]RemoteResource, error)
	ListResourceTemplates(ctx context.Context, serverName string) ([]RemoteResourceTemplate, error)
	ReadResource(ctx context.Context, serverName string, uri string) ([]ResourceContent, error)
	SubscribeResource(ctx context.Context, serverName string, uri string) error
	ListPrompts(ctx context.Context, serverName string) ([]RemotePrompt, error)
	GetPrompt(ctx context.Context, serverName string, promptName string, arguments map[string]string) (PromptResult, error)
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
		buildListResourceTemplatesTool(options),
		buildReadResourceTool(options),
		buildSubscribeResourceTool(options),
	}
}

func BuildPromptTools(options ToolBuildOptions) []tool.Tool {
	return []tool.Tool{
		buildListPromptsTool(options),
		buildGetPromptTool(options),
	}
}

func buildTool(options ToolBuildOptions, remote RemoteTool) tool.Tool {
	fullName := BuildToolName(options.ServerName, remote.Name)
	schema := remote.InputSchema
	if schema == nil {
		schema = contracts.JSONSchema{"type": "object"}
	}
	// MCP-49: truncate tool description to MaxMCPDescriptionLength.
	// CC ref: src/services/mcp/client.ts:1791-1793.
	description := TruncateMCPText(remote.Description)
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               fullName,
			Description:        description,
			InputSchema:        schema,
			OutputSchema:       remote.OutputSchema,
			ReadOnly:           remote.ReadOnly,
			Destructive:        remote.Destructive,
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

func buildListResourceTemplatesTool(options ToolBuildOptions) tool.Tool {
	name := BuildToolName(options.ServerName, "list_resource_templates")
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            name,
			Description:     "List MCP resource templates for " + options.ServerName,
			InputSchema:     contracts.JSONSchema{"type": "object"},
			ReadOnly:        true,
			ConcurrencySafe: true,
			MCP: &contracts.MCPToolRef{
				ServerName: options.ServerName,
				ToolName:   "list_resource_templates",
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			if options.Client == nil {
				return contracts.ToolResult{}, fmt.Errorf("mcp client is nil")
			}
			templates, err := options.Client.ListResourceTemplates(ctx.Context, options.ServerName)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			return ProcessToolResult(map[string]any{"structuredContent": resourceTemplatesToStructuredContent(templates)}, ResultOptions{
				ServerName:     options.ServerName,
				ToolName:       "list_resource_templates",
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
			input.URI = strings.TrimSpace(input.URI)
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

func buildSubscribeResourceTool(options ToolBuildOptions) tool.Tool {
	name := BuildToolName(options.ServerName, "subscribe_resource")
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        name,
			Description: "Subscribe to MCP resource updates from " + options.ServerName,
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
				ToolName:   "subscribe_resource",
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
			input.URI = strings.TrimSpace(input.URI)
			if input.URI == "" {
				return contracts.ToolResult{}, fmt.Errorf("uri is required")
			}
			if err := options.Client.SubscribeResource(ctx.Context, options.ServerName, input.URI); err != nil {
				return contracts.ToolResult{}, err
			}
			return ProcessToolResult(map[string]any{
				"structuredContent": map[string]any{
					"uri":        input.URI,
					"subscribed": true,
				},
			}, ResultOptions{
				ServerName:     options.ServerName,
				ToolName:       "subscribe_resource",
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

func resourceTemplatesToStructuredContent(templates []RemoteResourceTemplate) []map[string]any {
	out := make([]map[string]any, 0, len(templates))
	for _, template := range templates {
		out = append(out, map[string]any{
			"uriTemplate": template.URITemplate,
			"name":        template.Name,
			"description": template.Description,
			"mimeType":    template.MimeType,
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

func buildListPromptsTool(options ToolBuildOptions) tool.Tool {
	name := BuildToolName(options.ServerName, "list_prompts")
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            name,
			Description:     "List MCP prompts for " + options.ServerName,
			InputSchema:     contracts.JSONSchema{"type": "object"},
			ReadOnly:        true,
			ConcurrencySafe: true,
			MCP: &contracts.MCPToolRef{
				ServerName: options.ServerName,
				ToolName:   "list_prompts",
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			if options.Client == nil {
				return contracts.ToolResult{}, fmt.Errorf("mcp client is nil")
			}
			prompts, err := options.Client.ListPrompts(ctx.Context, options.ServerName)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			return ProcessToolResult(map[string]any{"structuredContent": promptsToStructuredContent(prompts)}, ResultOptions{
				ServerName:     options.ServerName,
				ToolName:       "list_prompts",
				MaxChars:       options.MaxResultChars,
				ResultStoreDir: options.ResultStoreDir,
			})
		},
	}
}

func buildGetPromptTool(options ToolBuildOptions) tool.Tool {
	name := BuildToolName(options.ServerName, "get_prompt")
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        name,
			Description: "Get an MCP prompt from " + options.ServerName,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"arguments": map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string"},
					},
				},
			},
			ReadOnly:        true,
			ConcurrencySafe: true,
			MCP: &contracts.MCPToolRef{
				ServerName: options.ServerName,
				ToolName:   "get_prompt",
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			if options.Client == nil {
				return contracts.ToolResult{}, fmt.Errorf("mcp client is nil")
			}
			var input struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, err
			}
			input.Name = strings.TrimSpace(input.Name)
			if input.Name == "" {
				return contracts.ToolResult{}, fmt.Errorf("name is required")
			}
			if input.Arguments == nil {
				input.Arguments = map[string]string{}
			}
			result, err := options.Client.GetPrompt(ctx.Context, options.ServerName, input.Name, input.Arguments)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			return ProcessToolResult(map[string]any{"structuredContent": promptResultToStructuredContent(result)}, ResultOptions{
				ServerName:     options.ServerName,
				ToolName:       "get_prompt",
				MaxChars:       options.MaxResultChars,
				ResultStoreDir: options.ResultStoreDir,
			})
		},
	}
}

func promptsToStructuredContent(prompts []RemotePrompt) []map[string]any {
	out := make([]map[string]any, 0, len(prompts))
	for _, prompt := range prompts {
		out = append(out, map[string]any{
			"name":        prompt.Name,
			"description": prompt.Description,
			"arguments":   promptArgumentsToStructuredContent(prompt.Arguments),
		})
	}
	return out
}

func promptArgumentsToStructuredContent(arguments []PromptArgument) []map[string]any {
	out := make([]map[string]any, 0, len(arguments))
	for _, argument := range arguments {
		out = append(out, map[string]any{
			"name":        argument.Name,
			"description": argument.Description,
			"required":    argument.Required,
		})
	}
	return out
}

func promptResultToStructuredContent(result PromptResult) map[string]any {
	messages := make([]map[string]any, 0, len(result.Messages))
	for _, message := range result.Messages {
		messages = append(messages, map[string]any{
			"role":    message.Role,
			"content": message.Content,
		})
	}
	return map[string]any{
		"description": result.Description,
		"messages":    messages,
	}
}
