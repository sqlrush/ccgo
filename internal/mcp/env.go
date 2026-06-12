package mcp

import (
	"os"
	"regexp"
	"sort"
	"strings"

	"ccgo/internal/contracts"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

type StringEnvExpansion struct {
	Expanded    string
	MissingVars []string
}

type ServerEnvExpansion struct {
	Expanded    contracts.MCPServer
	MissingVars []string
}

func ExpandEnvVarsInString(value string) StringEnvExpansion {
	return expandEnvVarsInString(value, os.LookupEnv)
}

func ExpandEnvVarsInStringWithMap(value string, env map[string]string) StringEnvExpansion {
	return expandEnvVarsInString(value, func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	})
}

func ExpandServerEnvVars(server contracts.MCPServer) ServerEnvExpansion {
	return expandServerEnvVars(server, os.LookupEnv)
}

func ExpandServerEnvVarsWithMap(server contracts.MCPServer, env map[string]string) ServerEnvExpansion {
	return expandServerEnvVars(server, func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	})
}

func expandEnvVarsInString(value string, lookup func(string) (string, bool)) StringEnvExpansion {
	var missing []string
	expanded := envVarPattern.ReplaceAllStringFunc(value, func(match string) string {
		groups := envVarPattern.FindStringSubmatch(match)
		if len(groups) != 2 {
			return match
		}
		varName, defaultValue, hasDefault := strings.Cut(groups[1], ":-")
		if envValue, ok := lookup(varName); ok {
			return envValue
		}
		if hasDefault {
			return defaultValue
		}
		missing = append(missing, varName)
		return match
	})
	return StringEnvExpansion{Expanded: expanded, MissingVars: missing}
}

func expandServerEnvVars(server contracts.MCPServer, lookup func(string) (string, bool)) ServerEnvExpansion {
	expanded := cloneMCPServer(server)
	var missing []string
	expandString := func(value string) string {
		result := expandEnvVarsInString(value, lookup)
		missing = append(missing, result.MissingVars...)
		return result.Expanded
	}

	switch Transport(server) {
	case "", TransportStdio:
		expanded.Command = expandString(server.Command)
		expanded.Args = expandStringSlice(server.Args, expandString)
		expanded.Env = expandStringMap(server.Env, expandString)
	case TransportSSE, TransportHTTP, TransportWS:
		expanded.URL = expandString(server.URL)
		expanded.Headers = expandStringMap(server.Headers, expandString)
	}

	return ServerEnvExpansion{
		Expanded:    expanded,
		MissingVars: uniqueStrings(missing),
	}
}

func cloneMCPServer(server contracts.MCPServer) contracts.MCPServer {
	out := server
	out.Args = append([]string(nil), server.Args...)
	out.Env = cloneStringMap(server.Env)
	out.Headers = cloneStringMap(server.Headers)
	return out
}

func expandStringSlice(values []string, expand func(string) string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = expand(value)
	}
	return out
}

func expandStringMap(values map[string]string, expand func(string) string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out[key] = expand(values[key])
	}
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
