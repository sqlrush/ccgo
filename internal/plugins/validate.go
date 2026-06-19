package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ManifestValidationMessage struct {
	Path    string
	Message string
	Code    string
}

type ManifestValidationResult struct {
	Success        bool
	Errors         []ManifestValidationMessage
	Warnings       []ManifestValidationMessage
	FilePath       string
	FileType       string
	Plugin         LoadedPlugin
	PluginCount    int
	MarketplaceIDs []string
}

func ValidateManifestPath(path string, cwd string) (ManifestValidationResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ManifestValidationResult{}, fmt.Errorf("manifest path is empty")
	}
	absolutePath := resolveValidationPath(path, cwd)
	info, err := os.Stat(absolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			return validationReadError(absolutePath, "plugin", "file", "File not found: "+absolutePath, "ENOENT"), nil
		}
		return ManifestValidationResult{}, err
	}
	if info.IsDir() {
		return validateManifestDirectory(absolutePath), nil
	}
	switch detectManifestType(absolutePath) {
	case "marketplace":
		return validateMarketplaceManifestFile(absolutePath), nil
	case "plugin":
		return validatePluginManifestFile(absolutePath), nil
	default:
		return validateUnknownManifestFile(absolutePath), nil
	}
}

func resolveValidationPath(path string, cwd string) string {
	if !filepath.IsAbs(path) {
		if strings.TrimSpace(cwd) == "" {
			cwd = "."
		}
		path = filepath.Join(cwd, path)
	}
	return cleanAbs(path)
}

func validateManifestDirectory(dir string) ManifestValidationResult {
	candidates := []struct {
		path     string
		fileType string
	}{
		{filepath.Join(dir, ".claude-plugin", "marketplace.json"), "marketplace"},
		{filepath.Join(dir, ".claude-plugin", "plugin.json"), "plugin"},
		{filepath.Join(dir, "marketplace.json"), "marketplace"},
		{filepath.Join(dir, ManifestFileName), "plugin"},
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate.path); err != nil {
			continue
		}
		if candidate.fileType == "marketplace" {
			return validateMarketplaceManifestFile(candidate.path)
		}
		return validatePluginManifestFile(candidate.path)
	}
	return ManifestValidationResult{
		Success:  false,
		FilePath: dir,
		FileType: "plugin",
		Errors: []ManifestValidationMessage{{
			Path:    "directory",
			Message: "No manifest found in directory. Expected .claude-plugin/marketplace.json or .claude-plugin/plugin.json",
		}},
	}
}

func detectManifestType(path string) string {
	switch strings.ToLower(filepath.Base(path)) {
	case ManifestFileName:
		return "plugin"
	case "marketplace.json":
		return "marketplace"
	default:
		return ""
	}
}

func validateUnknownManifestFile(path string) ManifestValidationResult {
	raw, readResult, ok := readManifestJSON(path, "plugin")
	if !ok {
		return readResult
	}
	if object, ok := raw.(map[string]any); ok {
		if _, hasPlugins := object["plugins"].([]any); hasPlugins {
			return validateMarketplaceManifestRaw(path, raw)
		}
	}
	if _, ok := raw.([]any); ok {
		return validateMarketplaceManifestRaw(path, raw)
	}
	return validatePluginManifestRaw(path, raw)
}

func validatePluginManifestFile(path string) ManifestValidationResult {
	raw, readResult, ok := readManifestJSON(path, "plugin")
	if !ok {
		return readResult
	}
	return validatePluginManifestRaw(path, raw)
}

