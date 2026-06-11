package filetools

import (
	"fmt"
	"strings"
)

const diffContextLines = 3

type textDiff struct {
	Unified string
	Hunks   []map[string]any
}

func buildTextDiff(path string, oldText string, newText string) textDiff {
	oldLines := diffLines(oldText)
	newLines := diffLines(newText)
	if equalStringSlices(oldLines, newLines) {
		return textDiff{}
	}
	prefix := commonPrefixLines(oldLines, newLines)
	suffix := commonSuffixLines(oldLines[prefix:], newLines[prefix:])
	oldChangeEnd := len(oldLines) - suffix
	newChangeEnd := len(newLines) - suffix
	oldHunkStart := maxInt(prefix-diffContextLines, 0)
	newHunkStart := maxInt(prefix-diffContextLines, 0)
	oldHunkEnd := minInt(oldChangeEnd+diffContextLines, len(oldLines))
	newHunkEnd := minInt(newChangeEnd+diffContextLines, len(newLines))
	lineItems := make([]map[string]any, 0, (oldHunkEnd-oldHunkStart)+(newChangeEnd-newHunkStart))
	var unified strings.Builder
	fmt.Fprintf(&unified, "--- a/%s\n+++ b/%s\n", path, path)
	fmt.Fprintf(&unified, "@@ -%d,%d +%d,%d @@\n", oldHunkStart+1, oldHunkEnd-oldHunkStart, newHunkStart+1, newHunkEnd-newHunkStart)
	for i := oldHunkStart; i < prefix; i++ {
		appendDiffLine(&unified, &lineItems, "context", oldLines[i], i+1, newHunkStart+(i-oldHunkStart)+1)
	}
	for i := prefix; i < oldChangeEnd; i++ {
		appendDiffLine(&unified, &lineItems, "delete", oldLines[i], i+1, 0)
	}
	for i := prefix; i < newChangeEnd; i++ {
		appendDiffLine(&unified, &lineItems, "insert", newLines[i], 0, i+1)
	}
	for i := oldChangeEnd; i < oldHunkEnd; i++ {
		newLine := newChangeEnd + (i - oldChangeEnd) + 1
		appendDiffLine(&unified, &lineItems, "context", oldLines[i], i+1, newLine)
	}
	hunk := map[string]any{
		"old_start": oldHunkStart + 1,
		"old_lines": oldHunkEnd - oldHunkStart,
		"new_start": newHunkStart + 1,
		"new_lines": newHunkEnd - newHunkStart,
		"lines":     lineItems,
	}
	return textDiff{Unified: strings.TrimRight(unified.String(), "\n"), Hunks: []map[string]any{hunk}}
}

func appendDiffLine(unified *strings.Builder, lineItems *[]map[string]any, op string, text string, oldLine int, newLine int) {
	prefix := " "
	switch op {
	case "delete":
		prefix = "-"
	case "insert":
		prefix = "+"
	}
	fmt.Fprintf(unified, "%s%s\n", prefix, text)
	item := map[string]any{"op": op, "text": text}
	if oldLine > 0 {
		item["old_line"] = oldLine
	}
	if newLine > 0 {
		item["new_line"] = newLine
	}
	*lineItems = append(*lineItems, item)
}

func diffLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func equalStringSlices(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func commonPrefixLines(a []string, b []string) int {
	limit := minInt(len(a), len(b))
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return limit
}

func commonSuffixLines(a []string, b []string) int {
	limit := minInt(len(a), len(b))
	for i := 0; i < limit; i++ {
		if a[len(a)-1-i] != b[len(b)-1-i] {
			return i
		}
	}
	return limit
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
