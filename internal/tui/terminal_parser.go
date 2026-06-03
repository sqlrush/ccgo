package tui

import "unicode/utf8"

type TerminalActionType string

const (
	TerminalActionText      TerminalActionType = "text"
	TerminalActionCursor    TerminalActionType = "cursor"
	TerminalActionErase     TerminalActionType = "erase"
	TerminalActionScroll    TerminalActionType = "scroll"
	TerminalActionMode      TerminalActionType = "mode"
	TerminalActionLink      TerminalActionType = "link"
	TerminalActionTitle     TerminalActionType = "title"
	TerminalActionTabStatus TerminalActionType = "tabStatus"
	TerminalActionBell      TerminalActionType = "bell"
	TerminalActionReset     TerminalActionType = "reset"
	TerminalActionUnknown   TerminalActionType = "unknown"
)

type TerminalGrapheme struct {
	Value string
	Width int
}

type TerminalAction struct {
	Type      TerminalActionType
	Graphemes []TerminalGrapheme
	Style     TextStyle
	Cursor    CSICursorAction
	Erase     CSIEraseAction
	Scroll    CSIScrollAction
	Mode      CSIModeAction
	OSC       OSCAction
	Sequence  string
}

type TerminalParser struct {
	tokenizer *TerminalTokenizer
	style     TextStyle
	inLink    bool
	linkURL   string
}

func NewTerminalParser() *TerminalParser {
	return &TerminalParser{
		tokenizer: NewTerminalTokenizer(TerminalTokenizerOptions{}),
		style:     DefaultTextStyle(),
	}
}

func (p *TerminalParser) Feed(input string) []TerminalAction {
	if p.tokenizer == nil {
		p.tokenizer = NewTerminalTokenizer(TerminalTokenizerOptions{})
	}
	return p.processTokens(p.tokenizer.Feed(input))
}

func (p *TerminalParser) Flush() []TerminalAction {
	if p.tokenizer == nil {
		p.tokenizer = NewTerminalTokenizer(TerminalTokenizerOptions{})
	}
	return p.processTokens(p.tokenizer.Flush())
}

func (p *TerminalParser) Reset() {
	if p.tokenizer == nil {
		p.tokenizer = NewTerminalTokenizer(TerminalTokenizerOptions{})
	} else {
		p.tokenizer.Reset()
	}
	p.style = DefaultTextStyle()
	p.inLink = false
	p.linkURL = ""
}

func (p *TerminalParser) Style() TextStyle {
	return p.style
}

func (p *TerminalParser) InLink() bool {
	return p.inLink
}

func (p *TerminalParser) LinkURL() string {
	return p.linkURL
}

func (p *TerminalParser) processTokens(tokens []TerminalToken) []TerminalAction {
	actions := []TerminalAction{}
	for _, token := range tokens {
		switch token.Type {
		case TerminalTokenText:
			actions = append(actions, p.processText(token.Value)...)
		case TerminalTokenSequence:
			if action, ok := p.processSequence(token.Value); ok {
				actions = append(actions, action)
			}
		}
	}
	return actions
}

func (p *TerminalParser) processText(text string) []TerminalAction {
	actions := []TerminalAction{}
	current := ""
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		if r == utf8.RuneError && size == 0 {
			break
		}
		chunk := text[:size]
		text = text[size:]
		if r == rune(terminalBEL) {
			if current != "" {
				actions = append(actions, TerminalAction{Type: TerminalActionText, Graphemes: terminalGraphemes(current), Style: p.style})
				current = ""
			}
			actions = append(actions, TerminalAction{Type: TerminalActionBell})
			continue
		}
		current += chunk
	}
	if current != "" {
		actions = append(actions, TerminalAction{Type: TerminalActionText, Graphemes: terminalGraphemes(current), Style: p.style})
	}
	return actions
}

