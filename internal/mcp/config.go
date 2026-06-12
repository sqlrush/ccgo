package mcp

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"ccgo/internal/contracts"
)

const (
	TransportStdio         = "stdio"
	TransportSSE           = "sse"
	TransportSSEIDE        = "sse-ide"
	TransportHTTP          = "http"
	TransportWS            = "ws"
	TransportWSIDE         = "ws-ide"
	TransportSDK           = "sdk"
	TransportClaudeAIProxy = "claudeai-proxy"
)

var CCRProxyPathMarkers = []string{
	"/v2/session_ingress/shttp/mcp/",
	"/v2/ccr-sessions/",
}

type SuppressedServer struct {
	Name        string
	DuplicateOf string
}

type DedupResult struct {
	Servers    map[string]contracts.MCPServer
	Suppressed []SuppressedServer
}

func Transport(server contracts.MCPServer) string {
	transport := strings.TrimSpace(server.Type)
	if transport == "" && strings.TrimSpace(server.Command) != "" {
		return TransportStdio
	}
	return transport
}

func ServerCommandArray(server contracts.MCPServer) ([]string, bool) {
	transport := Transport(server)
	if transport != "" && transport != TransportStdio {
		return nil, false
	}
	command := strings.TrimSpace(server.Command)
	if command == "" {
		return nil, false
	}
	out := make([]string, 0, len(server.Args)+1)
	out = append(out, command)
	out = append(out, server.Args...)
	return out, true
}

func ServerURL(server contracts.MCPServer) (string, bool) {
	raw := strings.TrimSpace(server.URL)
	if raw == "" {
		return "", false
	}
	return raw, true
}

func UnwrapCCRProxyURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	for _, marker := range CCRProxyPathMarkers {
		if strings.Contains(parsed.Path, marker) {
			if target := parsed.Query().Get("mcp_url"); target != "" {
				return target
			}
			return raw
		}
	}
	return raw
}

func ServerSignature(server contracts.MCPServer) (string, bool) {
	if command, ok := ServerCommandArray(server); ok {
		encoded, err := json.Marshal(command)
		if err != nil {
			return "", false
		}
		return "stdio:" + string(encoded), true
	}
	if rawURL, ok := ServerURL(server); ok {
		return "url:" + UnwrapCCRProxyURL(rawURL), true
	}
	return "", false
}

func DedupPluginServers(pluginServers, manualServers map[string]contracts.MCPServer) DedupResult {
	result := DedupResult{
		Servers: make(map[string]contracts.MCPServer, len(pluginServers)),
	}
	signatures := map[string]string{}

	for _, name := range sortedServerNames(manualServers) {
		if signature, ok := ServerSignature(manualServers[name]); ok {
			signatures[signature] = name
		}
	}

	for _, name := range sortedServerNames(pluginServers) {
		server := pluginServers[name]
		signature, hasSignature := ServerSignature(server)
		if hasSignature {
			if existingName, duplicate := signatures[signature]; duplicate {
				result.Suppressed = append(result.Suppressed, SuppressedServer{
					Name:        name,
					DuplicateOf: existingName,
				})
				continue
			}
			signatures[signature] = name
		}
		result.Servers[name] = server
	}

	return result
}

func sortedServerNames(servers map[string]contracts.MCPServer) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
