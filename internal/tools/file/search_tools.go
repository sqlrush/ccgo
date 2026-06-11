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
	Pattern            string `json:"pattern"`
	Path               string `json:"path,omitempty"`
	Glob               string `json:"glob,omitempty"`
	Type               string `json:"type,omitempty"`
	OutputMode         string `json:"output_mode,omitempty"`
	OutputModeAlt      string `json:"outputMode,omitempty"`
	Limit              *int   `json:"limit,omitempty"`
	HeadLimit          *int   `json:"head_limit,omitempty"`
	HeadLimitAlt       *int   `json:"headLimit,omitempty"`
	Offset             *int   `json:"offset,omitempty"`
	Context            *int   `json:"context,omitempty"`
	ShortContext       *int   `json:"-C,omitempty"`
	BeforeContext      *int   `json:"before_context,omitempty"`
	BeforeContextAlt   *int   `json:"beforeContext,omitempty"`
	ShortBeforeContext *int   `json:"-B,omitempty"`
	AfterContext       *int   `json:"after_context,omitempty"`
	AfterContextAlt    *int   `json:"afterContext,omitempty"`
	ShortAfterContext  *int   `json:"-A,omitempty"`
	CaseInsensitive    bool   `json:"case_insensitive,omitempty"`
	CaseInsensitiveAlt bool   `json:"caseInsensitive,omitempty"`
	ShortIgnoreCase    bool   `json:"-i,omitempty"`
	FixedStrings       bool   `json:"fixed_strings,omitempty"`
	FixedStringsAlt    bool   `json:"fixedStrings,omitempty"`
	ShortFixedStrings  bool   `json:"-F,omitempty"`
}

type fileSearchMatch struct {
	Path    string
	RelPath string
	ModUnix int64
}

type grepMatch struct {
	Path    string
	Line    int
	Text    string
	Count   int
	Matched bool
}

