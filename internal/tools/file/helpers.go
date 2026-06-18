package filetools

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"ccgo/internal/platform"
)

const (
	fileUnchangedStub = "File unchanged since last read. The content from the earlier Read tool_result in this conversation is still current -- refer to that instead of re-reading."
	staleWriteError   = "File has been modified since read, either by the user or by a linter. Read it again before attempting to write it."
)

var blockedDevicePaths = map[string]struct{}{
	"/dev/zero":    {},
	"/dev/random":  {},
	"/dev/urandom": {},
	"/dev/full":    {},
	"/dev/stdin":   {},
	"/dev/tty":     {},
	"/dev/console": {},
	"/dev/stdout":  {},
	"/dev/stderr":  {},
	"/dev/fd/0":    {},
	"/dev/fd/1":    {},
	"/dev/fd/2":    {},
}

var binaryExtensions = map[string]struct{}{
	".7z": {}, ".a": {}, ".avi": {}, ".bin": {}, ".bmp": {}, ".class": {},
	".dll": {}, ".dmg": {}, ".doc": {}, ".docx": {}, ".exe": {}, ".gif": {},
	".gz": {}, ".ico": {}, ".jar": {}, ".jpeg": {}, ".jpg": {}, ".mov": {},
	".mp3": {}, ".mp4": {}, ".o": {}, ".pdf": {}, ".png": {}, ".so": {},
	".tar": {}, ".webp": {}, ".xls": {}, ".xlsx": {}, ".zip": {},
}

func resolvePath(cwd string, path string) string {
	if path == "" {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		return platform.ExpandPath(path)
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if cwd == "" {
		if current, err := os.Getwd(); err == nil {
			cwd = current
		}
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

func mtimeMillis(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.ModTime().UnixMilli(), nil
}

func readText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		return "", fmt.Errorf("UTF-16 text is not supported yet: %s", path)
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if bytes.ContainsRune(data, 0) || !utf8.Valid(data) {
		return "", fmt.Errorf("this tool cannot read binary files: %s", path)
	}
	return string(data), nil
}

func readTextAllowBinary(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	return string(data), nil
}

func readTextForEdit(path string) (content string, existed bool, crlf bool, mode os.FileMode, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return "", false, false, 0o644, nil
		}
		return "", false, false, 0, statErr
	}
	if info.IsDir() {
		return "", true, false, info.Mode().Perm(), fmt.Errorf("cannot edit directory: %s", path)
	}
	raw, err := readText(path)
	if err != nil {
		return "", true, false, info.Mode().Perm(), err
	}
	crlf = strings.Contains(raw, "\r\n")
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	return normalized, true, crlf, info.Mode().Perm(), nil
}

func writeText(path string, content string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	return platform.AtomicWriteFile(path, []byte(content), mode)
}

func writeNormalizedText(path string, content string, crlf bool, mode os.FileMode) error {
	if crlf {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}
	return writeText(path, content, mode)
}

func selectedLines(content string, offset int, limit *int) (string, int, int) {
	if content == "" {
		return "", 0, 0
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	total := len(lines)
	start := 0
	if offset > 1 {
		start = offset - 1
	}
	if start >= total {
		return "", 0, total
	}
	end := total
	if limit != nil && start+*limit < end {
		end = start + *limit
	}
	return strings.Join(lines[start:end], "\n"), end - start, total
}

func addLineNumbers(content string, startLine int) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = fmt.Sprintf("%d\t%s", startLine+i, line)
	}
	return strings.Join(out, "\n")
}

func fullRead(state ReadFileState) bool {
	return !state.PartialView && (state.Offset == nil || *state.Offset == 1) && state.Limit == nil
}

func isBlockedDevicePath(path string) bool {
	if _, ok := blockedDevicePaths[path]; ok {
		return true
	}
	return strings.HasPrefix(path, "/proc/") &&
		(strings.HasSuffix(path, "/fd/0") || strings.HasSuffix(path, "/fd/1") || strings.HasSuffix(path, "/fd/2"))
}

func hasBinaryExtension(path string) bool {
	_, ok := binaryExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func normalizeQuotes(s string) string {
	replacer := strings.NewReplacer(
		"‘", "'",
		"’", "'",
		"“", `"`,
		"”", `"`,
	)
	return replacer.Replace(s)
}

func findActualString(content string, search string) (string, bool) {
	if strings.Contains(content, search) {
		return search, true
	}
	normalizedContent := normalizeQuotes(content)
	normalizedSearch := normalizeQuotes(search)
	index := strings.Index(normalizedContent, normalizedSearch)
	if index < 0 {
		return "", false
	}
	runes := []rune(content)
	searchRunes := []rune(search)
	normalizedPrefix := []rune(normalizedContent[:index])
	start := len(normalizedPrefix)
	if start+len(searchRunes) > len(runes) {
		return "", false
	}
	return string(runes[start : start+len(searchRunes)]), true
}

func preserveQuoteStyle(oldString string, actualOldString string, newString string) string {
	if oldString == actualOldString {
		return newString
	}
	if strings.ContainsAny(actualOldString, "“”") {
		newString = applyCurlyDoubleQuotes(newString)
	}
	if strings.ContainsAny(actualOldString, "‘’") {
		newString = applyCurlySingleQuotes(newString)
	}
	return newString
}

func applyCurlyDoubleQuotes(s string) string {
	runes := []rune(s)
	var out strings.Builder
	for i, r := range runes {
		if r != '"' {
			out.WriteRune(r)
			continue
		}
		if isOpeningQuoteContext(runes, i) {
			out.WriteRune('“')
		} else {
			out.WriteRune('”')
		}
	}
	return out.String()
}

func applyCurlySingleQuotes(s string) string {
	runes := []rune(s)
	var out strings.Builder
	for i, r := range runes {
		if r != '\'' {
			out.WriteRune(r)
			continue
		}
		if i > 0 && i < len(runes)-1 && isLetter(runes[i-1]) && isLetter(runes[i+1]) {
			out.WriteRune('’')
			continue
		}
		if isOpeningQuoteContext(runes, i) {
			out.WriteRune('‘')
		} else {
			out.WriteRune('’')
		}
	}
	return out.String()
}

func isOpeningQuoteContext(runes []rune, index int) bool {
	if index == 0 {
		return true
	}
	switch runes[index-1] {
	case ' ', '\t', '\n', '\r', '(', '[', '{', '—', '–':
		return true
	default:
		return false
	}
}

func isLetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r > 127
}

func applyEdit(content string, oldString string, newString string, replaceAll bool) string {
	target := oldString
	if newString == "" && !strings.HasSuffix(oldString, "\n") && strings.Contains(content, oldString+"\n") {
		target = oldString + "\n"
	}
	if oldString == "" {
		return newString
	}
	if replaceAll {
		return strings.ReplaceAll(content, target, newString)
	}
	return strings.Replace(content, target, newString, 1)
}
