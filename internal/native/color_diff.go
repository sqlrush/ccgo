package native

import (
	"fmt"
	"strings"
)

const (
	diffColorReset = "\x1b[0m"
	diffColorRed   = "\x1b[31m"
	diffColorGreen = "\x1b[32m"
	diffColorCyan  = "\x1b[36m"
)

type ColorDiffOptions struct {
	Path         string
	ContextLines int
	Color        bool
}

type ColorDiff struct {
	Path    string `json:"path,omitempty"`
	Unified string `json:"unified,omitempty"`
	Colored string `json:"colored,omitempty"`
	Changed bool   `json:"changed"`
}

func BuildColorDiff(oldText string, newText string, opts ColorDiffOptions) ColorDiff {
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		path = "file"
	}
	contextLines := opts.ContextLines
	if contextLines < 0 {
		contextLines = 0
	}
	oldLines := diffTextLines(oldText)
	newLines := diffTextLines(newText)
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}
	if prefix == len(oldLines) && prefix == len(newLines) {
		return ColorDiff{Path: path}
	}
	oldChangeEnd := len(oldLines) - suffix
	newChangeEnd := len(newLines) - suffix
	oldStart := maxNativeInt(prefix-contextLines, 0)
	newStart := maxNativeInt(prefix-contextLines, 0)
	oldEnd := minNativeInt(oldChangeEnd+contextLines, len(oldLines))
	newEnd := minNativeInt(newChangeEnd+contextLines, len(newLines))
	unified := renderUnifiedDiff(path, oldLines, newLines, oldStart, oldEnd, newStart, newEnd, prefix, oldChangeEnd, newChangeEnd, false)
	diff := ColorDiff{
		Path:    path,
		Unified: unified,
		Changed: true,
	}
	if opts.Color {
		diff.Colored = renderUnifiedDiff(path, oldLines, newLines, oldStart, oldEnd, newStart, newEnd, prefix, oldChangeEnd, newChangeEnd, true)
	}
	return diff
}

func renderUnifiedDiff(path string, oldLines []string, newLines []string, oldStart int, oldEnd int, newStart int, newEnd int, prefix int, oldChangeEnd int, newChangeEnd int, color bool) string {
	var out strings.Builder
	appendDiffOutputLine(&out, "--- a/"+path, "header", color)
	appendDiffOutputLine(&out, "+++ b/"+path, "header", color)
	appendDiffOutputLine(&out, fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart+1, oldEnd-oldStart, newStart+1, newEnd-newStart), "hunk", color)
	for i := oldStart; i < prefix; i++ {
		appendDiffOutputLine(&out, " "+oldLines[i], "context", color)
	}
	for i := prefix; i < oldChangeEnd; i++ {
		appendDiffOutputLine(&out, "-"+oldLines[i], "delete", color)
	}
	for i := prefix; i < newChangeEnd; i++ {
		appendDiffOutputLine(&out, "+"+newLines[i], "insert", color)
	}
	for i := oldChangeEnd; i < oldEnd; i++ {
		appendDiffOutputLine(&out, " "+oldLines[i], "context", color)
	}
	return strings.TrimRight(out.String(), "\n")
}

func appendDiffOutputLine(out *strings.Builder, line string, kind string, color bool) {
	if !color {
		out.WriteString(line)
		out.WriteByte('\n')
		return
	}
	switch kind {
	case "delete":
		out.WriteString(diffColorRed)
	case "insert":
		out.WriteString(diffColorGreen)
	case "header", "hunk":
		out.WriteString(diffColorCyan)
	}
	out.WriteString(line)
	if kind == "delete" || kind == "insert" || kind == "header" || kind == "hunk" {
		out.WriteString(diffColorReset)
	}
	out.WriteByte('\n')
}

func diffTextLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.TrimSuffix(line, "\n"))
	}
	if len(out) > 0 && out[len(out)-1] == "" && strings.HasSuffix(text, "\n") {
		out = out[:len(out)-1]
	}
	return out
}

func minNativeInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxNativeInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
