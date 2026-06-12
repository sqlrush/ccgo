package mcp

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

type ClientHandle struct {
	Client Client
	Close  func() error
}

type ClientOpenFunc func(context.Context, string, contracts.MCPServer) (ClientHandle, error)

type ServerToolOptions struct {
	OpenClient       ClientOpenFunc
	ResultStoreDir   string
	MaxResultChars   int
	DisableResources bool
	DisablePrompts   bool
}

type ServerToolSet struct {
	ServerName string
	Client     Client
	Tools      []tool.Tool
	Close      func() error
}

func OpenServerClient(ctx context.Context, name string, server contracts.MCPServer) (ClientHandle, error) {
	switch Transport(server) {
	case TransportStdio:
		transport, err := StartStdioTransport(ctx, server)
		if err != nil {
			return ClientHandle{}, err
		}
		return ClientHandle{
			Client: NewProtocolClient(transport),
			Close:  transport.Close,
		}, nil
	default:
		return ClientHandle{}, fmt.Errorf("mcp server %q transport %q is not supported yet", name, Transport(server))
	}
}

func BuildServerToolSet(ctx context.Context, serverName string, server contracts.MCPServer, options ServerToolOptions) (ServerToolSet, error) {
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return ServerToolSet{}, fmt.Errorf("mcp server name is required")
	}
	openClient := options.OpenClient
	if openClient == nil {
		openClient = OpenServerClient
	}
	handle, err := openClient(ctx, serverName, server)
	if err != nil {
		return ServerToolSet{}, err
	}
	if handle.Client == nil {
		closeClient(handle)
		return ServerToolSet{}, fmt.Errorf("mcp server %q client is nil", serverName)
	}

	toolOptions := ToolBuildOptions{
		ServerName:     serverName,
		Client:         handle.Client,
		ResultStoreDir: options.ResultStoreDir,
		MaxResultChars: options.MaxResultChars,
	}
	tools, err := BuildTools(ctx, toolOptions)
	if err != nil {
		closeClient(handle)
		return ServerToolSet{}, err
	}
	if !options.DisableResources {
		tools = append(tools, BuildResourceTools(toolOptions)...)
	}
	if !options.DisablePrompts {
		tools = append(tools, BuildPromptTools(toolOptions)...)
	}

	return ServerToolSet{
		ServerName: serverName,
		Client:     handle.Client,
		Tools:      tools,
		Close:      handle.Close,
	}, nil
}

func closeClient(handle ClientHandle) {
	if handle.Close != nil {
		_ = handle.Close()
	}
}
