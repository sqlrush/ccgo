package mcp

import (
	"path/filepath"

	"ccgo/internal/contracts"
)

type LoadResult struct {
	Servers map[string]contracts.MCPServer
	Errors  []ValidationError
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

func appendNonMissingErrors(out []ValidationError, errors []ValidationError) []ValidationError {
	for _, err := range errors {
		if err.Message == "MCP config file not found" {
			continue
		}
		out = append(out, err)
	}
	return out
}
