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
const defaultGrepHeadLimit = 250
const grepMaxColumns = 500
const fileNotFoundCWDNote = "Note: your current working directory is"
const globTruncatedMessage = "(Results are truncated. Consider using a more specific path or pattern.)"
const grepOmittedLongMatchingLine = "[Omitted long matching line]"
const grepOmittedLongContextLine = "[Omitted long context line]"

var semanticNumberLiteralRE = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

var allowedGrepInputKeys = map[string]struct{}{
	"pattern": {}, "path": {}, "glob": {}, "type": {}, "output_mode": {}, "outputMode": {}, "limit": {},
	"head_limit": {}, "headLimit": {}, "offset": {}, "max_count": {}, "maxCount": {}, "-m": {},
	"context": {}, "-C": {}, "before_context": {}, "beforeContext": {}, "-B": {}, "after_context": {}, "afterContext": {}, "-A": {}, "line_numbers": {}, "lineNumbers": {}, "-n": {},
	"ignore_case": {}, "case_insensitive": {}, "caseInsensitive": {}, "-i": {},
	"fixed_strings": {}, "fixedStrings": {}, "-F": {}, "multiline": {},
	"word_regexp": {}, "wordRegexp": {}, "word-regexp": {}, "-w": {},
	"invert_match": {}, "invertMatch": {}, "invert-match": {}, "-v": {},
}

var grepSemanticNumberKeys = map[string]struct{}{
	"limit": {}, "head_limit": {}, "headLimit": {}, "offset": {}, "max_count": {}, "maxCount": {}, "-m": {},
	"context": {}, "-C": {}, "before_context": {}, "beforeContext": {}, "-B": {}, "after_context": {}, "afterContext": {}, "-A": {},
}

var grepSemanticBooleanKeys = map[string]struct{}{
	"line_numbers": {}, "lineNumbers": {}, "-n": {},
	"ignore_case": {}, "case_insensitive": {}, "caseInsensitive": {}, "-i": {},
	"fixed_strings": {}, "fixedStrings": {}, "-F": {}, "multiline": {},
	"word_regexp": {}, "wordRegexp": {}, "word-regexp": {}, "-w": {},
	"invert_match": {}, "invertMatch": {}, "invert-match": {}, "-v": {},
}

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
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
	MaxCount           *int   `json:"max_count,omitempty"`
	MaxCountAlt        *int   `json:"maxCount,omitempty"`
	ShortMaxCount      *int   `json:"-m,omitempty"`
	Context            *int   `json:"context,omitempty"`
	ShortContext       *int   `json:"-C,omitempty"`
	BeforeContext      *int   `json:"before_context,omitempty"`
	BeforeContextAlt   *int   `json:"beforeContext,omitempty"`
	ShortBeforeContext *int   `json:"-B,omitempty"`
	AfterContext       *int   `json:"after_context,omitempty"`
	AfterContextAlt    *int   `json:"afterContext,omitempty"`
	ShortAfterContext  *int   `json:"-A,omitempty"`
	LineNumbers        *bool  `json:"line_numbers,omitempty"`
	LineNumbersAlt     *bool  `json:"lineNumbers,omitempty"`
	ShortLineNumbers   *bool  `json:"-n,omitempty"`
	IgnoreCase         bool   `json:"ignore_case,omitempty"`
	CaseInsensitive    bool   `json:"case_insensitive,omitempty"`
	CaseInsensitiveAlt bool   `json:"caseInsensitive,omitempty"`
	ShortIgnoreCase    bool   `json:"-i,omitempty"`
	FixedStrings       bool   `json:"fixed_strings,omitempty"`
	FixedStringsAlt    bool   `json:"fixedStrings,omitempty"`
	ShortFixedStrings  bool   `json:"-F,omitempty"`
	WordRegexp         bool   `json:"word_regexp,omitempty"`
	WordRegexpAlt      bool   `json:"wordRegexp,omitempty"`
	WordRegexpDash     bool   `json:"word-regexp,omitempty"`
	ShortWordRegexp    bool   `json:"-w,omitempty"`
	InvertMatch        bool   `json:"invert_match,omitempty"`
	InvertMatchAlt     bool   `json:"invertMatch,omitempty"`
	InvertMatchDash    bool   `json:"invert-match,omitempty"`
	ShortInvertMatch   bool   `json:"-v,omitempty"`
	Multiline          bool   `json:"multiline,omitempty"`
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
	ModUnix int64
}

