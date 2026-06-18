package filetools

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
	"ccgo/internal/tool"
)

const defaultSearchLimit = 100
const defaultGrepHeadLimit = 250
const defaultGrepMaxColumns = 500
const fileNotFoundCWDNote = "Note: your current working directory is"
const globTruncatedMessage = "(Results are truncated. Consider using a more specific path or pattern.)"
const grepOmittedLongMatchingLine = "[Omitted long matching line]"
const grepOmittedLongContextLine = "[Omitted long context line]"
const grepOmittedLongLinePreviewSuffix = " [... omitted end of long line]"

var semanticNumberLiteralRE = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

var allowedGrepInputKeys = map[string]struct{}{
	"pattern": {}, "regex": {}, "regexp": {}, "--regexp": {}, "-e": {}, "path": {}, "glob": {}, "--glob": {}, "-g": {}, "type": {}, "--type": {}, "-t": {}, "output_mode": {}, "outputMode": {}, "limit": {},
	"head_limit": {}, "headLimit": {}, "offset": {}, "max_count": {}, "maxCount": {}, "-m": {},
	"max_columns": {}, "maxColumns": {}, "max-columns": {}, "--max-columns": {},
	"max_columns_preview": {}, "maxColumnsPreview": {}, "max-columns-preview": {}, "--max-columns-preview": {}, "no_max_columns_preview": {}, "noMaxColumnsPreview": {}, "no-max-columns-preview": {}, "--no-max-columns-preview": {},
	"sort": {}, "--sort": {}, "sortr": {}, "--sortr": {},
	"context": {}, "-C": {}, "before_context": {}, "beforeContext": {}, "-B": {}, "after_context": {}, "afterContext": {}, "-A": {}, "line_numbers": {}, "lineNumbers": {}, "line-number": {}, "--line-number": {}, "-n": {},
	"no_line_number": {}, "noLineNumber": {}, "no_line_numbers": {}, "noLineNumbers": {}, "no-line-number": {}, "no-line-numbers": {}, "--no-line-number": {}, "-N": {},
	"column": {}, "column_numbers": {}, "columnNumbers": {}, "column-number": {}, "--column": {},
	"ignore_case": {}, "case_insensitive": {}, "caseInsensitive": {}, "ignore-case": {}, "--ignore-case": {}, "-i": {},
	"case_sensitive": {}, "caseSensitive": {}, "case-sensitive": {}, "--case-sensitive": {}, "-s": {},
	"smart_case": {}, "smartCase": {}, "smart-case": {}, "--smart-case": {}, "-S": {},
	"fixed_strings": {}, "fixedStrings": {}, "fixed-strings": {}, "--fixed-strings": {}, "-F": {}, "multiline": {}, "--multiline": {}, "multiline-dotall": {}, "--multiline-dotall": {}, "-U": {},
	"text": {}, "--text": {}, "-a": {},
	"word_regexp": {}, "wordRegexp": {}, "word-regexp": {}, "--word-regexp": {}, "-w": {},
	"line_regexp": {}, "lineRegexp": {}, "line-regexp": {}, "--line-regexp": {}, "-x": {},
	"invert_match": {}, "invertMatch": {}, "invert-match": {}, "--invert-match": {}, "-v": {},
	"only_matching": {}, "onlyMatching": {}, "only-matching": {}, "--only-matching": {}, "-o": {},
	"vimgrep": {}, "--vimgrep": {},
	"passthru": {}, "passthrough": {}, "--passthru": {}, "--passthrough": {},
	"trim": {}, "--trim": {}, "no_trim": {}, "noTrim": {}, "no-trim": {}, "--no-trim": {},
	"files_with_match": {}, "filesWithMatch": {}, "files-with-match": {}, "--files-with-match": {}, "files_with_matches": {}, "filesWithMatches": {}, "files-with-matches": {}, "--files-with-matches": {}, "-l": {},
	"files_without_match": {}, "filesWithoutMatch": {}, "files-without-match": {}, "--files-without-match": {}, "files_without_matches": {}, "filesWithoutMatches": {}, "files-without-matches": {}, "--files-without-matches": {}, "-L": {},
	"count": {}, "--count": {}, "-c": {},
	"count_matches": {}, "countMatches": {}, "count-matches": {}, "--count-matches": {},
	"no_ignore": {}, "noIgnore": {}, "no-ignore": {}, "--no-ignore": {},
}

var grepSemanticNumberKeys = map[string]struct{}{
	"limit": {}, "head_limit": {}, "headLimit": {}, "offset": {}, "max_count": {}, "maxCount": {}, "-m": {},
	"max_columns": {}, "maxColumns": {}, "max-columns": {}, "--max-columns": {},
	"context": {}, "-C": {}, "before_context": {}, "beforeContext": {}, "-B": {}, "after_context": {}, "afterContext": {}, "-A": {},
}

