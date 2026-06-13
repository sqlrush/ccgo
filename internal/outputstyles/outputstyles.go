package outputstyles

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/memory"
	"ccgo/internal/platform"
	pluginpkg "ccgo/internal/plugins"
)

const DefaultName = "default"

type Source string

const (
	SourceBuiltIn Source = "built-in"
	SourceUser    Source = "userSettings"
	SourceProject Source = "projectSettings"
	SourcePlugin  Source = "plugin"
)

type Config struct {
	Name                   string
	Description            string
	Prompt                 string
	Source                 Source
	KeepCodingInstructions *bool
	ForceForPlugin         *bool
}

func Resolve(cwd string, settings contracts.Settings, plugins []pluginpkg.LoadedPlugin) (Config, bool) {
	styles := All(cwd, plugins)
	for _, style := range sortedStyles(styles) {
		if style.Source == SourcePlugin && style.ForceForPlugin != nil && *style.ForceForPlugin {
			return style, true
		}
	}
	name := strings.TrimSpace(settings.OutputStyle)
	if name == "" {
		name = DefaultName
	}
	if name == DefaultName {
		return Config{}, false
	}
	style, ok := styles[name]
	return style, ok
}

func EffectiveName(cwd string, settings contracts.Settings, plugins []pluginpkg.LoadedPlugin) string {
	if style, ok := Resolve(cwd, settings, plugins); ok {
		return style.Name
	}
	if name := strings.TrimSpace(settings.OutputStyle); name != "" {
		return name
	}
	return DefaultName
}

func Section(style Config) string {
	prompt := strings.TrimSpace(style.Prompt)
	if prompt == "" {
		return "# Output Style: " + strings.TrimSpace(style.Name)
	}
	return "# Output Style: " + strings.TrimSpace(style.Name) + "\n" + prompt
}

func All(cwd string, plugins []pluginpkg.LoadedPlugin) map[string]Config {
	styles := Builtins()
	for _, plugin := range plugins {
		for _, style := range plugin.OutputStyles {
			name := strings.TrimSpace(style.Name)
			if name == "" {
				continue
			}
			styles[name] = Config{
				Name:           name,
				Description:    strings.TrimSpace(style.Description),
				Prompt:         strings.TrimSpace(style.Prompt),
				Source:         SourcePlugin,
				ForceForPlugin: cloneBool(style.ForceForPlugin),
			}
		}
	}
	for _, dir := range outputStyleDirs(cwd) {
		source := SourceProject
		if samePath(dir, filepath.Join(platform.ClaudeHomeDir(), "output-styles")) {
			source = SourceUser
		}
		for _, style := range loadOutputStyleDir(dir, source) {
			styles[style.Name] = style
		}
	}
	return styles
}

func Builtins() map[string]Config {
	keepCoding := true
	return map[string]Config{
		"Explanatory": {
			Name:                   "Explanatory",
			Description:            "Claude explains its implementation choices and codebase patterns",
			Prompt:                 explanatoryPrompt,
			Source:                 SourceBuiltIn,
			KeepCodingInstructions: &keepCoding,
		},
		"Learning": {
			Name:                   "Learning",
			Description:            "Claude pauses and asks you to write small pieces of code for hands-on practice",
			Prompt:                 learningPrompt,
			Source:                 SourceBuiltIn,
			KeepCodingInstructions: &keepCoding,
		},
	}
}

func outputStyleDirs(cwd string) []string {
	var dirs []string
	userDir := filepath.Join(platform.ClaudeHomeDir(), "output-styles")
	if info, err := os.Stat(userDir); err == nil && info.IsDir() {
		dirs = append(dirs, userDir)
	}
	projectDirs := projectOutputStyleDirs(cwd)
	for i := len(projectDirs) - 1; i >= 0; i-- {
		dirs = append(dirs, projectDirs[i])
	}
	return dirs
}

func projectOutputStyleDirs(cwd string) []string {
	cwd = cleanAbs(cwd)
	home := cleanAbs(userHomeDir())
	gitRoot := findGitRoot(cwd)
	var out []string
	seen := map[string]struct{}{}
	for current := cwd; ; current = filepath.Dir(current) {
		if samePath(current, home) {
			break
		}
		dir := filepath.Join(current, ".claude", "output-styles")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			key := normalizePath(dir)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				out = append(out, dir)
			}
		}
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

func loadOutputStyleDir(dir string, source Source) []Config {
	var out []Config
	walkOutputStyleDir(dir, source, map[string]struct{}{}, &out)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Source < out[j].Source
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func walkOutputStyleDir(dir string, source Source, seen map[string]struct{}, out *[]Config) {
	key := normalizePath(dir)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			walkOutputStyleDir(path, source, seen, out)
			continue
		}
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		if style, ok := loadOutputStyleFile(path, source); ok {
			*out = append(*out, style)
		}
	}
}

func loadOutputStyleFile(path string, source Source) (Config, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, false
	}
	frontmatter, body := memory.ParseFrontmatter(string(data))
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name := firstNonEmpty(frontmatter["name"], base)
	if name == "" {
		return Config{}, false
	}
	return Config{
		Name:                   name,
		Description:            firstNonEmpty(frontmatter["description"], extractFirstMarkdownLine(body), "Custom "+base+" output style"),
		Prompt:                 strings.TrimSpace(body),
		Source:                 source,
		KeepCodingInstructions: parseOptionalBool(frontmatter["keep-coding-instructions"]),
	}, true
}

func sortedStyles(styles map[string]Config) []Config {
	names := make([]string, 0, len(styles))
	for name := range styles {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Config, 0, len(names))
	for _, name := range names {
		out = append(out, styles[name])
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
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

const explanatoryFeaturePrompt = `## Insights
In order to encourage learning, before and after writing code, always provide brief educational explanations about implementation choices.

These insights should be included in the conversation, not in the codebase. Focus on interesting insights that are specific to the codebase or the code you just wrote, rather than general programming concepts.`

const explanatoryPrompt = `You are an interactive CLI tool that helps users with software engineering tasks. In addition to software engineering tasks, you should provide educational insights about the codebase along the way.

You should be clear and educational, providing helpful explanations while remaining focused on the task. Balance educational content with task completion.

# Explanatory Style Active
` + explanatoryFeaturePrompt

const learningPrompt = `You are an interactive CLI tool that helps users with software engineering tasks. In addition to software engineering tasks, you should help users learn more about the codebase through hands-on practice and educational insights.

You should be collaborative and encouraging. Balance task completion with learning by requesting user input for meaningful design decisions while handling routine implementation yourself.

# Learning Style Active
## Requesting Human Contributions
Ask the human to contribute 2-10 line code pieces when generating 20+ lines involving design decisions, business logic with multiple valid approaches, key algorithms, or interface definitions.

Add exactly one TODO(human) section into the codebase before making the request, then wait for the human implementation before proceeding.

## After Contributions
Share one insight connecting their code to broader patterns or system effects. Avoid praise or repetition.

` + explanatoryFeaturePrompt
