package tui

import (
	"fmt"
	"strings"
)

const (
	CSIPrefix         = "\x1b["
	CursorLeft        = "\x1b[G"
	CursorSave        = "\x1b[s"
	CursorRestore     = "\x1b[u"
	EraseLine         = "\x1b[2K"
	ResetScrollRegion = "\x1b[r"
)

func CSISequence(args ...any) string {
	if len(args) == 0 {
		return CSIPrefix
	}
	if len(args) == 1 {
		return CSIPrefix + fmt.Sprint(args[0])
	}
	params := make([]string, 0, len(args)-1)
	for _, arg := range args[:len(args)-1] {
		params = append(params, fmt.Sprint(arg))
	}
	return CSIPrefix + strings.Join(params, ";") + fmt.Sprint(args[len(args)-1])
}

func CursorUp(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "A")
}

func CursorDown(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "B")
}

func CursorForward(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "C")
}

func CursorBack(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "D")
}

func CursorToColumn(column int) string {
	return CSISequence(column, "G")
}

func CursorPosition(row int, column int) string {
	return CSISequence(row, column, "H")
}

func CursorMove(x int, y int) string {
	var out strings.Builder
	if x < 0 {
		out.WriteString(CursorBack(-x))
	} else if x > 0 {
		out.WriteString(CursorForward(x))
	}
	if y < 0 {
		out.WriteString(CursorUp(-y))
	} else if y > 0 {
		out.WriteString(CursorDown(y))
	}
	return out.String()
}

func EraseToEndOfLine() string {
	return CSISequence("K")
}

func EraseToStartOfLine() string {
	return CSISequence(1, "K")
}

func EraseLineSequence() string {
	return EraseLine
}

func EraseToEndOfScreen() string {
	return CSISequence("J")
}

func EraseToStartOfScreen() string {
	return CSISequence(1, "J")
}

func EraseScreenSequence() string {
	return ClearScreen
}

func ScrollUp(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "S")
}

func ScrollDown(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "T")
}

func SetScrollRegion(top int, bottom int) string {
	return CSISequence(top, bottom, "r")
}