var grepSemanticBooleanKeys = map[string]struct{}{
	"max_columns_preview": {}, "maxColumnsPreview": {}, "max-columns-preview": {}, "--max-columns-preview": {}, "no_max_columns_preview": {}, "noMaxColumnsPreview": {}, "no-max-columns-preview": {}, "--no-max-columns-preview": {},
	"line_numbers": {}, "lineNumbers": {}, "line-number": {}, "--line-number": {}, "-n": {},
	"no_line_number": {}, "noLineNumber": {}, "no_line_numbers": {}, "noLineNumbers": {}, "no-line-number": {}, "no-line-numbers": {}, "--no-line-number": {}, "-N": {},
	"column": {}, "column_numbers": {}, "columnNumbers": {}, "column-number": {}, "--column": {},
	"ignore_case": {}, "case_insensitive": {}, "caseInsensitive": {}, "ignore-case": {}, "--ignore-case": {}, "-i": {},
	"case_sensitive": {}, "caseSensitive": {}, "case-sensitive": {}, "--case-sensitive": {}, "-s": {},
	"smart_case": {}, "smartCase": {}, "smart-case": {}, "--smart-case": {}, "-S": {},
	"fixed_strings": {}, "fixedStrings": {}, "fixed-strings": {}, "--fixed-strings": {}, "-F": {}, "multiline": {}, "--multiline": {}, "multiline-dotall": {}, "--multiline-dotall": {}, "-U": {},
	"text": {}, "--text": {}, "-a": {},
	"word_regexp": {}, "wordRegexp": {}, "word-regexp": {}, "--word-regexp": {}, "-w": {},
	"line_regexp": {}, "lineRegexp": {}, "line-regexp": {}, "--line-regexp": {}, "-x": {},
	"invert_match": {}, "invertMatch": {}, "invert-match": {}, "--invert-match": {}, "-v": {},
	"only_matching": {}, "onlyMatching": {}, "only-matching": {}, "--only-matching": {}, "-o": {},
	"vimgrep": {}, "--vimgrep": {},
	"passthru": {}, "passthrough": {}, "--passthru": {}, "--passthrough": {},
	"trim": {}, "--trim": {}, "no_trim": {}, "noTrim": {}, "no-trim": {}, "--no-trim": {},
	"files_with_match": {}, "filesWithMatch": {}, "files-with-match": {}, "--files-with-match": {}, "files_with_matches": {}, "filesWithMatches": {}, "files-with-matches": {}, "--files-with-matches": {}, "-l": {},
	"files_without_match": {}, "filesWithoutMatch": {}, "files-without-match": {}, "--files-without-match": {}, "files_without_matches": {}, "filesWithoutMatches": {}, "files-without-matches": {}, "--files-without-matches": {}, "-L": {},
	"count": {}, "--count": {}, "-c": {},
	"count_matches": {}, "countMatches": {}, "count-matches": {}, "--count-matches": {},
	"no_ignore": {}, "noIgnore": {}, "no-ignore": {}, "--no-ignore": {},
}

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type grepInput struct {
	Pattern                 string `json:"pattern"`
	Regex                   string `json:"regex,omitempty"`
	Regexp                  string `json:"regexp,omitempty"`
	LongRegexp              string `json:"--regexp,omitempty"`
	ShortRegexp             string `json:"-e,omitempty"`
	Path                    string `json:"path,omitempty"`
	Glob                    string `json:"glob,omitempty"`
	LongGlob                string `json:"--glob,omitempty"`
	ShortGlob               string `json:"-g,omitempty"`
	Type                    string `json:"type,omitempty"`
	LongType                string `json:"--type,omitempty"`
	ShortType               string `json:"-t,omitempty"`
	OutputMode              string `json:"output_mode,omitempty"`
	OutputModeAlt           string `json:"outputMode,omitempty"`
	Limit                   *int   `json:"limit,omitempty"`
	HeadLimit               *int   `json:"head_limit,omitempty"`
	HeadLimitAlt            *int   `json:"headLimit,omitempty"`
	Offset                  *int   `json:"offset,omitempty"`
	MaxCount                *int   `json:"max_count,omitempty"`
	MaxCountAlt             *int   `json:"maxCount,omitempty"`
	ShortMaxCount           *int   `json:"-m,omitempty"`
	MaxColumns              *int   `json:"max_columns,omitempty"`
	MaxColumnsAlt           *int   `json:"maxColumns,omitempty"`
	MaxColumnsDash          *int   `json:"max-columns,omitempty"`
	LongMaxColumns          *int   `json:"--max-columns,omitempty"`
	MaxColumnsPreview       bool   `json:"max_columns_preview,omitempty"`
	MaxColumnsPreviewAlt    bool   `json:"maxColumnsPreview,omitempty"`
	MaxColumnsPreviewDash   bool   `json:"max-columns-preview,omitempty"`
	LongMaxColumnsPreview   bool   `json:"--max-columns-preview,omitempty"`
	NoMaxColumnsPreview     bool   `json:"no_max_columns_preview,omitempty"`
	NoMaxColumnsPreviewAlt  bool   `json:"noMaxColumnsPreview,omitempty"`
	NoMaxColumnsPreviewDash bool   `json:"no-max-columns-preview,omitempty"`
	LongNoMaxColumnsPreview bool   `json:"--no-max-columns-preview,omitempty"`
	Sort                    string `json:"sort,omitempty"`
	LongSort                string `json:"--sort,omitempty"`
	SortReverse             string `json:"sortr,omitempty"`
	LongSortReverse         string `json:"--sortr,omitempty"`
	Context                 *int   `json:"context,omitempty"`
	ShortContext            *int   `json:"-C,omitempty"`
	BeforeContext           *int   `json:"before_context,omitempty"`
	BeforeContextAlt        *int   `json:"beforeContext,omitempty"`
	ShortBeforeContext      *int   `json:"-B,omitempty"`
	AfterContext            *int   `json:"after_context,omitempty"`
	AfterContextAlt         *int   `json:"afterContext,omitempty"`
	ShortAfterContext       *int   `json:"-A,omitempty"`
	LineNumbers             *bool  `json:"line_numbers,omitempty"`
	LineNumbersAlt          *bool  `json:"lineNumbers,omitempty"`
	LineNumbersDash         *bool  `json:"line-number,omitempty"`
	LongLineNumbers         *bool  `json:"--line-number,omitempty"`
	ShortLineNumbers        *bool  `json:"-n,omitempty"`
	NoLineNumber            bool   `json:"no_line_number,omitempty"`
	NoLineNumberAlt         bool   `json:"noLineNumber,omitempty"`
	NoLineNumbers           bool   `json:"no_line_numbers,omitempty"`
	NoLineNumbersAlt        bool   `json:"noLineNumbers,omitempty"`
	NoLineNumberDash        bool   `json:"no-line-number,omitempty"`
	NoLineNumbersDash       bool   `json:"no-line-numbers,omitempty"`
	LongNoLineNumber        bool   `json:"--no-line-number,omitempty"`
	ShortNoLineNumber       bool   `json:"-N,omitempty"`
	Column                  bool   `json:"column,omitempty"`
	ColumnNumbers           bool   `json:"column_numbers,omitempty"`
	ColumnNumbersAlt        bool   `json:"columnNumbers,omitempty"`
	ColumnNumbersDash       bool   `json:"column-number,omitempty"`
	LongColumn              bool   `json:"--column,omitempty"`
	IgnoreCase              bool   `json:"ignore_case,omitempty"`
	CaseInsensitive         bool   `json:"case_insensitive,omitempty"`
	CaseInsensitiveAlt      bool   `json:"caseInsensitive,omitempty"`
	IgnoreCaseDash          bool   `json:"ignore-case,omitempty"`
	LongIgnoreCase          bool   `json:"--ignore-case,omitempty"`
	ShortIgnoreCase         bool   `json:"-i,omitempty"`
	CaseSensitive           bool   `json:"case_sensitive,omitempty"`
	CaseSensitiveAlt        bool   `json:"caseSensitive,omitempty"`
	CaseSensitiveDash       bool   `json:"case-sensitive,omitempty"`
	LongCaseSensitive       bool   `json:"--case-sensitive,omitempty"`
	ShortCaseSensitive      bool   `json:"-s,omitempty"`
	SmartCase               bool   `json:"smart_case,omitempty"`
	SmartCaseAlt            bool   `json:"smartCase,omitempty"`
	SmartCaseDash           bool   `json:"smart-case,omitempty"`
	LongSmartCase           bool   `json:"--smart-case,omitempty"`
	ShortSmartCase          bool   `json:"-S,omitempty"`
	FixedStrings            bool   `json:"fixed_strings,omitempty"`
	FixedStringsAlt         bool   `json:"fixedStrings,omitempty"`
	FixedStringsDash        bool   `json:"fixed-strings,omitempty"`
	LongFixedStrings        bool   `json:"--fixed-strings,omitempty"`
	ShortFixedStrings       bool   `json:"-F,omitempty"`
	Text                    bool   `json:"text,omitempty"`
	LongText                bool   `json:"--text,omitempty"`
	ShortText               bool   `json:"-a,omitempty"`
	WordRegexp              bool   `json:"word_regexp,omitempty"`
	WordRegexpAlt           bool   `json:"wordRegexp,omitempty"`
	WordRegexpDash          bool   `json:"word-regexp,omitempty"`
	LongWordRegexp          bool   `json:"--word-regexp,omitempty"`
	ShortWordRegexp         bool   `json:"-w,omitempty"`
	LineRegexp              bool   `json:"line_regexp,omitempty"`
	LineRegexpAlt           bool   `json:"lineRegexp,omitempty"`
	LineRegexpDash          bool   `json:"line-regexp,omitempty"`
	LongLineRegexp          bool   `json:"--line-regexp,omitempty"`
	ShortLineRegexp         bool   `json:"-x,omitempty"`
	InvertMatch             bool   `json:"invert_match,omitempty"`
	InvertMatchAlt          bool   `json:"invertMatch,omitempty"`
	InvertMatchDash         bool   `json:"invert-match,omitempty"`
	LongInvertMatch         bool   `json:"--invert-match,omitempty"`
	ShortInvertMatch        bool   `json:"-v,omitempty"`
	OnlyMatching            bool   `json:"only_matching,omitempty"`
	OnlyMatchingAlt         bool   `json:"onlyMatching,omitempty"`
	OnlyMatchingDash        bool   `json:"only-matching,omitempty"`
	LongOnlyMatching        bool   `json:"--only-matching,omitempty"`
	ShortOnlyMatching       bool   `json:"-o,omitempty"`
	Vimgrep                 bool   `json:"vimgrep,omitempty"`
	LongVimgrep             bool   `json:"--vimgrep,omitempty"`
	Passthru                bool   `json:"passthru,omitempty"`
	Passthrough             bool   `json:"passthrough,omitempty"`
	LongPassthru            bool   `json:"--passthru,omitempty"`
	LongPassthrough         bool   `json:"--passthrough,omitempty"`
	Trim                    bool   `json:"trim,omitempty"`
	LongTrim                bool   `json:"--trim,omitempty"`
	NoTrim                  bool   `json:"no_trim,omitempty"`
	NoTrimAlt               bool   `json:"noTrim,omitempty"`
	NoTrimDash              bool   `json:"no-trim,omitempty"`
	LongNoTrim              bool   `json:"--no-trim,omitempty"`
	FilesWithMatch          bool   `json:"files_with_match,omitempty"`
	FilesWithMatchAlt       bool   `json:"filesWithMatch,omitempty"`
	FilesWithMatchDash      bool   `json:"files-with-match,omitempty"`
	LongFilesWithMatch      bool   `json:"--files-with-match,omitempty"`
	FilesWithMatches        bool   `json:"files_with_matches,omitempty"`
	FilesWithMatchesAlt     bool   `json:"filesWithMatches,omitempty"`
	FilesWithMatchesDash    bool   `json:"files-with-matches,omitempty"`
	LongFilesWithMatches    bool   `json:"--files-with-matches,omitempty"`
	ShortFilesWithMatches   bool   `json:"-l,omitempty"`
	FilesWithoutMatch       bool   `json:"files_without_match,omitempty"`
	FilesWithoutMatchAlt    bool   `json:"filesWithoutMatch,omitempty"`
	FilesWithoutMatchDash   bool   `json:"files-without-match,omitempty"`
	LongFilesWithoutMatch   bool   `json:"--files-without-match,omitempty"`
	FilesWithoutMatches     bool   `json:"files_without_matches,omitempty"`
	FilesWithoutMatchesAlt  bool   `json:"filesWithoutMatches,omitempty"`
	FilesWithoutMatchesDash bool   `json:"files-without-matches,omitempty"`
	LongFilesWithoutMatches bool   `json:"--files-without-matches,omitempty"`
	ShortFilesWithoutMatch  bool   `json:"-L,omitempty"`
	Count                   bool   `json:"count,omitempty"`
	LongCount               bool   `json:"--count,omitempty"`
	ShortCount              bool   `json:"-c,omitempty"`
	CountMatches            bool   `json:"count_matches,omitempty"`
	CountMatchesAlt         bool   `json:"countMatches,omitempty"`
	CountMatchesDash        bool   `json:"count-matches,omitempty"`
	LongCountMatches        bool   `json:"--count-matches,omitempty"`
	NoIgnore                bool   `json:"no_ignore,omitempty"`
	NoIgnoreAlt             bool   `json:"noIgnore,omitempty"`
	NoIgnoreDash            bool   `json:"no-ignore,omitempty"`
	LongNoIgnore            bool   `json:"--no-ignore,omitempty"`
	Multiline               bool   `json:"multiline,omitempty"`
	LongMultiline           bool   `json:"--multiline,omitempty"`
	MultilineDotall         bool   `json:"multiline-dotall,omitempty"`
	LongMultilineDotall     bool   `json:"--multiline-dotall,omitempty"`
	ShortMultiline          bool   `json:"-U,omitempty"`
}