func validatePluginManifestRaw(path string, raw any) ManifestValidationResult {
	result := ManifestValidationResult{
		Success:  true,
		FilePath: cleanAbs(path),
		FileType: "plugin",
	}
	object, ok := raw.(map[string]any)
	if !ok {
		result.Errors = append(result.Errors, ManifestValidationMessage{Path: "root", Message: "Plugin manifest must be a JSON object"})
		result.Success = false
		return result
	}
	for _, warning := range pluginMarketplaceFieldWarnings(object) {
		result.Warnings = append(result.Warnings, warning)
	}
	result.Errors = append(result.Errors, pluginManifestTypeErrors(object)...)
	result.Errors = append(result.Errors, pluginComponentPathErrors(object)...)
	result.Errors = append(result.Errors, unknownPluginManifestFieldErrors(object)...)
	if len(result.Errors) == 0 {
		parsed, plugin := parsePluginManifestForValidation(path, object)
		result.Plugin = plugin
		if strings.TrimSpace(parsed.Name) == "" {
			result.Errors = append(result.Errors, ManifestValidationMessage{Path: "name", Message: "Plugin name is required"})
		} else if strings.Contains(parsed.Name, " ") {
			result.Errors = append(result.Errors, ManifestValidationMessage{Path: "name", Message: "Plugin name cannot contain spaces. Use kebab-case (e.g., \"my-plugin\")"})
		} else if !isKebabCaseName(parsed.Name) {
			result.Warnings = append(result.Warnings, ManifestValidationMessage{Path: "name", Message: fmt.Sprintf("Plugin name %q is not kebab-case. Claude Code accepts it, but marketplace sync expects lowercase letters, digits, and hyphens.", parsed.Name)})
		}
		if strings.TrimSpace(parsed.Version) == "" {
			result.Warnings = append(result.Warnings, ManifestValidationMessage{Path: "version", Message: "No version specified. Consider adding a version following semver (e.g., \"1.0.0\")"})
		}
		if strings.TrimSpace(parsed.Description) == "" {
			result.Warnings = append(result.Warnings, ManifestValidationMessage{Path: "description", Message: "No description provided. Adding a description helps users understand what your plugin does"})
		}
		if _, ok := object["author"]; !ok {
			result.Warnings = append(result.Warnings, ManifestValidationMessage{Path: "author", Message: "No author information provided. Consider adding author details for plugin attribution"})
		}
	}
	result.Success = len(result.Errors) == 0
	return result
}

func readManifestJSON(path string, fileType string) (any, ManifestValidationResult, bool) {
	path = cleanAbs(path)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, validationReadError(path, fileType, "file", "File not found: "+path, "ENOENT"), false
		}
		return nil, validationReadError(path, fileType, "file", "Failed to read file: "+err.Error(), ""), false
	}
	if info.IsDir() {
		return nil, validationReadError(path, fileType, "file", "Path is not a file: "+path, "EISDIR"), false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, validationReadError(path, fileType, "file", "Failed to read file: "+err.Error(), ""), false
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, ManifestValidationResult{
			Success:  false,
			FilePath: path,
			FileType: fileType,
			Errors: []ManifestValidationMessage{{
				Path:    "json",
				Message: "Invalid JSON syntax: " + err.Error(),
			}},
		}, false
	}
	return raw, ManifestValidationResult{}, true
}

func validationReadError(path string, fileType string, messagePath string, message string, code string) ManifestValidationResult {
	return ManifestValidationResult{
		Success:  false,
		FilePath: cleanAbs(path),
		FileType: fileType,
		Errors: []ManifestValidationMessage{{
			Path:    messagePath,
			Message: message,
			Code:    code,
		}},
	}
}

func parsePluginManifestForValidation(path string, object map[string]any) (manifest, LoadedPlugin) {
	data, _ := json.Marshal(object)
	var parsed manifest
	_ = json.Unmarshal(data, &parsed)
	root := pluginValidationRoot(path)
	name := firstNonEmpty(parsed.Name, filepath.Base(root))
	plugin := LoadedPlugin{
		Root:        root,
		Name:        name,
		Version:     strings.TrimSpace(parsed.Version),
		Description: strings.TrimSpace(parsed.Description),
		Marketplace: pluginMarketplaceName(parsed),
	}
	commands, prompts := pluginCommands(root, name, parsed.Commands)
	plugin.Commands = append(plugin.Commands, commands...)
	plugin.PromptTemplates = append(plugin.PromptTemplates, prompts...)
	skillPrompts := pluginSkillPrompts(root, name, parsed.Skills)
	plugin.PromptTemplates = append(plugin.PromptTemplates, skillPrompts...)
	for _, prompt := range skillPrompts {
		plugin.SkillCommands = append(plugin.SkillCommands, prompt.Command)
	}
	plugin.MCPServers = pluginMCPServers(root, name, parsed.MCPServers, parsed.MCPServersSnake)
	plugin.Agents = pluginAgents(root, name, parsed.Agents)
	plugin.Hooks = pluginHooks(root, parsed.Hooks)
	plugin.HookEvents = pluginHookEventsFromRaw(plugin.Hooks)
	plugin.OutputStyles = pluginOutputStyles(root, name, parsed.OutputStyles)
	return parsed, plugin
}

