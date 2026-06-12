package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"ccgo/internal/contracts"
)

type Config struct {
	MCPServers map[string]contracts.MCPServer `json:"mcpServers"`
}

type ParseOptions struct {
	ExpandVars bool
	Scope      string
	FilePath   string
	Env        map[string]string
	UseEnvMap  bool
}

type ValidationError struct {
	File       string
	Path       string
	Message    string
	Suggestion string
	Scope      string
	ServerName string
	Severity   string
}

type ParseResult struct {
	Config *Config
	Errors []ValidationError
}

func ParseConfigJSON(data []byte, options ParseOptions) (ParseResult, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return ParseResult{
			Config: nil,
			Errors: []ValidationError{{
				File:     options.FilePath,
				Path:     "",
				Message:  "Does not adhere to MCP server configuration schema",
				Scope:    options.Scope,
				Severity: "fatal",
			}},
		}, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return ParseResult{}, err
	}
	return ParseConfigRaw(raw, options), nil
}

func ParseConfigFile(path string, options ParseOptions) (ParseResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if options.FilePath == "" {
				options.FilePath = path
			}
			return ParseResult{
				Config: nil,
				Errors: []ValidationError{{
					File:     options.FilePath,
					Path:     "",
					Message:  "MCP config file not found",
					Scope:    options.Scope,
					Severity: "fatal",
				}},
			}, nil
		}
		return ParseResult{}, err
	}
	if options.FilePath == "" {
		options.FilePath = path
	}
	return ParseConfigJSON(data, options)
}

func ParseConfigRaw(raw map[string]json.RawMessage, options ParseOptions) ParseResult {
	serversRaw, ok := raw["mcpServers"]
	if !ok {
		return fatalParseResult(options, "mcpServers", "Does not adhere to MCP server configuration schema")
	}

	var serverObjects map[string]json.RawMessage
	if err := json.Unmarshal(serversRaw, &serverObjects); err != nil {
		return fatalParseResult(options, "mcpServers", "Does not adhere to MCP server configuration schema")
	}

	var errors []ValidationError
	servers := make(map[string]contracts.MCPServer, len(serverObjects))
	for _, name := range sortedRawNames(serverObjects) {
		server, serverErrors := parseServer(name, serverObjects[name], options)
		if len(serverErrors) > 0 {
			errors = append(errors, serverErrors...)
			continue
		}
		if options.ExpandVars {
			expansion := expandServerWithOptions(server, options)
			if len(expansion.MissingVars) > 0 {
				errors = append(errors, ValidationError{
					File:       options.FilePath,
					Path:       "mcpServers." + name,
					Message:    fmt.Sprintf("Missing environment variables: %s", strings.Join(expansion.MissingVars, ", ")),
					Suggestion: fmt.Sprintf("Set the following environment variables: %s", strings.Join(expansion.MissingVars, ", ")),
					Scope:      options.Scope,
					ServerName: name,
					Severity:   "warning",
				})
			}
			server = expansion.Expanded
		}
		servers[name] = server
	}

	if hasFatal(errors) {
		return ParseResult{Config: nil, Errors: errors}
	}
	return ParseResult{Config: &Config{MCPServers: servers}, Errors: errors}
}

