package mcp

import (
	"context"
	"errors"
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

type ServerToolError struct {
	ServerName string
	Err        error
}

func (e ServerToolError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("mcp server %q failed", e.ServerName)
	}
	return fmt.Sprintf("mcp server %q failed: %v", e.ServerName, e.Err)
}

func (e ServerToolError) Unwrap() error {
	return e.Err
}

type MultiServerToolSet struct {
	Servers []ServerToolSet
	Tools   []tool.Tool
	Errors  []ServerToolError
}

func (s MultiServerToolSet) Registry() (*tool.Registry, error) {
	return tool.NewRegistry(s.Tools...)
}

func (s MultiServerToolSet) Close() error {
	var errs []error
	for _, server := range s.Servers {
		if server.Close == nil {
			continue
		}
		if err := server.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
	case TransportHTTP:
		transport := NewHTTPTransport(server.URL, server.Headers, nil)
		return ClientHandle{
			Client: NewProtocolClient(transport),
			Close:  transport.Close,
		}, nil
	case TransportSSE:
		transport := NewSSETransport(server.URL, server.Headers, nil)
		return ClientHandle{
			Client: NewProtocolClient(transport),
			Close:  transport.Close,
		}, nil
	default:
		return ClientHandle{}, fmt.Errorf("mcp server %q transport %q is not supported yet", name, Transport(server))
	}
}

func BuildServerToolSets(ctx context.Context, servers map[string]contracts.MCPServer, options ServerToolOptions) MultiServerToolSet {
	result := MultiServerToolSet{}
	for _, name := range sortedServerNames(servers) {
		toolset, err := BuildServerToolSet(ctx, name, servers[name], options)
		if err != nil {
			result.Errors = append(result.Errors, ServerToolError{ServerName: name, Err: err})
			continue
		}
		result.Servers = append(result.Servers, toolset)
		result.Tools = append(result.Tools, toolset.Tools...)
	}
	return result
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