func (p *TerminalParser) processSequence(sequence string) (TerminalAction, bool) {
	action, ok := ParseTerminalSequence(sequence)
	if !ok {
		return TerminalAction{}, false
	}
	switch action.Type {
	case TerminalSequenceCSI:
		switch action.CSI.Type {
		case CSIActionSGR:
			p.style = ApplySGR(action.CSI.SGRParams, p.style)
			return TerminalAction{}, false
		case CSIActionCursor:
			return TerminalAction{Type: TerminalActionCursor, Cursor: action.CSI.Cursor}, true
		case CSIActionErase:
			return TerminalAction{Type: TerminalActionErase, Erase: action.CSI.Erase}, true
		case CSIActionScroll:
			return TerminalAction{Type: TerminalActionScroll, Scroll: action.CSI.Scroll}, true
		case CSIActionMode:
			return TerminalAction{Type: TerminalActionMode, Mode: action.CSI.Mode}, true
		default:
			return TerminalAction{Type: TerminalActionUnknown, Sequence: action.CSI.Sequence}, true
		}
	case TerminalSequenceOSC:
		switch action.OSC.Type {
		case OSCActionLink:
			if action.OSC.Hyperlink.End {
				p.inLink = false
				p.linkURL = ""
			} else {
				p.inLink = true
				p.linkURL = action.OSC.Hyperlink.URL
			}
			return TerminalAction{Type: TerminalActionLink, OSC: action.OSC}, true
		case OSCActionTitle:
			return TerminalAction{Type: TerminalActionTitle, OSC: action.OSC}, true
		case OSCActionTabStatus:
			return TerminalAction{Type: TerminalActionTabStatus, OSC: action.OSC}, true
		default:
			return TerminalAction{Type: TerminalActionUnknown, Sequence: action.OSC.Sequence}, true
		}
	case TerminalSequenceESC:
		switch action.ESC.Type {
		case ESCActionCursor:
			return TerminalAction{Type: TerminalActionCursor, Cursor: action.ESC.Cursor}, true
		case ESCActionReset:
			p.Reset()
			return TerminalAction{Type: TerminalActionReset}, true
		default:
			return TerminalAction{Type: TerminalActionUnknown, Sequence: action.ESC.Sequence}, true
		}
	default:
		return TerminalAction{Type: TerminalActionUnknown, Sequence: action.Sequence}, true
	}
}

func terminalGraphemes(text string) []TerminalGrapheme {
	graphemes := []TerminalGrapheme{}
	for len(text) > 0 {
		value, size := nextTerminalGrapheme(text)
		if size == 0 {
			break
		}
		text = text[size:]
		graphemes = append(graphemes, TerminalGrapheme{Value: value, Width: terminalGraphemeStringWidth(value)})
	}
	return graphemes
}

func nextTerminalGrapheme(text string) (string, int) {
	first, size := utf8.DecodeRuneInString(text)
	if first == utf8.RuneError && size == 0 {
		return "", 0
	}
	end := size
	previousWasZWJ := false
	regionalCount := 0
	if isTerminalRegionalIndicator(first) {
		regionalCount = 1
	}
	for end < len(text) {
		r, nextSize := utf8.DecodeRuneInString(text[end:])
		if r == utf8.RuneError && nextSize == 0 {
			break
		}
		if isTerminalCombiningMark(r) || isTerminalVariationSelector(r) || isTerminalEmojiModifier(r) {
			end += nextSize
			previousWasZWJ = false
			continue
		}
		if r == 0x200d {
			end += nextSize
			previousWasZWJ = true
			continue
		}
		if previousWasZWJ {
			end += nextSize
			previousWasZWJ = false
			continue
		}
		if regionalCount == 1 && isTerminalRegionalIndicator(r) {
			end += nextSize
			regionalCount++
			continue
		}
		break
	}
	return text[:end], end
}

func terminalGraphemeStringWidth(grapheme string) int {
	count := 0
	var first rune
	for _, r := range grapheme {
		if count == 0 {
			first = r
		}
		count++
		if count > 1 {
			return 2
		}
	}
	if count == 0 {
		return 1
	}
	if isTerminalEmoji(first) || isTerminalEastAsianWide(first) {
		return 2
	}
	return 1
}

func isTerminalEmoji(r rune) bool {
	return (r >= 0x2600 && r <= 0x26ff) ||
		(r >= 0x2700 && r <= 0x27bf) ||
		(r >= 0x1f300 && r <= 0x1f9ff) ||
		(r >= 0x1fa00 && r <= 0x1faff) ||
		(r >= 0x1f1e0 && r <= 0x1f1ff)
}

func isTerminalEastAsianWide(r rune) bool {
	return (r >= 0x1100 && r <= 0x115f) ||
		(r >= 0x2e80 && r <= 0x9fff) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe1f) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x20000 && r <= 0x2fffd) ||
		(r >= 0x30000 && r <= 0x3fffd)
}

func isTerminalCombiningMark(r rune) bool {
	return (r >= 0x0300 && r <= 0x036f) ||
		(r >= 0x1ab0 && r <= 0x1aff) ||
		(r >= 0x1dc0 && r <= 0x1dff) ||
		(r >= 0x20d0 && r <= 0x20ff) ||
		(r >= 0xfe20 && r <= 0xfe2f)
}

func isTerminalVariationSelector(r rune) bool {
	return (r >= 0xfe00 && r <= 0xfe0f) ||
		(r >= 0xe0100 && r <= 0xe01ef)
}

func isTerminalEmojiModifier(r rune) bool {
	return r >= 0x1f3fb && r <= 0x1f3ff
}

func isTerminalRegionalIndicator(r rune) bool {
	return r >= 0x1f1e0 && r <= 0x1f1ff
}
