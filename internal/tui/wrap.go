package tui

import "strings"

func wrapLine(line string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	graphemes := terminalGraphemes(line)
	if len(graphemes) == 0 {
		return []string{""}
	}
	var out []string
	for terminalGraphemesWidth(graphemes) > width {
		split, breakAt := visibleWrapSplit(graphemes, width)
		if split <= 0 {
			split = 1
		}
		if breakAt > 0 {
			split = breakAt
		}
		out = append(out, strings.TrimRight(graphemesString(graphemes[:split]), " \t"))
		graphemes = trimLeftSpaceGraphemes(graphemes[split:])
	}
	out = append(out, graphemesString(graphemes))
	return out
}

func wrapText(text string, width int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, wrapLine(line, width)...)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func padOrTrim(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if strings.ContainsRune(line, rune(terminalESC)) {
		return padOrTrimANSI(line, width)
	}
	visible := terminalGraphemesWidth(terminalGraphemes(line))
	if visible > width {
		return trimPlainVisibleWidth(line, width)
	}
	if visible < width {
		return line + strings.Repeat(" ", width-visible)
	}
	return line
}

func visibleWrapSplit(graphemes []TerminalGrapheme, width int) (int, int) {
	visible := 0
	breakAt := 0
	for i, grapheme := range graphemes {
		if visible > 0 && visible+grapheme.Width > width {
			return i, breakAt
		}
		visible += grapheme.Width
		if isWrapBreakGrapheme(grapheme.Value) {
			breakAt = i + 1
		}
		if visible >= width {
			return i + 1, breakAt
		}
	}
	return len(graphemes), breakAt
}

func isWrapBreakGrapheme(value string) bool {
	return value == " " || value == "\t"
}

func trimLeftSpaceGraphemes(graphemes []TerminalGrapheme) []TerminalGrapheme {
	for len(graphemes) > 0 && isWrapBreakGrapheme(graphemes[0].Value) {
		graphemes = graphemes[1:]
	}
	return graphemes
}

func graphemesString(graphemes []TerminalGrapheme) string {
	var out strings.Builder
	for _, grapheme := range graphemes {
		out.WriteString(grapheme.Value)
	}
	return out.String()
}

func terminalGraphemesWidth(graphemes []TerminalGrapheme) int {
	width := 0
	for _, grapheme := range graphemes {
		if isTerminalLineBreakGrapheme(grapheme.Value) {
			continue
		}
		width += grapheme.Width
	}
	return width
}

func trimPlainVisibleWidth(line string, width int) string {
	if width <= 0 {
		return ""
	}
	graphemes := terminalGraphemes(line)
	var out strings.Builder
	visible := 0
	for _, grapheme := range graphemes {
		if isTerminalLineBreakGrapheme(grapheme.Value) {
			continue
		}
		if visible > 0 && visible+grapheme.Width > width {
			break
		}
		out.WriteString(grapheme.Value)
		visible += grapheme.Width
		if visible >= width {
			break
		}
	}
	return out.String()
}

func padOrTrimANSI(line string, width int) string {
	visible := TerminalVisibleWidth(line)
	if visible < width {
		return line + strings.Repeat(" ", width-visible)
	}
	if visible == width {
		return line
	}
	return trimANSIVisibleWidth(line, width)
}

func trimANSIVisibleWidth(line string, width int) string {
	if width <= 0 {
		return ""
	}
	parser := NewTerminalParser()
	actions := parser.Feed(line)
	actions = append(actions, parser.Flush()...)
	var out strings.Builder
	current := DefaultTextStyle()
	visible := 0
	var last TerminalGrapheme
	lastStyle := DefaultTextStyle()
	hasLast := false
	finish := func() string {
		if !TextStylesEqual(current, DefaultTextStyle()) {
			out.WriteString(CSISequence("0m"))
		}
		return out.String()
	}
	writeGrapheme := func(grapheme TerminalGrapheme, style TextStyle) bool {
		if isTerminalLineBreakGrapheme(grapheme.Value) {
			hasLast = false
			return true
		}
		if visible > 0 && visible+grapheme.Width > width {
			return false
		}
		writeTextStyleTransition(&out, &current, style)
		out.WriteString(grapheme.Value)
		visible += grapheme.Width
		if isRepeatableTerminalGrapheme(grapheme) {
			last = grapheme
			lastStyle = style
			hasLast = true
		} else {
			hasLast = false
		}
		return visible < width
	}
	for _, action := range actions {
		switch action.Type {
		case TerminalActionText:
			for _, grapheme := range action.Graphemes {
				if !writeGrapheme(grapheme, action.Style) {
					return finish()
				}
			}
		case TerminalActionBell:
			hasLast = false
		case TerminalActionEdit:
			if action.Edit.Type == CSIEditActionRepeatChars && hasLast && action.Edit.Count > 0 {
				repeat := last
				style := lastStyle
				for i := 0; i < action.Edit.Count; i++ {
					if !writeGrapheme(repeat, style) {
						return finish()
					}
				}
			}
		}
	}
	return finish()
}