type grepOptions struct {
	Mode          string
	Limit         int
	Offset        int
	BeforeContext int
	AfterContext  int
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
					"type":        map[string]any{"type": "string"},
					"output_mode": map[string]any{"type": "string", "enum": []any{"files_with_matches", "content", "count"}},
					"outputMode":  map[string]any{"type": "string", "enum": []any{"files_with_matches", "content", "count"}},
					"limit":       map[string]any{"type": "integer"},
					"head_limit":  map[string]any{"type": "integer"},
					"headLimit":   map[string]any{"type": "integer"},
					"offset":      map[string]any{"type": "integer"},
					"context":     map[string]any{"type": "integer"},
					"-C":          map[string]any{"type": "integer"},
					"before_context": map[string]any{
						"type": "integer",
					},
					"beforeContext": map[string]any{"type": "integer"},
					"-B":            map[string]any{"type": "integer"},
					"after_context": map[string]any{
						"type": "integer",
					},
					"afterContext":     map[string]any{"type": "integer"},
					"-A":               map[string]any{"type": "integer"},
					"case_insensitive": map[string]any{"type": "boolean"},
					"caseInsensitive":  map[string]any{"type": "boolean"},
					"-i":               map[string]any{"type": "boolean"},
					"fixed_strings":    map[string]any{"type": "boolean"},
					"fixedStrings":     map[string]any{"type": "boolean"},
					"-F":               map[string]any{"type": "boolean"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Searches text files under path using a regular expression or fixed string. output_mode may be files_with_matches, content, or count; glob and type optionally filter file paths. content mode supports context, before_context, after_context, -C, -B, -A, offset, and head_limit pagination. Use fixed_strings or -F for literal matching.", nil
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
	if _, err := compileGrepPattern(input); err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	mode := normalizedGrepOutputMode(input)
	switch mode {
	case "files_with_matches", "content", "count":
	default:
		return fmt.Errorf("output_mode must be one of files_with_matches, content, or count")
	}
	if input.Limit != nil && *input.Limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	if input.HeadLimit != nil && *input.HeadLimit <= 0 {
		return fmt.Errorf("head_limit must be positive")
	}
	if input.HeadLimitAlt != nil && *input.HeadLimitAlt <= 0 {
		return fmt.Errorf("head_limit must be positive")
	}
	if input.Offset != nil && *input.Offset < 0 {
		return fmt.Errorf("offset must be non-negative")
	}
	if _, err := grepTypeExtensions(input.Type); err != nil {
		return err
	}
	before, after := grepContextLines(input)
	if before < 0 || after < 0 {
		return fmt.Errorf("context values must be non-negative")
	}
	if mode != "content" && (before > 0 || after > 0) {
		return fmt.Errorf("context is only supported with output_mode content")
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
	expr, err := compileGrepPattern(input)
	if err != nil {
		return contracts.ToolResult{}, fmt.Errorf("invalid pattern: %w", err)
	}
	root := searchRoot(ctx.WorkingDirectory, input.Path)
	mode := normalizedGrepOutputMode(input)
	before, after := grepContextLines(input)
	options := grepOptions{
		Mode:          mode,
		Limit:         grepLimit(input),
		Offset:        grepOffset(input),
		BeforeContext: before,
		AfterContext:  after,
	}
	matches, totalMatches, truncated, err := collectGrepMatches(root, input.Glob, input.Type, expr, options)
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
			"type":             "grep",
			"pattern":          input.Pattern,
			"path":             input.Path,
			"glob":             input.Glob,
			"type_filter":      input.Type,
			"output_mode":      mode,
			"matches":          grepStructuredMatches(matches, mode),
			"total_matches":    totalMatches,
			"offset":           options.Offset,
			"limit":            options.Limit,
			"before_context":   options.BeforeContext,
			"after_context":    options.AfterContext,
			"case_insensitive": grepCaseInsensitive(input),
			"fixed_strings":    grepFixedStrings(input),
			"truncated":        truncated,
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
	if err := decodeStrict(raw, map[string]struct{}{
		"pattern": {}, "path": {}, "glob": {}, "type": {}, "output_mode": {}, "outputMode": {}, "limit": {},
		"head_limit": {}, "headLimit": {}, "offset": {},
		"context": {}, "-C": {}, "before_context": {}, "beforeContext": {}, "-B": {}, "after_context": {}, "afterContext": {}, "-A": {},
		"case_insensitive": {}, "caseInsensitive": {}, "-i": {},
		"fixed_strings": {}, "fixedStrings": {}, "-F": {},
	}, &input); err != nil {
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

func collectGrepMatches(root string, glob string, typeFilter string, expr *regexp.Regexp, options grepOptions) ([]grepMatch, int, bool, error) {
	typeExtensions, err := grepTypeExtensions(typeFilter)
	if err != nil {
		return nil, 0, false, err
	}
	var matches []grepMatch
	err = walkSearchFiles(root, func(path string, rel string, _ os.FileInfo) error {
		if glob != "" {
			ok, err := matchGlobPath(glob, rel)
			if err != nil || !ok {
				return err
			}
		}
		if len(typeExtensions) > 0 && !grepTypeMatches(path, typeExtensions) {
			return nil
		}
		if hasBinaryExtension(path) {
			return nil
		}
		content, err := readText(path)
		if err != nil {
			return nil
		}
		lineMatches := grepFileMatches(rel, content, expr, options.BeforeContext, options.AfterContext)
		if len(lineMatches) == 0 {
			return nil
		}
		switch options.Mode {
		case "files_with_matches":
			matches = append(matches, grepMatch{Path: rel})
		case "count":
			matches = append(matches, grepMatch{Path: rel, Count: countMatchedLines(lineMatches)})
		default:
			matches = append(matches, lineMatches...)
		}
		return nil
	})
	if err != nil {
		return nil, 0, false, err
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Path != matches[j].Path {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].Line < matches[j].Line
	})
	totalMatches := len(matches)
	start := options.Offset
	if start > len(matches) {
		start = len(matches)
	}
	end := start + options.Limit
	if end > len(matches) {
		end = len(matches)
	}
	truncated := end < len(matches)
	matches = matches[start:end]
	return matches, totalMatches, truncated, nil
}

func grepFileMatches(path string, content string, expr *regexp.Regexp, beforeContext int, afterContext int) []grepMatch {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	matched := map[int]bool{}
	included := map[int]bool{}
	for i, line := range lines {
		if expr.MatchString(line) {
			matched[i] = true
			start := i - beforeContext
			if start < 0 {
				start = 0
			}
			end := i + afterContext
			if end >= len(lines) {
				end = len(lines) - 1
			}
			for n := start; n <= end; n++ {
				included[n] = true
			}
		}
	}
	matches := make([]grepMatch, 0, len(included))
	for i := range lines {
		if !included[i] {
			continue
		}
		matches = append(matches, grepMatch{Path: path, Line: i + 1, Text: lines[i], Matched: matched[i]})
	}
	return matches
}

func countMatchedLines(matches []grepMatch) int {
	count := 0
	for _, match := range matches {
		if match.Matched {
			count++
		}
	}
	return count
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
			separator := ":"
			if !match.Matched {
				separator = "-"
			}
			lines = append(lines, fmt.Sprintf("%s%s%d%s%s", match.Path, separator, match.Line, separator, match.Text))
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
			item["matched"] = match.Matched
		case "count":
			item["count"] = match.Count
		}
		out = append(out, item)
	}
	return out
}

func normalizedGrepOutputMode(input grepInput) string {
	mode := strings.TrimSpace(input.OutputMode)
	if mode == "" {
		mode = strings.TrimSpace(input.OutputModeAlt)
	}
	if mode == "" {
		return "files_with_matches"
	}
	return mode
}

func compileGrepPattern(input grepInput) (*regexp.Regexp, error) {
	pattern := input.Pattern
	if grepFixedStrings(input) {
		pattern = regexp.QuoteMeta(pattern)
	}
	if grepCaseInsensitive(input) {
		pattern = "(?i:" + pattern + ")"
	}
	return regexp.Compile(pattern)
}

func grepCaseInsensitive(input grepInput) bool {
	return input.CaseInsensitive || input.CaseInsensitiveAlt || input.ShortIgnoreCase
}

func grepFixedStrings(input grepInput) bool {
	return input.FixedStrings || input.FixedStringsAlt || input.ShortFixedStrings
}

func grepTypeExtensions(typeFilter string) ([]string, error) {
	typeFilter = strings.TrimSpace(strings.TrimPrefix(typeFilter, "."))
	if typeFilter == "" {
		return nil, nil
	}
	if strings.ContainsAny(typeFilter, `/\*?[]`) {
		return nil, fmt.Errorf("type must be a file type or extension, not a glob")
	}
	switch strings.ToLower(typeFilter) {
	case "c":
		return []string{".c", ".h"}, nil
	case "cpp", "c++":
		return []string{".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"}, nil
	case "go", "golang":
		return []string{".go"}, nil
	case "js", "javascript":
		return []string{".js", ".jsx", ".mjs", ".cjs"}, nil
	case "ts", "typescript":
		return []string{".ts", ".tsx", ".mts", ".cts"}, nil
	case "py", "python":
		return []string{".py", ".pyw"}, nil
	case "rb", "ruby":
		return []string{".rb"}, nil
	case "rs", "rust":
		return []string{".rs"}, nil
	case "java":
		return []string{".java"}, nil
	case "kt", "kotlin":
		return []string{".kt", ".kts"}, nil
	case "swift":
		return []string{".swift"}, nil
	case "php":
		return []string{".php"}, nil
	case "sh", "shell":
		return []string{".sh", ".bash", ".zsh", ".fish"}, nil
	case "sql":
		return []string{".sql"}, nil
	case "html":
		return []string{".html", ".htm"}, nil
	case "css":
		return []string{".css", ".scss", ".sass", ".less"}, nil
	case "json":
		return []string{".json", ".jsonc"}, nil
	case "yaml", "yml":
		return []string{".yaml", ".yml"}, nil
	case "md", "markdown":
		return []string{".md", ".markdown"}, nil
	case "txt", "text":
		return []string{".txt"}, nil
	default:
		return []string{"." + strings.ToLower(typeFilter)}, nil
	}
}

func grepTypeMatches(path string, extensions []string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, candidate := range extensions {
		if ext == candidate {
			return true
		}
	}
	return false
}

func grepLimit(input grepInput) int {
	if input.Limit != nil {
		return *input.Limit
	}
	if input.HeadLimit != nil {
		return *input.HeadLimit
	}
	if input.HeadLimitAlt != nil {
		return *input.HeadLimitAlt
	}
	return defaultSearchLimit
}

func grepOffset(input grepInput) int {
	if input.Offset == nil {
		return 0
	}
	return *input.Offset
}

func grepContextLines(input grepInput) (int, int) {
	before := 0
	after := 0
	if input.ShortContext != nil {
		before = *input.ShortContext
		after = *input.ShortContext
	}
	if input.Context != nil {
		before = *input.Context
		after = *input.Context
	}
	if input.ShortBeforeContext != nil {
		before = *input.ShortBeforeContext
	}
	if input.BeforeContext != nil {
		before = *input.BeforeContext
	} else if input.BeforeContextAlt != nil {
		before = *input.BeforeContextAlt
	}
	if input.ShortAfterContext != nil {
		after = *input.ShortAfterContext
	}
	if input.AfterContext != nil {
		after = *input.AfterContext
	} else if input.AfterContextAlt != nil {
		after = *input.AfterContextAlt
	}
	return before, after
}

func walkSearchFiles(root string, visit func(path string, rel string, info os.FileInfo) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return visit(root, filepath.Base(root), info)
	}
	ignoreRules := loadSearchIgnoreRules(root, "")
	return walkSearchDir(root, root, ignoreRules, visit)
}

