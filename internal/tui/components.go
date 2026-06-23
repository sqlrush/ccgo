package tui

import (
	"fmt"
	"strings"
)

const reverseSearchPromptPrefix = "(reverse-i-search) `"

type promptLayout struct {
	Lines      []string
	CursorLine int
	CursorCol  int
}

type promptLineChunk struct {
	Text      string
	StartRune int
	EndRune   int
	Graphemes []TerminalGrapheme
}

func RenderMessages(messages []Message, width int) []string {
	var lines []string
	for _, message := range messages {
		prefix := rolePrefix(message.Role)
		bodyWidth := width - len(prefix)
		if bodyWidth < 4 {
			bodyWidth = width
			prefix = ""
		}
		wrapped := renderMessageBodyLines(message.Text, bodyWidth)
		for i, line := range wrapped {
			if i == 0 {
				lines = append(lines, prefix+line)
			} else {
				lines = append(lines, strings.Repeat(" ", len(prefix))+line)
			}
		}
	}
	return lines
}

func renderMessageBodyLines(text string, width int) []string {
	if strings.ContainsRune(text, rune(terminalESC)) {
		return wrapANSIText(text, width)
	}
	return wrapText(text, width)
}

func wrapANSIText(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	parser := NewTerminalParser()
	actions := parser.Feed(text)
	actions = append(actions, parser.Flush()...)
	var lines []string
	var line strings.Builder
	current := DefaultTextStyle()
	lineWidth := 0
	wroteAny := false
	endedWithBreak := false
	var last TerminalGrapheme
	lastStyle := DefaultTextStyle()
	hasLast := false
	finishLine := func() {
		if !TextStylesEqual(current, DefaultTextStyle()) {
			line.WriteString(CSISequence("0m"))
			current = DefaultTextStyle()
		}
		lines = append(lines, line.String())
		line.Reset()
		lineWidth = 0
		wroteAny = true
	}
	writeGrapheme := func(grapheme TerminalGrapheme, style TextStyle) {
		if lineWidth > 0 && lineWidth+grapheme.Width > width {
			finishLine()
		}
		endedWithBreak = false
		writeTextStyleTransition(&line, &current, style)
		line.WriteString(grapheme.Value)
		lineWidth += grapheme.Width
		wroteAny = true
		if isRepeatableTerminalGrapheme(grapheme) {
			last = grapheme
			lastStyle = style
			hasLast = true
		} else {
			hasLast = false
		}
	}
	for _, action := range actions {
		switch action.Type {
		case TerminalActionText:
			for _, grapheme := range action.Graphemes {
				if grapheme.Value == "\n" || grapheme.Value == "\r\n" {
					finishLine()
					endedWithBreak = true
					hasLast = false
					continue
				}
				writeGrapheme(grapheme, action.Style)
			}
		case TerminalActionBell:
			line.WriteByte(terminalBEL)
			wroteAny = true
			endedWithBreak = false
			hasLast = false
		case TerminalActionEdit:
			if action.Edit.Type == CSIEditActionRepeatChars && hasLast && action.Edit.Count > 0 {
				repeat := last
				style := lastStyle
				for i := 0; i < action.Edit.Count; i++ {
					writeGrapheme(repeat, style)
				}
			}
		}
	}
	if !wroteAny || line.Len() > 0 || endedWithBreak {
		finishLine()
	}
	return lines
}

func writeTextStyleTransition(out *strings.Builder, current *TextStyle, next TextStyle) {
	if TextStylesEqual(*current, next) {
		return
	}
	if TextStylesEqual(next, DefaultTextStyle()) {
		out.WriteString(CSISequence("0m"))
		*current = next
		return
	}
	out.WriteString(TextStyleSGRSequence(next))
	*current = next
}

func RenderStatusLine(status string, width int) string {
	return RenderStatusLineWithTheme(status, width, "")
}

// RenderStatusLineWithCommand renders the status bar incorporating custom command
// output. When cmdOutput is non-empty, it is appended to the status string before
// rendering. This supports the statusLine setting (type:"command") from CC.
// CC ref: utils/settings/types.ts statusLine:{type:"command",command:string}.
func RenderStatusLineWithCommand(cmdOutput string, width int, theme string) string {
	if strings.TrimSpace(cmdOutput) == "" {
		return RenderStatusLineWithTheme("", width, theme)
	}
	return RenderStatusLineWithTheme(strings.TrimSpace(cmdOutput), width, theme)
}

