package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

// claudeDesktopConfigPath returns the platform-appropriate path to
// Claude Desktop's claude_desktop_config.json.
// CC ref: src/utils/claudeDesktop.ts:getClaudeDesktopConfigPath.
func claudeDesktopConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json")
	default:
		// Linux: Claude Desktop not officially supported; return empty.
		return ""
	}
}

// readClaudeDesktopServers reads the Claude Desktop config and extracts
// the mcpServers map.  Returns empty map (no error) when the file is absent
// or contains no servers.
// CC ref: src/utils/claudeDesktop.ts:readClaudeDesktopMcpServers.
func readClaudeDesktopServers(configPath string) (map[string]contracts.MCPServer, error) {
	if configPath == "" {
		return map[string]contracts.MCPServer{}, nil
	}
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return map[string]contracts.MCPServer{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read claude desktop config %s: %w", configPath, err)
	}
	var raw struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]contracts.MCPServer{}, nil // malformed — treat as empty
	}
	servers := make(map[string]contracts.MCPServer, len(raw.MCPServers))
	for name, serverRaw := range raw.MCPServers {
		var srv contracts.MCPServer
		if err := json.Unmarshal(serverRaw, &srv); err != nil {
			continue // skip malformed entries
		}
		if srv.Command == "" && srv.URL == "" {
			continue // skip empty entries
		}
		servers[name] = srv
	}
	return servers, nil
}

// mcpAddFromClaudeDesktop implements `claude mcp add-from-claude-desktop`.
// In headless mode it reads Claude Desktop's config and imports all stdio
// servers to the requested scope (default: local).  The TUI multi-select
// dialog is MANUAL (requires Bubble Tea) — headless import is the automated path.
// CC ref: src/cli/handlers/mcp.tsx:mcpAddFromDesktopHandler.
func mcpAddFromClaudeDesktop(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	scope := mcp.ScopeLocal
	desktopPath := env.DesktopConfigPath
	if desktopPath == "" {
		desktopPath = claudeDesktopConfigPath()
	}

	// Parse -s / --scope flag.
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-s", "--scope":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "ccgo mcp add-from-claude-desktop: --scope requires a value")
				return 1
			}
			i++
			scope = args[i]
		}
	}

	if desktopPath == "" {
		fmt.Fprintln(stderr, "ccgo mcp add-from-claude-desktop: Claude Desktop is not supported on this platform")
		return 1
	}

	servers, err := readClaudeDesktopServers(desktopPath)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add-from-claude-desktop: %v\n", err)
		return 1
	}
	if len(servers) == 0 {
		fmt.Fprintln(stdout, "No MCP servers found in Claude Desktop configuration or configuration file does not exist.")
		return 0
	}

	path, err := env.pathForScope(scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add-from-claude-desktop: %v\n", err)
		return 1
	}

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	imported := 0
	for _, name := range names {
		if err := writeServerToScope(path, name, servers[name]); err != nil {
			fmt.Fprintf(stderr, "ccgo mcp add-from-claude-desktop: import %q: %v\n", name, err)
			continue
		}
		fmt.Fprintf(stdout, "Imported MCP server %q (%s) to %s scope.\n", name, mcp.Transport(servers[name]), scope)
		imported++
	}
	if imported == 0 {
		return 1
	}
	return 0
}