func pluginValidationRoot(path string) string {
	dir := filepath.Dir(cleanAbs(path))
	if strings.EqualFold(filepath.Base(dir), ".claude-plugin") {
		return filepath.Dir(dir)
	}
	return dir
}

func pluginMarketplaceFieldWarnings(object map[string]any) []ManifestValidationMessage {
	marketplaceOnly := map[string]struct{}{
		"category": {},
		"source":   {},
		"tags":     {},
		"strict":   {},
		"id":       {},
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		if _, ok := marketplaceOnly[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	warnings := make([]ManifestValidationMessage, 0, len(keys))
	for _, key := range keys {
		warnings = append(warnings, ManifestValidationMessage{
			Path:    key,
			Message: fmt.Sprintf("Field %q belongs in the marketplace entry (marketplace.json), not plugin.json. It is ignored at load time.", key),
		})
	}
	return warnings
}

func pluginManifestTypeErrors(object map[string]any) []ManifestValidationMessage {
	var errors []ManifestValidationMessage
	stringFields := []string{"name", "displayName", "description", "version", "marketplace", "marketplaceName", "marketplace_name", "homepage", "repository", "license"}
	for _, field := range stringFields {
		if value, ok := object[field]; ok && value != nil {
			if _, isString := value.(string); !isString {
				errors = append(errors, ManifestValidationMessage{Path: field, Message: fmt.Sprintf("%s must be a string", field)})
			}
		}
	}
	if value, ok := object["keywords"]; ok && value != nil {
		items, ok := value.([]any)
		if !ok {
			errors = append(errors, ManifestValidationMessage{Path: "keywords", Message: "keywords must be an array of strings"})
		} else {
			for i, item := range items {
				if _, ok := item.(string); !ok {
					errors = append(errors, ManifestValidationMessage{Path: fmt.Sprintf("keywords[%d]", i), Message: "keyword must be a string"})
				}
			}
		}
	}
	if value, ok := object["author"]; ok && value != nil {
		if _, ok := value.(map[string]any); !ok {
			errors = append(errors, ManifestValidationMessage{Path: "author", Message: "author must be an object"})
		}
	}
	return errors
}

func pluginComponentPathErrors(object map[string]any) []ManifestValidationMessage {
	var errors []ManifestValidationMessage
	for _, field := range []string{"commands", "agents", "skills"} {
		for index, value := range validationArrayish(object[field]) {
			text, ok := value.(string)
			if !ok {
				continue
			}
			if strings.Contains(text, "..") {
				errors = append(errors, ManifestValidationMessage{Path: fmt.Sprintf("%s[%d]", field, index), Message: fmt.Sprintf("Path contains \"..\" which could be a path traversal attempt: %s", text)})
			}
		}
	}
	return errors
}

func validationArrayish(value any) []any {
	if value == nil {
		return nil
	}
	if items, ok := value.([]any); ok {
		return items
	}
	return []any{value}
}

func unknownPluginManifestFieldErrors(object map[string]any) []ManifestValidationMessage {
	known := map[string]struct{}{
		"name": {}, "displayName": {}, "description": {}, "version": {}, "author": {}, "homepage": {}, "repository": {}, "license": {}, "keywords": {}, "dependencies": {},
		"marketplace": {}, "marketplaceName": {}, "marketplace_name": {},
		"commands": {}, "skills": {}, "agents": {}, "hooks": {}, "outputStyles": {}, "mcpServers": {}, "mcp_servers": {},
		"category": {}, "source": {}, "tags": {}, "strict": {}, "id": {},
	}
	var keys []string
	for key := range object {
		if _, ok := known[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	errors := make([]ManifestValidationMessage, 0, len(keys))
	for _, key := range keys {
		errors = append(errors, ManifestValidationMessage{Path: key, Message: fmt.Sprintf("Unrecognized key: %s", key)})
	}
	return errors
}

func isKebabCaseName(name string) bool {
	if name == "" {
		return false
	}
	lastDash := false
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			lastDash = false
		case r >= '0' && r <= '9':
			lastDash = false
		case r == '-':
			if i == 0 || lastDash {
				return false
			}
			lastDash = true
		default:
			return false
		}
	}
	return !lastDash
}

func validateMarketplaceManifestFile(path string) ManifestValidationResult {
	raw, readResult, ok := readManifestJSON(path, "marketplace")
	if !ok {
		return readResult
	}
	return validateMarketplaceManifestRaw(path, raw)
}

func validateMarketplaceManifestRaw(path string, raw any) ManifestValidationResult {
	result := ManifestValidationResult{
		Success:  true,
		FilePath: cleanAbs(path),
		FileType: "marketplace",
	}
	plugins, rootOK := marketplaceValidationPlugins(raw)
	if !rootOK {
		result.Errors = append(result.Errors, ManifestValidationMessage{Path: "root", Message: "Marketplace manifest must be a JSON object with a plugins array or an array of plugin entries"})
		result.Success = false
		return result
	}
	if len(plugins) == 0 {
		result.Warnings = append(result.Warnings, ManifestValidationMessage{Path: "plugins", Message: "Marketplace has no plugins defined"})
	}
	seenNames := map[string]int{}
	for i, plugin := range plugins {
		name := stringFromValidationMap(plugin, "name")
		if name == "" {
			result.Errors = append(result.Errors, ManifestValidationMessage{Path: fmt.Sprintf("plugins[%d].name", i), Message: "Plugin entry name is required"})
		} else {
			seenNames[name]++
			if seenNames[name] > 1 {
				result.Errors = append(result.Errors, ManifestValidationMessage{Path: fmt.Sprintf("plugins[%d].name", i), Message: fmt.Sprintf("Duplicate plugin name %q found in marketplace", name)})
			}
		}
		if source, ok := plugin["source"]; ok {
			switch typed := source.(type) {
			case string:
				if strings.TrimSpace(typed) == "" {
					result.Errors = append(result.Errors, ManifestValidationMessage{Path: fmt.Sprintf("plugins[%d].source", i), Message: "Plugin source is required"})
				}
				if strings.Contains(typed, "..") {
					result.Errors = append(result.Errors, ManifestValidationMessage{Path: fmt.Sprintf("plugins[%d].source", i), Message: marketplaceSourceTraversalMessage(typed)})
				}
			case map[string]any:
				if path := stringFromValidationMap(typed, "path"); strings.Contains(path, "..") {
					result.Errors = append(result.Errors, ManifestValidationMessage{Path: fmt.Sprintf("plugins[%d].source.path", i), Message: fmt.Sprintf("Path contains \"..\" which could be a path traversal attempt: %s", path)})
				}
			default:
				result.Errors = append(result.Errors, ManifestValidationMessage{Path: fmt.Sprintf("plugins[%d].source", i), Message: "Plugin source must be a string or object"})
			}
		} else {
			result.Errors = append(result.Errors, ManifestValidationMessage{Path: fmt.Sprintf("plugins[%d].source", i), Message: "Plugin source is required"})
		}
	}
	if object, ok := raw.(map[string]any); ok {
		if metadata, ok := object["metadata"].(map[string]any); !ok || strings.TrimSpace(stringFromValidationMap(metadata, "description")) == "" {
			result.Warnings = append(result.Warnings, ManifestValidationMessage{Path: "metadata.description", Message: "No marketplace description provided. Adding a description helps users understand what this marketplace offers"})
		}
	}
	result.PluginCount = len(plugins)
	result.MarketplaceIDs = marketplaceValidationIDs(plugins)
	result.Success = len(result.Errors) == 0
	return result
}

func marketplaceValidationPlugins(raw any) ([]map[string]any, bool) {
	switch typed := raw.(type) {
	case []any:
		return marketplaceValidationPluginItems(typed), true
	case map[string]any:
		plugins, ok := typed["plugins"].([]any)
		if !ok {
			return nil, false
		}
		return marketplaceValidationPluginItems(plugins), true
	default:
		return nil, false
	}
}

func marketplaceValidationPluginItems(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			out = append(out, object)
			continue
		}
		out = append(out, map[string]any{})
	}
	return out
}

func marketplaceValidationIDs(plugins []map[string]any) []string {
	ids := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		name := stringFromValidationMap(plugin, "name")
		if name == "" {
			name = stringFromValidationMap(plugin, "id")
		}
		if name != "" {
			ids = append(ids, name)
		}
	}
	sort.Strings(ids)
	return ids
}

func marketplaceSourceTraversalMessage(path string) string {
	corrected := strings.TrimLeft(path, ".")
	corrected = strings.TrimLeft(corrected, "/")
	if corrected == "" || corrected == path {
		corrected = "plugins/my-plugin"
	}
	return fmt.Sprintf("Path contains \"..\": %s. Plugin source paths are resolved relative to the marketplace root. Use \"./%s\" instead.", path, corrected)
}

func stringFromValidationMap(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}
