package filetools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const defaultSearchLimit = 100

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Limit   *int   `json:"limit,omitempty"`
}

type grepInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	OutputMode string `json:"output_mode,omitempty"`
	Limit      *int   `json:"limit,omitempty"`
}

type fileSearchMatch struct {
	Path    string
	RelPath string
	ModUnix int64
}

type grepMatch struct {
	Path  string
	Line  int
	Text  string
	Count int
}

func NewGlobTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "Glob",
			Description:     "Find files by glob pattern.",
			SearchHint:      "find files by pattern",
			ReadOnly:        true,
			ConcurrencySafe: true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"pattern"},
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string"},
					"path":    map[string]any{"type": "string"},
					"limit":   map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Finds files under path matching a glob pattern. Supports ** for recursive directory matches. Results are sorted by most recently modified first.", nil
		},
		ValidateFunc:    validateGlob,
		CallFunc:        callGlob,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewGrepTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "Grep",
			Description:     "Search file contents by regular expression.",
			SearchHint:      "search text in files",
			ReadOnly:        true,
			ConcurrencySafe: true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"pattern"},
				"properties": map[string]any{
					"pattern":     map[string]any{"type": "string"},
					"path":        map[string]any{"type": "string"},
					"glob":        map[string]any{"type": "string"},
					"output_mode": map[string]any{"type": "string", "enum": []any{"files_with_matches", "content", "count"}},
					"limit":       map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Searches text files under path using a regular expression. output_mode may be files_with_matches, content, or count; glob optionally filters file paths.", nil
		},
		ValidateFunc:    validateGrep,
		CallFunc:        callGrep,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func validateGlob(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeGlob(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Pattern) == "" {
		return fmt.Errorf("pattern is required")
	}
	if input.Limit != nil && *input.Limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	root := searchRoot(ctx.WorkingDirectory, input.Path)
	if isBlockedDevicePath(root) {
		return fmt.Errorf("cannot search %q: this device path would block or produce infinite output", input.Path)
	}
	return nil
}

func callGlob(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeGlob(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	root := searchRoot(ctx.WorkingDirectory, input.Path)
	limit := inputLimit(input.Limit)
	matches, truncated, err := collectGlobMatches(root, input.Pattern, limit)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		paths = append(paths, match.RelPath)
	}
	content := strings.Join(paths, "\n")
	if content == "" {
		content = "No files found"
	}
	return contracts.ToolResult{
		Content: content,
		StructuredContent: map[string]any{
			"type":      "glob",
			"pattern":   input.Pattern,
			"path":      input.Path,
			"files":     paths,
			"truncated": truncated,
		},
	}, nil
}

func validateGrep(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeGrep(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Pattern) == "" {
		return fmt.Errorf("pattern is required")
	}
	if _, err := regexp.Compile(input.Pattern); err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	switch normalizedGrepOutputMode(input.OutputMode) {
	case "files_with_matches", "content", "count":
	default:
		return fmt.Errorf("output_mode must be one of files_with_matches, content, or count")
	}
	if input.Limit != nil && *input.Limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	root := searchRoot(ctx.WorkingDirectory, input.Path)
	if isBlockedDevicePath(root) {
		return fmt.Errorf("cannot search %q: this device path would block or produce infinite output", input.Path)
	}
	return nil
}

