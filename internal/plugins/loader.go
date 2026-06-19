package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/memory"
	"ccgo/internal/skills"
)

const ManifestFileName = "plugin.json"

type PromptTemplate struct {
	Command contracts.Command
	Content string
}

type PluginAgent struct {
	Name           string
	Path           string
	Description    string
	Prompt         string
	Model          string
	PermissionMode contracts.PermissionMode
	AllowedTools   []string
}

type PluginHookEvent struct {
	Event string
	Count int
}

type PluginOutputStyle struct {
	Name           string
	Path           string
	Description    string
	Prompt         string
	ForceForPlugin *bool
}

type LoadedPlugin struct {
	Root            string
	Name            string
	Version         string
	Description     string
	Marketplace     string
	Commands        []contracts.Command
	PromptTemplates []PromptTemplate
	SkillCommands   []contracts.Command
	MCPServers      map[string]contracts.MCPServer
	Agents          []PluginAgent
	Hooks           map[string]any
	HookEvents      []PluginHookEvent
	OutputStyles    []PluginOutputStyle
}

type manifest struct {
	Name             string `json:"name"`
	DisplayName      string `json:"displayName"`
	Description      string `json:"description"`
	Version          string `json:"version"`
	Marketplace      string `json:"marketplace"`
	MarketplaceName  string `json:"marketplaceName"`
	MarketplaceSnake string `json:"marketplace_name"`
	Source           any    `json:"source"`
	Commands         any    `json:"commands"`
	Skills           any    `json:"skills"`
	Agents           any    `json:"agents"`
	Hooks            any    `json:"hooks"`
	OutputStyles     any    `json:"outputStyles"`
	MCPServers       any    `json:"mcpServers"`
	MCPServersSnake  any    `json:"mcp_servers"`
}

type commandManifest struct {
	Type                   string   `json:"type"`
	Name                   string   `json:"name"`
	DisplayName            string   `json:"displayName"`
	Description            string   `json:"description"`
	ArgumentHint           string   `json:"argumentHint"`
	ArgumentNames          []string `json:"argumentNames"`
	Prompt                 string   `json:"prompt"`
	Content                string   `json:"content"`
	Path                   string   `json:"path"`
	AllowedTools           []string `json:"allowedTools"`
	AllowedToolsSnake      []string `json:"allowed_tools"`
	WhenToUse              string   `json:"whenToUse"`
	WhenToUseSnake         string   `json:"when_to_use"`
	Version                string   `json:"version"`
	Model                  string   `json:"model"`
	Context                string   `json:"context"`
	Agent                  string   `json:"agent"`
	Effort                 string   `json:"effort"`
	DisableModelInvocation bool     `json:"disableModelInvocation"`
	Hidden                 bool     `json:"hidden"`
}

