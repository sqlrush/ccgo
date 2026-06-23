// Package agentfile provides the file model for .claude/agents/*.md agent definition files.
// Each file contains YAML frontmatter (name, description, tools, model, effort, color, memory)
// followed by a body that serves as the agent's system prompt.
package agentfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"ccgo/internal/memory"
)

// AgentFile represents a parsed .claude/agents/*.md file.
// All fields are immutable — callers must not modify the struct after creation.
type AgentFile struct {
	Name         string
	Description  string
	Tools        []string
	Model        string
	Effort       string
	Color        string
	Memory       string
	Isolation    string // "worktree" causes the agent to run in an isolated git worktree by default
	Background   bool   // when true the agent always runs as a background task
	OmitClaudeMd bool   // when true the agent's system prompt omits the CLAUDE.md hierarchy
	Prompt       string
	Path         string // absolute path, empty when not loaded from disk
}

// validName matches agent names that are safe as file names and identifiers.
var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Parse parses a raw .md file (name + content) into an AgentFile.
// The content must begin with a --- frontmatter block. The remainder
// of the file after the closing --- delimiter is used verbatim as the
// system prompt (Prompt field). Path is left empty; callers set it
// when loading from disk.
func Parse(name string, content []byte) (AgentFile, error) {
	if name == "" {
		return AgentFile{}, errors.New("agentfile: name must not be empty")
	}

	fm, body := memory.ParseFrontmatter(string(content))

	// frontmatter name overrides the caller-supplied name only when present and non-empty.
	if fmName := strings.TrimSpace(fm["name"]); fmName != "" {
		name = fmName
	}

	if !validName.MatchString(name) {
		return AgentFile{}, fmt.Errorf("agentfile: invalid name %q (must match [a-zA-Z0-9_-]+)", name)
	}

	return AgentFile{
		Name:         name,
		Description:  strings.TrimSpace(fm["description"]),
		Tools:        parseToolList(fm["tools"]),
		Model:        strings.TrimSpace(fm["model"]),
		Effort:       strings.TrimSpace(fm["effort"]),
		Color:        strings.TrimSpace(fm["color"]),
		Memory:       strings.TrimSpace(fm["memory"]),
		Isolation:    parseIsolation(fm["isolation"]),
		Background:   parseBoolField(fm["background"]),
		OmitClaudeMd: parseBoolField(fm["omitClaudeMd"]),
		Prompt:       body,
	}, nil
}

// Format serialises an AgentFile back into the .md wire format.
// Empty optional fields are omitted from the frontmatter.
// The result is always parseable by Parse.
func Format(a AgentFile) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: ")
	sb.WriteString(a.Name)
	sb.WriteByte('\n')
	if a.Description != "" {
		sb.WriteString("description: ")
		sb.WriteString(a.Description)
		sb.WriteByte('\n')
	}
	if len(a.Tools) > 0 {
		sb.WriteString("tools: ")
		sb.WriteString(strings.Join(a.Tools, ", "))
		sb.WriteByte('\n')
	}
	if a.Model != "" {
		sb.WriteString("model: ")
		sb.WriteString(a.Model)
		sb.WriteByte('\n')
	}
	if a.Effort != "" {
		sb.WriteString("effort: ")
		sb.WriteString(a.Effort)
		sb.WriteByte('\n')
	}
	if a.Color != "" {
		sb.WriteString("color: ")
		sb.WriteString(a.Color)
		sb.WriteByte('\n')
	}
	if a.Memory != "" {
		sb.WriteString("memory: ")
		sb.WriteString(a.Memory)
		sb.WriteByte('\n')
	}
	if a.Isolation != "" {
		sb.WriteString("isolation: ")
		sb.WriteString(a.Isolation)
		sb.WriteByte('\n')
	}
	if a.Background {
		sb.WriteString("background: true\n")
	}
	if a.OmitClaudeMd {
		sb.WriteString("omitClaudeMd: true\n")
	}
	sb.WriteString("---\n")
	sb.WriteString(a.Prompt)
	return sb.String()
}

// ProjectDir returns the project-scoped agents directory for the given
// working directory: <cwd>/.claude/agents.
func ProjectDir(cwd string) string {
	return filepath.Join(cwd, ".claude", "agents")
}

// UserDir returns the user-scoped agents directory: ~/.claude/agents.
func UserDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("agentfile: cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "agents"), nil
}

// List reads zero or more agent directories and returns the parsed AgentFiles
// found across all of them. Directories that do not exist are silently skipped.
// The order within each directory is determined by the filesystem glob.
func List(dirs ...string) ([]AgentFile, error) {
	var out []AgentFile
	for _, dir := range dirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
		if err != nil {
			return nil, fmt.Errorf("agentfile: glob %s: %w", dir, err)
		}
		for _, path := range matches {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("agentfile: read %s: %w", path, err)
			}
			name := strings.TrimSuffix(filepath.Base(path), ".md")
			a, err := Parse(name, data)
			if err != nil {
				return nil, fmt.Errorf("agentfile: parse %s: %w", path, err)
			}
			a.Path = path
			out = append(out, a)
		}
	}
	return out, nil
}

// Save writes the agent to <dir>/<a.Name>.md.
// It fails with an error if the file already exists (no-overwrite semantics,
// equivalent to POSIX O_EXCL / open(2) with O_CREAT|O_EXCL).
// It also validates the name to prevent path traversal.
func Save(dir string, a AgentFile) error {
	if err := validateNameForFS(a.Name); err != nil {
		return err
	}
	path := filepath.Join(dir, a.Name+".md")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("agentfile: %s already exists; delete it first to replace", path)
		}
		return fmt.Errorf("agentfile: create %s: %w", path, err)
	}
	_, writeErr := f.WriteString(Format(a))
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(path) // best-effort cleanup on write failure
		return fmt.Errorf("agentfile: write %s: %w", path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("agentfile: close %s: %w", path, closeErr)
	}
	return nil
}

// Delete removes <dir>/<name>.md from disk.
// It validates the name to prevent path traversal.
func Delete(dir, name string) error {
	if err := validateNameForFS(name); err != nil {
		return err
	}
	path := filepath.Join(dir, name+".md")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("agentfile: delete %s: %w", path, err)
	}
	return nil
}

// validateNameForFS rejects names that would escape the intended directory.
func validateNameForFS(name string) error {
	if name == "" {
		return errors.New("agentfile: name must not be empty")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("agentfile: unsafe name %q", name)
	}
	if !validName.MatchString(name) {
		return fmt.Errorf("agentfile: invalid name %q (must match [a-zA-Z0-9_-]+)", name)
	}
	return nil
}

// parseIsolation validates and normalises the isolation frontmatter value.
// Only "worktree" is accepted; anything else is silently ignored.
func parseIsolation(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "worktree" {
		return "worktree"
	}
	return ""
}

// parseBoolField parses "true"/"false" frontmatter values (case-insensitive).
// Returns true only for the literal string "true".
func parseBoolField(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), "true")
}

// parseToolList splits a comma-separated tools string (e.g. "Read, Grep, Bash")
// into a trimmed slice. Returns nil for empty input.
func parseToolList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Strip optional surrounding brackets: [Read, Grep]
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		raw = strings.TrimSpace(raw[1 : len(raw)-1])
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
