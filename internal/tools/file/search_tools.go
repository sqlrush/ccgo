package filetools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

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

type rawJSON = json.RawMessage

var allowedGrepInputKeys = map[string]struct{}{
	"pattern": {}, "regex": {}, "regexp": {}, "--regexp": {}, "-e": {}, "pattern_file": {}, "patternFile": {}, "pattern-file": {}, "--file": {}, "-f": {}, "path": {}, "glob": {}, "--glob": {}, "-g": {}, "iglob": {}, "--iglob": {}, "glob_case_insensitive": {}, "globCaseInsensitive": {}, "glob-case-insensitive": {}, "--glob-case-insensitive": {}, "no_glob_case_insensitive": {}, "noGlobCaseInsensitive": {}, "no-glob-case-insensitive": {}, "--no-glob-case-insensitive": {}, "type": {}, "--type": {}, "-t": {}, "type_not": {}, "typeNot": {}, "type-not": {}, "--type-not": {}, "-T": {}, "output_mode": {}, "outputMode": {}, "limit": {},
	"head_limit": {}, "headLimit": {}, "offset": {}, "max_count": {}, "maxCount": {}, "-m": {},
	"max_columns": {}, "maxColumns": {}, "max-columns": {}, "--max-columns": {},
	"max_filesize": {}, "maxFilesize": {}, "max-filesize": {}, "--max-filesize": {},
	"max_depth": {}, "maxDepth": {}, "max-depth": {}, "--max-depth": {}, "-d": {},
	"max_columns_preview": {}, "maxColumnsPreview": {}, "max-columns-preview": {}, "--max-columns-preview": {}, "no_max_columns_preview": {}, "noMaxColumnsPreview": {}, "no-max-columns-preview": {}, "--no-max-columns-preview": {},
	"replace": {}, "--replace": {}, "-r": {},
	"with_filename": {}, "withFilename": {}, "with-filename": {}, "--with-filename": {}, "-H": {}, "no_filename": {}, "noFilename": {}, "no-filename": {}, "--no-filename": {}, "-I": {},
	"heading": {}, "--heading": {}, "no_heading": {}, "noHeading": {}, "no-heading": {}, "--no-heading": {},
	"path_separator": {}, "pathSeparator": {}, "path-separator": {}, "--path-separator": {},
	"null": {}, "--null": {}, "-0": {},
	"field_match_separator": {}, "fieldMatchSeparator": {}, "field-match-separator": {}, "--field-match-separator": {},
	"field_context_separator": {}, "fieldContextSeparator": {}, "field-context-separator": {}, "--field-context-separator": {},
	"context_separator": {}, "contextSeparator": {}, "context-separator": {}, "--context-separator": {},
	"no_context_separator": {}, "noContextSeparator": {}, "no-context-separator": {}, "--no-context-separator": {},
	"byte_offset": {}, "byteOffset": {}, "byte-offset": {}, "--byte-offset": {}, "-b": {},
	"hidden": {}, "--hidden": {}, "no_hidden": {}, "noHidden": {}, "no-hidden": {}, "--no-hidden": {},
	"sort": {}, "--sort": {}, "sortr": {}, "--sortr": {}, "sort_files": {}, "sortFiles": {}, "sort-files": {}, "--sort-files": {},
	"context": {}, "-C": {}, "before_context": {}, "beforeContext": {}, "-B": {}, "after_context": {}, "afterContext": {}, "-A": {}, "line_numbers": {}, "lineNumbers": {}, "line-number": {}, "--line-number": {}, "-n": {},
	"no_line_number": {}, "noLineNumber": {}, "no_line_numbers": {}, "noLineNumbers": {}, "no-line-number": {}, "no-line-numbers": {}, "--no-line-number": {}, "-N": {},
	"column": {}, "column_numbers": {}, "columnNumbers": {}, "column-number": {}, "--column": {},
	"ignore_case": {}, "case_insensitive": {}, "caseInsensitive": {}, "ignore-case": {}, "--ignore-case": {}, "-i": {},
	"case_sensitive": {}, "caseSensitive": {}, "case-sensitive": {}, "--case-sensitive": {}, "-s": {},
	"smart_case": {}, "smartCase": {}, "smart-case": {}, "--smart-case": {}, "-S": {},
	"encoding": {}, "--encoding": {}, "-E": {}, "no_encoding": {}, "noEncoding": {}, "no-encoding": {}, "--no-encoding": {},
	"crlf": {}, "--crlf": {}, "no_crlf": {}, "noCrlf": {}, "noCRLF": {}, "no-crlf": {}, "--no-crlf": {},
	"null_data": {}, "nullData": {}, "null-data": {}, "--null-data": {}, "no_null_data": {}, "noNullData": {}, "no-null-data": {}, "--no-null-data": {},
	"fixed_strings": {}, "fixedStrings": {}, "fixed-strings": {}, "--fixed-strings": {}, "-F": {}, "multiline": {}, "--multiline": {}, "multiline-dotall": {}, "--multiline-dotall": {}, "-U": {},
	"text": {}, "--text": {}, "-a": {}, "no_text": {}, "noText": {}, "no-text": {}, "--no-text": {},
	"word_regexp": {}, "wordRegexp": {}, "word-regexp": {}, "--word-regexp": {}, "-w": {},
	"line_regexp": {}, "lineRegexp": {}, "line-regexp": {}, "--line-regexp": {}, "-x": {},
	"invert_match": {}, "invertMatch": {}, "invert-match": {}, "--invert-match": {}, "-v": {},
	"only_matching": {}, "onlyMatching": {}, "only-matching": {}, "--only-matching": {}, "-o": {},
	"vimgrep": {}, "--vimgrep": {},
	"passthru": {}, "passthrough": {}, "--passthru": {}, "--passthrough": {},
	"trim": {}, "--trim": {}, "no_trim": {}, "noTrim": {}, "no-trim": {}, "--no-trim": {},
	"stats": {}, "--stats": {}, "no_stats": {}, "noStats": {}, "no-stats": {}, "--no-stats": {},
	"files": {}, "--files": {},
	"files_with_match": {}, "filesWithMatch": {}, "files-with-match": {}, "--files-with-match": {}, "files_with_matches": {}, "filesWithMatches": {}, "files-with-matches": {}, "--files-with-matches": {}, "-l": {},
	"files_without_match": {}, "filesWithoutMatch": {}, "files-without-match": {}, "--files-without-match": {}, "files_without_matches": {}, "filesWithoutMatches": {}, "files-without-matches": {}, "--files-without-matches": {},
	"count": {}, "--count": {}, "-c": {},
	"count_matches": {}, "countMatches": {}, "count-matches": {}, "--count-matches": {},
	"include_zero": {}, "includeZero": {}, "include-zero": {}, "--include-zero": {},
	"follow": {}, "--follow": {}, "-L": {}, "no_follow": {}, "noFollow": {}, "no-follow": {}, "--no-follow": {},
	"no_ignore": {}, "noIgnore": {}, "no-ignore": {}, "--no-ignore": {},
	"ignore_file": {}, "ignoreFile": {}, "ignore-file": {}, "--ignore-file": {},
	"ignore_files": {}, "ignoreFiles": {}, "ignore-files": {}, "--ignore-files": {}, "no_ignore_files": {}, "noIgnoreFiles": {}, "no-ignore-files": {}, "--no-ignore-files": {},
	"ignore_dot": {}, "ignoreDot": {}, "ignore-dot": {}, "--ignore-dot": {}, "no_ignore_dot": {}, "noIgnoreDot": {}, "no-ignore-dot": {}, "--no-ignore-dot": {},
	"ignore_vcs": {}, "ignoreVCS": {}, "ignore-vcs": {}, "--ignore-vcs": {}, "no_ignore_vcs": {}, "noIgnoreVCS": {}, "no-ignore-vcs": {}, "--no-ignore-vcs": {},
}

var grepSemanticNumberKeys = map[string]struct{}{
	"limit": {}, "head_limit": {}, "headLimit": {}, "offset": {}, "max_count": {}, "maxCount": {}, "-m": {},
	"max_columns": {}, "maxColumns": {}, "max-columns": {}, "--max-columns": {},
	"max_depth": {}, "maxDepth": {}, "max-depth": {}, "--max-depth": {}, "-d": {},
	"context": {}, "-C": {}, "before_context": {}, "beforeContext": {}, "-B": {}, "after_context": {}, "afterContext": {}, "-A": {},
}

