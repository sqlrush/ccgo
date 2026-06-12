package skills

import (
	"bufio"
	"fmt"
	"os"
	slashpath "path"
	"path/filepath"
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
	displayName := strings.TrimSpace(frontmatter["name"])
	description := strings.TrimSpace(frontmatter["description"])
	hasDescription := description != ""
	if description == "" {
		description = extractDescription(body)
	}
	userInvocable := parseBoolDefault(frontmatter["user-invocable"], true)
	paths := parseSkillPaths(frontmatter["paths"])

	command := contracts.Command{
		Type:                    contracts.CommandPrompt,
		Name:                    filepath.Base(root),
		DisplayName:             displayName,
		Description:             description,
		ArgumentHint:            strings.TrimSpace(frontmatter["argument-hint"]),
		ArgumentNames:           parseFrontmatterList(frontmatter["arguments"]),
		Source:                  source,
		LoadedFrom:              string(source),
		SkillRoot:               root,
		DisableModelInvocation:  parseBoolDefault(frontmatter["disable-model-invocation"], false),
		Hidden:                  !userInvocable,
		AllowedTools:            parseFrontmatterList(frontmatter["allowed-tools"]),
		WhenToUse:               strings.TrimSpace(frontmatter["when_to_use"]),
		Version:                 strings.TrimSpace(frontmatter["version"]),
		Model:                   strings.TrimSpace(frontmatter["model"]),
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
