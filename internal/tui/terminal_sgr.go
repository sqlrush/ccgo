package tui

type NamedColor string

const (
	NamedColorBlack         NamedColor = "black"
	NamedColorRed           NamedColor = "red"
	NamedColorGreen         NamedColor = "green"
	NamedColorYellow        NamedColor = "yellow"
	NamedColorBlue          NamedColor = "blue"
	NamedColorMagenta       NamedColor = "magenta"
	NamedColorCyan          NamedColor = "cyan"
	NamedColorWhite         NamedColor = "white"
	NamedColorBrightBlack   NamedColor = "brightBlack"
	NamedColorBrightRed     NamedColor = "brightRed"
	NamedColorBrightGreen   NamedColor = "brightGreen"
	NamedColorBrightYellow  NamedColor = "brightYellow"
	NamedColorBrightBlue    NamedColor = "brightBlue"
	NamedColorBrightMagenta NamedColor = "brightMagenta"
	NamedColorBrightCyan    NamedColor = "brightCyan"
	NamedColorBrightWhite   NamedColor = "brightWhite"
)

var sgrNamedColors = []NamedColor{
	NamedColorBlack,
	NamedColorRed,
	NamedColorGreen,
	NamedColorYellow,
	NamedColorBlue,
	NamedColorMagenta,
	NamedColorCyan,
	NamedColorWhite,
	NamedColorBrightBlack,
	NamedColorBrightRed,
	NamedColorBrightGreen,
	NamedColorBrightYellow,
	NamedColorBrightBlue,
	NamedColorBrightMagenta,
	NamedColorBrightCyan,
	NamedColorBrightWhite,
}

type TerminalColorType string

const (
	TerminalColorDefault TerminalColorType = "default"
	TerminalColorNamed   TerminalColorType = "named"
	TerminalColorIndexed TerminalColorType = "indexed"
	TerminalColorRGB     TerminalColorType = "rgb"
)

type TerminalColor struct {
	Type  TerminalColorType
	Name  NamedColor
	Index int
	RGB   RGBColor
}

type UnderlineStyle string

const (
	UnderlineNone   UnderlineStyle = "none"
	UnderlineSingle UnderlineStyle = "single"
	UnderlineDouble UnderlineStyle = "double"
	UnderlineCurly  UnderlineStyle = "curly"
	UnderlineDotted UnderlineStyle = "dotted"
	UnderlineDashed UnderlineStyle = "dashed"
)

var sgrUnderlineStyles = []UnderlineStyle{
	UnderlineNone,
	UnderlineSingle,
	UnderlineDouble,
	UnderlineCurly,
	UnderlineDotted,
	UnderlineDashed,
}

type TextStyle struct {
	Bold           bool
	Dim            bool
	Italic         bool
	Underline      UnderlineStyle
	Blink          bool
	Inverse        bool
	Hidden         bool
	Strikethrough  bool
	Overline       bool
	Foreground     TerminalColor
	Background     TerminalColor
	UnderlineColor TerminalColor
}

type sgrParam struct {
	Value     *int
	Subparams []int
	Colon     bool
}

type sgrExtendedColor struct {
	Indexed bool
	Index   int
	RGB     RGBColor
}

func DefaultTextStyle() TextStyle {
	return TextStyle{
		Underline:      UnderlineNone,
		Foreground:     TerminalColor{Type: TerminalColorDefault},
		Background:     TerminalColor{Type: TerminalColorDefault},
		UnderlineColor: TerminalColor{Type: TerminalColorDefault},
	}
}

func ApplySGR(paramStr string, style TextStyle) TextStyle {
	params := parseSGRParams(paramStr)
	next := style
	for i := 0; i < len(params); {
		param := params[i]
		code := sgrParamValue(param, 0)
		switch {
		case code == 0:
			next = DefaultTextStyle()
			i++
		case code == 1:
			next.Bold = true
			i++
		case code == 2:
			next.Dim = true
			i++
		case code == 3:
			next.Italic = true
			i++
		case code == 4:
			next.Underline = UnderlineSingle
			if param.Colon && len(param.Subparams) > 0 && param.Subparams[0] >= 0 && param.Subparams[0] < len(sgrUnderlineStyles) {
				next.Underline = sgrUnderlineStyles[param.Subparams[0]]
			}
			i++
		case code == 5 || code == 6:
			next.Blink = true
			i++
		case code == 7:
			next.Inverse = true
			i++
		case code == 8:
			next.Hidden = true
			i++
		case code == 9:
			next.Strikethrough = true
			i++
		case code == 21:
			next.Underline = UnderlineDouble
			i++
		case code == 22:
			next.Bold = false
			next.Dim = false
			i++
		case code == 23:
			next.Italic = false
			i++
		case code == 24:
			next.Underline = UnderlineNone
			i++
		case code == 25:
			next.Blink = false
			i++
		case code == 27:
			next.Inverse = false
			i++
		case code == 28:
			next.Hidden = false
			i++
		case code == 29:
			next.Strikethrough = false
			i++
		case code == 53:
			next.Overline = true
			i++
		case code == 55:
			next.Overline = false
			i++
		case code >= 30 && code <= 37:
			next.Foreground = namedTerminalColor(sgrNamedColors[code-30])
			i++
		case code == 39:
			next.Foreground = TerminalColor{Type: TerminalColorDefault}
			i++
		case code >= 40 && code <= 47:
			next.Background = namedTerminalColor(sgrNamedColors[code-40])
			i++
		case code == 49:
			next.Background = TerminalColor{Type: TerminalColorDefault}
			i++
		case code >= 90 && code <= 97:
			next.Foreground = namedTerminalColor(sgrNamedColors[code-90+8])
			i++
		case code >= 100 && code <= 107:
			next.Background = namedTerminalColor(sgrNamedColors[code-100+8])
			i++
		case code == 38:
			color, ok := parseSGRExtendedColor(params, i)
			if ok {
				next.Foreground = terminalColorFromSGR(color)
				i += sgrExtendedColorParamCount(params[i], color)
			} else {
				i++
			}
		case code == 48:
			color, ok := parseSGRExtendedColor(params, i)
			if ok {
				next.Background = terminalColorFromSGR(color)
				i += sgrExtendedColorParamCount(params[i], color)
			} else {
				i++
			}
		case code == 58:
			color, ok := parseSGRExtendedColor(params, i)
			if ok {
				next.UnderlineColor = terminalColorFromSGR(color)
				i += sgrExtendedColorParamCount(params[i], color)
			} else {
				i++
			}
		case code == 59:
			next.UnderlineColor = TerminalColor{Type: TerminalColorDefault}
			i++
		default:
			i++
		}
	}
	return next
}

