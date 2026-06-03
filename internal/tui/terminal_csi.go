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
	PasteStart        = "\x1b[200~"
	PasteEnd          = "\x1b[201~"
	FocusInSequence   = "\x1b[I"
	FocusOutSequence  = "\x1b[O"

	CSIParamStart        = 0x30
	CSIParamEnd          = 0x3f
	CSIIntermediateStart = 0x20
	CSIIntermediateEnd   = 0x2f
	CSIFinalStart        = 0x40
	CSIFinalEnd          = 0x7e
)

type CursorStyle string

const (
	CursorStyleBlock     CursorStyle = "block"
	CursorStyleUnderline CursorStyle = "underline"
	CursorStyleBar       CursorStyle = "bar"
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

func IsCSIParam(b byte) bool {
	return b >= CSIParamStart && b <= CSIParamEnd
}

func IsCSIIntermediate(b byte) bool {
	return b >= CSIIntermediateStart && b <= CSIIntermediateEnd
}

func IsCSIFinal(b byte) bool {
	return b >= CSIFinalStart && b <= CSIFinalEnd
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

func SetCursorStyleSequence(style CursorStyle, blinking bool) string {
	code := 0
	switch style {
	case CursorStyleBlock:
		if blinking {
			code = 1
		} else {
			code = 2
		}
	case CursorStyleUnderline:
		if blinking {
			code = 3
		} else {
			code = 4
		}
	case CursorStyleBar:
		if blinking {
			code = 5
		} else {
			code = 6
		}
	default:
		code = 0
	}
	return CSISequence(fmt.Sprintf("%d q", code))
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

func EraseLinesSequence(n int) string {
	if n <= 0 {
		return ""
	}
	var out strings.Builder
	for i := 0; i < n; i++ {
		out.WriteString(EraseLine)
		if i < n-1 {
			out.WriteString(CursorUp(1))
		}
	}
	out.WriteString(CursorLeft)
	return out.String()
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
