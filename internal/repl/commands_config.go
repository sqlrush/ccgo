package repl

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"ccgo/internal/contracts"
)

// configHandler returns a CommandHandler for /config that renders a summary of
// key settings from the merged config. It is VIEW-ONLY: reads contracts.Settings
// and formats them as text; no write operations are performed.
//
// CMD-CONFIG-01: in headless mode the conversation runner's formatConfigSummary
// is authoritative; this handler provides the same information in the interactive
// REPL so the command doesn't fall through to the model.
//
// CC ref: src/commands/config/index.ts:5
func configHandler(settingsLoader func() contracts.Settings, cwd string) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		s := settingsLoader()
		dir := cwd
		if cc.CWD != "" {
			dir = cc.CWD
		}
		return CommandOutcome{
			Handled: true,
			Status:  formatREPLConfigSummary(s, dir),
		}, nil
	}
}

// pluginHandler returns a CommandHandler for /plugin that renders a summary of
// loaded plugins from the merged settings. VIEW-ONLY.
//
// CMD-PLUGIN-01: provides interactive output so /plugin doesn't fall through.
//
// CC ref: src/commands/plugin/index.tsx:4
func pluginHandler(settingsLoader func() contracts.Settings) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		s := settingsLoader()
		return CommandOutcome{
			Handled: true,
			Status:  formatREPLPluginSummary(s),
		}, nil
	}
}

// formatREPLConfigSummary renders key config fields as a human-readable string.
// Subset of conversation.Runner.formatConfigSummary focusing on fields available
// via contracts.Settings without a full conversation.Runner context.
func formatREPLConfigSummary(s contracts.Settings, cwd string) string {
	if cwd == "" {
		cwd = "."
	}
	var b strings.Builder
	b.WriteString("Config\n")
	b.WriteString("Working directory: " + cwd + "\n")
	if s.Model != "" {
		b.WriteString("Model: " + s.Model + "\n")
	}
	// Permissions
	if s.Permissions != nil && (len(s.Permissions.Allow) > 0 || len(s.Permissions.Deny) > 0) {
		b.WriteString("\nPermissions:\n")
		for _, rule := range s.Permissions.Allow {
			b.WriteString(fmt.Sprintf("  allow: %s\n", rule))
		}
		for _, rule := range s.Permissions.Deny {
			b.WriteString(fmt.Sprintf("  deny: %s\n", rule))
		}
	} else {
		b.WriteString("Permissions: default (none configured)\n")
	}
	// Sandbox
	if len(s.Sandbox) > 0 {
		if enabled, ok := s.Sandbox["enabled"]; ok {
			b.WriteString(fmt.Sprintf("Sandbox: enabled=%v\n", enabled))
		} else {
			b.WriteString("Sandbox: configured\n")
		}
	}
	// Plugin count (defer to pluginHandler for details)
	pluginCount := len(s.Plugins)
	if pluginCount > 0 {
		b.WriteString(fmt.Sprintf("Plugins: %d loaded (use /plugin for details)\n", pluginCount))
	}
	// Hooks count (defer to /hooks for details)
	if len(s.Hooks) > 0 {
		b.WriteString(fmt.Sprintf("Hooks: %d event type(s) (use /hooks for details)\n", len(s.Hooks)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatREPLPluginSummary renders a human-readable summary of configured plugins.
func formatREPLPluginSummary(s contracts.Settings) string {
	total := len(s.Plugins)
	if total == 0 && len(s.PluginConfigs) == 0 {
		return "No plugins configured."
	}
	var b strings.Builder
	b.WriteString("Plugins\n")
	// PluginConfigs is the primary source; Plugins is a legacy key.
	names := make([]string, 0, len(s.PluginConfigs))
	for name := range s.PluginConfigs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b.WriteString(fmt.Sprintf("  %s\n", name))
	}
	// Legacy plugins map
	legacyNames := make([]string, 0, len(s.Plugins))
	for name := range s.Plugins {
		if _, already := s.PluginConfigs[name]; !already {
			legacyNames = append(legacyNames, name)
		}
	}
	sort.Strings(legacyNames)
	for _, name := range legacyNames {
		b.WriteString(fmt.Sprintf("  %s: (legacy)\n", name))
	}
	enabled := len(s.EnabledPlugins)
	if enabled > 0 {
		b.WriteString(fmt.Sprintf("\nEnabled plugins override: %d\n", enabled))
	}
	return strings.TrimRight(b.String(), "\n")
}