func callGrep(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeGrep(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	expr, err := regexp.Compile(input.Pattern)
	if err != nil {
		return contracts.ToolResult{}, fmt.Errorf("invalid pattern: %w", err)
	}
	root := searchRoot(ctx.WorkingDirectory, input.Path)
	mode := normalizedGrepOutputMode(input.OutputMode)
	limit := inputLimit(input.Limit)
	matches, truncated, err := collectGrepMatches(root, input.Glob, expr, mode, limit)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	content := formatGrepMatches(matches, mode)
	if content == "" {
		content = "No matches found"
	}
	return contracts.ToolResult{
		Content: content,
		StructuredContent: map[string]any{
			"type":        "grep",
			"pattern":     input.Pattern,
			"path":        input.Path,
			"glob":        input.Glob,
			"output_mode": mode,
			"matches":     grepStructuredMatches(matches, mode),
			"truncated":   truncated,
		},
	}, nil
}

func decodeGlob(raw json.RawMessage) (globInput, error) {
	var input globInput
	if err := decodeStrict(raw, map[string]struct{}{"pattern": {}, "path": {}, "limit": {}}, &input); err != nil {
		return globInput{}, err
	}
	return input, nil
}

func decodeGrep(raw json.RawMessage) (grepInput, error) {
	var input grepInput
	if err := decodeStrict(raw, map[string]struct{}{"pattern": {}, "path": {}, "glob": {}, "output_mode": {}, "limit": {}}, &input); err != nil {
		return grepInput{}, err
	}
	return input, nil
}

func searchRoot(cwd string, path string) string {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	return resolvePath(cwd, path)
}

func inputLimit(limit *int) int {
	if limit == nil {
		return defaultSearchLimit
	}
	return *limit
}

func collectGlobMatches(root string, pattern string, limit int) ([]fileSearchMatch, bool, error) {
	var matches []fileSearchMatch
	err := walkSearchFiles(root, func(path string, rel string, info os.FileInfo) error {
		ok, err := matchGlobPath(pattern, rel)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		matches = append(matches, fileSearchMatch{Path: path, RelPath: rel, ModUnix: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].ModUnix != matches[j].ModUnix {
			return matches[i].ModUnix > matches[j].ModUnix
		}
		return matches[i].RelPath < matches[j].RelPath
	})
	truncated := len(matches) > limit
	if truncated {
		matches = matches[:limit]
	}
	return matches, truncated, nil
}

func collectGrepMatches(root string, glob string, expr *regexp.Regexp, mode string, limit int) ([]grepMatch, bool, error) {
	var matches []grepMatch
	err := walkSearchFiles(root, func(path string, rel string, _ os.FileInfo) error {
		if glob != "" {
			ok, err := matchGlobPath(glob, rel)
			if err != nil || !ok {
				return err
			}
		}
		if hasBinaryExtension(path) {
			return nil
		}
		content, err := readText(path)
		if err != nil {
			return nil
		}
		lineMatches := grepFileMatches(rel, content, expr)
		if len(lineMatches) == 0 {
			return nil
		}
		switch mode {
		case "files_with_matches":
			matches = append(matches, grepMatch{Path: rel})
		case "count":
			matches = append(matches, grepMatch{Path: rel, Count: len(lineMatches)})
		default:
			matches = append(matches, lineMatches...)
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Path != matches[j].Path {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].Line < matches[j].Line
	})
	truncated := len(matches) > limit
	if truncated {
		matches = matches[:limit]
	}
	return matches, truncated, nil
}

func grepFileMatches(path string, content string, expr *regexp.Regexp) []grepMatch {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	matches := make([]grepMatch, 0)
	for i, line := range lines {
		if expr.MatchString(line) {
			matches = append(matches, grepMatch{Path: path, Line: i + 1, Text: line})
		}
	}
	return matches
}

func formatGrepMatches(matches []grepMatch, mode string) string {
	lines := make([]string, 0, len(matches))
	for _, match := range matches {
		switch mode {
		case "files_with_matches":
			lines = append(lines, match.Path)
		case "count":
			lines = append(lines, fmt.Sprintf("%s:%d", match.Path, match.Count))
		default:
			lines = append(lines, fmt.Sprintf("%s:%d:%s", match.Path, match.Line, match.Text))
		}
	}
	return strings.Join(lines, "\n")
}

func grepStructuredMatches(matches []grepMatch, mode string) []map[string]any {
	out := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		item := map[string]any{"path": match.Path}
		switch mode {
		case "content":
			item["line"] = match.Line
			item["text"] = match.Text
		case "count":
			item["count"] = match.Count
		}
		out = append(out, item)
	}
	return out
}

func normalizedGrepOutputMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return "files_with_matches"
	}
	return mode
}

func walkSearchFiles(root string, visit func(path string, rel string, info os.FileInfo) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return visit(root, filepath.Base(root), info)
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && ignoredSearchDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return visit(path, filepath.ToSlash(rel), info)
	})
}

func ignoredSearchDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func matchGlobPath(pattern string, path string) (bool, error) {
	pattern = filepath.ToSlash(filepath.Clean(pattern))
	path = filepath.ToSlash(filepath.Clean(path))
	if pattern == "." {
		return path == ".", nil
	}
	if !strings.Contains(pattern, "/") {
		if ok, err := filepath.Match(pattern, filepath.Base(path)); ok || err != nil {
			return ok, err
		}
	}
	return matchGlobSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func matchGlobSegments(pattern []string, path []string) (bool, error) {
	if len(pattern) == 0 {
		return len(path) == 0, nil
	}
	if pattern[0] == "**" {
		if ok, err := matchGlobSegments(pattern[1:], path); ok || err != nil {
			return ok, err
		}
		for i := range path {
			if ok, err := matchGlobSegments(pattern[1:], path[i+1:]); ok || err != nil {
				return ok, err
			}
		}
		return false, nil
	}
	if len(path) == 0 {
		return false, nil
	}
	ok, err := filepath.Match(pattern[0], path[0])
	if err != nil || !ok {
		return ok, err
	}
	return matchGlobSegments(pattern[1:], path[1:])
}
