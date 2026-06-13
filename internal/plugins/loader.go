package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/skills"
)

const ManifestFileName = "plugin.json"

type PromptTemplate struct {
	Command contracts.Command
	Content string
}

type LoadedPlugin struct {
	Root            string
	Name            string
	Version         string
	Description     string
	Commands        []contracts.Command
	PromptTemplates []PromptTemplate
}

type manifest struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"displayName"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Commands    []commandManifest `json:"commands"`
	Skills      []skillManifest   `json:"skills"`
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
	return loaded, nil
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