func walkSearchDir(root string, dir string, ignoreRules searchIgnoreRules, visit func(path string, rel string, info os.FileInfo) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if ignoredSearchDir(entry.Name()) || ignoreRules.Ignored(rel, true) {
				continue
			}
			dirRules := append(searchIgnoreRules(nil), ignoreRules...)
			dirRules = append(dirRules, loadSearchIgnoreRules(path, rel)...)
			if err := walkSearchDir(root, path, dirRules, visit); err != nil {
				return err
			}
			continue
		}
		if ignoreRules.Ignored(rel, false) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := visit(path, rel, info); err != nil {
			return err
		}
	}
	return nil
}

func ignoredSearchDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

type searchIgnoreRules []searchIgnoreRule

type searchIgnoreRule struct {
	Pattern       string
	Base          string
	Negate        bool
	DirectoryOnly bool
	Anchored      bool
}

func loadSearchIgnoreRules(dir string, base string) searchIgnoreRules {
	var rules searchIgnoreRules
	for _, name := range []string{".gitignore", ".ignore"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		rules = append(rules, parseSearchIgnoreRules(string(data), base)...)
	}
	return rules
}

func parseSearchIgnoreRules(content string, base string) searchIgnoreRules {
	var rules searchIgnoreRules
	base = filepath.ToSlash(filepath.Clean(base))
	if base == "." {
		base = ""
	}
	for _, rawLine := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negate := false
		if strings.HasPrefix(line, "!") {
			negate = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
			if line == "" {
				continue
			}
		}
		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		directoryOnly := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		line = filepath.ToSlash(filepath.Clean(line))
		if line == "." || line == "" {
			continue
		}
		rules = append(rules, searchIgnoreRule{
			Pattern:       line,
			Base:          base,
			Negate:        negate,
			DirectoryOnly: directoryOnly,
			Anchored:      anchored,
		})
	}
	return rules
}