func parseServer(name string, data json.RawMessage, options ParseOptions) (contracts.MCPServer, []ValidationError) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return contracts.MCPServer{}, []ValidationError{schemaError(options, "mcpServers."+name, name)}
	}

	var server contracts.MCPServer
	if err := json.Unmarshal(data, &server); err != nil {
		return contracts.MCPServer{}, []ValidationError{schemaError(options, "mcpServers."+name, name)}
	}

	var errors []ValidationError
	switch Transport(server) {
	case "", TransportStdio:
		if strings.TrimSpace(server.Command) == "" {
			errors = append(errors, schemaError(options, "mcpServers."+name+".command", name))
		}
		if _, ok := raw["args"]; !ok {
			server.Args = []string{}
		}
		errors = append(errors, validateOptionalStringArray(raw, "args", options, name)...)
		errors = append(errors, validateOptionalStringMap(raw, "env", options, name)...)
	case TransportSSE, TransportHTTP, TransportWS:
		if _, ok := raw["url"]; !ok || server.URL == "" && !rawStringFieldIsPresent(raw, "url") {
			errors = append(errors, schemaError(options, "mcpServers."+name+".url", name))
		}
		errors = append(errors, validateOptionalStringMap(raw, "headers", options, name)...)
		errors = append(errors, validateOptionalString(raw, "headersHelper", options, name)...)
	case TransportSSEIDE:
		if _, ok := raw["url"]; !ok || server.URL == "" && !rawStringFieldIsPresent(raw, "url") {
			errors = append(errors, schemaError(options, "mcpServers."+name+".url", name))
		}
		if strings.TrimSpace(server.IDEName) == "" {
			errors = append(errors, schemaError(options, "mcpServers."+name+".ideName", name))
		}
	case TransportWSIDE:
		if _, ok := raw["url"]; !ok || server.URL == "" && !rawStringFieldIsPresent(raw, "url") {
			errors = append(errors, schemaError(options, "mcpServers."+name+".url", name))
		}
		if strings.TrimSpace(server.IDEName) == "" {
			errors = append(errors, schemaError(options, "mcpServers."+name+".ideName", name))
		}
		errors = append(errors, validateOptionalString(raw, "authToken", options, name)...)
	case TransportSDK:
		if strings.TrimSpace(server.Name) == "" {
			errors = append(errors, schemaError(options, "mcpServers."+name+".name", name))
		}
	case TransportClaudeAIProxy:
		if _, ok := raw["url"]; !ok || server.URL == "" && !rawStringFieldIsPresent(raw, "url") {
			errors = append(errors, schemaError(options, "mcpServers."+name+".url", name))
		}
		if strings.TrimSpace(server.ID) == "" {
			errors = append(errors, schemaError(options, "mcpServers."+name+".id", name))
		}
	default:
		errors = append(errors, schemaError(options, "mcpServers."+name+".type", name))
	}

	if len(errors) > 0 {
		return contracts.MCPServer{}, errors
	}
	server.Scope = options.Scope
	return server, nil
}

func validateOptionalStringArray(raw map[string]json.RawMessage, field string, options ParseOptions, name string) []ValidationError {
	value, ok := raw[field]
	if !ok {
		return nil
	}
	var parsed []string
	if err := json.Unmarshal(value, &parsed); err != nil {
		return []ValidationError{schemaError(options, "mcpServers."+name+"."+field, name)}
	}
	return nil
}

func validateOptionalStringMap(raw map[string]json.RawMessage, field string, options ParseOptions, name string) []ValidationError {
	value, ok := raw[field]
	if !ok {
		return nil
	}
	var parsed map[string]string
	if err := json.Unmarshal(value, &parsed); err != nil {
		return []ValidationError{schemaError(options, "mcpServers."+name+"."+field, name)}
	}
	return nil
}

func validateOptionalString(raw map[string]json.RawMessage, field string, options ParseOptions, name string) []ValidationError {
	value, ok := raw[field]
	if !ok {
		return nil
	}
	var parsed string
	if err := json.Unmarshal(value, &parsed); err != nil {
		return []ValidationError{schemaError(options, "mcpServers."+name+"."+field, name)}
	}
	return nil
}

func rawStringFieldIsPresent(raw map[string]json.RawMessage, field string) bool {
	value, ok := raw[field]
	if !ok {
		return false
	}
	var parsed string
	return json.Unmarshal(value, &parsed) == nil
}

func schemaError(options ParseOptions, path string, serverName string) ValidationError {
	return ValidationError{
		File:       options.FilePath,
		Path:       path,
		Message:    "Does not adhere to MCP server configuration schema",
		Scope:      options.Scope,
		ServerName: serverName,
		Severity:   "fatal",
	}
}

func fatalParseResult(options ParseOptions, path string, message string) ParseResult {
	return ParseResult{
		Config: nil,
		Errors: []ValidationError{{
			File:     options.FilePath,
			Path:     path,
			Message:  message,
			Scope:    options.Scope,
			Severity: "fatal",
		}},
	}
}

func hasFatal(errors []ValidationError) bool {
	for _, err := range errors {
		if err.Severity == "fatal" {
			return true
		}
	}
	return false
}

func expandServerWithOptions(server contracts.MCPServer, options ParseOptions) ServerEnvExpansion {
	if options.UseEnvMap {
		return ExpandServerEnvVarsWithMap(server, options.Env)
	}
	return expandServerEnvVars(server, os.LookupEnv)
}

func sortedRawNames(values map[string]json.RawMessage) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
