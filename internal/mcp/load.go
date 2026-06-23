package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"ccgo/internal/contracts"
)

type LoadResult struct {
	Servers map[string]contracts.MCPServer
	Errors  []ValidationError
}

type ManualConfigSources struct {
	User    map[string]contracts.MCPServer
	Project map[string]contracts.MCPServer
	Local   map[string]contracts.MCPServer
	Plugin  map[string]contracts.MCPServer
	Policy  Policy
}

type ManualConfigResult struct {
	Servers map[string]contracts.MCPServer
	Blocked []string
}

func LoadSettingsServers(settings contracts.Settings, scope string, options ParseOptions) (LoadResult, error) {
	if settings.MCPServers == nil {
		return LoadResult{Servers: map[string]contracts.MCPServer{}}, nil
	}

	data, err := json.Marshal(Config{MCPServers: settings.MCPServers})
	if err != nil {
		return LoadResult{}, err
	}
	parseOptions := options
	parseOptions.Scope = scope
	parsed, err := ParseConfigJSON(data, parseOptions)
	if err != nil {
		return LoadResult{}, err
	}
	if parsed.Config == nil {
		return LoadResult{Servers: map[string]contracts.MCPServer{}, Errors: parsed.Errors}, nil
	}
	return LoadResult{Servers: parsed.Config.MCPServers, Errors: parsed.Errors}, nil
}

func MergeManualConfigSources(sources ManualConfigSources) ManualConfigResult {
	manual := MergeServers(sources.User, sources.Project, sources.Local)
	plugin := pluginServersWithoutManualNameConflicts(DedupPluginServers(sources.Plugin, manual).Servers, manual)
	merged := MergeServers(manual, plugin)
	allowed, blocked := FilterServersByPolicy(merged, sources.Policy)
	return ManualConfigResult{
		Servers: allowed,
		Blocked: blocked,
	}
}

func pluginServersWithoutManualNameConflicts(plugin map[string]contracts.MCPServer, manual map[string]contracts.MCPServer) map[string]contracts.MCPServer {
	if len(plugin) == 0 {
		return nil
	}
	out := map[string]contracts.MCPServer{}
	for _, name := range sortedServerNames(plugin) {
		if _, exists := manual[name]; exists {
			continue
		}
		out[name] = plugin[name]
	}
	return out
}

func LoadProjectConfigChain(cwd string, options ParseOptions) (LoadResult, error) {
	dirs := projectConfigDirs(cwd)
	result := LoadResult{Servers: map[string]contracts.MCPServer{}}

	for _, dir := range dirs {
		path := filepath.Join(dir, ".mcp.json")
		parseOptions := options
		parseOptions.Scope = ScopeProject
		parseOptions.FilePath = path

		parsed, err := ParseConfigFile(path, parseOptions)
		if err != nil {
			return LoadResult{}, err
		}
		if parsed.Config == nil {
			result.Errors = appendNonMissingErrors(result.Errors, parsed.Errors)
			continue
		}
		result.Errors = append(result.Errors, parsed.Errors...)
		result.Servers = MergeServers(result.Servers, parsed.Config.MCPServers)
	}

	return result, nil
}

func projectConfigDirs(cwd string) []string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = filepath.Clean(cwd)
	}
	var dirs []string
	for {
		dirs = append(dirs, abs)
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}

func appendNonMissingErrors(out []ValidationError, errs []ValidationError) []ValidationError {
	for _, err := range errs {
		if err.Message == "MCP config file not found" {
			continue
		}
		out = append(out, err)
	}
	return out
}

// DoesEnterpriseMCPConfigExist returns true when a managed-mcp.json file is
// present at EnterpriseMCPPath().  Used to gate user/project scope loading and
// block mcp add when enterprise config is active (MCP-27).
// CC ref: src/services/mcp/config.ts:1080 (doesEnterpriseMcpConfigExist).
func DoesEnterpriseMCPConfigExist(enterprisePath string) bool {
	if enterprisePath == "" {
		return false
	}
	_, err := os.Stat(enterprisePath)
	return err == nil
}

// LoadEnterpriseMCPConfig reads managed-mcp.json and returns the contained
// servers.  Missing file returns an empty result without error (expected case).
// Malformed files return an error.
// CC ref: src/services/mcp/config.ts:996-1012 (getMcpConfigsByScope 'enterprise').
func LoadEnterpriseMCPConfig(enterprisePath string) (LoadResult, error) {
	if enterprisePath == "" {
		return LoadResult{Servers: map[string]contracts.MCPServer{}}, nil
	}
	parsed, err := ParseConfigFile(enterprisePath, ParseOptions{Scope: ScopeEnterprise, FilePath: enterprisePath})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LoadResult{Servers: map[string]contracts.MCPServer{}}, nil
		}
		return LoadResult{}, err
	}
	if parsed.Config == nil {
		return LoadResult{Servers: map[string]contracts.MCPServer{}, Errors: parsed.Errors}, nil
	}
	return LoadResult{Servers: parsed.Config.MCPServers, Errors: parsed.Errors}, nil
}