func (rules searchIgnoreRules) Ignored(rel string, isDir bool) bool {
	ignored := false
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." {
		return false
	}
	for _, rule := range rules {
		if rule.Matches(rel, isDir) {
			ignored = !rule.Negate
		}
	}
	return ignored
}

func (rule searchIgnoreRule) Matches(rel string, isDir bool) bool {
	if rule.Pattern == "" {
		return false
	}
	localRel := rel
	if rule.Base != "" {
		if rel == rule.Base || !strings.HasPrefix(rel, rule.Base+"/") {
			return false
		}
		localRel = strings.TrimPrefix(rel, rule.Base+"/")
	}
	if rule.DirectoryOnly && !isDir && !pathUnderIgnoreDirectory(rule.Pattern, localRel, rule.Anchored) {
		return false
	}
	if !strings.Contains(rule.Pattern, "/") && !rule.Anchored {
		return matchIgnoreBasename(rule.Pattern, localRel)
	}
	if rule.DirectoryOnly {
		return localRel == rule.Pattern || strings.HasPrefix(localRel, rule.Pattern+"/")
	}
	ok, err := matchGlobPath(rule.Pattern, localRel)
	return err == nil && ok
}

func pathUnderIgnoreDirectory(pattern string, rel string, anchored bool) bool {
	if anchored || strings.Contains(pattern, "/") {
		return rel == pattern || strings.HasPrefix(rel, pattern+"/")
	}
	segments := strings.Split(rel, "/")
	for i := 0; i < len(segments)-1; i++ {
		ok, err := filepath.Match(pattern, segments[i])
		if err == nil && ok {
			return true
		}
	}
	return false
}

func matchIgnoreBasename(pattern string, rel string) bool {
	for _, segment := range strings.Split(rel, "/") {
		ok, err := filepath.Match(pattern, segment)
		if err == nil && ok {
			return true
		}
	}
	return false
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