// sessionColorANSI maps a session color name to an ANSI SGR sequence.
// CC ref: src/tools/AgentTool/agentColorManager.ts AGENT_COLORS list.
func sessionColorANSI(color string) string {
	switch strings.ToLower(strings.TrimSpace(color)) {
	case "blue":
		return "\x1b[34m"
	case "green":
		return "\x1b[32m"
	case "yellow":
		return "\x1b[33m"
	case "red":
		return "\x1b[31m"
	case "cyan":
		return "\x1b[36m"
	case "magenta":
		return "\x1b[35m"
	case "orange":
		return "\x1b[38;5;214m"
	case "pink":
		return "\x1b[38;5;213m"
	case "purple":
		return "\x1b[35m"
	case "white":
		return "\x1b[37m"
	default:
		return ""
	}
}

// RenderStatusLineWithSessionColor renders the status bar with optional session
// colour indicator. When sessionColor is non-empty and recognised, a small
// coloured bullet is prepended to the status text.
// CC ref: src/commands/color/color.ts — /color sets agent session colour.
func RenderStatusLineWithSessionColor(status string, width int, theme string, sessionColor string) string {
	if strings.TrimSpace(sessionColor) != "" {
		ansi := sessionColorANSI(sessionColor)
		if ansi != "" {
			indicator := ansi + "●\x1b[0m "
			if strings.TrimSpace(status) == "" {
				status = "ready"
			}
			return RenderStatusLineWithTheme(indicator+strings.TrimSpace(status), width, theme)
		}
	}
	return RenderStatusLineWithTheme(status, width, theme)
}

// RenderStatusLineWithTheme renders the status bar with ANSI styling chosen by theme.
// Dark (or empty/default) → reverse-video (standard dark terminal appearance).
// Light → bold + underline (legible on light backgrounds without color inversion).
// CC ref: utils/settings/types.ts theme — controls colour palette.
func RenderStatusLineWithTheme(status string, width int, theme string) string {
	if strings.TrimSpace(status) == "" {
		status = "ready"
	}
	padded := padOrTrim(" "+status, width)
	switch theme {
	case "light", "light-daltonism":
		// Bold + underline for light backgrounds — avoids color inversion which
		// renders poorly on white/pale terminals.
		return "\x1b[1;4m" + padded + "\x1b[0m"
	default:
		// Dark (dark, dark-daltonism, or any unrecognised value) → reverse-video.
		return reverseVideo(padded)
	}
}

func RenderPromptLine(prompt PromptState, width int) string {
	layout := layoutPrompt(prompt, width)
	if len(layout.Lines) == 0 {
		return padOrTrim("> ", width)
	}
	return layout.Lines[0]
}

func RenderPromptLines(prompt PromptState, width int) []string {
	return layoutPrompt(prompt, width).Lines
}

func layoutPrompt(prompt PromptState, width int) promptLayout {
	prefix := "> "
	continuation := "  "
	rawLines := strings.Split(prompt.Text, "\n")
	if len(rawLines) == 0 {
		rawLines = []string{""}
	}
	cursorLine, cursorCol := promptCursorLogicalPosition(prompt)
	layout := promptLayout{}
	for index, line := range rawLines {
		linePrefix := continuation
		if index == 0 {
			linePrefix = prefix
		}
		contentWidth := width - TerminalVisibleWidth(linePrefix)
		if contentWidth < 1 {
			contentWidth = 1
		}
		chunks := promptLineChunks(line, contentWidth)
		for chunkIndex, chunk := range chunks {
			chunkPrefix := linePrefix
			if chunkIndex > 0 {
				chunkPrefix = continuation
			}
			if index == cursorLine {
				lastChunk := chunkIndex == len(chunks)-1
				if cursorCol >= chunk.StartRune && (cursorCol < chunk.EndRune || (lastChunk && cursorCol == chunk.EndRune)) {
					layout.CursorLine = len(layout.Lines)
					layout.CursorCol = TerminalVisibleWidth(chunkPrefix) + promptChunkCursorColumn(chunk, cursorCol)
				}
			}
			layout.Lines = append(layout.Lines, padOrTrim(chunkPrefix+chunk.Text, width))
		}
	}
	return layout
}

