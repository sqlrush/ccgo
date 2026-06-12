package skills

import (
	"bufio"
	"fmt"
	"os"
	slashpath "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/memory"
)

const skillFileName = "SKILL.md"

type Skill struct {
	Root          string
	FilePath      string
	Command       contracts.Command
	Content       string
	Paths         []string
	UserInvocable bool
}

func LoadSkillDir(root string, source contracts.CommandSource) (Skill, error) {
	if strings.TrimSpace(root) == "" {
		return Skill{}, fmt.Errorf("skill root is empty")
	}
	root = cleanAbs(root)
	filePath := filepath.Join(root, skillFileName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Skill{}, err
	}

	frontmatter, body := memory.ParseFrontmatter(string(data))
	if source == "" {
		source = contracts.CommandSourceSkills
	}
	displayName := strings.TrimSpace(frontmatterField(frontmatter, "name"))
	description := strings.TrimSpace(frontmatterField(frontmatter, "description"))
	hasDescription := description != ""
	if description == "" {
		description = extractDescription(body)
	}
	userInvocable := parseBoolDefault(frontmatterField(frontmatter, "user-invocable", "user_invocable", "userInvocable"), true)
	paths := parseSkillPaths(frontmatterField(frontmatter, "paths"))

	command := contracts.Command{
		Type:                    contracts.CommandPrompt,
		Name:                    filepath.Base(root),
		DisplayName:             displayName,
		Description:             description,
		ArgumentHint:            strings.TrimSpace(frontmatterField(frontmatter, "argument-hint", "argument_hint", "argumentHint")),
		ArgumentNames:           parseArgumentNames(frontmatterField(frontmatter, "arguments")),
		Source:                  source,
		LoadedFrom:              string(source),
		SkillRoot:               root,
		DisableModelInvocation:  parseBoolDefault(frontmatterField(frontmatter, "disable-model-invocation", "disable_model_invocation", "disableModelInvocation"), false),
		Hidden:                  !userInvocable,
		AllowedTools:            parseFrontmatterList(frontmatterField(frontmatter, "allowed-tools", "allowed_tools", "allowedTools")),
		WhenToUse:               strings.TrimSpace(frontmatterField(frontmatter, "when_to_use", "when-to-use", "whenToUse")),
		Version:                 strings.TrimSpace(frontmatterField(frontmatter, "version")),
		Model:                   parseSkillModel(frontmatterField(frontmatter, "model")),
		Context:                 parseSkillContext(frontmatterField(frontmatter, "context")),
		Agent:                   strings.TrimSpace(frontmatterField(frontmatter, "agent")),
		Effort:                  strings.TrimSpace(frontmatterField(frontmatter, "effort")),
		Paths:                   paths,
		ContentLength:           len(body),
		ProgressMessage:         "running",
		HasUserSpecifiedDetails: hasDescription,
	}

	return Skill{
		Root:          root,
		FilePath:      filePath,
		Command:       command,
		Content:       renderSkillContent(root, body),
		Paths:         paths,
		UserInvocable: userInvocable,
	}, nil
}

func LoadSkillDirs(roots []string, source contracts.CommandSource) []Skill {
	out := make([]Skill, 0, len(roots))
	seen := map[string]struct{}{}
	for _, root := range roots {
		root = cleanAbs(root)
		key := normalizePath(root)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		skill, err := LoadSkillDir(root, source)
		if err != nil {
			continue
		}
		out = append(out, skill)
	}
	return out
}

func ProjectSkillCommands(cwd string) []contracts.Command {
	skills := LoadSkillDirs(ProjectSkillDirs(cwd), contracts.CommandSourceSkills)
	commands := make([]contracts.Command, 0, len(skills))
	for _, skill := range skills {
		commands = append(commands, skill.Command)
	}
	return commands
}

func LoadLegacyCommandSkills(cwd string) []Skill {
	commandFiles := projectLegacyCommandFiles(cwd)
	out := make([]Skill, 0, len(commandFiles))
	for _, commandFile := range commandFiles {
		skill, err := loadLegacyCommandFile(commandFile)
		if err != nil {
			continue
		}
		out = append(out, skill)
	}
	return out
}

type legacyCommandFile struct {
	BaseDir string
	Path    string
	IsSkill bool
}

func projectLegacyCommandFiles(cwd string) []legacyCommandFile {
	dirs := projectLegacyCommandDirs(cwd)
	out := make([]legacyCommandFile, 0)
	for _, dir := range dirs {
		out = append(out, legacyCommandFilesInDir(dir)...)
	}
	return out
}

func projectLegacyCommandDirs(cwd string) []string {
	cwd = cleanAbs(cwd)
	home := cleanAbs(userHomeDir())
	gitRoot := findGitRoot(cwd)
	var out []string
	seen := map[string]struct{}{}
	for current := cwd; ; current = filepath.Dir(current) {
		if samePath(current, home) {
			break
		}
		dir := filepath.Join(current, ".claude", "commands")
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

func legacyCommandFilesInDir(baseDir string) []legacyCommandFile {
	var markdown []string
	_ = filepath.WalkDir(baseDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			markdown = append(markdown, path)
		}
		return nil
	})
	sort.Strings(markdown)
	skillDirs := map[string]string{}
	for _, path := range markdown {
		if isSkillFile(path) {
			dir := filepath.Dir(path)
			if _, ok := skillDirs[dir]; !ok {
				skillDirs[dir] = path
			}
		}
	}
	out := make([]legacyCommandFile, 0, len(markdown))
	for _, path := range markdown {
		if skillPath, ok := skillDirs[filepath.Dir(path)]; ok && skillPath != path {
			continue
		}
		out = append(out, legacyCommandFile{
			BaseDir: baseDir,
			Path:    path,
			IsSkill: isSkillFile(path),
		})
	}
	return out
}