type fileSearchMatch struct {
	Path    string
	RelPath string
	ModUnix int64
}

type grepMatch struct {
	Path    string
	Line    int
	Column  int
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
	MaxColumns    int
	MaxPreview    bool
	BeforeContext int
	AfterContext  int
	LineNumbers   bool
	Multiline     bool
	InvertMatch   bool
	OnlyMatching  bool
	Vimgrep       bool
	Passthru      bool
	Trim          bool
	CountMatches  bool
	ColumnNumbers bool
	Text          bool
	SortMode      string
	SortReverse   bool
	SortExplicit  bool
}

type searchWalkOptions struct {
	UseIgnoreFiles bool
	IncludeHidden  bool
	ExcludeVCSDirs bool
	ExtraIgnores   searchIgnoreRules
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
				"type": "object",
				"properties": map[string]any{
					"pattern":     map[string]any{"type": "string"},
					"regex":       map[string]any{"type": "string"},
					"regexp":      map[string]any{"type": "string"},
					"--regexp":    map[string]any{"type": "string"},
					"-e":          map[string]any{"type": "string"},
					"path":        map[string]any{"type": "string"},
					"glob":        map[string]any{"type": "string"},
					"--glob":      map[string]any{"type": "string"},
					"-g":          map[string]any{"type": "string"},
					"type":        map[string]any{"type": "string"},
					"--type":      map[string]any{"type": "string"},
					"-t":          map[string]any{"type": "string"},
					"output_mode": map[string]any{"type": "string", "enum": []any{"files_with_match", "files_with_matches", "files_without_match", "files_without_matches", "content", "count"}},
					"outputMode":  map[string]any{"type": "string", "enum": []any{"files_with_match", "files_with_matches", "files_without_match", "files_without_matches", "content", "count"}},
					"limit":       map[string]any{"type": "integer"},
					"head_limit":  map[string]any{"type": "integer"},
					"headLimit":   map[string]any{"type": "integer"},
					"offset":      map[string]any{"type": "integer"},
					"max_count":   map[string]any{"type": "integer"},
					"maxCount":    map[string]any{"type": "integer"},
					"-m":          map[string]any{"type": "integer"},
					"max_columns": map[string]any{"type": "integer"},
					"maxColumns":  map[string]any{"type": "integer"},
					"max-columns": map[string]any{"type": "integer"},
					"--max-columns": map[string]any{
						"type": "integer",
					},
					"max_columns_preview":    map[string]any{"type": "boolean"},
					"maxColumnsPreview":      map[string]any{"type": "boolean"},
					"max-columns-preview":    map[string]any{"type": "boolean"},
					"--max-columns-preview":  map[string]any{"type": "boolean"},
					"no_max_columns_preview": map[string]any{"type": "boolean"},
					"noMaxColumnsPreview":    map[string]any{"type": "boolean"},
					"no-max-columns-preview": map[string]any{"type": "boolean"},
					"--no-max-columns-preview": map[string]any{
						"type": "boolean",
					},
					"sort":   map[string]any{"type": "string", "enum": []any{"path", "name", "file", "modified", "mtime", "modtime", "time", "none"}},
					"--sort": map[string]any{"type": "string", "enum": []any{"path", "name", "file", "modified", "mtime", "modtime", "time", "none"}},
					"sortr":  map[string]any{"type": "string", "enum": []any{"path", "name", "file", "modified", "mtime", "modtime", "time", "none"}},
					"--sortr": map[string]any{
						"type": "string", "enum": []any{"path", "name", "file", "modified", "mtime", "modtime", "time", "none"},
					},
					"context": map[string]any{"type": "integer"},
					"-C":      map[string]any{"type": "integer"},
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
					"line-number":      map[string]any{"type": "boolean"},
					"--line-number":    map[string]any{"type": "boolean"},
					"-n":               map[string]any{"type": "boolean"},
					"no_line_number":   map[string]any{"type": "boolean"},
					"noLineNumber":     map[string]any{"type": "boolean"},
					"no_line_numbers":  map[string]any{"type": "boolean"},
					"noLineNumbers":    map[string]any{"type": "boolean"},
					"no-line-number":   map[string]any{"type": "boolean"},
					"no-line-numbers":  map[string]any{"type": "boolean"},
					"--no-line-number": map[string]any{"type": "boolean"},
					"-N":               map[string]any{"type": "boolean"},
					"column":           map[string]any{"type": "boolean"},
					"column_numbers":   map[string]any{"type": "boolean"},
					"columnNumbers":    map[string]any{"type": "boolean"},
					"column-number":    map[string]any{"type": "boolean"},
					"--column":         map[string]any{"type": "boolean"},
					"ignore_case":      map[string]any{"type": "boolean"},
					"case_insensitive": map[string]any{"type": "boolean"},
					"caseInsensitive":  map[string]any{"type": "boolean"},
					"ignore-case":      map[string]any{"type": "boolean"},
					"--ignore-case":    map[string]any{"type": "boolean"},
					"-i":               map[string]any{"type": "boolean"},
					"case_sensitive":   map[string]any{"type": "boolean"},
					"caseSensitive":    map[string]any{"type": "boolean"},
					"case-sensitive":   map[string]any{"type": "boolean"},
					"--case-sensitive": map[string]any{"type": "boolean"},
					"-s":               map[string]any{"type": "boolean"},
					"smart_case":       map[string]any{"type": "boolean"},
					"smartCase":        map[string]any{"type": "boolean"},
					"smart-case":       map[string]any{"type": "boolean"},
					"--smart-case":     map[string]any{"type": "boolean"},
					"-S":               map[string]any{"type": "boolean"},
					"fixed_strings":    map[string]any{"type": "boolean"},
					"fixedStrings":     map[string]any{"type": "boolean"},
					"fixed-strings":    map[string]any{"type": "boolean"},
					"--fixed-strings":  map[string]any{"type": "boolean"},
					"-F":               map[string]any{"type": "boolean"},
					"text":             map[string]any{"type": "boolean"},
					"--text":           map[string]any{"type": "boolean"},
					"-a":               map[string]any{"type": "boolean"},
					"word_regexp":      map[string]any{"type": "boolean"},
					"wordRegexp":       map[string]any{"type": "boolean"},
					"word-regexp":      map[string]any{"type": "boolean"},
					"--word-regexp":    map[string]any{"type": "boolean"},
					"-w":               map[string]any{"type": "boolean"},
					"line_regexp":      map[string]any{"type": "boolean"},
					"lineRegexp":       map[string]any{"type": "boolean"},
					"line-regexp":      map[string]any{"type": "boolean"},
					"--line-regexp":    map[string]any{"type": "boolean"},
					"-x":               map[string]any{"type": "boolean"},
					"invert_match":     map[string]any{"type": "boolean"},
					"invertMatch":      map[string]any{"type": "boolean"},
					"invert-match":     map[string]any{"type": "boolean"},
					"--invert-match":   map[string]any{"type": "boolean"},
					"-v":               map[string]any{"type": "boolean"},
					"only_matching":    map[string]any{"type": "boolean"},
					"onlyMatching":     map[string]any{"type": "boolean"},
					"only-matching":    map[string]any{"type": "boolean"},
					"--only-matching":  map[string]any{"type": "boolean"},
					"-o":               map[string]any{"type": "boolean"},
					"vimgrep":          map[string]any{"type": "boolean"},
					"--vimgrep":        map[string]any{"type": "boolean"},
					"passthru":         map[string]any{"type": "boolean"},
					"passthrough":      map[string]any{"type": "boolean"},
					"--passthru":       map[string]any{"type": "boolean"},
					"--passthrough":    map[string]any{"type": "boolean"},
					"trim":             map[string]any{"type": "boolean"},
					"--trim":           map[string]any{"type": "boolean"},
					"no_trim":          map[string]any{"type": "boolean"},
					"noTrim":           map[string]any{"type": "boolean"},
					"no-trim":          map[string]any{"type": "boolean"},
					"--no-trim":        map[string]any{"type": "boolean"},
					"files_with_match": map[string]any{
						"type": "boolean",
					},
					"filesWithMatch": map[string]any{"type": "boolean"},
					"files-with-match": map[string]any{
						"type": "boolean",
					},
					"--files-with-match": map[string]any{
						"type": "boolean",
					},
					"files_with_matches": map[string]any{
						"type": "boolean",
					},
					"filesWithMatches": map[string]any{"type": "boolean"},
					"files-with-matches": map[string]any{
						"type": "boolean",
					},
					"--files-with-matches": map[string]any{
						"type": "boolean",
					},
					"-l": map[string]any{"type": "boolean"},
					"files_without_match": map[string]any{
						"type": "boolean",
					},
					"filesWithoutMatch": map[string]any{"type": "boolean"},
					"files-without-match": map[string]any{
						"type": "boolean",
					},
					"--files-without-match": map[string]any{
						"type": "boolean",
					},
					"files_without_matches": map[string]any{
						"type": "boolean",
					},
					"filesWithoutMatches": map[string]any{"type": "boolean"},
					"files-without-matches": map[string]any{
						"type": "boolean",
					},
					"--files-without-matches": map[string]any{
						"type": "boolean",
					},
					"-L":              map[string]any{"type": "boolean"},
					"count":           map[string]any{"type": "boolean"},
					"--count":         map[string]any{"type": "boolean"},
					"-c":              map[string]any{"type": "boolean"},
					"count_matches":   map[string]any{"type": "boolean"},
					"countMatches":    map[string]any{"type": "boolean"},
					"count-matches":   map[string]any{"type": "boolean"},
					"--count-matches": map[string]any{"type": "boolean"},
					"no_ignore":       map[string]any{"type": "boolean"},
					"noIgnore":        map[string]any{"type": "boolean"},
					"no-ignore":       map[string]any{"type": "boolean"},
					"--no-ignore":     map[string]any{"type": "boolean"},
					"multiline":       map[string]any{"type": "boolean"},
					"--multiline":     map[string]any{"type": "boolean"},
					"multiline-dotall": map[string]any{
						"type": "boolean",
					},
					"--multiline-dotall": map[string]any{
						"type": "boolean",
					},
					"-U": map[string]any{"type": "boolean"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Searches text files under path using a regular expression or fixed string. pattern is the canonical search expression; regex/regexp/--regexp/-e are accepted aliases. output_mode may be files_with_matches, files_without_matches, content, or count; glob/-g/--glob and type/-t/--type optionally filter file paths. glob accepts whitespace/comma-separated patterns and brace alternation. content mode supports context, before_context, after_context, -C, -B, -A, -n/--line-number and -N/--no-line-number line-number control, --column column-number output, offset, head_limit pagination, max_count/-m per-file match limiting, max_columns/--max-columns long-line omission, --max-columns-preview long-line previews, only_matching/-o/--only-matching matched-text output, vimgrep/--vimgrep per-match line output, passthru/--passthru/--passthrough all-line output, and trim/--trim leading-whitespace trimming. Use files_with_matches or -l to list files with matches, files_without_match or -L to list files without matches, and count/--count/-c for count mode. Count mode supports count_matches/--count-matches for occurrence counts. Use sort/--sort or sortr/--sortr with path or modified to control result ordering. Use fixed_strings/-F/--fixed-strings for literal matching, text/-a/--text to search binary-extension files as text, word_regexp/-w/--word-regexp for whole-word matches, line_regexp/-x/--line-regexp for whole-line matches, ignore_case/-i/--ignore-case for case-insensitive search, case_sensitive/-s/--case-sensitive to force case-sensitive matching, smart_case/-S/--smart-case for lowercase-only patterns, and invert_match/-v/--invert-match to select non-matching lines. Set no_ignore/--no-ignore to skip .gitignore/.ignore files while still excluding VCS metadata and read-denied paths. Set multiline to allow patterns to span lines with dot matching newlines.", nil
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
	matches, truncated, err := collectGlobMatches(root, displayRoot, pattern, defaultSearchLimit, globWalkOptions(ctx, root))
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
	if strings.TrimSpace(grepPattern(input)) == "" {
		return fmt.Errorf("pattern is required")
	}
	if _, err := compileGrepPattern(input); err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	mode := normalizedGrepOutputMode(input)
	switch mode {
	case "files_with_matches", "files_without_matches", "content", "count":
	default:
		return fmt.Errorf("output_mode must be one of files_with_matches, files_without_matches, content, or count")
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
	if input.MaxColumns != nil && *input.MaxColumns < 0 {
		return fmt.Errorf("max_columns must be non-negative")
	}
	if input.MaxColumnsAlt != nil && *input.MaxColumnsAlt < 0 {
		return fmt.Errorf("max_columns must be non-negative")
	}
	if input.MaxColumnsDash != nil && *input.MaxColumnsDash < 0 {
		return fmt.Errorf("max_columns must be non-negative")
	}
	if input.LongMaxColumns != nil && *input.LongMaxColumns < 0 {
		return fmt.Errorf("max_columns must be non-negative")
	}
	if _, _, _, err := grepSort(input); err != nil {
		return err
	}
	if _, err := grepTypeExtensions(grepTypeFilter(input)); err != nil {
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
	invertMatch := grepInvertMatch(input)
	onlyMatching := grepOnlyMatching(input) && mode == "content" && !invertMatch
	if onlyMatching {
		before = 0
		after = 0
	}
	vimgrep := grepVimgrep(input) && mode == "content"
	passthru := grepPassthru(input) && mode == "content" && !onlyMatching
	if passthru {
		before = 0
		after = 0
	}
	trim := grepTrim(input) && mode == "content"
	countMatches := grepCountMatches(input) && mode == "count" && !invertMatch
	sortMode, sortReverse, sortExplicit, err := grepSort(input)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	options := grepOptions{
		Mode:          mode,
		Limit:         grepLimit(input),
		Offset:        grepOffset(input),
		MaxCount:      grepMaxCount(input),
		MaxColumns:    grepMaxColumns(input),
		MaxPreview:    grepMaxColumnsPreview(input),
		BeforeContext: before,
		AfterContext:  after,
		LineNumbers:   grepLineNumbers(input, mode),
		Multiline:     grepMultiline(input),
		InvertMatch:   invertMatch,
		OnlyMatching:  onlyMatching,
		Vimgrep:       vimgrep,
		Passthru:      passthru,
		Trim:          trim,
		CountMatches:  countMatches,
		ColumnNumbers: grepColumnNumbers(input),
		Text:          grepText(input),
		SortMode:      sortMode,
		SortReverse:   sortReverse,
		SortExplicit:  sortExplicit,
	}
	noIgnore := grepNoIgnore(input)
	globFilter := grepGlobFilter(input)
	typeFilter := grepTypeFilter(input)
	matches, totalMatches, truncated, err := collectGrepMatches(root, displayRoot, globFilter, typeFilter, expr, options, grepWalkOptions(ctx, root, noIgnore))
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
			"type":                "grep",
			"pattern":             grepPattern(input),
			"path":                input.Path,
			"glob":                globFilter,
			"type_filter":         typeFilter,
			"output_mode":         mode,
			"matches":             grepStructuredMatches(matches, mode),
			"total_matches":       totalMatches,
			"offset":              options.Offset,
			"limit":               options.Limit,
			"max_count":           options.MaxCount,
			"max_columns":         options.MaxColumns,
			"max_columns_preview": options.MaxPreview,
			"before_context":      options.BeforeContext,
			"after_context":       options.AfterContext,
			"line_numbers":        options.LineNumbers,
			"column_numbers":      options.ColumnNumbers,
			"case_insensitive":    grepEffectiveCaseInsensitive(input),
			"case_sensitive":      grepCaseSensitive(input),
			"smart_case":          grepSmartCase(input),
			"fixed_strings":       grepFixedStrings(input),
			"text":                options.Text,
			"word_regexp":         grepWordRegexp(input),
			"line_regexp":         grepLineRegexp(input),
			"invert_match":        invertMatch,
			"only_matching":       onlyMatching,
			"vimgrep":             vimgrep,
			"passthru":            passthru,
			"trim":                trim,
			"files_with_matches":  mode == "files_with_matches",
			"files_without_match": mode == "files_without_matches",
			"count":               mode == "count",
			"count_matches":       countMatches,
			"no_ignore":           noIgnore,
			"multiline":           grepMultiline(input),
			"sort":                grepStructuredSortMode(options),
			"sort_reverse":        grepStructuredSortReverse(options),
			"sort_explicit":       sortExplicit,
			"truncated":           truncated,
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
			obj[key] = semanticJSONNumberRaw(text)
		}
	}
}

func semanticJSONNumberRaw(text string) json.RawMessage {
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return json.RawMessage(text)
	}
	if math.Trunc(number) == number {
		return json.RawMessage(strconv.FormatInt(int64(number), 10))
	}
	return json.RawMessage(strconv.FormatFloat(number, 'f', -1, 64))
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

func collectGlobMatches(root string, displayRoot string, pattern string, limit int, walkOptions searchWalkOptions) ([]fileSearchMatch, bool, error) {
	var matches []fileSearchMatch
	err := walkSearchFiles(root, walkOptions, func(path string, rel string, info os.FileInfo) error {
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

func collectGrepMatches(root string, displayRoot string, glob string, typeFilter string, expr *regexp.Regexp, options grepOptions, walkOptions searchWalkOptions) ([]grepMatch, int, bool, error) {
	typeExtensions, err := grepTypeExtensions(typeFilter)
	if err != nil {
		return nil, 0, false, err
	}
	globPatterns := splitGrepGlobPatterns(glob)
	var matches []grepMatch
	err = walkSearchFiles(root, walkOptions, func(path string, rel string, info os.FileInfo) error {
		if len(globPatterns) > 0 {
			ok, err := matchAnyGlobPath(globPatterns, rel)
			if err != nil || !ok {
				return err
			}
		}
		if len(typeExtensions) > 0 && !grepTypeMatches(path, typeExtensions) {
			return nil
		}
		if hasBinaryExtension(path) && !options.Text {
			return nil
		}
		content, err := readGrepText(path, options.Text)
		if err != nil {
			return nil
		}
		displayRel := searchDisplayPath(displayRoot, path, rel)
		matchOptions := options
		if options.CountMatches {
			matchOptions.OnlyMatching = true
			matchOptions.BeforeContext = 0
			matchOptions.AfterContext = 0
		}
		lineMatches := grepFileMatches(displayRel, content, expr, matchOptions)
		for i := range lineMatches {
			lineMatches[i].ModUnix = info.ModTime().UnixNano()
		}
		if options.Mode == "files_without_matches" {
			if len(lineMatches) == 0 {
				matches = append(matches, grepMatch{Path: displayRel, ModUnix: info.ModTime().UnixNano()})
			}
			return nil
		}
		if len(lineMatches) == 0 {
			return nil
		}
		switch options.Mode {
		case "files_with_matches":
			matches = append(matches, grepMatch{Path: displayRel, ModUnix: info.ModTime().UnixNano()})
		case "count":
			count := countMatchedLines(lineMatches)
			if options.CountMatches {
				count = len(lineMatches)
			}
			matches = append(matches, grepMatch{Path: displayRel, Count: count, ModUnix: info.ModTime().UnixNano()})
		default:
			matches = append(matches, lineMatches...)
		}
		return nil
	})
	if err != nil {
		return nil, 0, false, err
	}
	sortGrepMatches(matches, options)
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

func readGrepText(path string, allowBinary bool) (string, error) {
	if allowBinary {
		return readTextAllowBinary(path)
	}
	return readText(path)
}

func grepFileMatches(path string, content string, expr *regexp.Regexp, options grepOptions) []grepMatch {
	content = normalizeGrepContent(content)
	lines := strings.Split(content, "\n")
	if options.OnlyMatching {
		if options.Multiline {
			return grepMultilineOnlyMatches(path, lines, content, expr, options.MaxCount, options.MaxColumns, options.MaxPreview, options.Trim)
		}
		return grepLineOnlyMatches(path, lines, expr, options.MaxCount, options.MaxColumns, options.MaxPreview, options.Trim)
	}
	matched := map[int]bool{}
	included := map[int]bool{}
	if options.Multiline {
		markMultilineMatches(lines, content, expr, options.MaxCount, options.BeforeContext, options.AfterContext, options.InvertMatch, matched, included)
	} else {
		markLineMatches(lines, expr, options.MaxCount, options.BeforeContext, options.AfterContext, options.InvertMatch, matched, included)
	}
	if options.Passthru {
		for i := range lines {
			included[i] = true
		}
	}
	if options.Vimgrep && !options.InvertMatch {
		return grepVimgrepMatches(path, lines, expr, matched, included, options)
	}
	matches := make([]grepMatch, 0, len(included))
	for i := range lines {
		if !included[i] {
			continue
		}
		column := 0
		if matched[i] && options.ColumnNumbers && !options.InvertMatch {
			if span := expr.FindStringIndex(lines[i]); len(span) == 2 {
				column = span[0] + 1
			}
		}
		matches = append(matches, grepMatch{Path: path, Line: i + 1, Column: column, Text: grepDisplayLine(lines[i], matched[i], options.MaxColumns, options.MaxPreview, options.Trim), Matched: matched[i]})
	}
	return matches
}

func grepVimgrepMatches(path string, lines []string, expr *regexp.Regexp, matched map[int]bool, included map[int]bool, options grepOptions) []grepMatch {
	matches := make([]grepMatch, 0, len(included))
	for i := range lines {
		if !included[i] {
			continue
		}
		text := grepDisplayLine(lines[i], matched[i], options.MaxColumns, options.MaxPreview, options.Trim)
		if !matched[i] {
			matches = append(matches, grepMatch{Path: path, Line: i + 1, Text: text, Matched: false})
			continue
		}
		spans := expr.FindAllStringIndex(lines[i], -1)
		if len(spans) == 0 {
			matches = append(matches, grepMatch{Path: path, Line: i + 1, Column: 1, Text: text, Matched: true})
			continue
		}
		for _, span := range spans {
			matches = append(matches, grepMatch{Path: path, Line: i + 1, Column: span[0] + 1, Text: text, Matched: true})
		}
	}
	return matches
}

func grepLineOnlyMatches(path string, lines []string, expr *regexp.Regexp, maxCount int, maxColumns int, maxPreview bool, trim bool) []grepMatch {
	var matches []grepMatch
	matchedLines := 0
	for i, line := range lines {
		spans := expr.FindAllStringIndex(line, -1)
		if len(spans) == 0 {
			continue
		}
		if maxCount > 0 && matchedLines >= maxCount {
			break
		}
		for _, span := range spans {
			matches = append(matches, grepMatch{
				Path:    path,
				Line:    i + 1,
				Column:  span[0] + 1,
				Text:    grepDisplayLine(line[span[0]:span[1]], true, maxColumns, maxPreview, trim),
				Matched: true,
			})
		}
		matchedLines++
	}
	return matches
}

func grepMultilineOnlyMatches(path string, lines []string, content string, expr *regexp.Regexp, maxCount int, maxColumns int, maxPreview bool, trim bool) []grepMatch {
	if content == "" {
		return nil
	}
	starts := grepLineStarts(lines)
	var matches []grepMatch
	for matchIndex, span := range expr.FindAllStringIndex(content, -1) {
		if maxCount > 0 && matchIndex >= maxCount {
			break
		}
		for i, lineStart := range starts {
			lineEnd := lineStart + len(lines[i])
			lineSpanEnd := lineEnd
			if i < len(lines)-1 {
				lineSpanEnd++
			}
			if !grepSpanTouchesLine(span[0], span[1], lineStart, lineEnd, lineSpanEnd) {
				continue
			}
			fragmentStart := span[0]
			if fragmentStart < lineStart {
				fragmentStart = lineStart
			}
			fragmentEnd := span[1]
			if fragmentEnd > lineEnd {
				fragmentEnd = lineEnd
			}
			if fragmentStart > fragmentEnd {
				fragmentStart = fragmentEnd
			}
			matches = append(matches, grepMatch{
				Path:    path,
				Line:    i + 1,
				Column:  fragmentStart - lineStart + 1,
				Text:    grepDisplayLine(lines[i][fragmentStart-lineStart:fragmentEnd-lineStart], true, maxColumns, maxPreview, trim),
				Matched: true,
			})
		}
	}
	return matches
}

func normalizeGrepContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.TrimSuffix(content, "\n")
}

func grepDisplayLine(line string, matched bool, maxColumns int, maxPreview bool, trim bool) string {
	if maxColumns <= 0 || len(line) < maxColumns {
		return grepTrimDisplayLine(line, trim)
	}
	if maxPreview {
		display := grepTrimDisplayLine(line, trim)
		if len(display) > maxColumns {
			display = display[:maxColumns]
		}
		return display + grepOmittedLongLinePreviewSuffix
	}
	if matched {
		return grepOmittedLongMatchingLine
	}
	return grepOmittedLongContextLine
}

func grepTrimDisplayLine(line string, trim bool) string {
	if !trim {
		return line
	}
	return strings.TrimLeft(line, " \t\n\v\f\r")
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
	starts := grepLineStarts(lines)
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

func grepLineStarts(lines []string) []int {
	starts := make([]int, len(lines))
	offset := 0
	for i, line := range lines {
		starts[i] = offset
		offset += len(line)
		if i < len(lines)-1 {
			offset++
		}
	}
	return starts
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
		case "files_with_matches", "files_without_matches":
			lines = append(lines, match.Path)
		case "count":
			lines = append(lines, fmt.Sprintf("%s:%d", match.Path, match.Count))
		default:
			separator := ":"
			if !match.Matched {
				separator = "-"
			}
			if options.LineNumbers {
				if (options.ColumnNumbers || options.Vimgrep) && match.Matched && match.Column > 0 {
					lines = append(lines, fmt.Sprintf("%s%s%d%s%d%s%s", match.Path, separator, match.Line, separator, match.Column, separator, match.Text))
					continue
				}
				lines = append(lines, fmt.Sprintf("%s%s%d%s%s", match.Path, separator, match.Line, separator, match.Text))
			} else {
				if options.Vimgrep && match.Matched && match.Column > 0 {
					lines = append(lines, fmt.Sprintf("%s%s%d%s%s", match.Path, separator, match.Column, separator, match.Text))
					continue
				}
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
	case "files_with_matches", "files_without_matches":
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

func grepModeIsFileList(mode string) bool {
	return mode == "files_with_matches" || mode == "files_without_matches"
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
			if match.Column > 0 {
				item["column"] = match.Column
			}
		case "count":
			item["count"] = match.Count
		}
		out = append(out, item)
	}
	return out
}

func normalizedGrepOutputMode(input grepInput) string {
	if grepFilesWithoutMatch(input) {
		return "files_without_matches"
	}
	if grepFilesWithMatches(input) {
		return "files_with_matches"
	}
	if grepCount(input) {
		return "count"
	}
	mode := strings.TrimSpace(input.OutputMode)
	if mode == "" {
		mode = strings.TrimSpace(input.OutputModeAlt)
	}
	if mode == "" {
		return "files_with_matches"
	}
	if mode == "files_with_match" {
		return "files_with_matches"
	}
	if mode == "files_without_match" {
		return "files_without_matches"
	}
	return mode
}

func grepSort(input grepInput) (string, bool, bool, error) {
	if raw := firstNonEmpty(input.SortReverse, input.LongSortReverse); raw != "" {
		mode, err := normalizeGrepSortMode(raw)
		return mode, true, true, err
	}
	if raw := firstNonEmpty(input.Sort, input.LongSort); raw != "" {
		mode, err := normalizeGrepSortMode(raw)
		return mode, false, true, err
	}
	return "", false, false, nil
}

func normalizeGrepSortMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "":
		return "", nil
	case "path", "name", "file":
		return "path", nil
	case "modified", "mtime", "modtime", "time":
		return "modified", nil
	case "none":
		return "none", nil
	default:
		return "", fmt.Errorf("sort must be one of path, modified, or none")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sortGrepMatches(matches []grepMatch, options grepOptions) {
	if options.SortExplicit && options.SortMode == "none" {
		return
	}
	sort.Slice(matches, func(i, j int) bool {
		switch {
		case options.SortExplicit && options.SortMode == "modified":
			return grepModifiedLess(matches[i], matches[j], options.SortReverse)
		case options.SortExplicit && options.SortMode == "path":
			return grepPathLess(matches[i], matches[j], options.SortReverse)
		case grepModeIsFileList(options.Mode) && matches[i].ModUnix != matches[j].ModUnix:
			return matches[i].ModUnix > matches[j].ModUnix
		default:
			return grepPathLess(matches[i], matches[j], false)
		}
	})
}

func grepModifiedLess(left grepMatch, right grepMatch, reverse bool) bool {
	if left.ModUnix != right.ModUnix {
		if reverse {
			return left.ModUnix > right.ModUnix
		}
		return left.ModUnix < right.ModUnix
	}
	return grepPathLess(left, right, false)
}

func grepPathLess(left grepMatch, right grepMatch, reverse bool) bool {
	if left.Path != right.Path {
		if reverse {
			return left.Path > right.Path
		}
		return left.Path < right.Path
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	return left.Column < right.Column
}

func grepStructuredSortMode(options grepOptions) string {
	if options.SortExplicit {
		return options.SortMode
	}
	if grepModeIsFileList(options.Mode) {
		return "modified"
	}
	return "path"
}

func grepStructuredSortReverse(options grepOptions) bool {
	if options.SortExplicit {
		return options.SortReverse
	}
	return grepModeIsFileList(options.Mode)
}

func grepPattern(input grepInput) string {
	for _, pattern := range []string{input.Pattern, input.Regex, input.Regexp, input.LongRegexp, input.ShortRegexp} {
		if strings.TrimSpace(pattern) != "" {
			return pattern
		}
	}
	return ""
}

func compileGrepPattern(input grepInput) (*regexp.Regexp, error) {
	pattern := grepPattern(input)
	if grepFixedStrings(input) {
		pattern = regexp.QuoteMeta(pattern)
	}
	lineRegexp := grepLineRegexp(input)
	if lineRegexp {
		pattern = `^(?:` + pattern + `)$`
	} else if grepWordRegexp(input) {
		pattern = `\b(?:` + pattern + `)\b`
	}
	flags := ""
	if grepEffectiveCaseInsensitive(input) {
		flags += "i"
	}
	if grepMultiline(input) {
		flags += "s"
		if lineRegexp {
			flags += "m"
		}
	}
	if flags != "" {
		pattern = "(?" + flags + ":" + pattern + ")"
	}
	return regexp.Compile(pattern)
}

func grepGlobFilter(input grepInput) string {
	if strings.TrimSpace(input.Glob) != "" {
		return input.Glob
	}
	if strings.TrimSpace(input.LongGlob) != "" {
		return input.LongGlob
	}
	return input.ShortGlob
}

func grepTypeFilter(input grepInput) string {
	if strings.TrimSpace(input.Type) != "" {
		return input.Type
	}
	if strings.TrimSpace(input.LongType) != "" {
		return input.LongType
	}
	return input.ShortType
}

func grepCaseInsensitive(input grepInput) bool {
	return input.IgnoreCase ||
		input.CaseInsensitive ||
		input.CaseInsensitiveAlt ||
		input.IgnoreCaseDash ||
		input.LongIgnoreCase ||
		input.ShortIgnoreCase
}

func grepCaseSensitive(input grepInput) bool {
	return input.CaseSensitive ||
		input.CaseSensitiveAlt ||
		input.CaseSensitiveDash ||
		input.LongCaseSensitive ||
		input.ShortCaseSensitive
}

func grepSmartCase(input grepInput) bool {
	return input.SmartCase ||
		input.SmartCaseAlt ||
		input.SmartCaseDash ||
		input.LongSmartCase ||
		input.ShortSmartCase
}

func grepEffectiveCaseInsensitive(input grepInput) bool {
	if grepCaseSensitive(input) {
		return false
	}
	if grepCaseInsensitive(input) {
		return true
	}
	return grepSmartCase(input) && !grepPatternHasUpper(grepPattern(input))
}

func grepPatternHasUpper(pattern string) bool {
	for _, r := range pattern {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func grepFixedStrings(input grepInput) bool {
	return input.FixedStrings ||
		input.FixedStringsAlt ||
		input.FixedStringsDash ||
		input.LongFixedStrings ||
		input.ShortFixedStrings
}

func grepText(input grepInput) bool {
	return input.Text || input.LongText || input.ShortText
}

func grepWordRegexp(input grepInput) bool {
	return input.WordRegexp ||
		input.WordRegexpAlt ||
		input.WordRegexpDash ||
		input.LongWordRegexp ||
		input.ShortWordRegexp
}

func grepLineRegexp(input grepInput) bool {
	return input.LineRegexp ||
		input.LineRegexpAlt ||
		input.LineRegexpDash ||
		input.LongLineRegexp ||
		input.ShortLineRegexp
}

func grepInvertMatch(input grepInput) bool {
	return input.InvertMatch ||
		input.InvertMatchAlt ||
		input.InvertMatchDash ||
		input.LongInvertMatch ||
		input.ShortInvertMatch
}

func grepOnlyMatching(input grepInput) bool {
	return input.OnlyMatching ||
		input.OnlyMatchingAlt ||
		input.OnlyMatchingDash ||
		input.LongOnlyMatching ||
		input.ShortOnlyMatching
}

func grepVimgrep(input grepInput) bool {
	return input.Vimgrep || input.LongVimgrep
}

func grepPassthru(input grepInput) bool {
	return input.Passthru ||
		input.Passthrough ||
		input.LongPassthru ||
		input.LongPassthrough
}

func grepTrim(input grepInput) bool {
	if input.NoTrim || input.NoTrimAlt || input.NoTrimDash || input.LongNoTrim {
		return false
	}
	return input.Trim || input.LongTrim
}

func grepMultiline(input grepInput) bool {
	return input.Multiline ||
		input.LongMultiline ||
		input.MultilineDotall ||
		input.LongMultilineDotall ||
		input.ShortMultiline
}

func grepFilesWithMatches(input grepInput) bool {
	return input.FilesWithMatch ||
		input.FilesWithMatchAlt ||
		input.FilesWithMatchDash ||
		input.LongFilesWithMatch ||
		input.FilesWithMatches ||
		input.FilesWithMatchesAlt ||
		input.FilesWithMatchesDash ||
		input.LongFilesWithMatches ||
		input.ShortFilesWithMatches
}

func grepFilesWithoutMatch(input grepInput) bool {
	return input.FilesWithoutMatch ||
		input.FilesWithoutMatchAlt ||
		input.FilesWithoutMatchDash ||
		input.LongFilesWithoutMatch ||
		input.FilesWithoutMatches ||
		input.FilesWithoutMatchesAlt ||
		input.FilesWithoutMatchesDash ||
		input.LongFilesWithoutMatches ||
		input.ShortFilesWithoutMatch
}

func grepCount(input grepInput) bool {
	return input.Count || input.LongCount || input.ShortCount
}

func grepCountMatches(input grepInput) bool {
	return input.CountMatches || input.CountMatchesAlt || input.CountMatchesDash || input.LongCountMatches
}

func grepNoIgnore(input grepInput) bool {
	return input.NoIgnore || input.NoIgnoreAlt || input.NoIgnoreDash || input.LongNoIgnore
}

func grepColumnNumbers(input grepInput) bool {
	return input.Column ||
		input.ColumnNumbers ||
		input.ColumnNumbersAlt ||
		input.ColumnNumbersDash ||
		input.LongColumn
}

func grepNoLineNumbers(input grepInput) bool {
	return input.NoLineNumber ||
		input.NoLineNumberAlt ||
		input.NoLineNumbers ||
		input.NoLineNumbersAlt ||
		input.NoLineNumberDash ||
		input.NoLineNumbersDash ||
		input.LongNoLineNumber ||
		input.ShortNoLineNumber
}

func grepLineNumbers(input grepInput, mode string) bool {
	if mode != "content" {
		return false
	}
	if grepNoLineNumbers(input) {
		return false
	}
	if input.LineNumbers != nil {
		return *input.LineNumbers
	}
	if input.LineNumbersAlt != nil {
		return *input.LineNumbersAlt
	}
	if input.LineNumbersDash != nil {
		return *input.LineNumbersDash
	}
	if input.LongLineNumbers != nil {
		return *input.LongLineNumbers
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
		for _, part := range splitGrepGlobToken(raw) {
			if part = strings.TrimSpace(part); part != "" {
				patterns = append(patterns, part)
			}
		}
	}
	return patterns
}

func splitGrepGlobToken(raw string) []string {
	var parts []string
	start := 0
	depth := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, raw[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, raw[start:])
	return parts
}

func matchAnyGlobPath(patterns []string, path string) (bool, error) {
	hasPositive := false
	matchedPositive := false
	for _, pattern := range patterns {
		negated := strings.HasPrefix(pattern, "!")
		if negated {
			pattern = strings.TrimSpace(strings.TrimPrefix(pattern, "!"))
			if pattern == "" {
				continue
			}
		} else {
			hasPositive = true
		}
		ok, err := matchGlobPath(pattern, path)
		if err != nil {
			return false, err
		}
		if negated && ok {
			return false, nil
		}
		if ok {
			matchedPositive = true
		}
	}
	if hasPositive {
		return matchedPositive, nil
	}
	return true, nil
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

func grepMaxColumns(input grepInput) int {
	if input.MaxColumns != nil {
		return *input.MaxColumns
	}
	if input.MaxColumnsAlt != nil {
		return *input.MaxColumnsAlt
	}
	if input.MaxColumnsDash != nil {
		return *input.MaxColumnsDash
	}
	if input.LongMaxColumns != nil {
		return *input.LongMaxColumns
	}
	return defaultGrepMaxColumns
}

func grepMaxColumnsPreview(input grepInput) bool {
	if input.NoMaxColumnsPreview ||
		input.NoMaxColumnsPreviewAlt ||
		input.NoMaxColumnsPreviewDash ||
		input.LongNoMaxColumnsPreview {
		return false
	}
	return input.MaxColumnsPreview ||
		input.MaxColumnsPreviewAlt ||
		input.MaxColumnsPreviewDash ||
		input.LongMaxColumnsPreview
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

func globWalkOptions(ctx tool.Context, root string) searchWalkOptions {
	return searchWalkOptions{
		UseIgnoreFiles: !envTruthyDefault("CLAUDE_CODE_GLOB_NO_IGNORE", true),
		IncludeHidden:  envTruthyDefault("CLAUDE_CODE_GLOB_HIDDEN", true),
		ExtraIgnores:   readDenySearchIgnoreRules(ctx, root),
	}
}

func grepWalkOptions(ctx tool.Context, root string, noIgnore bool) searchWalkOptions {
	return searchWalkOptions{
		UseIgnoreFiles: !noIgnore,
		IncludeHidden:  true,
		ExcludeVCSDirs: true,
		ExtraIgnores:   readDenySearchIgnoreRules(ctx, root),
	}
}

func readDenySearchIgnoreRules(ctx tool.Context, root string) searchIgnoreRules {
	engine, ok := permissionEngineFromToolContext(ctx)
	if !ok {
		return nil
	}
	var rules searchIgnoreRules
	for _, rule := range engine.Rules() {
		if rule.Behavior != contracts.PermissionDeny || rule.ToolName != "Read" {
			continue
		}
		rules = append(rules, searchIgnoreRulesForReadDeny(rule.Pattern, ctx.WorkingDirectory, root)...)
	}
	return rules
}

func permissionEngineFromToolContext(ctx tool.Context) (permissions.Engine, bool) {
	switch decider := ctx.Permissions.(type) {
	case tool.EnginePermissionDecider:
		return decider.Engine, true
	case *tool.EnginePermissionDecider:
		if decider != nil {
			return decider.Engine, true
		}
	}
	return permissions.Engine{}, false
}

func searchIgnoreRulesForReadDeny(pattern string, cwd string, root string) searchIgnoreRules {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return searchIgnoreRules{{Pattern: "*"}}
	}
	pattern = strings.TrimPrefix(pattern, "!")
	pattern = strings.TrimSpace(pattern)
	if strings.HasPrefix(pattern, "./") {
		pattern = strings.TrimPrefix(pattern, "./")
	}
	directoryOnly := strings.HasSuffix(pattern, "/") || strings.HasSuffix(pattern, string(filepath.Separator))
	pattern = strings.TrimRight(pattern, `/\`)

	if filepath.IsAbs(pattern) || pattern == "~" || strings.HasPrefix(pattern, "~/") {
		rel, ok := searchRelativePatternForAbsoluteDeny(pattern, cwd, root)
		if !ok {
			return nil
		}
		pattern = "/" + rel
	}

	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	pattern = filepath.ToSlash(filepath.Clean(pattern))
	if pattern == "" || pattern == "." {
		return nil
	}
	if !anchored && strings.Contains(pattern, "/") && !strings.HasPrefix(pattern, "**/") {
		pattern = "**/" + pattern
	}
	if directoryOnly && strings.Contains(pattern, "/") {
		pattern = strings.TrimSuffix(pattern, "/") + "/**"
		directoryOnly = false
	}
	return searchIgnoreRules{{
		Pattern:       pattern,
		DirectoryOnly: directoryOnly,
		Anchored:      anchored,
	}}
}

func searchRelativePatternForAbsoluteDeny(pattern string, cwd string, root string) (string, bool) {
	resolved := resolvePath(cwd, pattern)
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." {
		return "*", true
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
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
	if len(options.ExtraIgnores) > 0 {
		ignoreRules = append(ignoreRules, options.ExtraIgnores...)
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
			if (options.ExcludeVCSDirs && ignoredVCSDir(entry.Name())) || (len(ignoreRules) > 0 && ignoreRules.Ignored(rel, true)) {
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
		if len(ignoreRules) > 0 && ignoreRules.Ignored(rel, false) {
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
