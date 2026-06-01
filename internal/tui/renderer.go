package tui

import (
	"strings"
)

const (
	EnterAlternateScreen = "\x1b[?1049h"
	ExitAlternateScreen  = "\x1b[?1049l"
	ClearScreen          = "\x1b[2J"
	HomeCursor           = "\x1b[H"
	HideCursor           = "\x1b[?25l"
	ShowCursor           = "\x1b[?25h"
)

type Renderer struct {
	Width  int
	Height int
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

	bodyHeight := height - 2
	if frame.Dialog != nil {
		bodyHeight = height - 2
	}
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
	if frame.ReverseSearch != nil && frame.ReverseSearch.Active {
		lines = append(lines, RenderReverseSearchLine(*frame.ReverseSearch, width))
	} else {
		lines = append(lines, RenderPromptLine(frame.Prompt, width))
	}

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
		cursorCol := promptCursorColumn(frame, width)
		if cursorCol > width {
			cursorCol = width
		}
		out.WriteString("\x1b[")
		out.WriteString(itoa(height))
		out.WriteString(";")
		out.WriteString(itoa(cursorCol))
		out.WriteString("H")
	}
	return out.String()
}

func promptCursorColumn(frame Frame, width int) int {
	if frame.ReverseSearch != nil && frame.ReverseSearch.Active {
		col := len([]rune("(reverse-i-search) `")) + len([]rune(frame.ReverseSearch.Query)) + 1
		if col < 1 {
			return 1
		}
		return col
	}
	return 3 + frame.Prompt.Cursor
}

func RenderOnce(width int, height int, frame Frame) string {
	return NewRenderer(width, height).Render(frame)
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
