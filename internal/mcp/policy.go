package mcp

import (
	"regexp"
	"strings"

	"ccgo/internal/contracts"
)

type Policy struct {
	AllowlistSet bool
	Allowed      []contracts.MCPServerPolicyEntry
	Denied       []contracts.MCPServerPolicyEntry
}

type PolicyDecision struct {
	Allowed bool
	Reason  string
}

func PolicyFromSettings(settings contracts.Settings) Policy {
	return Policy{
		AllowlistSet: settings.AllowedMCPServers != nil,
		Allowed:      settings.AllowedMCPServers,
		Denied:       settings.DeniedMCPServers,
	}
}

func EvaluatePolicy(name string, server contracts.MCPServer, policy Policy) PolicyDecision {
	if Transport(server) == TransportSDK {
		return PolicyDecision{Allowed: true, Reason: "sdk-exempt"}
	}
	if IsServerDenied(name, server, policy.Denied) {
		return PolicyDecision{Allowed: false, Reason: "denied"}
	}
	if !policy.AllowlistSet {
		return PolicyDecision{Allowed: true, Reason: "allowlist-unset"}
	}
	if len(policy.Allowed) == 0 {
		return PolicyDecision{Allowed: false, Reason: "allowlist-empty"}
	}
	if isAllowedByEntries(name, server, policy.Allowed) {
		return PolicyDecision{Allowed: true, Reason: "allowed"}
	}
	return PolicyDecision{Allowed: false, Reason: "not-allowed"}
}

func IsServerDenied(name string, server contracts.MCPServer, entries []contracts.MCPServerPolicyEntry) bool {
	for _, entry := range entries {
		if entryName := policyEntryName(entry); entryName != "" && entryName == name {
			return true
		}
	}

	if command, ok := ServerCommandArray(server); ok {
		for _, entry := range entries {
			if policyEntryCommandMatches(entry, command) {
				return true
			}
		}
	}

	if rawURL, ok := ServerURL(server); ok {
		for _, entry := range entries {
			if pattern := policyEntryURL(entry); pattern != "" && urlMatchesPattern(rawURL, pattern) {
				return true
			}
		}
	}

	return false
}

func FilterServersByPolicy(servers map[string]contracts.MCPServer, policy Policy) (map[string]contracts.MCPServer, []string) {
	allowed := make(map[string]contracts.MCPServer, len(servers))
	var blocked []string

	for _, name := range sortedServerNames(servers) {
		server := servers[name]
		if EvaluatePolicy(name, server, policy).Allowed {
			allowed[name] = server
		} else {
			blocked = append(blocked, name)
		}
	}

	return allowed, blocked
}

func isAllowedByEntries(name string, server contracts.MCPServer, entries []contracts.MCPServerPolicyEntry) bool {
	hasCommandEntries := false
	hasURLEntries := false
	for _, entry := range entries {
		if len(policyEntryCommand(entry)) > 0 {
			hasCommandEntries = true
		}
		if policyEntryURL(entry) != "" {
			hasURLEntries = true
		}
	}

	if command, ok := ServerCommandArray(server); ok {
		if hasCommandEntries {
			for _, entry := range entries {
				if policyEntryCommandMatches(entry, command) {
					return true
				}
			}
			return false
		}
		return policyNameMatches(entries, name)
	}

	if rawURL, ok := ServerURL(server); ok {
		if hasURLEntries {
			for _, entry := range entries {
				if pattern := policyEntryURL(entry); pattern != "" && urlMatchesPattern(rawURL, pattern) {
					return true
				}
			}
			return false
		}
		return policyNameMatches(entries, name)
	}

	return policyNameMatches(entries, name)
}

func policyNameMatches(entries []contracts.MCPServerPolicyEntry, name string) bool {
	for _, entry := range entries {
		if entryName := policyEntryName(entry); entryName != "" && entryName == name {
			return true
		}
	}
	return false
}

func policyEntryName(entry contracts.MCPServerPolicyEntry) string {
	if entry.ServerName != "" {
		return entry.ServerName
	}
	return entry.Name
}

func policyEntryCommand(entry contracts.MCPServerPolicyEntry) []string {
	if len(entry.ServerCommand) > 0 {
		return entry.ServerCommand
	}
	if entry.Command != "" {
		return []string{entry.Command}
	}
	return nil
}

func policyEntryCommandMatches(entry contracts.MCPServerPolicyEntry, command []string) bool {
	expected := policyEntryCommand(entry)
	if len(expected) != len(command) {
		return false
	}
	for i := range expected {
		if expected[i] != command[i] {
			return false
		}
	}
	return len(expected) > 0
}

func policyEntryURL(entry contracts.MCPServerPolicyEntry) string {
	if entry.ServerURL != "" {
		return entry.ServerURL
	}
	return entry.URL
}

func urlMatchesPattern(rawURL, pattern string) bool {
	var builder strings.Builder
	builder.WriteString("^")
	for _, r := range pattern {
		if r == '*' {
			builder.WriteString(".*")
			continue
		}
		builder.WriteString(regexp.QuoteMeta(string(r)))
	}
	builder.WriteString("$")
	return regexp.MustCompile(builder.String()).MatchString(rawURL)
}