func parseSGRParams(paramStr string) []sgrParam {
	if paramStr == "" {
		zero := 0
		return []sgrParam{{Value: &zero}}
	}
	result := []sgrParam{}
	current := sgrParam{}
	num := ""
	inSub := false
	for i := 0; i <= len(paramStr); i++ {
		var c byte
		if i < len(paramStr) {
			c = paramStr[i]
		}
		switch {
		case c == ';' || i == len(paramStr):
			n, ok := sgrParseNumber(num)
			if inSub {
				if ok {
					current.Subparams = append(current.Subparams, n)
				}
			} else if ok {
				current.Value = &n
			}
			result = append(result, current)
			current = sgrParam{}
			num = ""
			inSub = false
		case c == ':':
			n, ok := sgrParseNumber(num)
			if !inSub {
				if ok {
					current.Value = &n
				}
				current.Colon = true
				inSub = true
			} else if ok {
				current.Subparams = append(current.Subparams, n)
			}
			num = ""
		case c >= '0' && c <= '9':
			num += string(c)
		}
	}
	return result
}

func sgrParseNumber(num string) (int, bool) {
	if num == "" {
		return 0, false
	}
	value := 0
	for _, r := range num {
		value = value*10 + int(r-'0')
	}
	return value, true
}

func sgrParamValue(param sgrParam, fallback int) int {
	if param.Value == nil {
		return fallback
	}
	return *param.Value
}

func parseSGRExtendedColor(params []sgrParam, index int) (sgrExtendedColor, bool) {
	param := params[index]
	if param.Colon && len(param.Subparams) >= 1 {
		switch param.Subparams[0] {
		case 5:
			if len(param.Subparams) >= 2 {
				return sgrExtendedColor{Indexed: true, Index: param.Subparams[1]}, true
			}
		case 2:
			if len(param.Subparams) >= 4 {
				offset := 0
				if len(param.Subparams) >= 5 {
					offset = 1
				}
				return sgrExtendedColor{RGB: RGBColor{
					R: param.Subparams[1+offset],
					G: param.Subparams[2+offset],
					B: param.Subparams[3+offset],
				}}, true
			}
		}
	}

	if index+1 >= len(params) {
		return sgrExtendedColor{}, false
	}
	nextValue := params[index+1].Value
	if nextValue == nil {
		return sgrExtendedColor{}, false
	}
	if *nextValue == 5 && index+2 < len(params) && params[index+2].Value != nil {
		return sgrExtendedColor{Indexed: true, Index: *params[index+2].Value}, true
	}
	if *nextValue == 2 && index+4 < len(params) {
		red, green, blue := params[index+2].Value, params[index+3].Value, params[index+4].Value
		if red != nil && green != nil && blue != nil {
			return sgrExtendedColor{RGB: RGBColor{R: *red, G: *green, B: *blue}}, true
		}
	}
	return sgrExtendedColor{}, false
}

func sgrExtendedColorParamCount(param sgrParam, color sgrExtendedColor) int {
	if param.Colon {
		return 1
	}
	if color.Indexed {
		return 3
	}
	return 5
}

func terminalColorFromSGR(color sgrExtendedColor) TerminalColor {
	if color.Indexed {
		return TerminalColor{Type: TerminalColorIndexed, Index: color.Index}
	}
	return TerminalColor{Type: TerminalColorRGB, RGB: color.RGB}
}

func namedTerminalColor(color NamedColor) TerminalColor {
	return TerminalColor{Type: TerminalColorNamed, Name: color}
}

func TerminalColorsEqual(a TerminalColor, b TerminalColor) bool {
	return a == b
}

func TextStylesEqual(a TextStyle, b TextStyle) bool {
	if a.Bold != b.Bold ||
		a.Dim != b.Dim ||
		a.Italic != b.Italic ||
		a.Underline != b.Underline ||
		a.Blink != b.Blink ||
		a.Inverse != b.Inverse ||
		a.Hidden != b.Hidden ||
		a.Strikethrough != b.Strikethrough ||
		a.Overline != b.Overline {
		return false
	}
	return TerminalColorsEqual(a.Foreground, b.Foreground) &&
		TerminalColorsEqual(a.Background, b.Background) &&
		TerminalColorsEqual(a.UnderlineColor, b.UnderlineColor)
}

func ParseSGRSequence(sequence string, style TextStyle) (TextStyle, bool) {
	action, ok := ParseCSISequence(sequence)
	if !ok || action.Type != CSIActionSGR {
		return style, false
	}
	return ApplySGR(action.SGRParams, style), true
}