func loadLegacyCommandFile(commandFile legacyCommandFile) (Skill, error) {
	data, err := os.ReadFile(commandFile.Path)
	if err != nil {
		return Skill{}, err
	}
	frontmatter, body := memory.ParseFrontmatter(string(data))
	root := ""
	if commandFile.IsSkill {
		root = filepath.Dir(commandFile.Path)
	}
	name := legacyCommandName(commandFile)
	description := strings.TrimSpace(frontmatterField(frontmatter, "description"))
	hasDescription := description != ""
	if description == "" {
		description = extractDescription(body)
	}
	userInvocable := parseBoolDefault(frontmatterField(frontmatter, "user-invocable", "user_invocable", "userInvocable"), true)
	content := body
	if root != "" {
		content = renderSkillContent(root, body)
	}
	command := contracts.Command{
		Type:                    contracts.CommandPrompt,
		Name:                    name,
		DisplayName:             strings.TrimSpace(frontmatterField(frontmatter, "name")),
		Description:             description,
		ArgumentHint:            strings.TrimSpace(frontmatterField(frontmatter, "argument-hint", "argument_hint", "argumentHint")),
		ArgumentNames:           parseArgumentNames(frontmatterField(frontmatter, "arguments")),
		Source:                  contracts.CommandSourceSkills,
		LoadedFrom:              "commands_DEPRECATED",
		SkillRoot:               root,
		DisableModelInvocation:  parseBoolDefault(frontmatterField(frontmatter, "disable-model-invocation", "disable_model_invocation", "disableModelInvocation"), false),
		Hidden:                  !userInvocable,
		AllowedTools:            parseFrontmatterList(frontmatterField(frontmatter, "allowed-tools", "allowed_tools", "allowedTools")),
		WhenToUse:               strings.TrimSpace(frontmatterField(frontmatter, "when_to_use", "when-to-use", "whenToUse")),
		Version:                 strings.TrimSpace(frontmatterField(frontmatter, "version")),
		Model:                   parseSkillModel(frontmatterField(frontmatter, "model")),
		Context:                 parseSkillContext(frontmatterField(frontmatter, "context")),
		Agent:                   strings.TrimSpace(frontmatterField(frontmatter, "agent")),
		Effort:                  strings.TrimSpace(frontmatterField(frontmatter, "effort")),
		ContentLength:           len(body),
		ProgressMessage:         "running",
		HasUserSpecifiedDetails: hasDescription,
	}
	return Skill{
		Root:          root,
		FilePath:      commandFile.Path,
		Command:       command,
		Content:       content,
		UserInvocable: userInvocable,
	}, nil
}

func legacyCommandName(commandFile legacyCommandFile) string {
	path := commandFile.Path
	if commandFile.IsSkill {
		path = filepath.Dir(path)
	} else {
		path = strings.TrimSuffix(path, filepath.Ext(path))
	}
	rel, err := filepath.Rel(commandFile.BaseDir, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return strings.Join(out, ":")
}

func frontmatterField(frontmatter map[string]string, keys ...string) string {
	for _, key := range keys {
		if value, ok := frontmatter[key]; ok {
			return value
		}
	}
	return ""
}

func isSkillFile(filePath string) bool {
	return strings.EqualFold(filepath.Base(filePath), skillFileName)
}

func extractDescription(markdown string) string {
	scanner := bufio.NewScanner(strings.NewReader(markdown))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			line = strings.TrimLeft(line, "#")
			return strings.TrimSpace(line)
		}
		return line
	}
	return ""
}

func parseBoolDefault(raw string, fallback bool) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func parseSkillModel(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, "inherit") {
		return ""
	}
	return raw
}

func parseSkillContext(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, "fork") {
		return "fork"
	}
	return ""
}

func parseFrontmatterList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
	}
	parts := splitTopLevelComma(raw)
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

func parseArgumentNames(raw string) []string {
	parts := parseFrontmatterList(raw)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || allDigits(part) {
			continue
		}
		out = append(out, part)
	}
	return out
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func splitTopLevelComma(raw string) []string {
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
		case ',':
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
			return raw
		}
	}
	return raw
}

func parseSkillPaths(raw string) []string {
	parts := parseFrontmatterList(raw)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimSuffix(part, "/**"))
		if part == "" || part == "**" {
			continue
		}
		part = slashpath.Clean(part)
		if part == "." {
			continue
		}
		out = append(out, part)
	}
	return out
}

func renderSkillContent(root string, body string) string {
	body = strings.ReplaceAll(body, "${CLAUDE_SKILL_DIR}", root)
	return fmt.Sprintf("Base directory for this skill: %s\n\n%s", root, body)
}
