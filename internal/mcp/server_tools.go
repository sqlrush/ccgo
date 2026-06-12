package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

type ClientHandle struct {
	Client Client
	Close  func() error
}

type ClientOpenFunc func(context.Context, string, contracts.MCPServer) (ClientHandle, error)

type InitializingClient interface {
	EnsureInitialized(context.Context) error
}

type ServerToolOptions struct {
	OpenClient          ClientOpenFunc
	HeaderProvider      ServerHeaderProvider
	AccessTokenProvider ServerAccessTokenProvider
	ClientRoots         []Root
	ResultStoreDir      string
	MaxResultChars      int
	DisableResources    bool
	DisablePrompts      bool
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
	return OpenServerClientWithOptions(ctx, name, server, ServerToolOptions{})
}

func OpenServerClientWithOptions(ctx context.Context, name string, server contracts.MCPServer, options ServerToolOptions) (ClientHandle, error) {
	tokenSource := newServerAccessTokenSource(name, server, options.AccessTokenProvider)
	switch Transport(server) {
	case TransportStdio:
		transport, err := StartStdioTransport(ctx, server)
		if err != nil {
			return ClientHandle{}, err
		}
		client := newProtocolClientWithOptions(transport, options)
		return ClientHandle{
			Client: client,
			Close:  transport.Close,
		}, nil
	case TransportHTTP:
		transport := NewHTTPTransport(server.URL, TransportHeaders(server), nil)
		transport.HeaderProvider = serverHeaderProvider(name, server, options, tokenSource)
		transport.AuthorizationRefresher = serverAuthorizationRefresher(tokenSource)
		client := newProtocolClientWithOptions(transport, options)
		return ClientHandle{
			Client: client,
			Close:  transport.Close,
		}, nil
	case TransportSSE:
		transport := NewSSETransport(server.URL, TransportHeaders(server), nil)
		transport.HeaderProvider = serverHeaderProvider(name, server, options, tokenSource)
		transport.AuthorizationRefresher = serverAuthorizationRefresher(tokenSource)
		client := newProtocolClientWithOptions(transport, options)
		return ClientHandle{
			Client: client,
			Close:  transport.Close,
		}, nil
	case TransportWS:
		transport := NewWSTransport(server.URL, TransportHeaders(server))
		transport.HeaderProvider = serverHeaderProvider(name, server, options, tokenSource)
		transport.AuthorizationRefresher = serverAuthorizationRefresher(tokenSource)
		client := newProtocolClientWithOptions(transport, options)
		return ClientHandle{
			Client: client,
			Close:  transport.Close,
		}, nil
	default:
		return ClientHandle{}, fmt.Errorf("mcp server %q transport %q is not supported yet", name, Transport(server))
	}
}

func newProtocolClientWithOptions(transport RPCTransport, options ServerToolOptions) *ProtocolClient {
	client := NewProtocolClient(transport)
	if len(options.ClientRoots) > 0 {
		client.SetRoots(options.ClientRoots)
	}
	return client
}

type serverAccessTokenSource struct {
	name     string
	server   contracts.MCPServer
	provider ServerAccessTokenProvider

	mu     sync.Mutex
	loaded bool
	token  AccessTokenProvider
}

func newServerAccessTokenSource(name string, server contracts.MCPServer, provider ServerAccessTokenProvider) *serverAccessTokenSource {
	if provider == nil || server.OAuth == nil {
		return nil
	}
	return &serverAccessTokenSource{name: name, server: server, provider: provider}
}

func (s *serverAccessTokenSource) providerFor(ctx context.Context) (AccessTokenProvider, error) {
	if s == nil || s.provider == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return s.token, nil
	}
	token, err := s.provider(ctx, s.name, s.server)
	if err != nil {
		return nil, err
	}
	s.token = token
	s.loaded = true
	return s.token, nil
}

func (s *serverAccessTokenSource) CurrentAccessToken(ctx context.Context) (string, error) {
	token, err := s.providerFor(ctx)
	if err != nil || token == nil {
		return "", err
	}
	return token.CurrentAccessToken(ctx)
}

func (s *serverAccessTokenSource) RefreshAccessToken(ctx context.Context) (bool, error) {
	token, err := s.providerFor(ctx)
	if err != nil || token == nil {
		return false, err
	}
	refresher, ok := token.(RefreshingAccessTokenProvider)
	if !ok {
		return false, nil
	}
	_, err = refresher.RefreshAccessToken(ctx)
	return true, err
}

func serverHeaderProvider(name string, server contracts.MCPServer, options ServerToolOptions, tokenSource *serverAccessTokenSource) func(context.Context) (map[string]string, error) {
	if options.HeaderProvider == nil && tokenSource == nil {
		return nil
	}
	return func(ctx context.Context) (map[string]string, error) {
		var headers map[string]string
		if tokenSource != nil {
			token, err := tokenSource.CurrentAccessToken(ctx)
			if err != nil {
				return nil, err
			}
			token = strings.TrimSpace(token)
			if token != "" {
				headers = MergeTransportHeaders(headers, map[string]string{"Authorization": bearerHeaderValue(token)})
			}
		}
		if options.HeaderProvider != nil {
			explicitHeaders, err := options.HeaderProvider(ctx, name, server)
			if err != nil {
				return nil, err
			}
			headers = MergeTransportHeaders(headers, explicitHeaders)
		}
		return headers, nil
	}
}

func serverAuthorizationRefresher(tokenSource *serverAccessTokenSource) func(context.Context) error {
	if tokenSource == nil {
		return nil
	}
	return func(ctx context.Context) error {
		refreshed, err := tokenSource.RefreshAccessToken(ctx)
		if err != nil {
			return err
		}
		if !refreshed {
			return fmt.Errorf("mcp oauth access token provider cannot refresh")
		}
		return nil
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
		openClient = func(ctx context.Context, name string, server contracts.MCPServer) (ClientHandle, error) {
			return OpenServerClientWithOptions(ctx, name, server, options)
		}
	}
	handle, err := openClient(ctx, serverName, server)
	if err != nil {
		return ServerToolSet{}, err
	}
	if handle.Client == nil {
		closeClient(handle)
		return ServerToolSet{}, fmt.Errorf("mcp server %q client is nil", serverName)
	}
	if initializer, ok := handle.Client.(InitializingClient); ok {
		if err := initializer.EnsureInitialized(ctx); err != nil {
			closeClient(handle)
			return ServerToolSet{}, err
		}
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
