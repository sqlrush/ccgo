package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

const mcpUsage = "Usage: claude mcp <add|add-json|list|get|remove|serve>"

// mcpCLIEnv injects settings file locations so tests avoid the real $HOME.
type mcpCLIEnv struct {
	UserPath    string
	ProjectRoot string
}

// defaultMCPCLIEnv builds a production env from config helpers.
func defaultMCPCLIEnv(projectRoot string) mcpCLIEnv {
	return mcpCLIEnv{
		UserPath:    config.UserSettingsPath(),
		ProjectRoot: projectRoot,
	}
}

// runMCPCommand is the top-level dispatcher for all `claude mcp` subcommands.
func runMCPCommand(args []string, stdout, stderr io.Writer, env mcpCLIEnv) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "ccgo mcp: missing subcommand")
		fmt.Fprintln(stderr, mcpUsage)
		return 1
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return mcpList(env, stdout, stderr)
	case "get":
		return mcpGet(args[1:], env, stdout, stderr)
	case "add":
		return mcpAdd(args[1:], env, stdout, stderr) // implemented in Task 2
	case "add-json":
		return mcpAddJSON(args[1:], env, stdout, stderr) // implemented in Task 3
	case "remove":
		return mcpRemove(args[1:], env, stdout, stderr) // implemented in Task 4
	case "serve":
		return mcpServe(args[1:], stdout, stderr) // implemented in Task 7
	default:
		fmt.Fprintf(stderr, "ccgo mcp: unknown subcommand %q\n", args[0])
		fmt.Fprintln(stderr, mcpUsage)
		return 1
	}
}

// scopedServer pairs an MCPServer with the settings scope it came from.
type scopedServer struct {
	scope  string
	server contracts.MCPServer
}

// allConfiguredServers merges user+project+local scopes. Later scopes win on
// name collision (local > project > user), matching CC precedence. Files that
// do not exist are silently skipped.
func allConfiguredServers(env mcpCLIEnv) (map[string]scopedServer, error) {
	scoped := map[string]scopedServer{}
	order := []struct {
		scope string
		path  string
	}{
		{mcp.ScopeUser, env.UserPath},
		{mcp.ScopeProject, config.ProjectSettingsPath(env.ProjectRoot)},
		{mcp.ScopeLocal, config.LocalSettingsPath(env.ProjectRoot)},
	}
	for _, o := range order {
		settings, err := config.LoadSettingsFile(o.path)
		if err != nil {
			// Missing settings files are silently skipped; any other error surfaces.
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("load %s settings (%s): %w", o.scope, o.path, err)
		}
		for name, server := range settings.MCPServers {
			scoped[name] = scopedServer{scope: o.scope, server: server}
		}
	}
	return scoped, nil
}

// mcpList prints all configured MCP servers sorted by name.
// Sensitive header values and auth tokens are redacted in output.
func mcpList(env mcpCLIEnv, stdout, stderr io.Writer) int {
	servers, err := allConfiguredServers(env)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp list: %v\n", err)
		return 1
	}
	if len(servers) == 0 {
		fmt.Fprintln(stdout, "No MCP servers configured.")
		return 0
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := servers[name]
		fmt.Fprintln(stdout, formatServerLine(name, s))
	}
	return 0
}

// formatServerLine returns a single tab-separated summary line for a server.
// URL is shown as-is; command/args are joined with spaces.
func formatServerLine(name string, s scopedServer) string {
	transport := mcp.Transport(s.server)
	target := s.server.URL
	if target == "" {
		parts := make([]string, 0, 1+len(s.server.Args))
		if s.server.Command != "" {
			parts = append(parts, s.server.Command)
		}
		parts = append(parts, s.server.Args...)
		target = strings.Join(parts, " ")
	}
	return fmt.Sprintf("%s\t[%s]\t%s\t(%s)", name, transport, target, s.scope)
}

// mcpGet prints the full configuration of a single named server.
// Sensitive fields (Headers, AuthToken) are redacted.
func mcpGet(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "ccgo mcp get: server name is required")
		return 1
	}
	name := args[0]
	servers, err := allConfiguredServers(env)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp get: %v\n", err)
		return 1
	}
	s, ok := servers[name]
	if !ok {
		fmt.Fprintf(stderr, "ccgo mcp get: no MCP server named %q\n", name)
		return 1
	}
	fmt.Fprintf(stdout, "%s:\n", name)
	fmt.Fprintf(stdout, "  scope:     %s\n", s.scope)
	fmt.Fprintf(stdout, "  transport: %s\n", mcp.Transport(s.server))
	if s.server.URL != "" {
		fmt.Fprintf(stdout, "  url:       %s\n", s.server.URL)
	}
	if s.server.Command != "" {
		parts := make([]string, 0, 1+len(s.server.Args))
		parts = append(parts, s.server.Command)
		parts = append(parts, s.server.Args...)
		fmt.Fprintf(stdout, "  command:   %s\n", strings.Join(parts, " "))
	}
	if len(s.server.Headers) > 0 {
		fmt.Fprintf(stdout, "  headers:   %d header(s) [redacted]\n", len(s.server.Headers))
	}
	if s.server.HeadersHelper != "" {
		fmt.Fprintf(stdout, "  headersHelper: %s\n", s.server.HeadersHelper)
	}
	if s.server.AuthToken != "" {
		fmt.Fprintln(stdout, "  authToken: [redacted]")
	}
	if s.server.OAuth != nil {
		fmt.Fprintln(stdout, "  oauth:     enabled")
	}
	return 0
}

// ---------------------------------------------------------------------------
// Stubs for subcommands implemented in later tasks.
// ---------------------------------------------------------------------------

func mcpAdd(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "ccgo mcp add: not yet implemented")
	return 1
}

func mcpAddJSON(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "ccgo mcp add-json: not yet implemented")
	return 1
}

func mcpRemove(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "ccgo mcp remove: not yet implemented")
	return 1
}

func mcpServe(args []string, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "ccgo mcp serve: not yet implemented")
	return 1
}