var grepSemanticBooleanKeys = map[string]struct{}{
	"max_columns_preview": {}, "maxColumnsPreview": {}, "max-columns-preview": {}, "--max-columns-preview": {}, "no_max_columns_preview": {}, "noMaxColumnsPreview": {}, "no-max-columns-preview": {}, "--no-max-columns-preview": {},
	"with_filename": {}, "withFilename": {}, "with-filename": {}, "--with-filename": {}, "-H": {}, "no_filename": {}, "noFilename": {}, "no-filename": {}, "--no-filename": {}, "-I": {},
	"heading": {}, "--heading": {}, "no_heading": {}, "noHeading": {}, "no-heading": {}, "--no-heading": {},
	"null": {}, "--null": {}, "-0": {},
	"no_context_separator": {}, "noContextSeparator": {}, "no-context-separator": {}, "--no-context-separator": {},
	"byte_offset": {}, "byteOffset": {}, "byte-offset": {}, "--byte-offset": {}, "-b": {},
	"hidden": {}, "--hidden": {}, "no_hidden": {}, "noHidden": {}, "no-hidden": {}, "--no-hidden": {},
	"sort_files": {}, "sortFiles": {}, "sort-files": {}, "--sort-files": {},
	"line_numbers": {}, "lineNumbers": {}, "line-number": {}, "--line-number": {}, "-n": {},
	"no_line_number": {}, "noLineNumber": {}, "no_line_numbers": {}, "noLineNumbers": {}, "no-line-number": {}, "no-line-numbers": {}, "--no-line-number": {}, "-N": {},
	"column": {}, "column_numbers": {}, "columnNumbers": {}, "column-number": {}, "--column": {},
	"ignore_case": {}, "case_insensitive": {}, "caseInsensitive": {}, "ignore-case": {}, "--ignore-case": {}, "-i": {},
	"case_sensitive": {}, "caseSensitive": {}, "case-sensitive": {}, "--case-sensitive": {}, "-s": {},
	"smart_case": {}, "smartCase": {}, "smart-case": {}, "--smart-case": {}, "-S": {},
	"no_encoding": {}, "noEncoding": {}, "no-encoding": {}, "--no-encoding": {},
	"crlf": {}, "--crlf": {}, "no_crlf": {}, "noCrlf": {}, "noCRLF": {}, "no-crlf": {}, "--no-crlf": {},
	"null_data": {}, "nullData": {}, "null-data": {}, "--null-data": {}, "no_null_data": {}, "noNullData": {}, "no-null-data": {}, "--no-null-data": {},
	"fixed_strings": {}, "fixedStrings": {}, "fixed-strings": {}, "--fixed-strings": {}, "-F": {}, "multiline": {}, "--multiline": {}, "multiline-dotall": {}, "--multiline-dotall": {}, "-U": {},
	"glob_case_insensitive": {}, "globCaseInsensitive": {}, "glob-case-insensitive": {}, "--glob-case-insensitive": {}, "no_glob_case_insensitive": {}, "noGlobCaseInsensitive": {}, "no-glob-case-insensitive": {}, "--no-glob-case-insensitive": {},
	"text": {}, "--text": {}, "-a": {}, "no_text": {}, "noText": {}, "no-text": {}, "--no-text": {},
	"word_regexp": {}, "wordRegexp": {}, "word-regexp": {}, "--word-regexp": {}, "-w": {},
	"line_regexp": {}, "lineRegexp": {}, "line-regexp": {}, "--line-regexp": {}, "-x": {},
	"invert_match": {}, "invertMatch": {}, "invert-match": {}, "--invert-match": {}, "-v": {},
	"only_matching": {}, "onlyMatching": {}, "only-matching": {}, "--only-matching": {}, "-o": {},
	"vimgrep": {}, "--vimgrep": {},
	"passthru": {}, "passthrough": {}, "--passthru": {}, "--passthrough": {},
	"trim": {}, "--trim": {}, "no_trim": {}, "noTrim": {}, "no-trim": {}, "--no-trim": {},
	"stats": {}, "--stats": {}, "no_stats": {}, "noStats": {}, "no-stats": {}, "--no-stats": {},
	"files": {}, "--files": {},
	"files_with_match": {}, "filesWithMatch": {}, "files-with-match": {}, "--files-with-match": {}, "files_with_matches": {}, "filesWithMatches": {}, "files-with-matches": {}, "--files-with-matches": {}, "-l": {},
	"files_without_match": {}, "filesWithoutMatch": {}, "files-without-match": {}, "--files-without-match": {}, "files_without_matches": {}, "filesWithoutMatches": {}, "files-without-matches": {}, "--files-without-matches": {},
	"count": {}, "--count": {}, "-c": {},
	"count_matches": {}, "countMatches": {}, "count-matches": {}, "--count-matches": {},
	"include_zero": {}, "includeZero": {}, "include-zero": {}, "--include-zero": {},
	"follow": {}, "--follow": {}, "-L": {}, "no_follow": {}, "noFollow": {}, "no-follow": {}, "--no-follow": {},
	"no_ignore": {}, "noIgnore": {}, "no-ignore": {}, "--no-ignore": {},
	"ignore_files": {}, "ignoreFiles": {}, "ignore-files": {}, "--ignore-files": {}, "no_ignore_files": {}, "noIgnoreFiles": {}, "no-ignore-files": {}, "--no-ignore-files": {},
	"ignore_dot": {}, "ignoreDot": {}, "ignore-dot": {}, "--ignore-dot": {}, "no_ignore_dot": {}, "noIgnoreDot": {}, "no-ignore-dot": {}, "--no-ignore-dot": {},
	"ignore_vcs": {}, "ignoreVCS": {}, "ignore-vcs": {}, "--ignore-vcs": {}, "no_ignore_vcs": {}, "noIgnoreVCS": {}, "no-ignore-vcs": {}, "--no-ignore-vcs": {},
}

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type grepInput struct {
	Pattern                   string  `json:"pattern"`
	Regex                     string  `json:"regex,omitempty"`
	Regexp                    string  `json:"regexp,omitempty"`
	LongRegexp                string  `json:"--regexp,omitempty"`
	ShortRegexp               string  `json:"-e,omitempty"`
	PatternFile               string  `json:"pattern_file,omitempty"`
	PatternFileAlt            string  `json:"patternFile,omitempty"`
	PatternFileDash           string  `json:"pattern-file,omitempty"`
	LongPatternFile           string  `json:"--file,omitempty"`
	ShortPatternFile          string  `json:"-f,omitempty"`
	Path                      string  `json:"path,omitempty"`
	Glob                      string  `json:"glob,omitempty"`
	LongGlob                  string  `json:"--glob,omitempty"`
	ShortGlob                 string  `json:"-g,omitempty"`
	IGlob                     string  `json:"iglob,omitempty"`
	LongIGlob                 string  `json:"--iglob,omitempty"`
	GlobCaseInsensitive       bool    `json:"glob_case_insensitive,omitempty"`
	GlobCaseInsensitiveAlt    bool    `json:"globCaseInsensitive,omitempty"`
	GlobCaseInsensitiveDash   bool    `json:"glob-case-insensitive,omitempty"`
	LongGlobCaseInsensitive   bool    `json:"--glob-case-insensitive,omitempty"`
	NoGlobCaseInsensitive     bool    `json:"no_glob_case_insensitive,omitempty"`
	NoGlobCaseInsensitiveAlt  bool    `json:"noGlobCaseInsensitive,omitempty"`
	NoGlobCaseInsensitiveDash bool    `json:"no-glob-case-insensitive,omitempty"`
	LongNoGlobCaseInsensitive bool    `json:"--no-glob-case-insensitive,omitempty"`
	Type                      string  `json:"type,omitempty"`
	LongType                  string  `json:"--type,omitempty"`
	ShortType                 string  `json:"-t,omitempty"`
	TypeNot                   string  `json:"type_not,omitempty"`
	TypeNotAlt                string  `json:"typeNot,omitempty"`
	TypeNotDash               string  `json:"type-not,omitempty"`
	LongTypeNot               string  `json:"--type-not,omitempty"`
	ShortTypeNot              string  `json:"-T,omitempty"`
	OutputMode                string  `json:"output_mode,omitempty"`
	OutputModeAlt             string  `json:"outputMode,omitempty"`
	Limit                     *int    `json:"limit,omitempty"`
	HeadLimit                 *int    `json:"head_limit,omitempty"`
	HeadLimitAlt              *int    `json:"headLimit,omitempty"`
	Offset                    *int    `json:"offset,omitempty"`
	MaxCount                  *int    `json:"max_count,omitempty"`
	MaxCountAlt               *int    `json:"maxCount,omitempty"`
	ShortMaxCount             *int    `json:"-m,omitempty"`
	MaxColumns                *int    `json:"max_columns,omitempty"`
	MaxColumnsAlt             *int    `json:"maxColumns,omitempty"`
	MaxColumnsDash            *int    `json:"max-columns,omitempty"`
	LongMaxColumns            *int    `json:"--max-columns,omitempty"`
	MaxFilesize               rawJSON `json:"max_filesize,omitempty"`
	MaxFilesizeAlt            rawJSON `json:"maxFilesize,omitempty"`
	MaxFilesizeDash           rawJSON `json:"max-filesize,omitempty"`
	LongMaxFilesize           rawJSON `json:"--max-filesize,omitempty"`
	MaxDepth                  *int    `json:"max_depth,omitempty"`
	MaxDepthAlt               *int    `json:"maxDepth,omitempty"`
	MaxDepthDash              *int    `json:"max-depth,omitempty"`
	LongMaxDepth              *int    `json:"--max-depth,omitempty"`
	ShortMaxDepth             *int    `json:"-d,omitempty"`
	MaxColumnsPreview         bool    `json:"max_columns_preview,omitempty"`
	MaxColumnsPreviewAlt      bool    `json:"maxColumnsPreview,omitempty"`
	MaxColumnsPreviewDash     bool    `json:"max-columns-preview,omitempty"`
	LongMaxColumnsPreview     bool    `json:"--max-columns-preview,omitempty"`
	NoMaxColumnsPreview       bool    `json:"no_max_columns_preview,omitempty"`
	NoMaxColumnsPreviewAlt    bool    `json:"noMaxColumnsPreview,omitempty"`
	NoMaxColumnsPreviewDash   bool    `json:"no-max-columns-preview,omitempty"`
	LongNoMaxColumnsPreview   bool    `json:"--no-max-columns-preview,omitempty"`
	Replace                   *string `json:"replace,omitempty"`
	LongReplace               *string `json:"--replace,omitempty"`
	ShortReplace              *string `json:"-r,omitempty"`
	WithFilename              bool    `json:"with_filename,omitempty"`
	WithFilenameAlt           bool    `json:"withFilename,omitempty"`
	WithFilenameDash          bool    `json:"with-filename,omitempty"`
	LongWithFilename          bool    `json:"--with-filename,omitempty"`
	ShortWithFilename         bool    `json:"-H,omitempty"`
	NoFilename                bool    `json:"no_filename,omitempty"`
	NoFilenameAlt             bool    `json:"noFilename,omitempty"`
	NoFilenameDash            bool    `json:"no-filename,omitempty"`
	LongNoFilename            bool    `json:"--no-filename,omitempty"`
	ShortNoFilename           bool    `json:"-I,omitempty"`
	Heading                   bool    `json:"heading,omitempty"`
	LongHeading               bool    `json:"--heading,omitempty"`
	NoHeading                 bool    `json:"no_heading,omitempty"`
	NoHeadingAlt              bool    `json:"noHeading,omitempty"`
	NoHeadingDash             bool    `json:"no-heading,omitempty"`
	LongNoHeading             bool    `json:"--no-heading,omitempty"`
	PathSeparator             string  `json:"path_separator,omitempty"`
	PathSeparatorAlt          string  `json:"pathSeparator,omitempty"`
	PathSeparatorDash         string  `json:"path-separator,omitempty"`
	LongPathSeparator         string  `json:"--path-separator,omitempty"`
	Null                      bool    `json:"null,omitempty"`
	LongNull                  bool    `json:"--null,omitempty"`
	ShortNull                 bool    `json:"-0,omitempty"`
	FieldMatchSeparator       *string `json:"field_match_separator,omitempty"`
	FieldMatchSeparatorAlt    *string `json:"fieldMatchSeparator,omitempty"`
	FieldMatchSeparatorDash   *string `json:"field-match-separator,omitempty"`
	LongFieldMatchSeparator   *string `json:"--field-match-separator,omitempty"`
	FieldContextSeparator     *string `json:"field_context_separator,omitempty"`
	FieldContextSeparatorAlt  *string `json:"fieldContextSeparator,omitempty"`
	FieldContextSeparatorDash *string `json:"field-context-separator,omitempty"`
	LongFieldContextSeparator *string `json:"--field-context-separator,omitempty"`
	ContextSeparator          *string `json:"context_separator,omitempty"`
	ContextSeparatorAlt       *string `json:"contextSeparator,omitempty"`
	ContextSeparatorDash      *string `json:"context-separator,omitempty"`
	LongContextSeparator      *string `json:"--context-separator,omitempty"`
	NoContextSeparator        bool    `json:"no_context_separator,omitempty"`
	NoContextSeparatorAlt     bool    `json:"noContextSeparator,omitempty"`
	NoContextSeparatorDash    bool    `json:"no-context-separator,omitempty"`
	LongNoContextSeparator    bool    `json:"--no-context-separator,omitempty"`
	ByteOffset                bool    `json:"byte_offset,omitempty"`
	ByteOffsetAlt             bool    `json:"byteOffset,omitempty"`
	ByteOffsetDash            bool    `json:"byte-offset,omitempty"`
	LongByteOffset            bool    `json:"--byte-offset,omitempty"`
	ShortByteOffset           bool    `json:"-b,omitempty"`
	Hidden                    *bool   `json:"hidden,omitempty"`
	LongHidden                *bool   `json:"--hidden,omitempty"`
	NoHidden                  bool    `json:"no_hidden,omitempty"`
	NoHiddenAlt               bool    `json:"noHidden,omitempty"`
	NoHiddenDash              bool    `json:"no-hidden,omitempty"`
	LongNoHidden              bool    `json:"--no-hidden,omitempty"`
	Sort                      string  `json:"sort,omitempty"`
	LongSort                  string  `json:"--sort,omitempty"`
	SortReverse               string  `json:"sortr,omitempty"`
	LongSortReverse           string  `json:"--sortr,omitempty"`
	SortFiles                 bool    `json:"sort_files,omitempty"`
	SortFilesAlt              bool    `json:"sortFiles,omitempty"`
	SortFilesDash             bool    `json:"sort-files,omitempty"`
	LongSortFiles             bool    `json:"--sort-files,omitempty"`
	Context                   *int    `json:"context,omitempty"`
	ShortContext              *int    `json:"-C,omitempty"`
	BeforeContext             *int    `json:"before_context,omitempty"`
	BeforeContextAlt          *int    `json:"beforeContext,omitempty"`
	ShortBeforeContext        *int    `json:"-B,omitempty"`
	AfterContext              *int    `json:"after_context,omitempty"`
	AfterContextAlt           *int    `json:"afterContext,omitempty"`
	ShortAfterContext         *int    `json:"-A,omitempty"`
	LineNumbers               *bool   `json:"line_numbers,omitempty"`
	LineNumbersAlt            *bool   `json:"lineNumbers,omitempty"`
	LineNumbersDash           *bool   `json:"line-number,omitempty"`
	LongLineNumbers           *bool   `json:"--line-number,omitempty"`
	ShortLineNumbers          *bool   `json:"-n,omitempty"`
	NoLineNumber              bool    `json:"no_line_number,omitempty"`
	NoLineNumberAlt           bool    `json:"noLineNumber,omitempty"`
	NoLineNumbers             bool    `json:"no_line_numbers,omitempty"`
	NoLineNumbersAlt          bool    `json:"noLineNumbers,omitempty"`
	NoLineNumberDash          bool    `json:"no-line-number,omitempty"`
	NoLineNumbersDash         bool    `json:"no-line-numbers,omitempty"`
	LongNoLineNumber          bool    `json:"--no-line-number,omitempty"`
	ShortNoLineNumber         bool    `json:"-N,omitempty"`
	Column                    bool    `json:"column,omitempty"`
	ColumnNumbers             bool    `json:"column_numbers,omitempty"`
	ColumnNumbersAlt          bool    `json:"columnNumbers,omitempty"`
	ColumnNumbersDash         bool    `json:"column-number,omitempty"`
	LongColumn                bool    `json:"--column,omitempty"`
	IgnoreCase                bool    `json:"ignore_case,omitempty"`
	CaseInsensitive           bool    `json:"case_insensitive,omitempty"`
	CaseInsensitiveAlt        bool    `json:"caseInsensitive,omitempty"`
	IgnoreCaseDash            bool    `json:"ignore-case,omitempty"`
	LongIgnoreCase            bool    `json:"--ignore-case,omitempty"`
	ShortIgnoreCase           bool    `json:"-i,omitempty"`
	CaseSensitive             bool    `json:"case_sensitive,omitempty"`
	CaseSensitiveAlt          bool    `json:"caseSensitive,omitempty"`
	CaseSensitiveDash         bool    `json:"case-sensitive,omitempty"`
	LongCaseSensitive         bool    `json:"--case-sensitive,omitempty"`
	ShortCaseSensitive        bool    `json:"-s,omitempty"`
	SmartCase                 bool    `json:"smart_case,omitempty"`
	SmartCaseAlt              bool    `json:"smartCase,omitempty"`
	SmartCaseDash             bool    `json:"smart-case,omitempty"`
	LongSmartCase             bool    `json:"--smart-case,omitempty"`
	ShortSmartCase            bool    `json:"-S,omitempty"`
	Encoding                  string  `json:"encoding,omitempty"`
	LongEncoding              string  `json:"--encoding,omitempty"`
	ShortEncoding             string  `json:"-E,omitempty"`
	NoEncoding                bool    `json:"no_encoding,omitempty"`
	NoEncodingAlt             bool    `json:"noEncoding,omitempty"`
	NoEncodingDash            bool    `json:"no-encoding,omitempty"`
	LongNoEncoding            bool    `json:"--no-encoding,omitempty"`
	CRLF                      bool    `json:"crlf,omitempty"`
	LongCRLF                  bool    `json:"--crlf,omitempty"`
	NoCRLF                    bool    `json:"no_crlf,omitempty"`
	NoCRLFAlt                 bool    `json:"noCrlf,omitempty"`
	NoCRLFUpperAlt            bool    `json:"noCRLF,omitempty"`
	NoCRLFDash                bool    `json:"no-crlf,omitempty"`
	LongNoCRLF                bool    `json:"--no-crlf,omitempty"`
	NullData                  bool    `json:"null_data,omitempty"`
	NullDataAlt               bool    `json:"nullData,omitempty"`
	NullDataDash              bool    `json:"null-data,omitempty"`
	LongNullData              bool    `json:"--null-data,omitempty"`
	NoNullData                bool    `json:"no_null_data,omitempty"`
	NoNullDataAlt             bool    `json:"noNullData,omitempty"`
	NoNullDataDash            bool    `json:"no-null-data,omitempty"`
	LongNoNullData            bool    `json:"--no-null-data,omitempty"`
	FixedStrings              bool    `json:"fixed_strings,omitempty"`
	FixedStringsAlt           bool    `json:"fixedStrings,omitempty"`
	FixedStringsDash          bool    `json:"fixed-strings,omitempty"`
	LongFixedStrings          bool    `json:"--fixed-strings,omitempty"`
	ShortFixedStrings         bool    `json:"-F,omitempty"`
	Text                      bool    `json:"text,omitempty"`
	LongText                  bool    `json:"--text,omitempty"`
	ShortText                 bool    `json:"-a,omitempty"`
	NoText                    bool    `json:"no_text,omitempty"`
	NoTextAlt                 bool    `json:"noText,omitempty"`
	NoTextDash                bool    `json:"no-text,omitempty"`
	LongNoText                bool    `json:"--no-text,omitempty"`
	WordRegexp                bool    `json:"word_regexp,omitempty"`
	WordRegexpAlt             bool    `json:"wordRegexp,omitempty"`
	WordRegexpDash            bool    `json:"word-regexp,omitempty"`
	LongWordRegexp            bool    `json:"--word-regexp,omitempty"`
	ShortWordRegexp           bool    `json:"-w,omitempty"`
	LineRegexp                bool    `json:"line_regexp,omitempty"`
	LineRegexpAlt             bool    `json:"lineRegexp,omitempty"`
	LineRegexpDash            bool    `json:"line-regexp,omitempty"`
	LongLineRegexp            bool    `json:"--line-regexp,omitempty"`
	ShortLineRegexp           bool    `json:"-x,omitempty"`
	InvertMatch               bool    `json:"invert_match,omitempty"`
	InvertMatchAlt            bool    `json:"invertMatch,omitempty"`
	InvertMatchDash           bool    `json:"invert-match,omitempty"`
	LongInvertMatch           bool    `json:"--invert-match,omitempty"`
	ShortInvertMatch          bool    `json:"-v,omitempty"`
	OnlyMatching              bool    `json:"only_matching,omitempty"`
	OnlyMatchingAlt           bool    `json:"onlyMatching,omitempty"`
	OnlyMatchingDash          bool    `json:"only-matching,omitempty"`
	LongOnlyMatching          bool    `json:"--only-matching,omitempty"`
	ShortOnlyMatching         bool    `json:"-o,omitempty"`
	Vimgrep                   bool    `json:"vimgrep,omitempty"`
	LongVimgrep               bool    `json:"--vimgrep,omitempty"`
	Passthru                  bool    `json:"passthru,omitempty"`
	Passthrough               bool    `json:"passthrough,omitempty"`
	LongPassthru              bool    `json:"--passthru,omitempty"`
	LongPassthrough           bool    `json:"--passthrough,omitempty"`
	Trim                      bool    `json:"trim,omitempty"`
	LongTrim                  bool    `json:"--trim,omitempty"`
	NoTrim                    bool    `json:"no_trim,omitempty"`
	NoTrimAlt                 bool    `json:"noTrim,omitempty"`
	NoTrimDash                bool    `json:"no-trim,omitempty"`
	LongNoTrim                bool    `json:"--no-trim,omitempty"`
	Stats                     bool    `json:"stats,omitempty"`
	LongStats                 bool    `json:"--stats,omitempty"`
	NoStats                   bool    `json:"no_stats,omitempty"`
	NoStatsAlt                bool    `json:"noStats,omitempty"`
	NoStatsDash               bool    `json:"no-stats,omitempty"`
	LongNoStats               bool    `json:"--no-stats,omitempty"`
	Files                     bool    `json:"files,omitempty"`
	LongFiles                 bool    `json:"--files,omitempty"`
	FilesWithMatch            bool    `json:"files_with_match,omitempty"`
	FilesWithMatchAlt         bool    `json:"filesWithMatch,omitempty"`
	FilesWithMatchDash        bool    `json:"files-with-match,omitempty"`
	LongFilesWithMatch        bool    `json:"--files-with-match,omitempty"`
	FilesWithMatches          bool    `json:"files_with_matches,omitempty"`
	FilesWithMatchesAlt       bool    `json:"filesWithMatches,omitempty"`
	FilesWithMatchesDash      bool    `json:"files-with-matches,omitempty"`
	LongFilesWithMatches      bool    `json:"--files-with-matches,omitempty"`
	ShortFilesWithMatches     bool    `json:"-l,omitempty"`
	FilesWithoutMatch         bool    `json:"files_without_match,omitempty"`
	FilesWithoutMatchAlt      bool    `json:"filesWithoutMatch,omitempty"`
	FilesWithoutMatchDash     bool    `json:"files-without-match,omitempty"`
	LongFilesWithoutMatch     bool    `json:"--files-without-match,omitempty"`
	FilesWithoutMatches       bool    `json:"files_without_matches,omitempty"`
	FilesWithoutMatchesAlt    bool    `json:"filesWithoutMatches,omitempty"`
	FilesWithoutMatchesDash   bool    `json:"files-without-matches,omitempty"`
	LongFilesWithoutMatches   bool    `json:"--files-without-matches,omitempty"`
	Count                     bool    `json:"count,omitempty"`
	LongCount                 bool    `json:"--count,omitempty"`
	ShortCount                bool    `json:"-c,omitempty"`
	CountMatches              bool    `json:"count_matches,omitempty"`
	CountMatchesAlt           bool    `json:"countMatches,omitempty"`
	CountMatchesDash          bool    `json:"count-matches,omitempty"`
	LongCountMatches          bool    `json:"--count-matches,omitempty"`
	IncludeZero               bool    `json:"include_zero,omitempty"`
	IncludeZeroAlt            bool    `json:"includeZero,omitempty"`
	IncludeZeroDash           bool    `json:"include-zero,omitempty"`
	LongIncludeZero           bool    `json:"--include-zero,omitempty"`
	Follow                    bool    `json:"follow,omitempty"`
	LongFollow                bool    `json:"--follow,omitempty"`
	ShortFollow               bool    `json:"-L,omitempty"`
	NoFollow                  bool    `json:"no_follow,omitempty"`
	NoFollowAlt               bool    `json:"noFollow,omitempty"`
	NoFollowDash              bool    `json:"no-follow,omitempty"`
	LongNoFollow              bool    `json:"--no-follow,omitempty"`
	NoIgnore                  bool    `json:"no_ignore,omitempty"`
	NoIgnoreAlt               bool    `json:"noIgnore,omitempty"`
	NoIgnoreDash              bool    `json:"no-ignore,omitempty"`
	LongNoIgnore              bool    `json:"--no-ignore,omitempty"`
	IgnoreFile                string  `json:"ignore_file,omitempty"`
	IgnoreFileAlt             string  `json:"ignoreFile,omitempty"`
	IgnoreFileDash            string  `json:"ignore-file,omitempty"`
	LongIgnoreFile            string  `json:"--ignore-file,omitempty"`
	IgnoreFiles               bool    `json:"ignore_files,omitempty"`
	IgnoreFilesAlt            bool    `json:"ignoreFiles,omitempty"`
	IgnoreFilesDash           bool    `json:"ignore-files,omitempty"`
	LongIgnoreFiles           bool    `json:"--ignore-files,omitempty"`
	NoIgnoreFiles             bool    `json:"no_ignore_files,omitempty"`
	NoIgnoreFilesAlt          bool    `json:"noIgnoreFiles,omitempty"`
	NoIgnoreFilesDash         bool    `json:"no-ignore-files,omitempty"`
	LongNoIgnoreFiles         bool    `json:"--no-ignore-files,omitempty"`
	IgnoreDot                 bool    `json:"ignore_dot,omitempty"`
	IgnoreDotAlt              bool    `json:"ignoreDot,omitempty"`
	IgnoreDotDash             bool    `json:"ignore-dot,omitempty"`
	LongIgnoreDot             bool    `json:"--ignore-dot,omitempty"`
	NoIgnoreDot               bool    `json:"no_ignore_dot,omitempty"`
	NoIgnoreDotAlt            bool    `json:"noIgnoreDot,omitempty"`
	NoIgnoreDotDash           bool    `json:"no-ignore-dot,omitempty"`
	LongNoIgnoreDot           bool    `json:"--no-ignore-dot,omitempty"`
	IgnoreVCS                 bool    `json:"ignore_vcs,omitempty"`
	IgnoreVCSAlt              bool    `json:"ignoreVCS,omitempty"`
	IgnoreVCSDash             bool    `json:"ignore-vcs,omitempty"`
	LongIgnoreVCS             bool    `json:"--ignore-vcs,omitempty"`
	NoIgnoreVCS               bool    `json:"no_ignore_vcs,omitempty"`
	NoIgnoreVCSAlt            bool    `json:"noIgnoreVCS,omitempty"`
	NoIgnoreVCSDash           bool    `json:"no-ignore-vcs,omitempty"`
	LongNoIgnoreVCS           bool    `json:"--no-ignore-vcs,omitempty"`
	Multiline                 bool    `json:"multiline,omitempty"`
	LongMultiline             bool    `json:"--multiline,omitempty"`
	MultilineDotall           bool    `json:"multiline-dotall,omitempty"`
	LongMultilineDotall       bool    `json:"--multiline-dotall,omitempty"`
	ShortMultiline            bool    `json:"-U,omitempty"`
}