func promptLineChunks(line string, width int) []promptLineChunk {
	if width <= 0 {
		return []promptLineChunk{{}}
	}
	graphemes := terminalGraphemes(line)
	if len(graphemes) == 0 {
		return []promptLineChunk{{}}
	}
	var chunks []promptLineChunk
	startRune := 0
	endRune := 0
	visible := 0
	current := []TerminalGrapheme{}
	for _, grapheme := range graphemes {
		graphemeRunes := len([]rune(grapheme.Value))
		if len(current) > 0 && visible+grapheme.Width > width {
			chunks = append(chunks, promptLineChunk{
				Text:      graphemesString(current),
				StartRune: startRune,
				EndRune:   endRune,
				Graphemes: append([]TerminalGrapheme(nil), current...),
			})
			startRune = endRune
			current = current[:0]
			visible = 0
		}
		current = append(current, grapheme)
		visible += grapheme.Width
		endRune += graphemeRunes
	}
	if len(current) > 0 {
		chunks = append(chunks, promptLineChunk{
			Text:      graphemesString(current),
			StartRune: startRune,
			EndRune:   endRune,
			Graphemes: append([]TerminalGrapheme(nil), current...),
		})
	}
	return chunks
}

func promptChunkCursorColumn(chunk promptLineChunk, cursorCol int) int {
	col := 0
	runeIndex := chunk.StartRune
	for _, grapheme := range chunk.Graphemes {
		nextRuneIndex := runeIndex + len([]rune(grapheme.Value))
		if cursorCol <= runeIndex {
			return col
		}
		if cursorCol < nextRuneIndex {
			return col + grapheme.Width
		}
		col += grapheme.Width
		runeIndex = nextRuneIndex
	}
	return col
}

func promptCursorLogicalPosition(prompt PromptState) (int, int) {
	runes := []rune(prompt.Text)
	cursor := prompt.Cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	line := 0
	col := 0
	for i := 0; i < cursor; i++ {
		if runes[i] == '\n' {
			line++
			col = 0
			continue
		}
		col++
	}
	return line, col
}

func RenderReverseSearchLine(state ReverseSearchState, width int) string {
	current, ok := state.Current()
	if !ok {
		current = "no matches"
	}
	line := reverseSearchPromptPrefix + state.Query + "': " + current
	return padOrTrim(line, width)
}

func RenderDialog(dialog Dialog, width int) []string {
	if width < 4 {
		return nil
	}
	inner := width - 4
	var lines []string
	title := strings.TrimSpace(dialog.Title)
	if title == "" {
		title = "Dialog"
	}
	lines = append(lines, "+"+strings.Repeat("-", width-2)+"+")
	lines = append(lines, "| "+padOrTrim(title, inner)+" |")
	lines = append(lines, "| "+padOrTrim("", inner)+" |")
	for _, line := range wrapText(dialog.Body, inner) {
		lines = append(lines, "| "+padOrTrim(line, inner)+" |")
	}
	if len(dialog.Actions) > 0 {
		lines = append(lines, "| "+padOrTrim("", inner)+" |")
		var actions []string
		for i, action := range dialog.Actions {
			label := fmt.Sprintf(" %s ", action)
			if i == dialog.Focused {
				label = "[" + action + "]"
			}
			actions = append(actions, label)
		}
		lines = append(lines, "| "+padOrTrim(strings.Join(actions, " "), inner)+" |")
	}
	lines = append(lines, "+"+strings.Repeat("-", width-2)+"+")
	return lines
}

func rolePrefix(role Role) string {
	switch role {
	case RoleUser:
		return "user: "
	case RoleAssistant:
		return "assistant: "
	case RoleTool:
		return "tool: "
	case RoleSystem:
		return "system: "
	default:
		return ""
	}
}

func reverseVideo(text string) string {
	return "\x1b[7m" + text + "\x1b[0m"
}
