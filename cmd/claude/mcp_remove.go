package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

// mcpAddJSON implements `claude mcp add-json <name> <json> [-s/--scope]`.
//
// It unmarshals the user-supplied JSON string into a contracts.MCPServer value,
// validates that the result is well-formed, and then delegates to
// writeServerToScope (defined in mcp_add.go) for the immutable write.
//
// Note: --client-secret prompting is intentionally omitted in Phase 6a because
// there is no secret-storage seam yet. A JSON blob that already contains an
// oauth.clientId field (e.g. from a pre-shared config) is still accepted and
// stored verbatim — only interactive prompting is skipped.
func mcpAddJSON(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	scope := mcp.ScopeLocal
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-s", "--scope":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "ccgo mcp add-json: --scope requires a value")
				return 1
			}
			i++
			scope = strings.ToLower(strings.TrimSpace(args[i]))
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) < 2 {
		fmt.Fprintln(stderr, "ccgo mcp add-json: usage: claude mcp add-json <name> <json> [--scope local|user|project]")
		return 1
	}
	name := positional[0]
	if strings.TrimSpace(name) == "" {
		fmt.Fprintln(stderr, "ccgo mcp add-json: server name must not be empty")
		return 1
	}

	// Validate scope early so we report a clean error before touching disk.
	switch scope {
	case mcp.ScopeLocal, mcp.ScopeUser, mcp.ScopeProject:
		// valid
	default:
		fmt.Fprintf(stderr, "ccgo mcp add-json: invalid --scope %q (want local|user|project)\n", scope)
		return 1
	}

	var server contracts.MCPServer
	if err := json.Unmarshal([]byte(positional[1]), &server); err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add-json: invalid server JSON: %v\n", err)
		return 1
	}

	// Require at least one of command or url so the server definition is useful.
	if strings.TrimSpace(server.Command) == "" && strings.TrimSpace(server.URL) == "" {
		fmt.Fprintln(stderr, "ccgo mcp add-json: server JSON must set \"command\" (stdio) or \"url\" (http/sse)")
		return 1
	}

	path, err := env.pathForScope(scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add-json: %v\n", err)
		return 1
	}
	if err := writeServerToScope(path, name, server); err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add-json: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Added MCP server %q (%s) to %s scope.\n", name, mcp.Transport(server), scope)
	return 0
}

// mcpRemove implements `claude mcp remove <name> [-s/--scope]`.
//
// If --scope is given, the server is removed only from that scope.
// If omitted, the scopes are searched in CC-compatible order
// (user → project → local — matching CC main.tsx:3916) and the server is
// removed from the first scope that contains it. A non-zero exit and an
// error message are returned when the server is not found in any searched scope.
func mcpRemove(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	scope := ""
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-s", "--scope":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "ccgo mcp remove: --scope requires a value")
				return 1
			}
			i++
			scope = strings.ToLower(strings.TrimSpace(args[i]))
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		fmt.Fprintln(stderr, "ccgo mcp remove: server name is required")
		return 1
	}
	name := positional[0]

	// Build the list of paths to search. If --scope is provided, constrain to
	// that single path; otherwise search all three scopes in CC order.
	type scopedPath struct {
		name string
		path string
	}
	var searchPaths []scopedPath
	if scope != "" {
		switch scope {
		case mcp.ScopeLocal, mcp.ScopeUser, mcp.ScopeProject:
			// valid
		default:
			fmt.Fprintf(stderr, "ccgo mcp remove: invalid --scope %q (want local|user|project)\n", scope)
			return 1
		}
		p, err := env.pathForScope(scope)
		if err != nil {
			fmt.Fprintf(stderr, "ccgo mcp remove: %v\n", err)
			return 1
		}
		searchPaths = []scopedPath{{scope, p}}
	} else {
		searchPaths = []scopedPath{
			{mcp.ScopeUser, env.UserPath},
			{mcp.ScopeProject, config.ProjectSettingsPath(env.ProjectRoot)},
			{mcp.ScopeLocal, config.LocalSettingsPath(env.ProjectRoot)},
		}
	}

	for _, sp := range searchPaths {
		removed, err := removeServerFromScope(sp.path, name)
		if err != nil {
			fmt.Fprintf(stderr, "ccgo mcp remove: %v\n", err)
			return 1
		}
		if removed {
			fmt.Fprintf(stdout, "Removed MCP server %q from %s scope.\n", name, sp.name)
			return 0
		}
	}

	fmt.Fprintf(stderr, "ccgo mcp remove: no MCP server named %q found in any configured scope\n", name)
	return 1
}

// removeServerFromScope performs an immutable delete of mcpServers[name] from
// the settings document at path. It reads the document, builds a fresh copy
// without the named server, and writes the copy back. All other top-level keys
// and all other mcpServers entries are preserved exactly.
//
// Returns (false, nil) when the file is absent or the server is not present
// (no write is performed in that case). Returns (true, nil) on success.
func removeServerFromScope(path, name string) (bool, error) {
	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		return false, fmt.Errorf("read settings %s: %w", path, err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers[name]; !ok {
		// Server not present — nothing to remove.
		return false, nil
	}

	// Build a shallow copy of the top-level document to avoid in-place mutation.
	updated := make(map[string]any, len(doc))
	for k, v := range doc {
		updated[k] = v
	}
	// Build a fresh mcpServers map that omits only the target server.
	newServers := make(map[string]any, len(servers)-1)
	for k, v := range servers {
		if k == name {
			continue
		}
		newServers[k] = v
	}
	updated["mcpServers"] = newServers

	if err := config.WriteSettingsDocument(path, updated); err != nil {
		return false, fmt.Errorf("write settings %s: %w", path, err)
	}
	return true, nil
}