type skillManifest struct {
	Path        string `json:"path"`
	Root        string `json:"root"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

type pluginRootEntry struct {
	Root        string
	Marketplace string
}

func LoadPluginDirs(roots []string) []LoadedPlugin {
	return loadPluginRootEntries(pluginRootEntriesFromRoots(roots))
}

func loadPluginRootEntries(roots []pluginRootEntry) []LoadedPlugin {
	out := make([]LoadedPlugin, 0, len(roots))
	seen := map[string]struct{}{}
	for _, entry := range roots {
		root := cleanAbs(entry.Root)
		key := normalizePath(root)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		plugin, err := LoadPluginDir(root)
		if err != nil {
			continue
		}
		if strings.TrimSpace(entry.Marketplace) != "" {
			plugin.Marketplace = strings.TrimSpace(entry.Marketplace)
		}
		out = append(out, plugin)
	}
	return out
}

func LoadPluginDirsWithSettings(roots []string, settings contracts.Settings) []LoadedPlugin {
	entries := pluginRootEntriesFromRoots(roots)
	entries = append(entries, marketplacePluginRootEntries(settings)...)
	return FilterPluginsWithSettings(loadPluginRootEntries(entries), settings)
}

func FilterPluginsWithSettings(plugins []LoadedPlugin, settings contracts.Settings) []LoadedPlugin {
	if len(plugins) == 0 {
		return nil
	}
	policy := NewMarketplacePolicy(settings)
	out := make([]LoadedPlugin, 0, len(plugins))
	for _, plugin := range plugins {
		if PluginEnabled(plugin, settings.EnabledPlugins) && PluginMarketplaceAllowed(plugin, policy) {
			out = append(out, plugin)
		}
	}
	return out
}

func FilterEnabledPlugins(plugins []LoadedPlugin, enabledPlugins map[string]any) []LoadedPlugin {
	if len(enabledPlugins) == 0 {
		return plugins
	}
	out := make([]LoadedPlugin, 0, len(plugins))
	for _, plugin := range plugins {
		if PluginEnabled(plugin, enabledPlugins) {
			out = append(out, plugin)
		}
	}
	return out
}

func PluginEnabled(plugin LoadedPlugin, enabledPlugins map[string]any) bool {
	for _, key := range pluginEnabledKeys(plugin) {
		if value, ok := enabledPlugins[key]; ok {
			enabled, recognized := pluginEnabledValue(value)
			if recognized && !enabled {
				return false
			}
		}
	}
	return true
}

func PluginMarketplaceAllowed(plugin LoadedPlugin, policy MarketplacePolicy) bool {
	if strings.TrimSpace(plugin.Marketplace) == "" {
		return true
	}
	return policy.Decision(plugin.Marketplace).Allowed
}

func pluginEnabledKeys(plugin LoadedPlugin) []string {
	seen := map[string]struct{}{}
	var keys []string
	for _, key := range []string{
		strings.TrimSpace(plugin.Name),
		strings.TrimSpace(filepath.Base(plugin.Root)),
		strings.TrimSpace(plugin.Root),
	} {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func pluginEnabledValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "enabled", "enable", "on", "1":
			return true, true
		case "false", "disabled", "disable", "off", "0":
			return false, true
		}
	case float64:
		if typed == 0 {
			return false, true
		}
		if typed == 1 {
			return true, true
		}
	case int:
		if typed == 0 {
			return false, true
		}
		if typed == 1 {
			return true, true
		}
	}
	return true, false
}

func LoadPluginDir(root string) (LoadedPlugin, error) {
	if strings.TrimSpace(root) == "" {
		return LoadedPlugin{}, fmt.Errorf("plugin root is empty")
	}
	root = cleanAbs(root)
	data, err := os.ReadFile(filepath.Join(root, ManifestFileName))
	if err != nil {
		return LoadedPlugin{}, err
	}
	var parsed manifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		return LoadedPlugin{}, err
	}
	name := firstNonEmpty(parsed.Name, filepath.Base(root))
	loaded := LoadedPlugin{
		Root:        root,
		Name:        name,
		Version:     strings.TrimSpace(parsed.Version),
		Description: strings.TrimSpace(parsed.Description),
		Marketplace: pluginMarketplaceName(parsed),
	}
	commands, prompts := pluginCommands(root, name, parsed.Commands)
	loaded.Commands = append(loaded.Commands, commands...)
	loaded.PromptTemplates = append(loaded.PromptTemplates, prompts...)
	skillPrompts := pluginSkillPrompts(root, name, parsed.Skills)
	loaded.PromptTemplates = append(loaded.PromptTemplates, skillPrompts...)
	for _, prompt := range skillPrompts {
		loaded.SkillCommands = append(loaded.SkillCommands, prompt.Command)
	}
	loaded.MCPServers = pluginMCPServers(root, name, parsed.MCPServers, parsed.MCPServersSnake)
	loaded.Agents = pluginAgents(root, name, parsed.Agents)
	loaded.Hooks = pluginHooks(root, parsed.Hooks)
	loaded.HookEvents = pluginHookEventsFromRaw(loaded.Hooks)
	loaded.OutputStyles = pluginOutputStyles(root, name, parsed.OutputStyles)
	return loaded, nil
}

func pluginMarketplaceName(parsed manifest) string {
	return firstNonEmpty(parsed.Marketplace, parsed.MarketplaceName, parsed.MarketplaceSnake, marketplaceNameFromAny(parsed.Source))
}

func pluginRootEntriesFromRoots(roots []string) []pluginRootEntry {
	entries := make([]pluginRootEntry, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		entries = append(entries, pluginRootEntry{Root: root})
	}
	return entries
}

func marketplacePluginRootEntries(settings contracts.Settings) []pluginRootEntry {
	if len(settings.ExtraKnownMarketplaces) == 0 {
		return nil
	}
	names := sortedMarketplaceMapKeys(settings.ExtraKnownMarketplaces)
	var entries []pluginRootEntry
	for _, name := range names {
		source, ok := settingsMarketplaceSource(settings.ExtraKnownMarketplaces[name])
		if !ok {
			continue
		}
		sourceType := strings.TrimSpace(stringFromAnyMap(source, "source"))
		marketplace := firstNonEmpty(stringFromAnyMap(source, "name"), name)
		switch sourceType {
		case "settings":
			plugins, _ := source["plugins"].([]any)
			for _, rawPlugin := range plugins {
				root := settingsMarketplacePluginRoot(rawPlugin)
				if root == "" {
					continue
				}
				entries = append(entries, pluginRootEntry{Root: root, Marketplace: marketplace})
			}
		case "directory":
			for _, root := range pluginRootsFromDirectory(stringFromAnyMap(source, "path")) {
				entries = append(entries, pluginRootEntry{Root: root, Marketplace: marketplace})
			}
		case "file":
			for _, root := range pluginRootsFromMarketplaceFile(stringFromAnyMap(source, "path")) {
				entries = append(entries, pluginRootEntry{Root: root, Marketplace: marketplace})
			}
		}
	}
	return entries
}

func pluginRootsFromDirectory(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return appendPluginRoots(nil, map[string]struct{}{}, path)
}

func pluginRootsFromMarketplaceFile(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return pluginRootsFromMarketplaceCatalog(raw)
}

func pluginRootsFromMarketplaceCatalog(raw any) []string {
	switch value := raw.(type) {
	case []any:
		roots := make([]string, 0, len(value))
		for _, item := range value {
			if root := settingsMarketplacePluginRoot(item); root != "" {
				roots = append(roots, root)
			}
		}
		return roots
	case map[string]any:
		if plugins, ok := value["plugins"].([]any); ok {
			return pluginRootsFromMarketplaceCatalog(plugins)
		}
		if root := settingsMarketplacePluginRoot(value); root != "" {
			return []string{root}
		}
	}
	return nil
}

func settingsMarketplaceSource(raw any) (map[string]any, bool) {
	entry, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	source, ok := entry["source"].(map[string]any)
	return source, ok
}

func settingsMarketplacePluginRoot(raw any) string {
	if text, ok := raw.(string); ok {
		return strings.TrimSpace(text)
	}
	item, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"path", "root", "dir", "directory"} {
		if value := stringFromAnyMap(item, key); value != "" {
			return value
		}
	}
	return ""
}

func stringFromAnyMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func sortedMarketplaceMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func LoadMCPServers(roots []string) map[string]contracts.MCPServer {
	return LoadMCPServersWithSettings(roots, contracts.Settings{})
}

func LoadMCPServersWithSettings(roots []string, settings contracts.Settings) map[string]contracts.MCPServer {
	servers := map[string]contracts.MCPServer{}
	for _, plugin := range LoadPluginDirsWithSettings(roots, settings) {
		for name, server := range plugin.MCPServers {
			servers[name] = server
		}
	}
	if len(servers) == 0 {
		return nil
	}
	return servers
}

func ProjectPluginDirs(cwd string) []string {
	cwd = cleanAbs(cwd)
	home := cleanAbs(userHomeDir())
	gitRoot := findGitRoot(cwd)
	var out []string
	seen := map[string]struct{}{}
	for current := cwd; ; current = filepath.Dir(current) {
		if samePath(current, home) {
			break
		}
		out = appendPluginRoots(out, seen, filepath.Join(current, ".claude", "plugins"))
		if gitRoot != "" && samePath(current, gitRoot) {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return out
}

func pluginCommands(root string, pluginName string, raw any) ([]contracts.Command, []PromptTemplate) {
	seen := map[string]struct{}{}
	return pluginCommandsWithSeen(root, pluginName, raw, seen)
}

func pluginCommandsWithSeen(root string, pluginName string, raw any, seen map[string]struct{}) ([]contracts.Command, []PromptTemplate) {
	if raw == nil {
		return loadPluginCommandsFromPath(filepath.Join(root, "commands"), filepath.Join(root, "commands"), pluginName, "", seen)
	}
	switch value := raw.(type) {
	case []any:
		var commands []contracts.Command
		var prompts []PromptTemplate
		for _, item := range value {
			itemCommands, itemPrompts := pluginCommandsWithSeen(root, pluginName, item, seen)
			commands = append(commands, itemCommands...)
			prompts = append(prompts, itemPrompts...)
		}
		return commands, prompts
	case string:
		path := safeJoin(root, value)
		baseDir := path
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			baseDir = filepath.Dir(path)
		}
		return loadPluginCommandsFromPath(path, baseDir, pluginName, "", seen)
	case map[string]any:
		if _, ok := value["name"]; ok {
			command, content, ok := commandFromManifestRaw(root, value)
			if !ok {
				return nil, nil
			}
			if command.Type == contracts.CommandPrompt && content != "" {
				return nil, []PromptTemplate{{Command: command, Content: content}}
			}
			return []contracts.Command{command}, nil
		}
		return pluginCommandsFromMapping(root, pluginName, value, seen)
	default:
		return nil, nil
	}
}

func commandFromManifestRaw(root string, raw any) (contracts.Command, string, bool) {
	data, err := json.Marshal(raw)
	if err != nil {
		return contracts.Command{}, "", false
	}
	var item commandManifest
	if err := json.Unmarshal(data, &item); err != nil {
		return contracts.Command{}, "", false
	}
	return commandFromManifest(root, item)
}

func pluginCommandsFromMapping(root string, pluginName string, object map[string]any, seen map[string]struct{}) ([]contracts.Command, []PromptTemplate) {
	var prompts []PromptTemplate
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := object[name]
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		metadata, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		commandName := pluginName + ":" + name
		if content, ok := metadata["content"].(string); ok && strings.TrimSpace(content) != "" {
			prompts = append(prompts, PromptTemplate{
				Command: pluginCommandFromMetadata(commandName, root, metadata, len(content)),
				Content: content,
			})
			continue
		}
		source, ok := metadata["source"].(string)
		if !ok || strings.TrimSpace(source) == "" {
			continue
		}
		_, sourcePrompts := loadPluginCommandsFromPath(safeJoin(root, source), filepath.Dir(safeJoin(root, source)), pluginName, commandName, seen)
		if len(sourcePrompts) == 0 {
			continue
		}
		prompt := sourcePrompts[0]
		prompt.Command = mergePluginCommandMetadata(prompt.Command, metadata)
		prompts = append(prompts, prompt)
	}
	return nil, prompts
}

func loadPluginCommandsFromPath(path string, baseDir string, pluginName string, explicitName string, seen map[string]struct{}) ([]contracts.Command, []PromptTemplate) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}
	if info.IsDir() {
		return loadPluginCommandsFromDir(path, baseDir, pluginName, seen)
	}
	if !strings.EqualFold(filepath.Ext(path), ".md") {
		return nil, nil
	}
	prompt, ok := loadPluginCommandFile(path, baseDir, pluginName, explicitName, seen)
	if !ok {
		return nil, nil
	}
	return nil, []PromptTemplate{prompt}
}

func loadPluginCommandsFromDir(dir string, baseDir string, pluginName string, seen map[string]struct{}) ([]contracts.Command, []PromptTemplate) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(entry.Name(), "SKILL.md") {
			prompt, ok := loadPluginCommandFile(filepath.Join(dir, entry.Name()), baseDir, pluginName, "", seen)
			if ok {
				return nil, []PromptTemplate{prompt}
			}
			return nil, nil
		}
	}
	var prompts []PromptTemplate
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			_, nested := loadPluginCommandsFromDir(path, baseDir, pluginName, seen)
			prompts = append(prompts, nested...)
			continue
		}
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		prompt, ok := loadPluginCommandFile(path, baseDir, pluginName, "", seen)
		if ok {
			prompts = append(prompts, prompt)
		}
	}
	return nil, prompts
}

func loadPluginCommandFile(path string, baseDir string, pluginName string, explicitName string, seen map[string]struct{}) (PromptTemplate, bool) {
	key := normalizePath(path)
	if _, ok := seen[key]; ok {
		return PromptTemplate{}, false
	}
	seen[key] = struct{}{}
	data, err := os.ReadFile(path)
	if err != nil {
		return PromptTemplate{}, false
	}
	frontmatter, body := memory.ParseFrontmatter(string(data))
	name := strings.TrimSpace(explicitName)
	if name == "" {
		name = pluginCommandNameFromFile(path, baseDir, pluginName)
	}
	command := contracts.Command{
		Type:            contracts.CommandPrompt,
		Name:            name,
		DisplayName:     strings.TrimSpace(frontmatter["name"]),
		Description:     firstNonEmpty(frontmatter["description"], extractFirstMarkdownLine(body)),
		ArgumentHint:    strings.TrimSpace(frontmatter["argument-hint"]),
		ArgumentNames:   parseFrontmatterWords(frontmatter["arguments"]),
		Source:          contracts.CommandSourcePlugin,
		LoadedFrom:      "plugin",
		SkillRoot:       filepath.Dir(path),
		Hidden:          parseFrontmatterFalse(frontmatter["user-invocable"]),
		AllowedTools:    parseFrontmatterWords(frontmatter["allowed-tools"]),
		WhenToUse:       firstNonEmpty(frontmatter["when_to_use"], frontmatter["when-to-use"], frontmatter["whenToUse"]),
		Version:         strings.TrimSpace(frontmatter["version"]),
		Model:           parsePluginCommandModel(frontmatter["model"]),
		Effort:          strings.TrimSpace(frontmatter["effort"]),
		ContentLength:   len(body),
		ProgressMessage: "running",
		HasUserSpecifiedDetails: strings.TrimSpace(frontmatter["description"]) != "" ||
			firstNonEmpty(frontmatter["when_to_use"], frontmatter["when-to-use"], frontmatter["whenToUse"]) != "",
	}
	if strings.EqualFold(filepath.Base(path), "SKILL.md") {
		command.ProgressMessage = "loading"
	}
	return PromptTemplate{Command: command, Content: body}, true
}

func pluginCommandFromMetadata(name string, root string, metadata map[string]any, contentLength int) contracts.Command {
	return mergePluginCommandMetadata(contracts.Command{
		Type:            contracts.CommandPrompt,
		Name:            name,
		Source:          contracts.CommandSourcePlugin,
		LoadedFrom:      "plugin",
		SkillRoot:       root,
		ContentLength:   contentLength,
		ProgressMessage: "running",
	}, metadata)
}

func mergePluginCommandMetadata(command contracts.Command, metadata map[string]any) contracts.Command {
	if description := metadataString(metadata, "description"); description != "" {
		command.Description = description
		command.HasUserSpecifiedDetails = true
	}
	if hint := firstNonEmpty(metadataString(metadata, "argumentHint"), metadataString(metadata, "argument-hint")); hint != "" {
		command.ArgumentHint = hint
	}
	if model := metadataString(metadata, "model"); model != "" {
		command.Model = parsePluginCommandModel(model)
	}
	if allowed := metadataStringSlice(metadata, "allowedTools", "allowed_tools"); len(allowed) > 0 {
		command.AllowedTools = allowed
	}
	if when := firstNonEmpty(metadataString(metadata, "whenToUse"), metadataString(metadata, "when_to_use")); when != "" {
		command.WhenToUse = when
		command.HasUserSpecifiedDetails = true
	}
	return command
}

func pluginCommandNameFromFile(path string, baseDir string, pluginName string) string {
	namePath := path
	if strings.EqualFold(filepath.Base(namePath), "SKILL.md") {
		namePath = filepath.Dir(namePath)
	} else {
		namePath = strings.TrimSuffix(namePath, filepath.Ext(namePath))
	}
	rel, err := filepath.Rel(baseDir, namePath)
	if err != nil {
		rel = filepath.Base(namePath)
	}
	parts := []string{pluginName}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		part = strings.TrimSpace(part)
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return strings.Join(compactStrings(parts), ":")
}

func commandFromManifest(root string, item commandManifest) (contracts.Command, string, bool) {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		return contracts.Command{}, "", false
	}
	commandType := commandTypeFromManifest(item.Type)
	if commandType == "" {
		return contracts.Command{}, "", false
	}
	content := firstNonEmpty(item.Prompt, item.Content)
	if content == "" && strings.TrimSpace(item.Path) != "" {
		data, err := os.ReadFile(safeJoin(root, item.Path))
		if err == nil {
			content = string(data)
		}
	}
	allowedTools := append([]string(nil), item.AllowedTools...)
	allowedTools = append(allowedTools, item.AllowedToolsSnake...)
	command := contracts.Command{
		Type:                   commandType,
		Name:                   name,
		DisplayName:            strings.TrimSpace(item.DisplayName),
		Description:            strings.TrimSpace(item.Description),
		ArgumentHint:           strings.TrimSpace(item.ArgumentHint),
		ArgumentNames:          append([]string(nil), item.ArgumentNames...),
		Source:                 contracts.CommandSourcePlugin,
		LoadedFrom:             "plugin",
		SkillRoot:              root,
		DisableModelInvocation: item.DisableModelInvocation,
		Hidden:                 item.Hidden,
		AllowedTools:           compactStrings(allowedTools),
		WhenToUse:              firstNonEmpty(item.WhenToUse, item.WhenToUseSnake),
		Version:                strings.TrimSpace(item.Version),
		Model:                  strings.TrimSpace(item.Model),
		Context:                strings.TrimSpace(item.Context),
		Agent:                  strings.TrimSpace(item.Agent),
		Effort:                 strings.TrimSpace(item.Effort),
		HasUserSpecifiedDetails: strings.TrimSpace(item.Description) != "" ||
			firstNonEmpty(item.WhenToUse, item.WhenToUseSnake) != "",
	}
	if command.Type == contracts.CommandPrompt && content == "" {
		return command, "", true
	}
	return command, content, true
}

func skillFromManifest(root string, item skillManifest) (skills.Skill, bool) {
	skillRoot := firstNonEmpty(item.Root, item.Path)
	if skillRoot == "" {
		return skills.Skill{}, false
	}
	skillRoot = safeJoin(root, skillRoot)
	if strings.EqualFold(filepath.Base(skillRoot), "SKILL.md") {
		skillRoot = filepath.Dir(skillRoot)
	}
	skill, err := skills.LoadSkillDir(skillRoot, contracts.CommandSourcePlugin)
	if err != nil {
		return skills.Skill{}, false
	}
	if name := strings.TrimSpace(item.Name); name != "" {
		skill.Command.Name = name
	}
	if display := strings.TrimSpace(item.DisplayName); display != "" {
		skill.Command.DisplayName = display
	}
	if description := strings.TrimSpace(item.Description); description != "" {
		skill.Command.Description = description
		skill.Command.HasUserSpecifiedDetails = true
	}
	skill.Command.Source = contracts.CommandSourcePlugin
	skill.Command.LoadedFrom = "plugin"
	return skill, true
}

func pluginSkillPrompts(root string, pluginName string, raw any) []PromptTemplate {
	seen := map[string]struct{}{}
	if raw == nil {
		return loadPluginSkillPromptsFromPath(filepath.Join(root, "skills"), pluginName, seen)
	}
	return pluginSkillPromptsFromSpec(root, pluginName, raw, seen)
}

func pluginSkillPromptsFromSpec(root string, pluginName string, raw any, seen map[string]struct{}) []PromptTemplate {
	switch value := raw.(type) {
	case nil:
		return nil
	case string:
		return loadPluginSkillPromptsFromPath(safeJoin(root, value), pluginName, seen)
	case []any:
		var out []PromptTemplate
		for _, item := range value {
			out = append(out, pluginSkillPromptsFromSpec(root, pluginName, item, seen)...)
		}
		return out
	case map[string]any:
		skill, ok := skillFromManifestRaw(root, value)
		if !ok {
			return nil
		}
		key := normalizePath(skill.FilePath)
		if _, exists := seen[key]; exists {
			return nil
		}
		seen[key] = struct{}{}
		applyPluginSkillName(pluginName, &skill)
		return []PromptTemplate{{Command: skill.Command, Content: skill.Content}}
	default:
		return nil
	}
}

func skillFromManifestRaw(root string, raw any) (skills.Skill, bool) {
	data, err := json.Marshal(raw)
	if err != nil {
		return skills.Skill{}, false
	}
	var item skillManifest
	if err := json.Unmarshal(data, &item); err != nil {
		return skills.Skill{}, false
	}
	return skillFromManifest(root, item)
}

func loadPluginSkillPromptsFromPath(path string, pluginName string, seen map[string]struct{}) []PromptTemplate {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return nil
	}
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil {
		skill, ok := loadPluginSkillDir(path, pluginName, seen)
		if !ok {
			return nil
		}
		return []PromptTemplate{{Command: skill.Command, Content: skill.Content}}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	var out []PromptTemplate
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		skill, ok := loadPluginSkillDir(filepath.Join(path, entry.Name()), pluginName, seen)
		if ok {
			out = append(out, PromptTemplate{Command: skill.Command, Content: skill.Content})
		}
	}
	return out
}

func loadPluginSkillDir(path string, pluginName string, seen map[string]struct{}) (skills.Skill, bool) {
	skill, err := skills.LoadSkillDir(path, contracts.CommandSourcePlugin)
	if err != nil {
		return skills.Skill{}, false
	}
	key := normalizePath(skill.FilePath)
	if _, ok := seen[key]; ok {
		return skills.Skill{}, false
	}
	seen[key] = struct{}{}
	applyPluginSkillName(pluginName, &skill)
	return skill, true
}

func applyPluginSkillName(pluginName string, skill *skills.Skill) {
	name := strings.TrimSpace(skill.Command.Name)
	if name == "" {
		name = filepath.Base(skill.Root)
	}
	if !strings.Contains(name, ":") && !strings.HasPrefix(name, pluginName+":") {
		name = pluginName + ":" + name
	}
	skill.Command.Name = name
	skill.Command.Source = contracts.CommandSourcePlugin
	skill.Command.LoadedFrom = "plugin"
}

func pluginMCPServers(root string, pluginName string, specs ...any) map[string]contracts.MCPServer {
	out := map[string]contracts.MCPServer{}
	mergePluginMCPServers(out, pluginName, loadPluginMCPServerFile(filepath.Join(root, ".mcp.json")))
	for _, spec := range specs {
		mergePluginMCPServers(out, pluginName, pluginMCPServersFromSpec(root, spec))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pluginMCPServersFromSpec(root string, spec any) map[string]contracts.MCPServer {
	switch value := spec.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(value) == "" || isExternalOrMCPBSource(value) {
			return nil
		}
		return loadPluginMCPServerFile(safeJoin(root, value))
	case []any:
		out := map[string]contracts.MCPServer{}
		for _, item := range value {
			mergePluginMCPServers(out, "", pluginMCPServersFromSpec(root, item))
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case map[string]any:
		return pluginMCPServersFromObject(value)
	default:
		return nil
	}
}

func loadPluginMCPServerFile(path string) map[string]contracts.MCPServer {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return pluginMCPServersFromSpec(filepath.Dir(path), raw)
}

func pluginMCPServersFromObject(object map[string]any) map[string]contracts.MCPServer {
	if nested, ok := object["mcpServers"]; ok {
		if nestedObject, ok := nested.(map[string]any); ok {
			object = nestedObject
		}
	}
	out := map[string]contracts.MCPServer{}
	for name, raw := range object {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		server, ok := pluginMCPServer(raw)
		if !ok {
			continue
		}
		out[name] = server
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pluginMCPServer(raw any) (contracts.MCPServer, bool) {
	data, err := json.Marshal(raw)
	if err != nil {
		return contracts.MCPServer{}, false
	}
	var server contracts.MCPServer
	if err := json.Unmarshal(data, &server); err != nil {
		return contracts.MCPServer{}, false
	}
	if server.Type == "" && server.Command == "" && server.URL == "" && len(server.Args) == 0 {
		return contracts.MCPServer{}, false
	}
	return server, true
}

func mergePluginMCPServers(dst map[string]contracts.MCPServer, pluginName string, src map[string]contracts.MCPServer) {
	for name, server := range src {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if server.Name == "" {
			server.Name = name
		}
		if pluginName != "" {
			server.PluginSource = pluginName
		}
		dst[name] = server
	}
}

func isExternalOrMCPBSource(path string) bool {
	path = strings.TrimSpace(strings.ToLower(path))
	return strings.HasSuffix(path, ".mcpb") || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

func pluginAgents(root string, pluginName string, manifestAgents any) []PluginAgent {
	seen := map[string]struct{}{}
	var out []PluginAgent
	defaultDir := filepath.Join(root, "agents")
	if info, err := os.Stat(defaultDir); err == nil && info.IsDir() {
		out = append(out, loadPluginAgentsFromPath(defaultDir, pluginName, nil, seen)...)
	}
	for _, path := range manifestAgentPaths(manifestAgents) {
		out = append(out, loadPluginAgentsFromPath(safeJoin(root, path), pluginName, nil, seen)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Path < out[j].Path
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func pluginOutputStyles(root string, pluginName string, raw any) []PluginOutputStyle {
	seen := map[string]struct{}{}
	if raw == nil {
		return loadPluginOutputStylesFromPath(filepath.Join(root, "output-styles"), pluginName, seen)
	}
	var out []PluginOutputStyle
	for _, path := range manifestPathSpecs(raw) {
		out = append(out, loadPluginOutputStylesFromPath(safeJoin(root, path), pluginName, seen)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Path < out[j].Path
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func loadPluginOutputStylesFromPath(path string, pluginName string, seen map[string]struct{}) []PluginOutputStyle {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		return loadPluginOutputStylesFromDir(path, pluginName, seen)
	}
	if !strings.EqualFold(filepath.Ext(path), ".md") {
		return nil
	}
	style, ok := loadPluginOutputStyleFile(path, pluginName, seen)
	if !ok {
		return nil
	}
	return []PluginOutputStyle{style}
}

func loadPluginOutputStylesFromDir(dir string, pluginName string, seen map[string]struct{}) []PluginOutputStyle {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	var out []PluginOutputStyle
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			out = append(out, loadPluginOutputStylesFromDir(path, pluginName, seen)...)
			continue
		}
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		style, ok := loadPluginOutputStyleFile(path, pluginName, seen)
		if ok {
			out = append(out, style)
		}
	}
	return out
}

func loadPluginOutputStyleFile(path string, pluginName string, seen map[string]struct{}) (PluginOutputStyle, bool) {
	key := normalizePath(path)
	if _, ok := seen[key]; ok {
		return PluginOutputStyle{}, false
	}
	seen[key] = struct{}{}
	data, err := os.ReadFile(path)
	if err != nil {
		return PluginOutputStyle{}, false
	}
	frontmatter, body := memory.ParseFrontmatter(string(data))
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name := firstNonEmpty(frontmatter["name"], base)
	if !strings.Contains(name, ":") {
		name = pluginName + ":" + name
	}
	return PluginOutputStyle{
		Name:           name,
		Path:           path,
		Description:    firstNonEmpty(frontmatter["description"], extractFirstMarkdownLine(body), "Output style from "+pluginName+" plugin"),
		Prompt:         strings.TrimSpace(body),
		ForceForPlugin: parseOptionalBool(frontmatter["force-for-plugin"]),
	}, true
}

func manifestAgentPaths(raw any) []string {
	switch value := raw.(type) {
	case string:
		return compactStrings([]string{value})
	case []any:
		var out []string
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return compactStrings(out)
	case []string:
		return compactStrings(value)
	default:
		return nil
	}
}

func loadPluginAgentsFromPath(path string, pluginName string, namespace []string, seen map[string]struct{}) []PluginAgent {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		return loadPluginAgentsFromDir(path, pluginName, namespace, seen)
	}
	if !strings.EqualFold(filepath.Ext(path), ".md") {
		return nil
	}
	agent, ok := loadPluginAgentFile(path, pluginName, namespace, seen)
	if !ok {
		return nil
	}
	return []PluginAgent{agent}
}

func loadPluginAgentsFromDir(dir string, pluginName string, namespace []string, seen map[string]struct{}) []PluginAgent {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	var out []PluginAgent
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			out = append(out, loadPluginAgentsFromDir(path, pluginName, append(namespace, entry.Name()), seen)...)
			continue
		}
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		agent, ok := loadPluginAgentFile(path, pluginName, namespace, seen)
		if ok {
			out = append(out, agent)
		}
	}
	return out
}

func loadPluginAgentFile(path string, pluginName string, namespace []string, seen map[string]struct{}) (PluginAgent, bool) {
	key := normalizePath(path)
	if _, ok := seen[key]; ok {
		return PluginAgent{}, false
	}
	seen[key] = struct{}{}
	data, err := os.ReadFile(path)
	if err != nil {
		return PluginAgent{}, false
	}
	frontmatter, body := memory.ParseFrontmatter(string(data))
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name := firstNonEmpty(frontmatter["name"], base)
	parts := append([]string{pluginName}, namespace...)
	parts = append(parts, name)
	description := firstNonEmpty(
		frontmatter["description"],
		frontmatter["when-to-use"],
		frontmatter["when_to_use"],
		frontmatter["whenToUse"],
		extractFirstMarkdownLine(body),
		"Agent from "+pluginName+" plugin",
	)
	return PluginAgent{
		Name:           strings.Join(compactStrings(parts), ":"),
		Path:           path,
		Description:    description,
		Prompt:         strings.TrimSpace(body),
		Model:          strings.TrimSpace(frontmatter["model"]),
		PermissionMode: contracts.PermissionMode(firstNonEmpty(frontmatter["permissionMode"], frontmatter["permission_mode"], frontmatter["permission-mode"])),
		AllowedTools:   parseFrontmatterWords(firstNonEmpty(frontmatter["tools"], frontmatter["allowed-tools"], frontmatter["allowed_tools"], frontmatter["allowedTools"])),
	}, true
}

func pluginHooks(root string, manifestHooks any) map[string]any {
	hooks := map[string]any{}
	seenFiles := map[string]struct{}{}
	mergeRawHooks(hooks, loadPluginHookFileOnce(filepath.Join(root, "hooks", "hooks.json"), seenFiles))
	for _, spec := range manifestHookSpecs(manifestHooks) {
		switch value := spec.(type) {
		case string:
			mergeRawHooks(hooks, loadPluginHookFileOnce(safeJoin(root, value), seenFiles))
		default:
			mergeRawHooks(hooks, rawHooksFromAny(value))
		}
	}
	if len(hooks) == 0 {
		return nil
	}
	return hooks
}

func pluginHookEventsFromRaw(raw map[string]any) []PluginHookEvent {
	counts := hookCountsFromRaw(raw)
	if len(counts) == 0 {
		return nil
	}
	events := make([]string, 0, len(counts))
	for event := range counts {
		events = append(events, event)
	}
	sort.Strings(events)
	out := make([]PluginHookEvent, 0, len(events))
	for _, event := range events {
		out = append(out, PluginHookEvent{Event: event, Count: counts[event]})
	}
	return out
}

func manifestHookSpecs(raw any) []any {
	if raw == nil {
		return nil
	}
	if list, ok := raw.([]any); ok {
		return list
	}
	return []any{raw}
}

func loadPluginHookFileOnce(path string, seen map[string]struct{}) map[string]any {
	key := normalizePath(path)
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}
	return loadPluginHookFile(path)
}

func loadPluginHookFile(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return rawHooksFromAny(raw)
}

func rawHooksFromAny(raw any) map[string]any {
	object, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	if hooks, ok := object["hooks"]; ok {
		object, ok = hooks.(map[string]any)
		if !ok {
			return nil
		}
	}
	out := map[string]any{}
	for event, value := range object {
		event = strings.TrimSpace(event)
		if event == "" || event == "description" {
			continue
		}
		specs := rawHookMatcherSpecs(value)
		if len(specs) > 0 {
			out[event] = specs
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func rawHookMatcherSpecs(raw any) []any {
	switch value := raw.(type) {
	case []any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, rawHookMatcherSpecs(item)...)
		}
		return out
	case nil:
		return nil
	default:
		return []any{value}
	}
}

func hookCountsFromRaw(raw any) map[string]int {
	object, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	if hooks, ok := object["hooks"]; ok {
		object, ok = hooks.(map[string]any)
		if !ok {
			return nil
		}
	}
	counts := map[string]int{}
	for event, value := range object {
		event = strings.TrimSpace(event)
		if event == "" || event == "description" {
			continue
		}
		if count := countHookMatchers(value); count > 0 {
			counts[event] += count
		}
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func mergeRawHooks(dst map[string]any, src map[string]any) {
	for event, value := range src {
		event = strings.TrimSpace(event)
		if event == "" {
			continue
		}
		existing := rawHookMatcherSpecs(dst[event])
		existing = append(existing, rawHookMatcherSpecs(value)...)
		if len(existing) > 0 {
			dst[event] = existing
		}
	}
}

func countHookMatchers(raw any) int {
	switch value := raw.(type) {
	case []any:
		count := 0
		for _, item := range value {
			count += countHookMatcher(item)
		}
		return count
	default:
		return countHookMatcher(value)
	}
}

func countHookMatcher(raw any) int {
	switch value := raw.(type) {
	case map[string]any:
		if hooks, ok := value["hooks"]; ok {
			return countHookCommands(hooks)
		}
		return countHookCommands(value)
	case string:
		if strings.TrimSpace(value) != "" {
			return 1
		}
	}
	return 0
}

func countHookCommands(raw any) int {
	switch value := raw.(type) {
	case []any:
		count := 0
		for _, item := range value {
			count += countHookCommands(item)
		}
		return count
	case map[string]any:
		if len(value) > 0 {
			return 1
		}
	case string:
		if strings.TrimSpace(value) != "" {
			return 1
		}
	}
	return 0
}

func mergeHookCounts(dst map[string]int, src map[string]int) {
	for event, count := range src {
		if count > 0 {
			dst[event] += count
		}
	}
}

func appendPluginRoots(out []string, seen map[string]struct{}, pluginsDir string) []string {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return out
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		root := filepath.Join(pluginsDir, entry.Name())
		if _, err := os.Stat(filepath.Join(root, ManifestFileName)); err != nil {
			continue
		}
		key := normalizePath(root)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, root)
	}
	return out
}

func commandTypeFromManifest(raw string) contracts.CommandType {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "prompt", "skill":
		return contracts.CommandPrompt
	case "local":
		return contracts.CommandLocal
	case "local-jsx", "local_jsx", "localjsx":
		return contracts.CommandLocalJSX
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func compactStrings(values []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func manifestPathSpecs(raw any) []string {
	switch value := raw.(type) {
	case string:
		return compactStrings([]string{value})
	case []any:
		var out []string
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return compactStrings(out)
	case []string:
		return compactStrings(value)
	default:
		return nil
	}
}

func parseFrontmatterWords(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
	}
	parts := splitFrontmatterWords(raw)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = trimFrontmatterScalar(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func splitFrontmatterWords(raw string) []string {
	var parts []string
	var current strings.Builder
	var quote rune
	depth := 0
	escaped := false
	for _, r := range raw {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			current.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			current.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			current.WriteRune(r)
		case '(', '[', '{':
			depth++
			current.WriteRune(r)
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
			current.WriteRune(r)
		case ',', ' ', '\t', '\n', '\r':
			if depth == 0 {
				parts = append(parts, current.String())
				current.Reset()
				continue
			}
			current.WriteRune(r)
		default:
			current.WriteRune(r)
		}
	}
	parts = append(parts, current.String())
	return parts
}

func trimFrontmatterScalar(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 1 {
		first := raw[0]
		if first == '"' || first == '\'' {
			raw = strings.TrimSpace(raw[1:])
			if len(raw) >= 1 && raw[len(raw)-1] == first {
				raw = strings.TrimSpace(raw[:len(raw)-1])
			}
		}
	}
	return raw
}

func parseFrontmatterFalse(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	return raw == "false" || raw == "0" || raw == "no" || raw == "off"
}

func parseOptionalBool(raw string) *bool {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "true", "1", "yes", "on":
		value := true
		return &value
	case "false", "0", "no", "off":
		value := false
		return &value
	default:
		return nil
	}
}

func parsePluginCommandModel(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, "inherit") {
		return ""
	}
	return raw
}

func metadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func metadataStringSlice(metadata map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []any:
			var out []string
			for _, item := range typed {
				if text, ok := item.(string); ok {
					out = append(out, text)
				}
			}
			return compactStrings(out)
		case []string:
			return compactStrings(typed)
		case string:
			return parseFrontmatterWords(typed)
		}
	}
	return nil
}

func extractFirstMarkdownLine(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
		return line
	}
	return ""
}

func findGitRoot(cwd string) string {
	for current := cwd; current != ""; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return ""
}

func cleanAbs(path string) string {
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func normalizePath(path string) string {
	return filepath.ToSlash(strings.ToLower(filepath.Clean(path)))
}

func samePath(a string, b string) bool {
	return normalizePath(a) == normalizePath(b)
}

func safeJoin(root string, path string) string {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, filepath.VolumeName(clean))
		clean = strings.TrimLeft(clean, `/\`)
	}
	for strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		clean = strings.TrimPrefix(clean, ".."+string(filepath.Separator))
		if clean == ".." {
			clean = "."
		}
	}
	return filepath.Join(root, clean)
}