type fileSearchMatch struct {
	Path    string
	RelPath string
	ModUnix int64
}

type grepMatch struct {
	Path       string
	Line       int
	Column     int
	ByteOffset int
	Text       string
	Count      int
	Matched    bool
	ModUnix    int64
}

type grepStats struct {
	Matches          int
	MatchedLines     int
	FilesWithMatches int
	FilesSearched    int
	BytesPrinted     int
	BytesSearched    int64
	SearchDuration   time.Duration
	TotalDuration    time.Duration
}

type grepOptions struct {
	Mode                  string
	Limit                 int
	Offset                int
	MaxCount              int
	MaxColumns            int
	MaxFilesize           int64
	HasMaxFilesize        bool
	MaxPreview            bool
	WithFilename          bool
	Heading               bool
	PathSeparator         string
	Null                  bool
	FieldMatchSeparator   string
	FieldContextSeparator string
	ContextSeparator      string
	NoContextSeparator    bool
	ByteOffset            bool
	HasReplace            bool
	Replace               string
	BeforeContext         int
	AfterContext          int
	LineNumbers           bool
	CRLF                  bool
	NullData              bool
	Multiline             bool
	InvertMatch           bool
	OnlyMatching          bool
	Vimgrep               bool
	Passthru              bool
	Trim                  bool
	Stats                 bool
	CountMatches          bool
	IncludeZero           bool
	ColumnNumbers         bool
	Text                  bool
	Encoding              string
	SortMode              string
	SortReverse           bool
	SortExplicit          bool
}

