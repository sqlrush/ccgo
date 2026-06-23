package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

// parseAddArgs parses `add <name> <commandOrUrl> [args...] [flags]`. Flags may
// appear anywhere after the name; everything else (in order) is the positional
// command + args. Mirrors CC's commands/mcp/addCommand.ts behavior.
//
// Transport inference: if --transport is http/sse OR commandOrUrl is an http(s)://
// URL, treat as remote server; else stdio command+args.
func parseAddArgs(args []string) (string, contracts.MCPServer, string, error) {
	var positional []string
	var server contracts.MCPServer
	scope := mcp.ScopeLocal
	transport := ""

	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s requires a value", a)
			}
			i++
			return args[i], nil
		}
		switch a {
		case "-t", "--transport":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			transport = strings.ToLower(strings.TrimSpace(v))
		case "-s", "--scope":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			scope = strings.ToLower(strings.TrimSpace(v))
		case "-e", "--env":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			k, val, ok := strings.Cut(v, "=")
			if !ok || strings.TrimSpace(k) == "" {
				return "", server, "", fmt.Errorf("invalid --env %q (want KEY=VALUE)", v)
			}
			if server.Env == nil {
				server.Env = map[string]string{}
			}
			server.Env[k] = val
		case "-H", "--header":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			k, val, ok := strings.Cut(v, ":")
			if !ok || strings.TrimSpace(k) == "" {
				return "", server, "", fmt.Errorf("invalid --header %q (want \"Key: Value\")", v)
			}
			if server.Headers == nil {
				server.Headers = map[string]string{}
			}
			server.Headers[strings.TrimSpace(k)] = strings.TrimSpace(val)
		case "--client-id":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			ensureOAuth(&server).ClientID = strings.TrimSpace(v)
		case "--callback-port":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			port, perr := parsePort(v)
			if perr != nil {
				return "", server, "", perr
			}
			ensureOAuth(&server).CallbackPort = &port
		default:
			// Once we have at least two positionals (name + commandOrUrl), unknown
			// dash-prefixed tokens are treated as args to the stdio command, not
			// our flags. This matches CC's CLI framework behavior where [args...]
			// captures remaining tokens after the known subcommand flags.
			if strings.HasPrefix(a, "-") && len(positional) >= 2 {
				positional = append(positional, a)
				break
			}
			if strings.HasPrefix(a, "-") {
				return "", server, "", fmt.Errorf("unknown flag %q", a)
			}
			positional = append(positional, a)
		}
	}

	// Validate scope.
	switch scope {
	case mcp.ScopeLocal, mcp.ScopeUser, mcp.ScopeProject:
		// valid
	default:
		return "", server, "", fmt.Errorf("invalid --scope %q (want local|user|project)", scope)
	}

	// Validate transport if explicitly set.
	if transport != "" {
		switch transport {
		case mcp.TransportStdio, mcp.TransportSSE, mcp.TransportHTTP:
			// valid
		default:
			return "", server, "", fmt.Errorf("invalid --transport %q (want stdio|sse|http)", transport)
		}
	}

	if len(positional) < 2 {
		return "", server, "", fmt.Errorf("usage: claude mcp add <name> <commandOrUrl> [args...]")
	}
	name := positional[0]
	if strings.TrimSpace(name) == "" {
		return "", server, "", fmt.Errorf("server name must not be empty")
	}
	target := positional[1]
	rest := positional[2:]

	// Infer remote vs stdio: explicit http/sse transport, or target is an http(s) URL.
	isRemote := transport == mcp.TransportHTTP || transport == mcp.TransportSSE || isHTTPURL(target)
	if isRemote {
		if !isHTTPURL(target) {
			return "", server, "", fmt.Errorf("remote transport requires an http(s) URL, got %q", target)
		}
		server.URL = target
		if transport == "" || transport == mcp.TransportHTTP {
			server.Type = mcp.TransportHTTP
		} else {
			server.Type = transport
		}
		if len(rest) > 0 {
			return "", server, "", fmt.Errorf("remote server takes no extra positional args, got %v", rest)
		}
	} else {
		server.Command = target
		if len(rest) > 0 {
			server.Args = rest
		}
		server.Type = mcp.TransportStdio
	}
	return name, server, scope, nil
}

func ensureOAuth(server *contracts.MCPServer) *contracts.MCPOAuthConfig {
	if server.OAuth == nil {
		server.OAuth = &contracts.MCPOAuthConfig{}
	}
	return server.OAuth
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func parsePort(s string) (int, error) {
	var port int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &port); err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid --callback-port %q", s)
	}
	return port, nil
}

// writeServerToScope reads the settings document at path, copies the document
// and its mcpServers sub-map, sets mcpServers[name] to the new server, and
// writes the result. It never mutates in-place: other top-level keys and other
// mcpServers entries are preserved exactly.
func writeServerToScope(path, name string, server contracts.MCPServer) error {
	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("read settings %s: %w", path, err)
	}
	// Build a new top-level map (shallow copy) so we don't mutate the decoded doc.
	updated := make(map[string]any, len(doc)+1)
	for k, v := range doc {
		updated[k] = v
	}
	// Build a new mcpServers map (copy existing entries) to avoid mutating sub-map.
	existing, _ := doc["mcpServers"].(map[string]any)
	newServers := make(map[string]any, len(existing)+1)
	for k, v := range existing {
		newServers[k] = v
	}
	// Marshal server to map[string]any via JSON (respects omitempty tags).
	newServers[name] = serverToDocument(server)
	updated["mcpServers"] = newServers
	if err := config.WriteSettingsDocument(path, updated); err != nil {
		return fmt.Errorf("write settings %s: %w", path, err)
	}
	return nil
}

// serverToDocument marshals server to map[string]any via json round-trip so
// that omitempty tags are respected and only non-zero fields appear in the doc.
func serverToDocument(server contracts.MCPServer) map[string]any {
	data, err := json.Marshal(server)
	if err != nil {
		// Contracts struct is always serialisable; this path is unreachable in practice.
		panic(fmt.Sprintf("serverToDocument: marshal: %v", err))
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		panic(fmt.Sprintf("serverToDocument: unmarshal: %v", err))
	}
	return out
}

// mcpAdd implements the `claude mcp add` subcommand.
func mcpAdd(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	// MCP-27: enterprise MCP config has exclusive control — block all adds.
	// CC ref: src/services/mcp/config.ts:650-653.
	if mcp.DoesEnterpriseMCPConfigExist(env.enterpriseMCPPath()) {
		fmt.Fprintln(stderr, "ccgo mcp add: enterprise MCP configuration is active and has exclusive control over MCP servers")
		return 1
	}
	name, server, scope, err := parseAddArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add: %v\n", err)
		return 1
	}
	path, err := env.pathForScope(scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add: %v\n", err)
		return 1
	}
	if err := writeServerToScope(path, name, server); err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Added MCP server %q (%s) to %s scope.\n", name, mcp.Transport(server), scope)
	return 0
}
