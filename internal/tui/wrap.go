package tui

import "strings"

func wrapLine(line string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	runes := []rune(line)
	if len(runes) == 0 {
		return []string{""}
	}
	var out []string
	for len(runes) > width {
		split := width
		for i := width; i > 0; i-- {
			if runes[i-1] == ' ' || runes[i-1] == '\t' {
				split = i
				break
			}
		}
		out = append(out, strings.TrimRight(string(runes[:split]), " \t"))
		runes = []rune(strings.TrimLeft(string(runes[split:]), " \t"))
	}
	out = append(out, string(runes))
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
	runes := []rune(line)
	if len(runes) > width {
		return string(runes[:width])
	}
	if len(runes) < width {
		return line + strings.Repeat(" ", width-len(runes))
	}
	return line
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
	for _, action := range actions {
		if action.Type != TerminalActionText {
			continue
		}
		for _, grapheme := range action.Graphemes {
			if grapheme.Value == "\n" || grapheme.Value == "\r" {
				continue
			}
			if visible > 0 && visible+grapheme.Width > width {
				if !TextStylesEqual(current, DefaultTextStyle()) {
					out.WriteString(CSISequence("0m"))
				}
				return out.String()
			}
			writeTextStyleTransition(&out, &current, action.Style)
			out.WriteString(grapheme.Value)
			visible += grapheme.Width
			if visible >= width {
				if !TextStylesEqual(current, DefaultTextStyle()) {
					out.WriteString(CSISequence("0m"))
				}
				return out.String()
			}
		}
	}
	if !TextStylesEqual(current, DefaultTextStyle()) {
		out.WriteString(CSISequence("0m"))
	}
	return out.String()
}