type grepOptions struct {
	Mode          string
	Limit         int
	Offset        int
	MaxCount      int
	BeforeContext int
	AfterContext  int
	LineNumbers   bool
	Multiline     bool
	InvertMatch   bool
}

type searchWalkOptions struct {
	UseIgnoreFiles bool
	IncludeHidden  bool
	ExcludeVCSDirs bool
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
					"max_count":   map[string]any{"type": "integer"},
					"maxCount":    map[string]any{"type": "integer"},
					"-m":          map[string]any{"type": "integer"},
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
					"line_numbers":     map[string]any{"type": "boolean"},
					"lineNumbers":      map[string]any{"type": "boolean"},
					"-n":               map[string]any{"type": "boolean"},
					"ignore_case":      map[string]any{"type": "boolean"},
					"case_insensitive": map[string]any{"type": "boolean"},
					"caseInsensitive":  map[string]any{"type": "boolean"},
					"-i":               map[string]any{"type": "boolean"},
					"fixed_strings":    map[string]any{"type": "boolean"},
					"fixedStrings":     map[string]any{"type": "boolean"},
					"-F":               map[string]any{"type": "boolean"},
					"word_regexp":      map[string]any{"type": "boolean"},
					"wordRegexp":       map[string]any{"type": "boolean"},
					"word-regexp":      map[string]any{"type": "boolean"},
					"-w":               map[string]any{"type": "boolean"},
					"invert_match":     map[string]any{"type": "boolean"},
					"invertMatch":      map[string]any{"type": "boolean"},
					"invert-match":     map[string]any{"type": "boolean"},
					"-v":               map[string]any{"type": "boolean"},
					"multiline":        map[string]any{"type": "boolean"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Searches text files under path using a regular expression or fixed string. output_mode may be files_with_matches, content, or count; glob and type optionally filter file paths. glob accepts whitespace/comma-separated patterns and brace alternation. content mode supports context, before_context, after_context, -C, -B, -A, -n line-number control, offset, head_limit pagination, and max_count/-m per-file match limiting. Use fixed_strings or -F for literal matching, word_regexp or -w for whole-word matches, and invert_match or -v to select non-matching lines. Set multiline to allow patterns to span lines with dot matching newlines.", nil
		},
		NormalizeFunc:   normalizeGrepRawInput,
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
	root := searchRoot(ctx.WorkingDirectory, input.Path)
	if isBlockedDevicePath(root) {
		return fmt.Errorf("cannot search %q: this device path would block or produce infinite output", input.Path)
	}
	if err := validateSearchPath(ctx.WorkingDirectory, input.Path, root, true); err != nil {
		return err
	}
	return nil
}