type searchWalkOptions struct {
	UseGitIgnoreFiles bool
	UseIgnoreFiles    bool
	IncludeHidden     bool
	SkipSymlinks      bool
	FollowSymlinks    bool
	ExcludeVCSDirs    bool
	MaxDepth          int
	ExtraIgnores      searchIgnoreRules
	CustomIgnoreRules searchIgnoreRules
	CustomIgnoreRoot  string
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

type grepContentView struct {
	Lines        []string
	MatchContent string
	MatchStarts  []int
	ByteStarts   []int
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
					"pattern":                    map[string]any{"type": "string"},
					"regex":                      map[string]any{"type": "string"},
					"regexp":                     map[string]any{"type": "string"},
					"--regexp":                   map[string]any{"type": "string"},
					"-e":                         map[string]any{"type": "string"},
					"pattern_file":               map[string]any{"type": "string"},
					"patternFile":                map[string]any{"type": "string"},
					"pattern-file":               map[string]any{"type": "string"},
					"--file":                     map[string]any{"type": "string"},
					"-f":                         map[string]any{"type": "string"},
					"path":                       map[string]any{"type": "string"},
					"glob":                       map[string]any{"type": "string"},
					"--glob":                     map[string]any{"type": "string"},
					"-g":                         map[string]any{"type": "string"},
					"iglob":                      map[string]any{"type": "string"},
					"--iglob":                    map[string]any{"type": "string"},
					"glob_case_insensitive":      map[string]any{"type": "boolean"},
					"globCaseInsensitive":        map[string]any{"type": "boolean"},
					"glob-case-insensitive":      map[string]any{"type": "boolean"},
					"--glob-case-insensitive":    map[string]any{"type": "boolean"},
					"no_glob_case_insensitive":   map[string]any{"type": "boolean"},
					"noGlobCaseInsensitive":      map[string]any{"type": "boolean"},
					"no-glob-case-insensitive":   map[string]any{"type": "boolean"},
					"--no-glob-case-insensitive": map[string]any{"type": "boolean"},
					"type":                       map[string]any{"type": "string"},
					"--type":                     map[string]any{"type": "string"},
					"-t":                         map[string]any{"type": "string"},
					"type_not":                   map[string]any{"type": "string"},
					"typeNot":                    map[string]any{"type": "string"},
					"type-not":                   map[string]any{"type": "string"},
					"--type-not":                 map[string]any{"type": "string"},
					"-T":                         map[string]any{"type": "string"},
					"output_mode":                map[string]any{"type": "string", "enum": []any{"files", "files_with_match", "files_with_matches", "files_without_match", "files_without_matches", "content", "count"}},
					"outputMode":                 map[string]any{"type": "string", "enum": []any{"files", "files_with_match", "files_with_matches", "files_without_match", "files_without_matches", "content", "count"}},
					"limit":                      map[string]any{"type": "integer"},
					"head_limit":                 map[string]any{"type": "integer"},
					"headLimit":                  map[string]any{"type": "integer"},
					"offset":                     map[string]any{"type": "integer"},
					"max_count":                  map[string]any{"type": "integer"},
					"maxCount":                   map[string]any{"type": "integer"},
					"-m":                         map[string]any{"type": "integer"},
					"max_columns":                map[string]any{"type": "integer"},
					"maxColumns":                 map[string]any{"type": "integer"},
					"max-columns":                map[string]any{"type": "integer"},
					"--max-columns": map[string]any{
						"type": "integer",
					},
					"max_filesize":   map[string]any{"type": []any{"string", "number"}},
					"maxFilesize":    map[string]any{"type": []any{"string", "number"}},
					"max-filesize":   map[string]any{"type": []any{"string", "number"}},
					"--max-filesize": map[string]any{"type": []any{"string", "number"}},
					"max_depth":      map[string]any{"type": "integer"},
					"maxDepth":       map[string]any{"type": "integer"},
					"max-depth":      map[string]any{"type": "integer"},
					"--max-depth": map[string]any{
						"type": "integer",
					},
					"-d":                     map[string]any{"type": "integer"},
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
					"replace":         map[string]any{"type": "string"},
					"--replace":       map[string]any{"type": "string"},
					"-r":              map[string]any{"type": "string"},
					"with_filename":   map[string]any{"type": "boolean"},
					"withFilename":    map[string]any{"type": "boolean"},
					"with-filename":   map[string]any{"type": "boolean"},
					"--with-filename": map[string]any{"type": "boolean"},
					"-H":              map[string]any{"type": "boolean"},
					"no_filename":     map[string]any{"type": "boolean"},
					"noFilename":      map[string]any{"type": "boolean"},
					"no-filename":     map[string]any{"type": "boolean"},
					"--no-filename":   map[string]any{"type": "boolean"},
					"-I":              map[string]any{"type": "boolean"},
					"heading":         map[string]any{"type": "boolean"},
					"--heading":       map[string]any{"type": "boolean"},
					"no_heading":      map[string]any{"type": "boolean"},
					"noHeading":       map[string]any{"type": "boolean"},
					"no-heading":      map[string]any{"type": "boolean"},
					"--no-heading":    map[string]any{"type": "boolean"},
					"path_separator":  map[string]any{"type": "string"},
					"pathSeparator":   map[string]any{"type": "string"},
					"path-separator":  map[string]any{"type": "string"},
					"--path-separator": map[string]any{
						"type": "string",
					},
					"null":                      map[string]any{"type": "boolean"},
					"--null":                    map[string]any{"type": "boolean"},
					"-0":                        map[string]any{"type": "boolean"},
					"field_match_separator":     map[string]any{"type": "string"},
					"fieldMatchSeparator":       map[string]any{"type": "string"},
					"field-match-separator":     map[string]any{"type": "string"},
					"--field-match-separator":   map[string]any{"type": "string"},
					"field_context_separator":   map[string]any{"type": "string"},
					"fieldContextSeparator":     map[string]any{"type": "string"},
					"field-context-separator":   map[string]any{"type": "string"},
					"--field-context-separator": map[string]any{"type": "string"},
					"context_separator":         map[string]any{"type": "string"},
					"contextSeparator":          map[string]any{"type": "string"},
					"context-separator":         map[string]any{"type": "string"},
					"--context-separator":       map[string]any{"type": "string"},
					"no_context_separator":      map[string]any{"type": "boolean"},
					"noContextSeparator":        map[string]any{"type": "boolean"},
					"no-context-separator":      map[string]any{"type": "boolean"},
					"--no-context-separator":    map[string]any{"type": "boolean"},
					"byte_offset":               map[string]any{"type": "boolean"},
					"byteOffset":                map[string]any{"type": "boolean"},
					"byte-offset":               map[string]any{"type": "boolean"},
					"--byte-offset":             map[string]any{"type": "boolean"},
					"-b":                        map[string]any{"type": "boolean"},
					"hidden":                    map[string]any{"type": "boolean"},
					"--hidden":                  map[string]any{"type": "boolean"},
					"no_hidden":                 map[string]any{"type": "boolean"},
					"noHidden":                  map[string]any{"type": "boolean"},
					"no-hidden":                 map[string]any{"type": "boolean"},
					"--no-hidden":               map[string]any{"type": "boolean"},
					"sort":                      map[string]any{"type": "string", "enum": []any{"path", "name", "file", "modified", "mtime", "modtime", "time", "none"}},
					"--sort":                    map[string]any{"type": "string", "enum": []any{"path", "name", "file", "modified", "mtime", "modtime", "time", "none"}},
					"sortr":                     map[string]any{"type": "string", "enum": []any{"path", "name", "file", "modified", "mtime", "modtime", "time", "none"}},
					"--sortr": map[string]any{
						"type": "string", "enum": []any{"path", "name", "file", "modified", "mtime", "modtime", "time", "none"},
					},
					"sort_files":   map[string]any{"type": "boolean"},
					"sortFiles":    map[string]any{"type": "boolean"},
					"sort-files":   map[string]any{"type": "boolean"},
					"--sort-files": map[string]any{"type": "boolean"},
					"context":      map[string]any{"type": "integer"},
					"-C":           map[string]any{"type": "integer"},
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
					"encoding":         map[string]any{"type": "string"},
					"--encoding":       map[string]any{"type": "string"},
					"-E":               map[string]any{"type": "string"},
					"no_encoding":      map[string]any{"type": "boolean"},
					"noEncoding":       map[string]any{"type": "boolean"},
					"no-encoding":      map[string]any{"type": "boolean"},
					"--no-encoding":    map[string]any{"type": "boolean"},
					"crlf":             map[string]any{"type": "boolean"},
					"--crlf":           map[string]any{"type": "boolean"},
					"no_crlf":          map[string]any{"type": "boolean"},
					"noCrlf":           map[string]any{"type": "boolean"},
					"noCRLF":           map[string]any{"type": "boolean"},
					"no-crlf":          map[string]any{"type": "boolean"},
					"--no-crlf":        map[string]any{"type": "boolean"},
					"null_data":        map[string]any{"type": "boolean"},
					"nullData":         map[string]any{"type": "boolean"},
					"null-data":        map[string]any{"type": "boolean"},
					"--null-data":      map[string]any{"type": "boolean"},
					"no_null_data":     map[string]any{"type": "boolean"},
					"noNullData":       map[string]any{"type": "boolean"},
					"no-null-data":     map[string]any{"type": "boolean"},
					"--no-null-data":   map[string]any{"type": "boolean"},
					"fixed_strings":    map[string]any{"type": "boolean"},
					"fixedStrings":     map[string]any{"type": "boolean"},
					"fixed-strings":    map[string]any{"type": "boolean"},
					"--fixed-strings":  map[string]any{"type": "boolean"},
					"-F":               map[string]any{"type": "boolean"},
					"text":             map[string]any{"type": "boolean"},
					"--text":           map[string]any{"type": "boolean"},
					"-a":               map[string]any{"type": "boolean"},
					"no_text":          map[string]any{"type": "boolean"},
					"noText":           map[string]any{"type": "boolean"},
					"no-text":          map[string]any{"type": "boolean"},
					"--no-text":        map[string]any{"type": "boolean"},
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
					"stats":            map[string]any{"type": "boolean"},
					"--stats":          map[string]any{"type": "boolean"},
					"no_stats":         map[string]any{"type": "boolean"},
					"noStats":          map[string]any{"type": "boolean"},
					"no-stats":         map[string]any{"type": "boolean"},
					"--no-stats":       map[string]any{"type": "boolean"},
					"files":            map[string]any{"type": "boolean"},
					"--files":          map[string]any{"type": "boolean"},
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
					"count":           map[string]any{"type": "boolean"},
					"--count":         map[string]any{"type": "boolean"},
					"-c":              map[string]any{"type": "boolean"},
					"count_matches":   map[string]any{"type": "boolean"},
					"countMatches":    map[string]any{"type": "boolean"},
					"count-matches":   map[string]any{"type": "boolean"},
					"--count-matches": map[string]any{"type": "boolean"},
					"include_zero":    map[string]any{"type": "boolean"},
					"includeZero":     map[string]any{"type": "boolean"},
					"include-zero":    map[string]any{"type": "boolean"},
					"--include-zero":  map[string]any{"type": "boolean"},
					"follow":          map[string]any{"type": "boolean"},
					"--follow":        map[string]any{"type": "boolean"},
					"-L":              map[string]any{"type": "boolean"},
					"no_follow":       map[string]any{"type": "boolean"},
					"noFollow":        map[string]any{"type": "boolean"},
					"no-follow":       map[string]any{"type": "boolean"},
					"--no-follow":     map[string]any{"type": "boolean"},
					"no_ignore":       map[string]any{"type": "boolean"},
					"noIgnore":        map[string]any{"type": "boolean"},
					"no-ignore":       map[string]any{"type": "boolean"},
					"--no-ignore":     map[string]any{"type": "boolean"},
					"ignore_file":     map[string]any{"type": "string"},
					"ignoreFile":      map[string]any{"type": "string"},
					"ignore-file":     map[string]any{"type": "string"},
					"--ignore-file":   map[string]any{"type": "string"},
					"ignore_files":    map[string]any{"type": "boolean"},
					"ignoreFiles":     map[string]any{"type": "boolean"},
					"ignore-files":    map[string]any{"type": "boolean"},
					"--ignore-files":  map[string]any{"type": "boolean"},
					"no_ignore_files": map[string]any{"type": "boolean"},
					"noIgnoreFiles":   map[string]any{"type": "boolean"},
					"no-ignore-files": map[string]any{"type": "boolean"},
					"--no-ignore-files": map[string]any{
						"type": "boolean",
					},
					"ignore_dot":    map[string]any{"type": "boolean"},
					"ignoreDot":     map[string]any{"type": "boolean"},
					"ignore-dot":    map[string]any{"type": "boolean"},
					"--ignore-dot":  map[string]any{"type": "boolean"},
					"no_ignore_dot": map[string]any{"type": "boolean"},
					"noIgnoreDot":   map[string]any{"type": "boolean"},
					"no-ignore-dot": map[string]any{"type": "boolean"},
					"--no-ignore-dot": map[string]any{
						"type": "boolean",
					},
					"ignore_vcs":    map[string]any{"type": "boolean"},
					"ignoreVCS":     map[string]any{"type": "boolean"},
					"ignore-vcs":    map[string]any{"type": "boolean"},
					"--ignore-vcs":  map[string]any{"type": "boolean"},
					"no_ignore_vcs": map[string]any{"type": "boolean"},
					"noIgnoreVCS":   map[string]any{"type": "boolean"},
					"no-ignore-vcs": map[string]any{"type": "boolean"},
					"--no-ignore-vcs": map[string]any{
						"type": "boolean",
					},
					"multiline":   map[string]any{"type": "boolean"},
					"--multiline": map[string]any{"type": "boolean"},
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
			return "Searches text files under path using a regular expression or fixed string. pattern is the canonical search expression; regex/regexp/--regexp/-e are accepted aliases, and pattern_file/--file/-f can read one pattern per line from a file. output_mode may be files, files_with_matches, files_without_matches, content, or count; glob/-g/--glob, iglob/--iglob, type/-t/--type, and type_not/-T/--type-not optionally filter file paths. glob and iglob accept whitespace/comma-separated patterns, negation, and brace alternation; glob_case_insensitive/--glob-case-insensitive makes glob patterns ignore case. content mode supports context, before_context, after_context, -C, -B, -A, -n/--line-number and -N/--no-line-number line-number control, --column column-number output, byte_offset/--byte-offset/-b byte offset output, -H/--with-filename and -I/--no-filename filename prefix control, heading/--heading grouped file headings, path_separator/--path-separator display path separator control, null/--null NUL path terminators/separators, field_match_separator/--field-match-separator and field_context_separator/--field-context-separator output field separators, context_separator/--context-separator and no_context_separator/--no-context-separator context group separator control, offset, head_limit pagination, max_count/-m per-file match limiting, max_columns/--max-columns long-line omission, --max-columns-preview long-line previews, replace/--replace/-r display-only replacement, only_matching/-o/--only-matching matched-text output, vimgrep/--vimgrep per-match line output, passthru/--passthru/--passthrough all-line output, trim/--trim leading-whitespace trimming, stats/--stats aggregate statistics, and hidden/--hidden or no_hidden/--no-hidden hidden file traversal control. Use files/--files to list files that would be searched without requiring pattern, files_with_matches or -l to list files with matches, files_without_match to list files without matches, and count/--count/-c for count mode. Count mode supports count_matches/--count-matches for occurrence counts and include_zero/--include-zero to include zero-count files. Use max_depth/--max-depth/-d to limit directory descent, max_filesize/--max-filesize with optional K/M/G suffix to skip larger files, follow/--follow/-L or no_follow/--no-follow to control symlink traversal, and sort/--sort or sortr/--sortr with path or modified to control result ordering; --sort-files is accepted as a path-sort alias. Use fixed_strings/-F/--fixed-strings for literal matching, encoding/--encoding/-E to choose auto/none/utf-8/utf-16/utf-16le/utf-16be text decoding, null_data/--null-data to use NUL as the input line terminator, crlf/--crlf to treat CRLF/CR/LF as line terminators for anchors, text/-a/--text to search binary-extension files as text, no_text/--no-text to disable text mode, word_regexp/-w/--word-regexp for whole-word matches, line_regexp/-x/--line-regexp for whole-line matches, ignore_case/-i/--ignore-case for case-insensitive search, case_sensitive/-s/--case-sensitive to force case-sensitive matching, smart_case/-S/--smart-case for lowercase-only patterns, and invert_match/-v/--invert-match to select non-matching lines. Set no_ignore/--no-ignore to skip .gitignore/.ignore/.rgignore files, no_ignore_dot/--no-ignore-dot to skip .ignore/.rgignore while keeping .gitignore active, no_ignore_vcs/--no-ignore-vcs to skip .gitignore while keeping .ignore/.rgignore active, ignore_file/--ignore-file to add a gitignore-formatted file matched relative to the current working directory, or no_ignore_files/--no-ignore-files to ignore explicit ignore_file inputs; VCS metadata and read-denied paths remain excluded. Set multiline to allow patterns to span lines with dot matching newlines.", nil
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
	mode := normalizedGrepOutputMode(input)
	switch mode {
	case "files", "files_with_matches", "files_without_matches", "content", "count":
	default:
		return fmt.Errorf("output_mode must be one of files, files_with_matches, files_without_matches, content, or count")
	}
	var patterns []string
	if mode != "files" {
		var err error
		patterns, err = grepPatterns(ctx.WorkingDirectory, input)
		if err != nil {
			return err
		}
		if len(patterns) == 0 {
			return fmt.Errorf("pattern is required")
		}
		if _, err := compileGrepPattern(input, patterns); err != nil {
			return fmt.Errorf("invalid pattern: %w", err)
		}
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
	if _, _, err := grepMaxFilesize(input); err != nil {
		return err
	}
	if _, err := grepEncoding(input); err != nil {
		return err
	}
	if err := validateGrepMaxDepth(input); err != nil {
		return err
	}
	if _, _, _, err := grepSort(input); err != nil {
		return err
	}
	if separator := grepPathSeparator(input); separator != "" && len(separator) != 1 {
		return fmt.Errorf("path_separator must be exactly one byte")
	}
	if _, err := grepTypeExtensions(grepTypeFilter(input)); err != nil {
		return err
	}
	if _, err := grepTypeExtensions(grepTypeNotFilter(input)); err != nil {
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
	if err := validateGrepIgnoreFile(ctx.WorkingDirectory, input); err != nil {
		return err
	}
	return nil
}

func callGrep(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	started := time.Now()
	input, err := decodeGrep(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	displayRoot := searchRoot(ctx.WorkingDirectory, ".")
	root := searchRoot(ctx.WorkingDirectory, input.Path)
	mode := normalizedGrepOutputMode(input)
	var expr *regexp.Regexp
	var patterns []string
	if mode != "files" {
		patterns, err = grepPatterns(ctx.WorkingDirectory, input)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		expr, err = compileGrepPattern(input, patterns)
		if err != nil {
			return contracts.ToolResult{}, fmt.Errorf("invalid pattern: %w", err)
		}
	}
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
	statsEnabled := grepStatsEnabled(input) && mode != "files"
	heading := grepHeading(input) && mode == "content" && !vimgrep
	pathSeparator := grepPathSeparator(input)
	null := grepNull(input)
	fieldMatchSeparator := grepFieldMatchSeparator(input)
	fieldContextSeparator := grepFieldContextSeparator(input)
	contextSeparator := grepContextSeparator(input)
	noContextSeparator := grepNoContextSeparator(input)
	byteOffset := grepByteOffset(input)
	nullData := grepNullData(input)
	encoding, err := grepEncoding(input)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	countMatches := grepCountMatches(input) && mode == "count" && !invertMatch
	includeZero := grepIncludeZero(input) && mode == "count"
	includeHidden := grepIncludeHidden(input)
	replace, hasReplace := grepReplacement(input)
	if mode != "content" {
		replace = ""
		hasReplace = false
	}
	sortMode, sortReverse, sortExplicit, err := grepSort(input)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	maxFilesize, hasMaxFilesize, err := grepMaxFilesize(input)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	options := grepOptions{
		Mode:                  mode,
		Limit:                 grepLimit(input),
		Offset:                grepOffset(input),
		MaxCount:              grepMaxCount(input),
		MaxColumns:            grepMaxColumns(input),
		MaxFilesize:           maxFilesize,
		HasMaxFilesize:        hasMaxFilesize,
		MaxPreview:            grepMaxColumnsPreview(input),
		WithFilename:          grepWithFilename(input, mode),
		Heading:               heading,
		PathSeparator:         pathSeparator,
		Null:                  null,
		FieldMatchSeparator:   fieldMatchSeparator,
		FieldContextSeparator: fieldContextSeparator,
		ContextSeparator:      contextSeparator,
		NoContextSeparator:    noContextSeparator,
		ByteOffset:            byteOffset,
		HasReplace:            hasReplace,
		Replace:               replace,
		BeforeContext:         before,
		AfterContext:          after,
		LineNumbers:           grepLineNumbers(input, mode),
		CRLF:                  grepCRLF(input) && !nullData,
		NullData:              nullData,
		Multiline:             grepMultiline(input),
		InvertMatch:           invertMatch,
		OnlyMatching:          onlyMatching,
		Vimgrep:               vimgrep,
		Passthru:              passthru,
		Trim:                  trim,
		Stats:                 statsEnabled,
		CountMatches:          countMatches,
		IncludeZero:           includeZero,
		ColumnNumbers:         grepColumnNumbers(input),
		Text:                  grepText(input) || nullData,
		Encoding:              encoding,
		SortMode:              sortMode,
		SortReverse:           sortReverse,
		SortExplicit:          sortExplicit,
	}
	noIgnore := grepNoIgnore(input)
	ignoreVCS := grepIgnoreVCS(input)
	ignoreDot := grepIgnoreDot(input)
	follow := grepFollow(input)
	globFilter := grepGlobFilter(input)
	iglobFilter := grepIGlobFilter(input)
	globCaseInsensitive := grepGlobCaseInsensitive(input)
	typeFilter := grepTypeFilter(input)
	typeNotFilter := grepTypeNotFilter(input)
	maxDepth := grepMaxDepth(input)
	customIgnoreRules, err := grepCustomIgnoreRules(ctx, input)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	matches, totalMatches, truncated, stats, err := collectGrepMatches(root, displayRoot, globFilter, iglobFilter, globCaseInsensitive, typeFilter, typeNotFilter, expr, options, grepWalkOptions(ctx, root, noIgnore, ignoreVCS, ignoreDot, includeHidden, follow, maxDepth, customIgnoreRules))
	if err != nil {
		return contracts.ToolResult{}, err
	}
	stats.SearchDuration = time.Since(started)
	content := formatGrepResultContent(matches, options, truncated, &stats)
	stats.TotalDuration = time.Since(started)
	if content == "" {
		content = "No matches found"
	}
	if options.Stats {
		content = appendGrepStats(content, stats)
	}
	return contracts.ToolResult{
		Content: content,
		StructuredContent: map[string]any{
			"type":                    "grep",
			"pattern":                 grepPattern(input),
			"pattern_file":            grepPatternFile(input),
			"pattern_count":           len(patterns),
			"path":                    input.Path,
			"glob":                    globFilter,
			"iglob":                   iglobFilter,
			"glob_case_insensitive":   globCaseInsensitive,
			"type_filter":             typeFilter,
			"type_not_filter":         typeNotFilter,
			"output_mode":             mode,
			"matches":                 grepStructuredMatches(matches, options),
			"total_matches":           totalMatches,
			"offset":                  options.Offset,
			"limit":                   options.Limit,
			"max_depth":               structuredOptionalInt(maxDepth),
			"max_count":               options.MaxCount,
			"max_columns":             options.MaxColumns,
			"max_filesize":            structuredOptionalInt64(options.MaxFilesize, options.HasMaxFilesize),
			"max_columns_preview":     options.MaxPreview,
			"with_filename":           options.WithFilename,
			"no_filename":             !options.WithFilename && (mode == "content" || mode == "count"),
			"heading":                 heading,
			"path_separator":          pathSeparator,
			"null":                    null,
			"field_match_separator":   fieldMatchSeparator,
			"field_context_separator": fieldContextSeparator,
			"context_separator":       contextSeparator,
			"no_context_separator":    noContextSeparator,
			"byte_offset":             byteOffset,
			"replace":                 replace,
			"has_replace":             hasReplace,
			"before_context":          options.BeforeContext,
			"after_context":           options.AfterContext,
			"line_numbers":            options.LineNumbers,
			"column_numbers":          options.ColumnNumbers,
			"crlf":                    options.CRLF,
			"no_crlf":                 grepNoCRLF(input),
			"null_data":               nullData,
			"no_null_data":            grepNoNullData(input),
			"case_insensitive":        grepEffectiveCaseInsensitive(input),
			"case_sensitive":          grepCaseSensitive(input),
			"smart_case":              grepSmartCase(input),
			"encoding":                encoding,
			"no_encoding":             grepNoEncoding(input),
			"fixed_strings":           grepFixedStrings(input),
			"text":                    options.Text,
			"no_text":                 grepNoText(input),
			"word_regexp":             grepWordRegexp(input),
			"line_regexp":             grepLineRegexp(input),
			"invert_match":            invertMatch,
			"only_matching":           onlyMatching,
			"vimgrep":                 vimgrep,
			"passthru":                passthru,
			"trim":                    trim,
			"stats_enabled":           statsEnabled,
			"stats":                   grepStructuredStats(stats, statsEnabled),
			"files":                   mode == "files",
			"files_with_matches":      mode == "files_with_matches",
			"files_without_match":     mode == "files_without_matches",
			"count":                   mode == "count",
			"count_matches":           countMatches,
			"include_zero":            includeZero,
			"follow":                  follow,
			"no_follow":               grepNoFollow(input),
			"no_ignore":               noIgnore,
			"ignore_vcs":              !noIgnore && ignoreVCS,
			"no_ignore_vcs":           noIgnore || !ignoreVCS,
			"ignore_dot":              !noIgnore && ignoreDot,
			"no_ignore_dot":           noIgnore || !ignoreDot,
			"ignore_file":             grepIgnoreFile(input),
			"ignore_files":            !grepNoIgnoreFiles(input),
			"no_ignore_files":         grepNoIgnoreFiles(input),
			"hidden":                  includeHidden,
			"no_hidden":               !includeHidden,
			"multiline":               grepMultiline(input),
			"sort":                    grepStructuredSortMode(options),
			"sort_reverse":            grepStructuredSortReverse(options),
			"sort_explicit":           sortExplicit,
			"sort_files":              grepSortFiles(input),
			"truncated":               truncated,
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

func validateGrepIgnoreFile(cwd string, input grepInput) error {
	raw := grepIgnoreFile(input)
	if raw == "" || grepNoIgnoreFiles(input) {
		return nil
	}
	path := resolvePath(cwd, raw)
	if isBlockedDevicePath(path) {
		return fmt.Errorf("cannot read ignore_file %q: this device path would block or produce infinite output", raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("ignore_file does not exist: %s", raw)
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("ignore_file is a directory: %s", raw)
	}
	return nil
}

func grepCustomIgnoreRules(ctx tool.Context, input grepInput) (searchIgnoreRules, error) {
	raw := grepIgnoreFile(input)
	if raw == "" || grepNoIgnoreFiles(input) {
		return nil, nil
	}
	path := resolvePath(ctx.WorkingDirectory, raw)
	if isBlockedDevicePath(path) {
		return nil, fmt.Errorf("cannot read ignore_file %q: this device path would block or produce infinite output", raw)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("ignore_file does not exist: %s", raw)
		}
		return nil, err
	}
	return parseSearchIgnoreRules(string(data), ""), nil
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

func collectGrepMatches(root string, displayRoot string, glob string, iglob string, globCaseInsensitive bool, typeFilter string, typeNotFilter string, expr *regexp.Regexp, options grepOptions, walkOptions searchWalkOptions) ([]grepMatch, int, bool, grepStats, error) {
	typeExtensions, err := grepTypeExtensions(typeFilter)
	if err != nil {
		return nil, 0, false, grepStats{}, err
	}
	typeNotExtensions, err := grepTypeExtensions(typeNotFilter)
	if err != nil {
		return nil, 0, false, grepStats{}, err
	}
	globPatterns := splitGrepGlobPatterns(glob, globCaseInsensitive)
	globPatterns = append(globPatterns, splitGrepGlobPatterns(iglob, true)...)
	var matches []grepMatch
	var stats grepStats
	err = walkSearchFiles(root, walkOptions, func(path string, rel string, info os.FileInfo) error {
		if len(globPatterns) > 0 {
			ok, err := matchAnyGrepGlobPath(globPatterns, rel)
			if err != nil || !ok {
				return err
			}
		}
		if len(typeExtensions) > 0 && !grepTypeMatches(path, typeExtensions) {
			return nil
		}
		if len(typeNotExtensions) > 0 && grepTypeMatches(path, typeNotExtensions) {
			return nil
		}
		displayRel := searchDisplayPath(displayRoot, path, rel)
		if options.HasMaxFilesize && info.Size() > options.MaxFilesize {
			return nil
		}
		if hasBinaryExtension(path) && !options.Text {
			return nil
		}
		if options.Mode == "files" {
			matches = append(matches, grepMatch{Path: displayRel, ModUnix: info.ModTime().UnixNano()})
			return nil
		}
		content, err := readGrepText(path, options.Text, options.Encoding)
		if err != nil {
			return nil
		}
		stats.FilesSearched++
		stats.BytesSearched += info.Size()
		matchOptions := options
		if options.CountMatches {
			matchOptions.OnlyMatching = true
			matchOptions.BeforeContext = 0
			matchOptions.AfterContext = 0
		}
		lineMatches := grepFileMatches(displayRel, content, expr, matchOptions)
		fileMatches, fileMatchedLines := grepStatsForFile(content, expr, matchOptions, lineMatches)
		stats.Matches += fileMatches
		stats.MatchedLines += fileMatchedLines
		if fileMatchedLines > 0 {
			stats.FilesWithMatches++
		}
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
			if options.Mode == "count" && options.IncludeZero {
				matches = append(matches, grepMatch{Path: displayRel, Count: 0, ModUnix: info.ModTime().UnixNano()})
			}
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
		return nil, 0, false, grepStats{}, err
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
	return matches, totalMatches, truncated, stats, nil
}

func grepStatsForFile(content string, expr *regexp.Regexp, options grepOptions, lineMatches []grepMatch) (int, int) {
	matchedLines := countUniqueMatchedLines(lineMatches)
	if options.InvertMatch {
		return 0, matchedLines
	}
	if options.OnlyMatching || options.Vimgrep {
		return countMatchedLines(lineMatches), matchedLines
	}
	view := newGrepContentView(content, options.CRLF, options.NullData)
	if options.Multiline {
		return grepMultilineStats(view, expr, options.MaxCount)
	}
	return grepLineStats(view.Lines, expr, options.MaxCount)
}

func grepLineStats(lines []string, expr *regexp.Regexp, maxCount int) (int, int) {
	matches := 0
	matchedLines := 0
	for _, line := range lines {
		spans := expr.FindAllStringIndex(line, -1)
		if len(spans) == 0 {
			continue
		}
		if maxCount > 0 && matchedLines >= maxCount {
			break
		}
		matches += len(spans)
		matchedLines++
	}
	return matches, matchedLines
}

func grepMultilineStats(view grepContentView, expr *regexp.Regexp, maxCount int) (int, int) {
	if view.MatchContent == "" {
		return 0, 0
	}
	matches := 0
	lines := map[int]bool{}
	for _, span := range expr.FindAllStringIndex(view.MatchContent, -1) {
		if maxCount > 0 && matches >= maxCount {
			break
		}
		matches++
		for i, lineStart := range view.MatchStarts {
			lineEnd := lineStart + len(view.Lines[i])
			lineSpanEnd := lineEnd
			if i < len(view.Lines)-1 {
				lineSpanEnd++
			}
			if grepSpanTouchesLine(span[0], span[1], lineStart, lineEnd, lineSpanEnd) {
				lines[i] = true
			}
		}
	}
	return matches, len(lines)
}

func countUniqueMatchedLines(matches []grepMatch) int {
	lines := map[string]bool{}
	for _, match := range matches {
		if !match.Matched {
			continue
		}
		lines[match.Path+":"+strconv.Itoa(match.Line)] = true
	}
	return len(lines)
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

func readGrepText(path string, allowBinary bool, encoding string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return decodeGrepText(data, allowBinary, encoding, path)
}

func decodeGrepText(data []byte, allowBinary bool, encoding string, path string) (string, error) {
	switch encoding {
	case "", "auto":
		if len(data) >= 3 && bytes.Equal(data[:3], []byte{0xef, 0xbb, 0xbf}) {
			data = data[3:]
			return decodeGrepUTF8(data, allowBinary, path)
		}
		if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
			return decodeGrepUTF16(data[2:], true), nil
		}
		if len(data) >= 2 && data[0] == 0xfe && data[1] == 0xff {
			return decodeGrepUTF16(data[2:], false), nil
		}
		return decodeGrepUTF8(data, allowBinary, path)
	case "none":
		return decodeGrepRaw(data, allowBinary, path)
	case "utf-8":
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		return decodeGrepUTF8(data, allowBinary, path)
	case "utf-16", "utf-16le", "utf-16be":
		littleEndian := encoding == "utf-16le"
		if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
			littleEndian = true
			data = data[2:]
		} else if len(data) >= 2 && data[0] == 0xfe && data[1] == 0xff {
			littleEndian = false
			data = data[2:]
		}
		return decodeGrepUTF16(data, littleEndian), nil
	default:
		return "", fmt.Errorf("encoding must be one of auto, none, utf-8, utf-16, utf-16le, or utf-16be")
	}
}

func decodeGrepUTF8(data []byte, allowBinary bool, path string) (string, error) {
	if !allowBinary && (bytes.ContainsRune(data, 0) || !utf8.Valid(data)) {
		return "", fmt.Errorf("this tool cannot read binary files: %s", path)
	}
	return string(data), nil
}

func decodeGrepRaw(data []byte, allowBinary bool, path string) (string, error) {
	if !allowBinary && (bytes.ContainsRune(data, 0) || !utf8.Valid(data)) {
		return "", fmt.Errorf("this tool cannot read binary files: %s", path)
	}
	return string(data), nil
}

func decodeGrepUTF16(data []byte, littleEndian bool) string {
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if littleEndian {
			units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
		} else {
			units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
		}
	}
	return string(utf16.Decode(units))
}

func grepFileMatches(path string, content string, expr *regexp.Regexp, options grepOptions) []grepMatch {
	view := newGrepContentView(content, options.CRLF, options.NullData)
	if options.OnlyMatching {
		if options.Multiline {
			return grepMultilineOnlyMatches(path, view, expr, options.MaxCount, options.MaxColumns, options.MaxPreview, options.Trim, options)
		}
		return grepLineOnlyMatches(path, view.Lines, view.ByteStarts, expr, options.MaxCount, options.MaxColumns, options.MaxPreview, options.Trim, options)
	}
	matched := map[int]bool{}
	included := map[int]bool{}
	if options.Multiline {
		markMultilineMatches(view, expr, options.MaxCount, options.BeforeContext, options.AfterContext, options.InvertMatch, matched, included)
	} else {
		markLineMatches(view.Lines, expr, options.MaxCount, options.BeforeContext, options.AfterContext, options.InvertMatch, matched, included)
	}
	if options.Passthru {
		for i := range view.Lines {
			included[i] = true
		}
	}
	if options.Vimgrep && !options.InvertMatch {
		return grepVimgrepMatches(path, view.Lines, view.ByteStarts, expr, matched, included, options)
	}
	matches := make([]grepMatch, 0, len(included))
	for i := range view.Lines {
		if !included[i] {
			continue
		}
		column := 0
		if matched[i] && options.ColumnNumbers && !options.InvertMatch {
			if span := expr.FindStringIndex(view.Lines[i]); len(span) == 2 {
				column = span[0] + 1
			}
		}
		matches = append(matches, grepMatch{Path: path, Line: i + 1, Column: column, ByteOffset: view.ByteStarts[i], Text: grepDisplayMatchedLine(expr, view.Lines[i], matched[i], options), Matched: matched[i]})
	}
	return matches
}

func grepVimgrepMatches(path string, lines []string, starts []int, expr *regexp.Regexp, matched map[int]bool, included map[int]bool, options grepOptions) []grepMatch {
	matches := make([]grepMatch, 0, len(included))
	for i := range lines {
		if !included[i] {
			continue
		}
		text := grepDisplayMatchedLine(expr, lines[i], matched[i], options)
		if !matched[i] {
			matches = append(matches, grepMatch{Path: path, Line: i + 1, ByteOffset: starts[i], Text: text, Matched: false})
			continue
		}
		spans := expr.FindAllStringIndex(lines[i], -1)
		if len(spans) == 0 {
			matches = append(matches, grepMatch{Path: path, Line: i + 1, Column: 1, ByteOffset: starts[i], Text: text, Matched: true})
			continue
		}
		for _, span := range spans {
			matches = append(matches, grepMatch{Path: path, Line: i + 1, Column: span[0] + 1, ByteOffset: starts[i] + span[0], Text: text, Matched: true})
		}
	}
	return matches
}

func grepLineOnlyMatches(path string, lines []string, starts []int, expr *regexp.Regexp, maxCount int, maxColumns int, maxPreview bool, trim bool, options grepOptions) []grepMatch {
	var matches []grepMatch
	matchedLines := 0
	for i, line := range lines {
		spans := expr.FindAllStringSubmatchIndex(line, -1)
		if len(spans) == 0 {
			continue
		}
		if maxCount > 0 && matchedLines >= maxCount {
			break
		}
		for _, span := range spans {
			matches = append(matches, grepMatch{
				Path:       path,
				Line:       i + 1,
				Column:     span[0] + 1,
				ByteOffset: starts[i] + span[0],
				Text:       grepDisplayLine(grepMatchedText(expr, line, span, options), true, maxColumns, maxPreview, trim),
				Matched:    true,
			})
		}
		matchedLines++
	}
	return matches
}

func grepMultilineOnlyMatches(path string, view grepContentView, expr *regexp.Regexp, maxCount int, maxColumns int, maxPreview bool, trim bool, options grepOptions) []grepMatch {
	if view.MatchContent == "" {
		return nil
	}
	var matches []grepMatch
	for matchIndex, span := range expr.FindAllStringSubmatchIndex(view.MatchContent, -1) {
		if maxCount > 0 && matchIndex >= maxCount {
			break
		}
		for i, lineStart := range view.MatchStarts {
			lineEnd := lineStart + len(view.Lines[i])
			lineSpanEnd := lineEnd
			if i < len(view.Lines)-1 {
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
			text := view.Lines[i][fragmentStart-lineStart : fragmentEnd-lineStart]
			if options.HasReplace {
				text = grepExpandReplacement(expr, view.MatchContent, span, options.Replace)
			}
			matches = append(matches, grepMatch{
				Path:       path,
				Line:       i + 1,
				Column:     fragmentStart - lineStart + 1,
				ByteOffset: view.ByteStarts[i] + fragmentStart - lineStart,
				Text:       grepDisplayLine(text, true, maxColumns, maxPreview, trim),
				Matched:    true,
			})
		}
	}
	return matches
}

func newGrepContentView(content string, crlf bool, nullData bool) grepContentView {
	if nullData {
		return newGrepDelimitedContentView(content, "\x00")
	}
	if crlf {
		return newGrepCRLFContentView(content)
	}
	matchContent := strings.TrimSuffix(content, "\n")
	lines := strings.Split(matchContent, "\n")
	starts := grepLineStarts(lines)
	return grepContentView{Lines: lines, MatchContent: matchContent, MatchStarts: starts, ByteStarts: starts}
}

func newGrepDelimitedContentView(content string, delimiter string) grepContentView {
	if content == "" {
		return grepContentView{Lines: []string{""}, MatchContent: "", MatchStarts: []int{0}, ByteStarts: []int{0}}
	}
	var lines []string
	var matchStarts []int
	var byteStarts []int
	var match strings.Builder
	appendLine := func(line string, byteStart int) {
		if len(lines) > 0 {
			match.WriteByte('\n')
		}
		matchStarts = append(matchStarts, match.Len())
		byteStarts = append(byteStarts, byteStart)
		lines = append(lines, line)
		match.WriteString(line)
	}
	lineStart := 0
	for {
		index := strings.Index(content[lineStart:], delimiter)
		if index < 0 {
			break
		}
		index += lineStart
		appendLine(content[lineStart:index], lineStart)
		lineStart = index + len(delimiter)
	}
	if lineStart < len(content) {
		appendLine(content[lineStart:], lineStart)
	}
	return grepContentView{Lines: lines, MatchContent: match.String(), MatchStarts: matchStarts, ByteStarts: byteStarts}
}

func newGrepCRLFContentView(content string) grepContentView {
	if content == "" {
		return grepContentView{Lines: []string{""}, MatchContent: "", MatchStarts: []int{0}, ByteStarts: []int{0}}
	}
	var lines []string
	var matchStarts []int
	var byteStarts []int
	var match strings.Builder
	appendLine := func(line string, byteStart int) {
		if len(lines) > 0 {
			match.WriteByte('\n')
		}
		matchStarts = append(matchStarts, match.Len())
		byteStarts = append(byteStarts, byteStart)
		lines = append(lines, line)
		match.WriteString(line)
	}
	lineStart := 0
	for i := 0; i < len(content); {
		if content[i] != '\r' && content[i] != '\n' {
			i++
			continue
		}
		appendLine(content[lineStart:i], lineStart)
		if content[i] == '\r' && i+1 < len(content) && content[i+1] == '\n' {
			i += 2
		} else {
			i++
		}
		lineStart = i
	}
	if lineStart < len(content) {
		appendLine(content[lineStart:], lineStart)
	}
	return grepContentView{Lines: lines, MatchContent: match.String(), MatchStarts: matchStarts, ByteStarts: byteStarts}
}

func grepDisplayMatchedLine(expr *regexp.Regexp, line string, matched bool, options grepOptions) string {
	if matched && options.HasReplace && !options.InvertMatch {
		line = expr.ReplaceAllString(line, options.Replace)
	}
	return grepDisplayLine(line, matched, options.MaxColumns, options.MaxPreview, options.Trim)
}

func grepMatchedText(expr *regexp.Regexp, source string, span []int, options grepOptions) string {
	if options.HasReplace {
		return grepExpandReplacement(expr, source, span, options.Replace)
	}
	return source[span[0]:span[1]]
}

func grepExpandReplacement(expr *regexp.Regexp, source string, span []int, replacement string) string {
	if len(span) < 2 {
		return ""
	}
	return string(expr.ExpandString(nil, replacement, source, span))
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

func markMultilineMatches(view grepContentView, expr *regexp.Regexp, maxCount int, beforeContext int, afterContext int, invert bool, matched map[int]bool, included map[int]bool) {
	if view.MatchContent == "" {
		return
	}
	matches := 0
	spanMatched := map[int]bool{}
	for _, span := range expr.FindAllStringIndex(view.MatchContent, -1) {
		first := -1
		last := -1
		for i, lineStart := range view.MatchStarts {
			lineEnd := lineStart + len(view.Lines[i])
			lineSpanEnd := lineEnd
			if i < len(view.Lines)-1 {
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
				markGrepLineRange(first, last, len(view.Lines), beforeContext, afterContext, matched, included)
				matches++
			}
		}
	}
	if !invert {
		return
	}
	for i := range view.Lines {
		if spanMatched[i] {
			continue
		}
		if maxCount > 0 && matches >= maxCount {
			break
		}
		markGrepLineRange(i, i, len(view.Lines), beforeContext, afterContext, matched, included)
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
	if options.Mode == "content" && options.Heading {
		return formatGrepHeadingMatches(matches, options)
	}
	if grepModeIsFileList(options.Mode) && grepNULTerminatedFileList(options) {
		paths := make([]string, 0, len(matches))
		for _, match := range matches {
			paths = append(paths, grepDisplayPath(match.Path, options))
		}
		if len(paths) == 0 {
			return ""
		}
		return strings.Join(paths, "\x00") + "\x00"
	}
	lines := make([]string, 0, len(matches))
	var previous grepMatch
	hasPrevious := false
	for _, match := range matches {
		switch options.Mode {
		case "files":
			lines = append(lines, grepDisplayPath(match.Path, options))
		case "files_with_matches", "files_without_matches":
			lines = append(lines, grepDisplayPath(match.Path, options))
		case "count":
			if options.WithFilename {
				lines = append(lines, fmt.Sprintf("%s%s%d", grepDisplayPath(match.Path, options), grepPathFieldSeparator(options, ":"), match.Count))
			} else {
				lines = append(lines, fmt.Sprintf("%d", match.Count))
			}
		default:
			if grepNeedsContextSeparator(previous, hasPrevious, match, options, true) {
				lines = append(lines, options.ContextSeparator)
			}
			lines = append(lines, formatGrepContentMatch(match, options))
		}
		previous = match
		hasPrevious = true
	}
	separator := grepRecordSeparator(options)
	content := strings.Join(lines, separator)
	if separator == "\x00" && len(lines) > 0 {
		content += "\x00"
	}
	return content
}

func formatGrepHeadingMatches(matches []grepMatch, options grepOptions) string {
	lines := make([]string, 0, len(matches))
	lineOptions := options
	lineOptions.WithFilename = false
	currentPath := ""
	var previous grepMatch
	hasPrevious := false
	for _, match := range matches {
		if match.Path != currentPath {
			if currentPath != "" {
				lines = append(lines, "")
			}
			currentPath = match.Path
			hasPrevious = false
			if options.WithFilename {
				header := grepDisplayPath(match.Path, options)
				if options.Null {
					lines = append(lines, header+"\x00"+formatGrepContentMatch(match, lineOptions))
					previous = match
					hasPrevious = true
					continue
				}
				lines = append(lines, header)
			}
		}
		if grepNeedsContextSeparator(previous, hasPrevious, match, lineOptions, false) {
			lines = append(lines, lineOptions.ContextSeparator)
		}
		lines = append(lines, formatGrepContentMatch(match, lineOptions))
		previous = match
		hasPrevious = true
	}
	separator := grepRecordSeparator(options)
	content := strings.Join(lines, separator)
	if separator == "\x00" && len(lines) > 0 {
		content += "\x00"
	}
	return content
}

func grepNeedsContextSeparator(previous grepMatch, hasPrevious bool, current grepMatch, options grepOptions, separateFiles bool) bool {
	if !hasPrevious || options.NoContextSeparator || options.Mode != "content" || options.BeforeContext+options.AfterContext <= 0 {
		return false
	}
	if previous.Path != current.Path {
		return separateFiles
	}
	return previous.Line > 0 && current.Line > previous.Line+1
}

func formatGrepContentMatch(match grepMatch, options grepOptions) string {
	separator := options.FieldMatchSeparator
	if !match.Matched {
		separator = options.FieldContextSeparator
	}
	fields := grepContentFields(match, options)
	if options.WithFilename {
		path := grepDisplayPath(match.Path, options)
		pathSeparator := grepPathFieldSeparator(options, separator)
		return path + pathSeparator + strings.Join(fields, separator)
	}
	return strings.Join(fields, separator)
}

func grepContentFields(match grepMatch, options grepOptions) []string {
	fields := make([]string, 0, 4)
	if options.LineNumbers {
		fields = append(fields, strconv.Itoa(match.Line))
	}
	if (options.ColumnNumbers || options.Vimgrep) && match.Matched && match.Column > 0 {
		fields = append(fields, strconv.Itoa(match.Column))
	}
	if options.ByteOffset {
		fields = append(fields, strconv.Itoa(match.ByteOffset))
	}
	fields = append(fields, match.Text)
	return fields
}

func grepPathFieldSeparator(options grepOptions, fallback string) string {
	if options.Null {
		return "\x00"
	}
	return fallback
}

func grepDisplayPath(path string, options grepOptions) string {
	if options.PathSeparator == "" || options.PathSeparator == "/" {
		return path
	}
	return strings.ReplaceAll(path, "/", options.PathSeparator)
}

func formatGrepResultContent(matches []grepMatch, options grepOptions, truncated bool, stats *grepStats) string {
	content := formatGrepMatches(matches, options)
	if stats != nil && options.Mode == "content" {
		stats.BytesPrinted = len([]byte(content))
	}
	switch options.Mode {
	case "content":
		return formatGrepContentResult(content, options, truncated)
	case "count":
		return formatGrepCountResult(content, matches, options, truncated)
	case "files", "files_with_matches", "files_without_matches":
		return formatGrepFilesResult(content, matches, options, truncated)
	default:
		return content
	}
}

func appendGrepStats(content string, stats grepStats) string {
	statsText := formatGrepStats(stats)
	if strings.TrimSpace(content) == "" {
		return statsText
	}
	return content + "\n\n" + statsText
}

func formatGrepStats(stats grepStats) string {
	return fmt.Sprintf("%d matches\n%d matched lines\n%d files contained matches\n%d files searched\n%d bytes printed\n%d bytes searched\n%.6f seconds spent searching\n%.6f seconds total",
		stats.Matches,
		stats.MatchedLines,
		stats.FilesWithMatches,
		stats.FilesSearched,
		stats.BytesPrinted,
		stats.BytesSearched,
		stats.SearchDuration.Seconds(),
		stats.TotalDuration.Seconds(),
	)
}

func grepStructuredStats(stats grepStats, enabled bool) any {
	if !enabled {
		return nil
	}
	return map[string]any{
		"matches":            stats.Matches,
		"matched_lines":      stats.MatchedLines,
		"files_with_matches": stats.FilesWithMatches,
		"files_searched":     stats.FilesSearched,
		"bytes_printed":      stats.BytesPrinted,
		"bytes_searched":     stats.BytesSearched,
		"search_seconds":     stats.SearchDuration.Seconds(),
		"total_seconds":      stats.TotalDuration.Seconds(),
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
	return mode == "files" || mode == "files_with_matches" || mode == "files_without_matches"
}

func grepNULTerminatedFileList(options grepOptions) bool {
	return options.Null || (options.NullData && options.Mode != "files")
}

func grepRecordSeparator(options grepOptions) string {
	if options.NullData && options.Mode != "files" {
		return "\x00"
	}
	return "\n"
}

func pluralWord(count int, singular string, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func grepStructuredMatches(matches []grepMatch, options grepOptions) []map[string]any {
	out := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		item := map[string]any{"path": match.Path}
		switch options.Mode {
		case "content":
			item["line"] = match.Line
			item["text"] = match.Text
			item["matched"] = match.Matched
			if match.Column > 0 {
				item["column"] = match.Column
			}
			if options.ByteOffset {
				item["byte_offset"] = match.ByteOffset
			}
		case "count":
			item["count"] = match.Count
		}
		out = append(out, item)
	}
	return out
}

func normalizedGrepOutputMode(input grepInput) string {
	if grepFiles(input) {
		return "files"
	}
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
	if mode == "files" {
		return "files"
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
	if grepSortFiles(input) {
		return "path", false, true, nil
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

func grepSortFiles(input grepInput) bool {
	return input.SortFiles ||
		input.SortFilesAlt ||
		input.SortFilesDash ||
		input.LongSortFiles
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

func grepPatternFile(input grepInput) string {
	return strings.TrimSpace(firstNonEmpty(input.PatternFile, input.PatternFileAlt, input.PatternFileDash, input.LongPatternFile, input.ShortPatternFile))
}

func grepPatterns(cwd string, input grepInput) ([]string, error) {
	var patterns []string
	if pattern := grepPattern(input); strings.TrimSpace(pattern) != "" {
		patterns = append(patterns, pattern)
	}
	patternFile := grepPatternFile(input)
	if patternFile == "" {
		return patterns, nil
	}
	data, err := readGrepPatternFile(cwd, patternFile)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return patterns, nil
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		patterns = append(patterns, strings.TrimSuffix(line, "\r"))
	}
	return patterns, nil
}

func readGrepPatternFile(cwd string, raw string) ([]byte, error) {
	path := resolvePath(cwd, raw)
	if isBlockedDevicePath(path) {
		return nil, fmt.Errorf("cannot read pattern_file %q: this device path would block or produce infinite output", raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("pattern_file does not exist: %s", raw)
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("pattern_file is a directory: %s", raw)
	}
	return os.ReadFile(path)
}

func compileGrepPattern(input grepInput, patterns []string) (*regexp.Regexp, error) {
	pattern := combinedGrepPattern(patterns)
	if grepFixedStrings(input) {
		quoted := make([]string, 0, len(patterns))
		for _, item := range patterns {
			quoted = append(quoted, regexp.QuoteMeta(item))
		}
		pattern = combinedGrepPattern(quoted)
	}
	lineRegexp := grepLineRegexp(input)
	if lineRegexp {
		pattern = `^(?:` + pattern + `)$`
	} else if grepWordRegexp(input) {
		pattern = `\b(?:` + pattern + `)\b`
	}
	flags := ""
	if grepEffectiveCaseInsensitiveForPattern(input, strings.Join(patterns, "\n")) {
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

func combinedGrepPattern(patterns []string) string {
	if len(patterns) == 1 {
		return patterns[0]
	}
	parts := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		parts = append(parts, "(?:"+pattern+")")
	}
	return strings.Join(parts, "|")
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

func grepIGlobFilter(input grepInput) string {
	if strings.TrimSpace(input.IGlob) != "" {
		return input.IGlob
	}
	return input.LongIGlob
}

func grepGlobCaseInsensitive(input grepInput) bool {
	if input.NoGlobCaseInsensitive ||
		input.NoGlobCaseInsensitiveAlt ||
		input.NoGlobCaseInsensitiveDash ||
		input.LongNoGlobCaseInsensitive {
		return false
	}
	return input.GlobCaseInsensitive ||
		input.GlobCaseInsensitiveAlt ||
		input.GlobCaseInsensitiveDash ||
		input.LongGlobCaseInsensitive
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

func grepTypeNotFilter(input grepInput) string {
	if strings.TrimSpace(input.TypeNot) != "" {
		return input.TypeNot
	}
	if strings.TrimSpace(input.TypeNotAlt) != "" {
		return input.TypeNotAlt
	}
	if strings.TrimSpace(input.TypeNotDash) != "" {
		return input.TypeNotDash
	}
	if strings.TrimSpace(input.LongTypeNot) != "" {
		return input.LongTypeNot
	}
	return input.ShortTypeNot
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

func grepEncoding(input grepInput) (string, error) {
	if grepNoEncoding(input) {
		return "auto", nil
	}
	raw := firstNonEmpty(input.Encoding, input.LongEncoding, input.ShortEncoding)
	if strings.TrimSpace(raw) == "" {
		return "auto", nil
	}
	encoding := strings.ToLower(strings.TrimSpace(raw))
	encoding = strings.ReplaceAll(encoding, "_", "-")
	switch encoding {
	case "auto", "none", "utf-8", "utf8", "utf-16", "utf16", "utf-16le", "utf16le", "utf-16be", "utf16be":
	default:
		return "", fmt.Errorf("encoding must be one of auto, none, utf-8, utf-16, utf-16le, or utf-16be")
	}
	switch encoding {
	case "utf8":
		return "utf-8", nil
	case "utf16":
		return "utf-16", nil
	case "utf16le":
		return "utf-16le", nil
	case "utf16be":
		return "utf-16be", nil
	default:
		return encoding, nil
	}
}

func grepNoEncoding(input grepInput) bool {
	return input.NoEncoding ||
		input.NoEncodingAlt ||
		input.NoEncodingDash ||
		input.LongNoEncoding
}

func grepCRLF(input grepInput) bool {
	if grepNoCRLF(input) {
		return false
	}
	return input.CRLF || input.LongCRLF
}

func grepNoCRLF(input grepInput) bool {
	return input.NoCRLF ||
		input.NoCRLFAlt ||
		input.NoCRLFUpperAlt ||
		input.NoCRLFDash ||
		input.LongNoCRLF
}

func grepNullData(input grepInput) bool {
	if grepNoNullData(input) {
		return false
	}
	return input.NullData ||
		input.NullDataAlt ||
		input.NullDataDash ||
		input.LongNullData
}

func grepNoNullData(input grepInput) bool {
	return input.NoNullData ||
		input.NoNullDataAlt ||
		input.NoNullDataDash ||
		input.LongNoNullData
}

func grepEffectiveCaseInsensitive(input grepInput) bool {
	return grepEffectiveCaseInsensitiveForPattern(input, grepPattern(input))
}

func grepEffectiveCaseInsensitiveForPattern(input grepInput, pattern string) bool {
	if grepCaseSensitive(input) {
		return false
	}
	if grepCaseInsensitive(input) {
		return true
	}
	return grepSmartCase(input) && !grepPatternHasUpper(pattern)
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
	if grepNoText(input) {
		return false
	}
	return input.Text || input.LongText || input.ShortText
}

func grepNoText(input grepInput) bool {
	return input.NoText ||
		input.NoTextAlt ||
		input.NoTextDash ||
		input.LongNoText
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

func grepFiles(input grepInput) bool {
	return input.Files || input.LongFiles
}

func grepTrim(input grepInput) bool {
	if input.NoTrim || input.NoTrimAlt || input.NoTrimDash || input.LongNoTrim {
		return false
	}
	return input.Trim || input.LongTrim
}

func grepStatsEnabled(input grepInput) bool {
	if input.NoStats || input.NoStatsAlt || input.NoStatsDash || input.LongNoStats {
		return false
	}
	return input.Stats || input.LongStats
}

func grepHeading(input grepInput) bool {
	if input.NoHeading || input.NoHeadingAlt || input.NoHeadingDash || input.LongNoHeading {
		return false
	}
	return input.Heading || input.LongHeading
}

func grepPathSeparator(input grepInput) string {
	for _, value := range []string{input.PathSeparator, input.PathSeparatorAlt, input.PathSeparatorDash, input.LongPathSeparator} {
		if value != "" {
			return value
		}
	}
	return ""
}

func grepNull(input grepInput) bool {
	return input.Null || input.LongNull || input.ShortNull
}

func grepFieldMatchSeparator(input grepInput) string {
	if value, ok := firstStringPointer(input.FieldMatchSeparator, input.FieldMatchSeparatorAlt, input.FieldMatchSeparatorDash, input.LongFieldMatchSeparator); ok {
		return value
	}
	return ":"
}

func grepFieldContextSeparator(input grepInput) string {
	if value, ok := firstStringPointer(input.FieldContextSeparator, input.FieldContextSeparatorAlt, input.FieldContextSeparatorDash, input.LongFieldContextSeparator); ok {
		return value
	}
	return "-"
}

func grepContextSeparator(input grepInput) string {
	if value, ok := firstStringPointer(input.ContextSeparator, input.ContextSeparatorAlt, input.ContextSeparatorDash, input.LongContextSeparator); ok {
		return value
	}
	return "--"
}

func grepNoContextSeparator(input grepInput) bool {
	return input.NoContextSeparator ||
		input.NoContextSeparatorAlt ||
		input.NoContextSeparatorDash ||
		input.LongNoContextSeparator
}

func grepByteOffset(input grepInput) bool {
	return input.ByteOffset ||
		input.ByteOffsetAlt ||
		input.ByteOffsetDash ||
		input.LongByteOffset ||
		input.ShortByteOffset
}

func grepIncludeHidden(input grepInput) bool {
	if input.NoHidden || input.NoHiddenAlt || input.NoHiddenDash || input.LongNoHidden {
		return false
	}
	if value, ok := firstBoolPointer(input.Hidden, input.LongHidden); ok {
		return value
	}
	return true
}

func firstBoolPointer(values ...*bool) (bool, bool) {
	for _, value := range values {
		if value != nil {
			return *value, true
		}
	}
	return false, false
}

func firstStringPointer(values ...*string) (string, bool) {
	for _, value := range values {
		if value != nil {
			return *value, true
		}
	}
	return "", false
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
		input.LongFilesWithoutMatches
}

func grepCount(input grepInput) bool {
	return input.Count || input.LongCount || input.ShortCount
}

func grepCountMatches(input grepInput) bool {
	return input.CountMatches || input.CountMatchesAlt || input.CountMatchesDash || input.LongCountMatches
}

func grepIncludeZero(input grepInput) bool {
	return input.IncludeZero || input.IncludeZeroAlt || input.IncludeZeroDash || input.LongIncludeZero
}

func grepNoFollow(input grepInput) bool {
	return input.NoFollow ||
		input.NoFollowAlt ||
		input.NoFollowDash ||
		input.LongNoFollow
}

func grepFollow(input grepInput) bool {
	if grepNoFollow(input) {
		return false
	}
	return input.Follow || input.LongFollow || input.ShortFollow
}

func grepNoIgnore(input grepInput) bool {
	return input.NoIgnore || input.NoIgnoreAlt || input.NoIgnoreDash || input.LongNoIgnore
}

func grepNoIgnoreVCS(input grepInput) bool {
	return input.NoIgnoreVCS ||
		input.NoIgnoreVCSAlt ||
		input.NoIgnoreVCSDash ||
		input.LongNoIgnoreVCS
}

func grepIgnoreVCS(input grepInput) bool {
	if grepNoIgnoreVCS(input) {
		return false
	}
	return true
}

func grepNoIgnoreFiles(input grepInput) bool {
	return input.NoIgnoreFiles ||
		input.NoIgnoreFilesAlt ||
		input.NoIgnoreFilesDash ||
		input.LongNoIgnoreFiles
}

func grepNoIgnoreDot(input grepInput) bool {
	return input.NoIgnoreDot ||
		input.NoIgnoreDotAlt ||
		input.NoIgnoreDotDash ||
		input.LongNoIgnoreDot
}

func grepIgnoreDot(input grepInput) bool {
	if grepNoIgnoreDot(input) {
		return false
	}
	return true
}

func grepIgnoreFile(input grepInput) string {
	return strings.TrimSpace(firstNonEmpty(input.IgnoreFile, input.IgnoreFileAlt, input.IgnoreFileDash, input.LongIgnoreFile))
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

type grepGlobPattern struct {
	Pattern    string
	IgnoreCase bool
}

func splitGrepGlobPatterns(glob string, ignoreCase bool) []grepGlobPattern {
	var patterns []grepGlobPattern
	for _, raw := range strings.Fields(glob) {
		for _, part := range splitGrepGlobToken(raw) {
			if part = strings.TrimSpace(part); part != "" {
				patterns = append(patterns, grepGlobPattern{Pattern: part, IgnoreCase: ignoreCase})
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

func matchAnyGrepGlobPath(patterns []grepGlobPattern, path string) (bool, error) {
	hasPositive := false
	matchedPositive := false
	for _, candidate := range patterns {
		pattern := candidate.Pattern
		negated := strings.HasPrefix(pattern, "!")
		if negated {
			pattern = strings.TrimSpace(strings.TrimPrefix(pattern, "!"))
			if pattern == "" {
				continue
			}
		} else {
			hasPositive = true
		}
		ok, err := matchGrepGlobPath(pattern, path, candidate.IgnoreCase)
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

func matchGrepGlobPath(pattern string, path string, ignoreCase bool) (bool, error) {
	if ignoreCase {
		pattern = strings.ToLower(pattern)
		path = strings.ToLower(path)
	}
	return matchGlobPath(pattern, path)
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

func grepMaxFilesize(input grepInput) (int64, bool, error) {
	raw := firstNonEmptyRaw(input.MaxFilesize, input.MaxFilesizeAlt, input.MaxFilesizeDash, input.LongMaxFilesize)
	if len(raw) == 0 {
		return 0, false, nil
	}
	text, err := scalarJSONText(raw)
	if err != nil {
		return 0, false, fmt.Errorf("max_filesize must be a non-negative integer with optional K, M, or G suffix")
	}
	size, err := parseGrepMaxFilesize(text)
	if err != nil {
		return 0, false, err
	}
	return size, true, nil
}

func firstNonEmptyRaw(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		text := strings.TrimSpace(string(value))
		if text != "" && text != "null" {
			return value
		}
	}
	return nil
}

func scalarJSONText(raw json.RawMessage) (string, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return "", fmt.Errorf("empty value")
	}
	if strings.HasPrefix(text, `"`) {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return "", err
		}
		return strings.TrimSpace(decoded), nil
	}
	return text, nil
}

func parseGrepMaxFilesize(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("max_filesize must be a non-empty size")
	}
	multiplier := int64(1)
	last := raw[len(raw)-1]
	switch last {
	case 'K', 'k':
		multiplier = 1024
		raw = strings.TrimSpace(raw[:len(raw)-1])
	case 'M', 'm':
		multiplier = 1024 * 1024
		raw = strings.TrimSpace(raw[:len(raw)-1])
	case 'G', 'g':
		multiplier = 1024 * 1024 * 1024
		raw = strings.TrimSpace(raw[:len(raw)-1])
	}
	if raw == "" {
		return 0, fmt.Errorf("max_filesize must be a non-empty size")
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("max_filesize must be a non-negative integer with optional K, M, or G suffix")
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("max_filesize is too large")
	}
	if value > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("max_filesize is too large")
	}
	return value * multiplier, nil
}

func validateGrepMaxDepth(input grepInput) error {
	for _, value := range []*int{input.MaxDepth, input.MaxDepthAlt, input.MaxDepthDash, input.LongMaxDepth, input.ShortMaxDepth} {
		if value != nil && *value < 0 {
			return fmt.Errorf("max_depth must be non-negative")
		}
	}
	return nil
}

func grepMaxDepth(input grepInput) int {
	if input.MaxDepth != nil {
		return *input.MaxDepth
	}
	if input.MaxDepthAlt != nil {
		return *input.MaxDepthAlt
	}
	if input.MaxDepthDash != nil {
		return *input.MaxDepthDash
	}
	if input.LongMaxDepth != nil {
		return *input.LongMaxDepth
	}
	if input.ShortMaxDepth != nil {
		return *input.ShortMaxDepth
	}
	return -1
}

func structuredOptionalInt(value int) any {
	if value < 0 {
		return nil
	}
	return value
}

func structuredOptionalInt64(value int64, ok bool) any {
	if !ok {
		return nil
	}
	return value
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

func grepReplacement(input grepInput) (string, bool) {
	if input.Replace != nil {
		return *input.Replace, true
	}
	if input.LongReplace != nil {
		return *input.LongReplace, true
	}
	if input.ShortReplace != nil {
		return *input.ShortReplace, true
	}
	return "", false
}

func grepWithFilename(input grepInput, mode string) bool {
	if mode != "content" && mode != "count" {
		return true
	}
	if input.NoFilename ||
		input.NoFilenameAlt ||
		input.NoFilenameDash ||
		input.LongNoFilename ||
		input.ShortNoFilename {
		return false
	}
	return true
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
	useIgnoreFiles := !envTruthyDefault("CLAUDE_CODE_GLOB_NO_IGNORE", true)
	return searchWalkOptions{
		UseGitIgnoreFiles: useIgnoreFiles,
		UseIgnoreFiles:    useIgnoreFiles,
		IncludeHidden:     envTruthyDefault("CLAUDE_CODE_GLOB_HIDDEN", true),
		MaxDepth:          -1,
		ExtraIgnores:      readDenySearchIgnoreRules(ctx, root),
	}
}

func grepWalkOptions(ctx tool.Context, root string, noIgnore bool, ignoreVCS bool, ignoreDot bool, includeHidden bool, follow bool, maxDepth int, customIgnoreRules searchIgnoreRules) searchWalkOptions {
	return searchWalkOptions{
		UseGitIgnoreFiles: !noIgnore && ignoreVCS,
		UseIgnoreFiles:    !noIgnore && ignoreDot,
		IncludeHidden:     includeHidden,
		SkipSymlinks:      !follow,
		FollowSymlinks:    follow,
		ExcludeVCSDirs:    true,
		MaxDepth:          maxDepth,
		ExtraIgnores:      readDenySearchIgnoreRules(ctx, root),
		CustomIgnoreRules: customIgnoreRules,
		CustomIgnoreRoot:  ctx.WorkingDirectory,
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
	if options.MaxDepth == 0 {
		return nil
	}
	var ignoreRules searchIgnoreRules
	if options.UseGitIgnoreFiles || options.UseIgnoreFiles {
		ignoreRules = loadSearchIgnoreRules(root, "", options)
	}
	seenDirs := map[string]struct{}{}
	if options.FollowSymlinks {
		if key, ok := searchDirectoryKey(root); ok {
			seenDirs[key] = struct{}{}
		}
	}
	return walkSearchDir(root, root, 0, options, ignoreRules, seenDirs, visit)
}

func walkSearchDir(root string, dir string, depth int, options searchWalkOptions, ignoreRules searchIgnoreRules, seenDirs map[string]struct{}, visit func(path string, rel string, info os.FileInfo) error) error {
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
		info, isDir, skip, err := searchEntryInfo(path, entry, options)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		if isDir {
			if (options.ExcludeVCSDirs && ignoredVCSDir(entry.Name())) || searchPathIgnored(path, rel, true, options, ignoreRules) {
				continue
			}
			if options.MaxDepth >= 0 && depth+1 >= options.MaxDepth {
				continue
			}
			if options.FollowSymlinks {
				if key, ok := searchDirectoryKey(path); ok {
					if _, seen := seenDirs[key]; seen {
						continue
					}
					seenDirs[key] = struct{}{}
				}
			}
			dirRules := append(searchIgnoreRules(nil), ignoreRules...)
			if options.UseGitIgnoreFiles || options.UseIgnoreFiles {
				dirRules = append(dirRules, loadSearchIgnoreRules(path, rel, options)...)
			}
			if err := walkSearchDir(root, path, depth+1, options, dirRules, seenDirs, visit); err != nil {
				return err
			}
			continue
		}
		if searchPathIgnored(path, rel, false, options, ignoreRules) {
			continue
		}
		if err := visit(path, rel, info); err != nil {
			return err
		}
	}
	return nil
}

func searchEntryInfo(path string, entry os.DirEntry, options searchWalkOptions) (os.FileInfo, bool, bool, error) {
	if entry.Type()&os.ModeSymlink != 0 {
		if options.FollowSymlinks {
			info, err := os.Stat(path)
			if err != nil {
				return nil, false, false, err
			}
			return info, info.IsDir(), false, nil
		}
		if options.SkipSymlinks {
			return nil, false, true, nil
		}
	}
	info, err := entry.Info()
	if err != nil {
		return nil, false, false, err
	}
	return info, info.IsDir(), false, nil
}

func searchDirectoryKey(path string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", false
	}
	return filepath.Clean(abs), true
}

func searchPathIgnored(path string, rel string, isDir bool, options searchWalkOptions, ignoreRules searchIgnoreRules) bool {
	ignored := len(ignoreRules) > 0 && ignoreRules.Ignored(rel, isDir)
	if len(options.CustomIgnoreRules) > 0 {
		if customRel, ok := searchCustomIgnoreRel(options.CustomIgnoreRoot, path); ok {
			for _, rule := range options.CustomIgnoreRules {
				if rule.Matches(customRel, isDir) {
					ignored = !rule.Negate
				}
			}
		}
	}
	if len(options.ExtraIgnores) > 0 && options.ExtraIgnores.Ignored(rel, isDir) {
		return true
	}
	return ignored
}

func searchCustomIgnoreRel(root string, path string) (string, bool) {
	if strings.TrimSpace(root) == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." {
		return "", false
	}
	return rel, true
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

func loadSearchIgnoreRules(dir string, base string, options searchWalkOptions) searchIgnoreRules {
	var rules searchIgnoreRules
	var names []string
	if options.UseGitIgnoreFiles {
		names = append(names, ".gitignore")
	}
	if options.UseIgnoreFiles {
		names = append(names, ".ignore", ".rgignore")
	}
	for _, name := range names {
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
