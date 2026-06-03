package tui

import (
	"strings"
)

const (
	EnterAlternateScreen    = "\x1b[?1049h"
	ExitAlternateScreen     = "\x1b[?1049l"
	EnableMouseTracking     = "\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1006h"
	DisableMouseTracking    = "\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l"
	EnableFocusEvents       = "\x1b[?1004h"
	DisableFocusEvents      = "\x1b[?1004l"
	EnableBracketedPaste    = "\x1b[?2004h"
	DisableBracketedPaste   = "\x1b[?2004l"
	BeginSynchronizedOutput = "\x1b[?2026h"
	EndSynchronizedOutput   = "\x1b[?2026l"
	EnableKittyKeyboard     = "\x1b[>1u"
	DisableKittyKeyboard    = "\x1b[<u"
	EnableModifyOtherKeys   = "\x1b[>4;2m"
	DisableModifyOtherKeys  = "\x1b[>4m"
	EnableExtendedKeys      = EnableKittyKeyboard + EnableModifyOtherKeys
	DisableExtendedKeys     = DisableModifyOtherKeys + DisableKittyKeyboard
	ReassertExtendedKeys    = DisableKittyKeyboard + EnableKittyKeyboard + EnableModifyOtherKeys
	ClearScreen             = "\x1b[2J"
	ClearScrollback         = "\x1b[3J"
	HomeCursor              = "\x1b[H"
	LegacyWindowsHomeCursor = "\x1b[0f"
	HideCursor              = "\x1b[?25l"
	ShowCursor              = "\x1b[?25h"
)

type Renderer struct {
	Width  int
	Height int
}

type RenderOptions struct {
	SynchronizedOutput bool
}

func ClearTerminalSequence() string {
	return ClearScreen + ClearScrollback + HomeCursor
}

func ClearLegacyWindowsTerminalSequence() string {
	return ClearScreen + LegacyWindowsHomeCursor
}

func NewRenderer(width int, height int) Renderer {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return Renderer{Width: width, Height: height}
}

func (r Renderer) Render(frame Frame) string {
	width := frame.Width
	height := frame.Height
	if width <= 0 {
		width = r.Width
	}
	if height <= 0 {
		height = r.Height
	}
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	promptLines := footerPromptLines(frame, width)
	hiddenPromptLines := 0
	maxPromptLines := height - 1
	if maxPromptLines < 0 {
		maxPromptLines = 0
	}
	if len(promptLines) > maxPromptLines {
		hiddenPromptLines = len(promptLines) - maxPromptLines
		promptLines = promptLines[hiddenPromptLines:]
	}

	bodyHeight := height - 1 - len(promptLines)
	if bodyHeight < 0 {
		bodyHeight = 0
	}

	body := append([]string(nil), frame.BodyLines...)
	if len(body) == 0 {
		body = RenderMessages(frame.Messages, width)
	}
	if frame.Dialog != nil {
		dialog := RenderDialog(*frame.Dialog, width)
		top := bodyHeight - len(dialog)
		if top < 0 {
			top = 0
		}
		if len(body) > top {
			body = body[len(body)-top:]
		}
		body = append(body, dialog...)
	}
	if len(body) > bodyHeight {
		body = body[len(body)-bodyHeight:]
	}
	for len(body) < bodyHeight {
		body = append([]string{""}, body...)
	}

	lines := make([]string, 0, height)
	for _, line := range body {
		lines = append(lines, padOrTrim(line, width))
	}
	lines = append(lines, RenderStatusLine(frame.Status, width))
	lines = append(lines, promptLines...)

	var out strings.Builder
	out.WriteString(HomeCursor)
	out.WriteString(ClearScreen)
	if !frame.ShowCursor {
		out.WriteString(HideCursor)
	} else {
		out.WriteString(ShowCursor)
	}
	out.WriteString(strings.Join(lines, "\r\n"))
	if frame.ShowCursor {
		cursorRow, cursorCol := promptCursorPosition(frame, width, height, len(promptLines), hiddenPromptLines)
		if cursorCol > width {
			cursorCol = width
		}
		out.WriteString("\x1b[")
		out.WriteString(itoa(cursorRow))
		out.WriteString(";")
		out.WriteString(itoa(cursorCol))
		out.WriteString("H")
	}
	return out.String()
}

func (r Renderer) RenderWithOptions(frame Frame, options RenderOptions) string {
	output := r.Render(frame)
	if options.SynchronizedOutput && output != "" {
		return BeginSynchronizedOutput + output + EndSynchronizedOutput
	}
	return output
}

func footerPromptLines(frame Frame, width int) []string {
	if frame.ReverseSearch != nil && frame.ReverseSearch.Active {
		return []string{RenderReverseSearchLine(*frame.ReverseSearch, width)}
	}
	return RenderPromptLines(frame.Prompt, width)
}

func promptCursorPosition(frame Frame, width int, height int, promptLineCount int, hiddenPromptLines int) (int, int) {
	if frame.ReverseSearch != nil && frame.ReverseSearch.Active {
		col := len([]rune("(reverse-i-search) `")) + frame.ReverseSearch.Cursor + 1
		if col < 1 {
			return height, 1
		}
		return height, col
	}
	if promptLineCount <= 0 {
		return height, 1
	}
	layout := layoutPrompt(frame.Prompt, width)
	line := layout.CursorLine - hiddenPromptLines
	col := layout.CursorCol
	if line < 0 {
		line = 0
		col = 0
	}
	if line >= promptLineCount {
		line = promptLineCount - 1
	}
	row := height - promptLineCount + line + 1
	if row < 1 {
		row = 1
	}
	return row, col + 1
}

func RenderOnce(width int, height int, frame Frame) string {
	return NewRenderer(width, height).Render(frame)
}

func RenderOnceWithOptions(width int, height int, frame Frame, options RenderOptions) string {
	return NewRenderer(width, height).RenderWithOptions(frame, options)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	return string(buf[i:])
}