func callGlob(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeGlob(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	displayRoot := searchRoot(ctx.WorkingDirectory, ".")
	root := searchRoot(ctx.WorkingDirectory, input.Path)
	pattern := input.Pattern
	if filepath.IsAbs(pattern) {
		root, pattern = globBaseDirectory(pattern)
	}
	matches, truncated, err := collectGlobMatches(root, displayRoot, pattern, defaultSearchLimit)
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
	} else if truncated {
		content += "\n" + globTruncatedMessage
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
	if input.HeadLimit != nil && *input.HeadLimit < 0 {
		return fmt.Errorf("head_limit must be non-negative")
	}
	if input.HeadLimitAlt != nil && *input.HeadLimitAlt < 0 {
		return fmt.Errorf("head_limit must be non-negative")
	}
	if input.Offset != nil && *input.Offset < 0 {
		return fmt.Errorf("offset must be non-negative")
	}
	if input.MaxCount != nil && *input.MaxCount < 0 {
		return fmt.Errorf("max_count must be non-negative")
	}
	if input.MaxCountAlt != nil && *input.MaxCountAlt < 0 {
		return fmt.Errorf("max_count must be non-negative")
	}
	if input.ShortMaxCount != nil && *input.ShortMaxCount < 0 {
		return fmt.Errorf("max_count must be non-negative")
	}
	if _, err := grepTypeExtensions(input.Type); err != nil {
		return err
	}
	before, after := grepContextLines(input)
	if before < 0 || after < 0 {
		return fmt.Errorf("context values must be non-negative")
	}
	root := searchRoot(ctx.WorkingDirectory, input.Path)
	if isBlockedDevicePath(root) {
		return fmt.Errorf("cannot search %q: this device path would block or produce infinite output", input.Path)
	}
	if err := validateSearchPath(ctx.WorkingDirectory, input.Path, root, false); err != nil {
		return err
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
	displayRoot := searchRoot(ctx.WorkingDirectory, ".")
	root := searchRoot(ctx.WorkingDirectory, input.Path)
	mode := normalizedGrepOutputMode(input)
	before, after := grepContextLines(input)
	if mode != "content" {
		before = 0
		after = 0
	}
	options := grepOptions{
		Mode:          mode,
		Limit:         grepLimit(input),
		Offset:        grepOffset(input),
		MaxCount:      grepMaxCount(input),
		BeforeContext: before,
		AfterContext:  after,
		LineNumbers:   grepLineNumbers(input, mode),
		Multiline:     input.Multiline,
		InvertMatch:   grepInvertMatch(input),
	}
	matches, totalMatches, truncated, err := collectGrepMatches(root, displayRoot, input.Glob, input.Type, expr, options)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	content := formatGrepResultContent(matches, options, truncated)
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
			"max_count":        options.MaxCount,
			"before_context":   options.BeforeContext,
			"after_context":    options.AfterContext,
			"line_numbers":     options.LineNumbers,
			"case_insensitive": grepCaseInsensitive(input),
			"fixed_strings":    grepFixedStrings(input),
			"word_regexp":      grepWordRegexp(input),
			"invert_match":     grepInvertMatch(input),
			"multiline":        input.Multiline,
			"truncated":        truncated,
		},
	}, nil
}

func decodeGlob(raw json.RawMessage) (globInput, error) {
	var input globInput
	if err := decodeStrict(raw, map[string]struct{}{"pattern": {}, "path": {}}, &input); err != nil {
		return globInput{}, err
	}
	return input, nil
}

func decodeGrep(raw json.RawMessage) (grepInput, error) {
	var input grepInput
	normalized, err := normalizeGrepRawInput(raw)
	if err != nil {
		return grepInput{}, err
	}
	if err := json.Unmarshal(normalized, &input); err != nil {
		return grepInput{}, err
	}
	return input, nil
}

func normalizeGrepRawInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeStrictObject(raw, allowedGrepInputKeys)
	if err != nil {
		return nil, err
	}
	coerceSemanticJSONStrings(obj, grepSemanticNumberKeys, grepSemanticBooleanKeys)
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func decodeStrictObject(raw json.RawMessage, allowed map[string]struct{}) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	return obj, nil
}

func coerceSemanticJSONStrings(obj map[string]json.RawMessage, numberKeys map[string]struct{}, boolKeys map[string]struct{}) {
	for key, raw := range obj {
		if len(raw) == 0 || raw[0] != '"' {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			continue
		}
		if _, ok := boolKeys[key]; ok {
			switch text {
			case "true", "false":
				obj[key] = json.RawMessage(text)
			}
			continue
		}
		if _, ok := numberKeys[key]; ok && semanticNumberLiteralRE.MatchString(text) {
			obj[key] = json.RawMessage(text)
		}
	}
}

func searchRoot(cwd string, path string) string {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	return resolvePath(cwd, path)
}

func validateSearchPath(cwd string, displayPath string, resolvedPath string, requireDirectory bool) error {
	if strings.TrimSpace(displayPath) == "" || isUNCSearchPath(displayPath, resolvedPath) {
		return nil
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			if requireDirectory {
				return fmt.Errorf("Directory does not exist: %s. %s %s.", displayPath, fileNotFoundCWDNote, cwd)
			}
			return fmt.Errorf("Path does not exist: %s. %s %s.", displayPath, fileNotFoundCWDNote, cwd)
		}
		return err
	}
	if requireDirectory && !info.IsDir() {
		return fmt.Errorf("Path is not a directory: %s", displayPath)
	}
	return nil
}

