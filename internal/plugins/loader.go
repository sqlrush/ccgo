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
	Name        string
	Path        string
	Description string
}

type PluginHookEvent struct {
	Event string
	Count int
}

type LoadedPlugin struct {
	Root            string
	Name            string
	Version         string
	Description     string
	Commands        []contracts.Command
	PromptTemplates []PromptTemplate
	MCPServers      map[string]contracts.MCPServer
	Agents          []PluginAgent
	HookEvents      []PluginHookEvent
}

type manifest struct {
	Name            string            `json:"name"`
	DisplayName     string            `json:"displayName"`
	Description     string            `json:"description"`
	Version         string            `json:"version"`
	Commands        []commandManifest `json:"commands"`
	Skills          []skillManifest   `json:"skills"`
	Agents          any               `json:"agents"`
	Hooks           any               `json:"hooks"`
	MCPServers      any               `json:"mcpServers"`
	MCPServersSnake any               `json:"mcp_servers"`
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

func LoadPluginDirs(roots []string) []LoadedPlugin {
	out := make([]LoadedPlugin, 0, len(roots))
	seen := map[string]struct{}{}
	for _, root := range roots {
		root = cleanAbs(root)
		key := normalizePath(root)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		plugin, err := LoadPluginDir(root)
		if err != nil {
			continue
		}
		out = append(out, plugin)
	}
	return out
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
	}
	for _, item := range parsed.Commands {
		command, content, ok := commandFromManifest(root, item)
		if !ok {
			continue
		}
		if command.Type == contracts.CommandPrompt && content != "" {
			loaded.PromptTemplates = append(loaded.PromptTemplates, PromptTemplate{Command: command, Content: content})
			continue
		}
		loaded.Commands = append(loaded.Commands, command)
	}
	for _, item := range parsed.Skills {
		skill, ok := skillFromManifest(root, item)
		if !ok {
			continue
		}
		loaded.PromptTemplates = append(loaded.PromptTemplates, PromptTemplate{Command: skill.Command, Content: skill.Content})
	}
	loaded.MCPServers = pluginMCPServers(root, name, parsed.MCPServers, parsed.MCPServersSnake)
	loaded.Agents = pluginAgents(root, name, parsed.Agents)
	loaded.HookEvents = pluginHookEvents(root, parsed.Hooks)
	return loaded, nil
}

func LoadMCPServers(roots []string) map[string]contracts.MCPServer {
	servers := map[string]contracts.MCPServer{}
	for _, plugin := range LoadPluginDirs(roots) {
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
		Name:        strings.Join(compactStrings(parts), ":"),
		Path:        path,
		Description: description,
	}, true
}

func pluginHookEvents(root string, manifestHooks any) []PluginHookEvent {
	counts := map[string]int{}
	seenFiles := map[string]struct{}{}
	mergeHookCounts(counts, loadPluginHookFileOnce(filepath.Join(root, "hooks", "hooks.json"), seenFiles))
	for _, spec := range manifestHookSpecs(manifestHooks) {
		switch value := spec.(type) {
		case string:
			mergeHookCounts(counts, loadPluginHookFileOnce(safeJoin(root, value), seenFiles))
		default:
			mergeHookCounts(counts, hookCountsFromRaw(value))
		}
	}
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

func loadPluginHookFileOnce(path string, seen map[string]struct{}) map[string]int {
	key := normalizePath(path)
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}
	return loadPluginHookFile(path)
}

func loadPluginHookFile(path string) map[string]int {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return hookCountsFromRaw(raw)
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