func isUNCSearchPath(rawPath string, resolvedPath string) bool {
	return strings.HasPrefix(rawPath, `\\`) ||
		strings.HasPrefix(rawPath, "//") ||
		strings.HasPrefix(resolvedPath, `\\`) ||
		strings.HasPrefix(resolvedPath, "//")
}

func collectGlobMatches(root string, displayRoot string, pattern string, limit int) ([]fileSearchMatch, bool, error) {
	var matches []fileSearchMatch
	err := walkSearchFiles(root, globWalkOptions(), func(path string, rel string, info os.FileInfo) error {
		ok, err := matchGlobPath(pattern, rel)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		matches = append(matches, fileSearchMatch{Path: path, RelPath: searchDisplayPath(displayRoot, path, rel), ModUnix: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].ModUnix != matches[j].ModUnix {
			return matches[i].ModUnix < matches[j].ModUnix
		}
		return matches[i].RelPath < matches[j].RelPath
	})
	truncated := len(matches) > limit
	if truncated {
		matches = matches[:limit]
	}
	return matches, truncated, nil
}

func collectGrepMatches(root string, displayRoot string, glob string, typeFilter string, expr *regexp.Regexp, options grepOptions) ([]grepMatch, int, bool, error) {
	typeExtensions, err := grepTypeExtensions(typeFilter)
	if err != nil {
		return nil, 0, false, err
	}
	globPatterns := splitGrepGlobPatterns(glob)
	var matches []grepMatch
	err = walkSearchFiles(root, grepWalkOptions(), func(path string, rel string, info os.FileInfo) error {
		if len(globPatterns) > 0 {
			ok, err := matchAnyGlobPath(globPatterns, rel)
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
		displayRel := searchDisplayPath(displayRoot, path, rel)
		lineMatches := grepFileMatches(displayRel, content, expr, options)
		if len(lineMatches) == 0 {
			return nil
		}
		switch options.Mode {
		case "files_with_matches":
			matches = append(matches, grepMatch{Path: displayRel, ModUnix: info.ModTime().UnixNano()})
		case "count":
			matches = append(matches, grepMatch{Path: displayRel, Count: countMatchedLines(lineMatches)})
		default:
			matches = append(matches, lineMatches...)
		}
		return nil
	})
	if err != nil {
		return nil, 0, false, err
	}
	sort.Slice(matches, func(i, j int) bool {
		if options.Mode == "files_with_matches" && matches[i].ModUnix != matches[j].ModUnix {
			return matches[i].ModUnix > matches[j].ModUnix
		}
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
	end := len(matches)
	if options.Limit > 0 {
		end = start + options.Limit
		if end > len(matches) {
			end = len(matches)
		}
	}
	truncated := end < len(matches)
	matches = matches[start:end]
	return matches, totalMatches, truncated, nil
}

func searchDisplayPath(displayRoot string, path string, fallback string) string {
	if displayRoot != "" {
		if rel, err := filepath.Rel(displayRoot, path); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			return filepath.ToSlash(rel)
		}
	}
	return fallback
}

func globBaseDirectory(pattern string) (string, string) {
	index := strings.IndexAny(pattern, "*?[{")
	if index < 0 {
		return filepath.Dir(pattern), filepath.Base(pattern)
	}
	prefix := pattern[:index]
	lastSep := strings.LastIndex(prefix, string(filepath.Separator))
	if filepath.Separator != '/' {
		if slash := strings.LastIndex(prefix, "/"); slash > lastSep {
			lastSep = slash
		}
	}
	if lastSep < 0 {
		return ".", pattern
	}
	base := prefix[:lastSep]
	if base == "" && lastSep == 0 {
		base = string(filepath.Separator)
	}
	return filepath.Clean(base), pattern[lastSep+1:]
}

func grepFileMatches(path string, content string, expr *regexp.Regexp, options grepOptions) []grepMatch {
	content = normalizeGrepContent(content)
	lines := strings.Split(content, "\n")
	matched := map[int]bool{}
	included := map[int]bool{}
	if options.Multiline {
		markMultilineMatches(lines, content, expr, options.MaxCount, options.BeforeContext, options.AfterContext, options.InvertMatch, matched, included)
	} else {
		markLineMatches(lines, expr, options.MaxCount, options.BeforeContext, options.AfterContext, options.InvertMatch, matched, included)
	}
	matches := make([]grepMatch, 0, len(included))
	for i := range lines {
		if !included[i] {
			continue
		}
		matches = append(matches, grepMatch{Path: path, Line: i + 1, Text: grepDisplayLine(lines[i], matched[i]), Matched: matched[i]})
	}
	return matches
}

func normalizeGrepContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.TrimSuffix(content, "\n")
}

func grepDisplayLine(line string, matched bool) string {
	if len(line) < grepMaxColumns {
		return line
	}
	if matched {
		return grepOmittedLongMatchingLine
	}
	return grepOmittedLongContextLine
}

func markLineMatches(lines []string, expr *regexp.Regexp, maxCount int, beforeContext int, afterContext int, invert bool, matched map[int]bool, included map[int]bool) {
	matches := 0
	for i, line := range lines {
		if expr.MatchString(line) == invert {
			continue
		}
		if maxCount > 0 && matches >= maxCount {
			break
		}
		markGrepLineRange(i, i, len(lines), beforeContext, afterContext, matched, included)
		matches++
	}
}

func markMultilineMatches(lines []string, content string, expr *regexp.Regexp, maxCount int, beforeContext int, afterContext int, invert bool, matched map[int]bool, included map[int]bool) {
	if content == "" {
		return
	}
	starts := make([]int, len(lines))
	offset := 0
	for i, line := range lines {
		starts[i] = offset
		offset += len(line)
		if i < len(lines)-1 {
			offset++
		}
	}
	matches := 0
	spanMatched := map[int]bool{}
	for _, span := range expr.FindAllStringIndex(content, -1) {
		first := -1
		last := -1
		for i, lineStart := range starts {
			lineEnd := lineStart + len(lines[i])
			lineSpanEnd := lineEnd
			if i < len(lines)-1 {
				lineSpanEnd++
			}
			if grepSpanTouchesLine(span[0], span[1], lineStart, lineEnd, lineSpanEnd) {
				if first == -1 {
					first = i
				}
				last = i
			}
		}
		if first >= 0 {
			for i := first; i <= last; i++ {
				spanMatched[i] = true
			}
			if !invert {
				if maxCount > 0 && matches >= maxCount {
					break
				}
				markGrepLineRange(first, last, len(lines), beforeContext, afterContext, matched, included)
				matches++
			}
		}
	}
	if !invert {
		return
	}
	for i := range lines {
		if spanMatched[i] {
			continue
		}
		if maxCount > 0 && matches >= maxCount {
			break
		}
		markGrepLineRange(i, i, len(lines), beforeContext, afterContext, matched, included)
		matches++
	}
}

func grepSpanTouchesLine(matchStart int, matchEnd int, lineStart int, lineEnd int, lineSpanEnd int) bool {
	if matchStart == matchEnd {
		return matchStart >= lineStart && matchStart <= lineEnd
	}
	return matchStart < lineSpanEnd && matchEnd > lineStart
}

func markGrepLineRange(first int, last int, lineCount int, beforeContext int, afterContext int, matched map[int]bool, included map[int]bool) {
	for i := first; i <= last; i++ {
		matched[i] = true
	}
	start := first - beforeContext
	if start < 0 {
		start = 0
	}
	end := last + afterContext
	if end >= lineCount {
		end = lineCount - 1
	}
	for n := start; n <= end; n++ {
		included[n] = true
	}
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

func formatGrepMatches(matches []grepMatch, options grepOptions) string {
	lines := make([]string, 0, len(matches))
	for _, match := range matches {
		switch options.Mode {
		case "files_with_matches":
			lines = append(lines, match.Path)
		case "count":
			lines = append(lines, fmt.Sprintf("%s:%d", match.Path, match.Count))
		default:
			separator := ":"
			if !match.Matched {
				separator = "-"
			}
			if options.LineNumbers {
				lines = append(lines, fmt.Sprintf("%s%s%d%s%s", match.Path, separator, match.Line, separator, match.Text))
			} else {
				lines = append(lines, fmt.Sprintf("%s%s%s", match.Path, separator, match.Text))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func formatGrepResultContent(matches []grepMatch, options grepOptions, truncated bool) string {
	content := formatGrepMatches(matches, options)
	switch options.Mode {
	case "content":
		return formatGrepContentResult(content, options, truncated)
	case "count":
		return formatGrepCountResult(content, matches, options, truncated)
	case "files_with_matches":
		return formatGrepFilesResult(content, matches, options, truncated)
	default:
		return content
	}
}

func formatGrepContentResult(content string, options grepOptions, truncated bool) string {
	limitInfo := grepPaginationInfo(options, truncated)
	if limitInfo == "" {
		return content
	}
	if content == "" {
		content = "No matches found"
	}
	return content + "\n\n[Showing results with pagination = " + limitInfo + "]"
}

func formatGrepCountResult(content string, matches []grepMatch, options grepOptions, truncated bool) string {
	if content == "" {
		content = "No matches found"
	}
	matchCount := 0
	for _, match := range matches {
		matchCount += match.Count
	}
	fileCount := len(matches)
	summary := fmt.Sprintf("Found %d total %s across %d %s.",
		matchCount,
		pluralWord(matchCount, "occurrence", "occurrences"),
		fileCount,
		pluralWord(fileCount, "file", "files"),
	)
	if limitInfo := grepPaginationInfo(options, truncated); limitInfo != "" {
		summary += " with pagination = " + limitInfo
	}
	return content + "\n\n" + summary
}

func formatGrepFilesResult(content string, matches []grepMatch, options grepOptions, truncated bool) string {
	fileCount := len(matches)
	if fileCount == 0 {
		return "No files found"
	}
	summary := fmt.Sprintf("Found %d %s", fileCount, pluralWord(fileCount, "file", "files"))
	if limitInfo := grepPaginationInfo(options, truncated); limitInfo != "" {
		summary += " " + limitInfo
	}
	return summary + "\n" + content
}

func grepPaginationInfo(options grepOptions, truncated bool) string {
	var parts []string
	if truncated {
		parts = append(parts, fmt.Sprintf("limit: %d", options.Limit))
	}
	if options.Offset > 0 {
		parts = append(parts, fmt.Sprintf("offset: %d", options.Offset))
	}
	return strings.Join(parts, ", ")
}

func pluralWord(count int, singular string, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
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
	if grepWordRegexp(input) {
		pattern = `\b(?:` + pattern + `)\b`
	}
	switch {
	case grepCaseInsensitive(input) && input.Multiline:
		pattern = "(?is:" + pattern + ")"
	case grepCaseInsensitive(input):
		pattern = "(?i:" + pattern + ")"
	case input.Multiline:
		pattern = "(?s:" + pattern + ")"
	}
	return regexp.Compile(pattern)
}

func grepCaseInsensitive(input grepInput) bool {
	return input.IgnoreCase || input.CaseInsensitive || input.CaseInsensitiveAlt || input.ShortIgnoreCase
}

func grepFixedStrings(input grepInput) bool {
	return input.FixedStrings || input.FixedStringsAlt || input.ShortFixedStrings
}

func grepWordRegexp(input grepInput) bool {
	return input.WordRegexp || input.WordRegexpAlt || input.WordRegexpDash || input.ShortWordRegexp
}

func grepInvertMatch(input grepInput) bool {
	return input.InvertMatch || input.InvertMatchAlt || input.InvertMatchDash || input.ShortInvertMatch
}

func grepLineNumbers(input grepInput, mode string) bool {
	if mode != "content" {
		return false
	}
	if input.LineNumbers != nil {
		return *input.LineNumbers
	}
	if input.LineNumbersAlt != nil {
		return *input.LineNumbersAlt
	}
	if input.ShortLineNumbers == nil {
		return true
	}
	return *input.ShortLineNumbers
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

func splitGrepGlobPatterns(glob string) []string {
	var patterns []string
	for _, raw := range strings.Fields(glob) {
		if strings.Contains(raw, "{") && strings.Contains(raw, "}") {
			patterns = append(patterns, raw)
			continue
		}
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				patterns = append(patterns, part)
			}
		}
	}
	return patterns
}

func matchAnyGlobPath(patterns []string, path string) (bool, error) {
	for _, pattern := range patterns {
		ok, err := matchGlobPath(pattern, path)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
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
	return defaultGrepHeadLimit
}

func grepOffset(input grepInput) int {
	if input.Offset == nil {
		return 0
	}
	return *input.Offset
}

func grepMaxCount(input grepInput) int {
	if input.MaxCount != nil {
		return *input.MaxCount
	}
	if input.MaxCountAlt != nil {
		return *input.MaxCountAlt
	}
	if input.ShortMaxCount != nil {
		return *input.ShortMaxCount
	}
	return 0
}

func grepContextLines(input grepInput) (int, int) {
	if input.Context != nil {
		return *input.Context, *input.Context
	}
	if input.ShortContext != nil {
		return *input.ShortContext, *input.ShortContext
	}
	before := 0
	after := 0
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

func globWalkOptions() searchWalkOptions {
	return searchWalkOptions{
		UseIgnoreFiles: !envTruthyDefault("CLAUDE_CODE_GLOB_NO_IGNORE", true),
		IncludeHidden:  envTruthyDefault("CLAUDE_CODE_GLOB_HIDDEN", true),
	}
}

func grepWalkOptions() searchWalkOptions {
	return searchWalkOptions{
		UseIgnoreFiles: true,
		IncludeHidden:  true,
		ExcludeVCSDirs: true,
	}
}

func envTruthyDefault(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func walkSearchFiles(root string, options searchWalkOptions, visit func(path string, rel string, info os.FileInfo) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return visit(root, filepath.Base(root), info)
	}
	var ignoreRules searchIgnoreRules
	if options.UseIgnoreFiles {
		ignoreRules = loadSearchIgnoreRules(root, "")
	}
	return walkSearchDir(root, root, options, ignoreRules, visit)
}

func walkSearchDir(root string, dir string, options searchWalkOptions, ignoreRules searchIgnoreRules, visit func(path string, rel string, info os.FileInfo) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !options.IncludeHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if (options.ExcludeVCSDirs && ignoredVCSDir(entry.Name())) || (options.UseIgnoreFiles && ignoreRules.Ignored(rel, true)) {
				continue
			}
			dirRules := append(searchIgnoreRules(nil), ignoreRules...)
			if options.UseIgnoreFiles {
				dirRules = append(dirRules, loadSearchIgnoreRules(path, rel)...)
			}
			if err := walkSearchDir(root, path, options, dirRules, visit); err != nil {
				return err
			}
			continue
		}
		if options.UseIgnoreFiles && ignoreRules.Ignored(rel, false) {
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

func ignoredVCSDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".bzr", ".jj", ".sl":
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
	expanded := expandGlobBraces(pattern)
	if len(expanded) > 1 {
		for _, candidate := range expanded {
			ok, err := matchGlobPath(candidate, path)
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	}
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

func expandGlobBraces(pattern string) []string {
	start := strings.Index(pattern, "{")
	if start < 0 {
		return []string{pattern}
	}
	depth := 0
	end := -1
	for i := start; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
				i = len(pattern)
			}
		}
	}
	if end < 0 {
		return []string{pattern}
	}
	body := pattern[start+1 : end]
	alternatives := splitBraceAlternatives(body)
	if len(alternatives) == 0 {
		return []string{pattern}
	}
	prefix := pattern[:start]
	suffixes := expandGlobBraces(pattern[end+1:])
	out := make([]string, 0, len(alternatives)*len(suffixes))
	for _, alternative := range alternatives {
		for _, suffix := range suffixes {
			out = append(out, prefix+alternative+suffix)
		}
	}
	return out
}

func splitBraceAlternatives(body string) []string {
	var alternatives []string
	depth := 0
	start := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				alternatives = append(alternatives, body[start:i])
				start = i + 1
			}
		}
	}
	alternatives = append(alternatives, body[start:])
	if len(alternatives) == 1 && alternatives[0] == body {
		return nil
	}
	return alternatives
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
