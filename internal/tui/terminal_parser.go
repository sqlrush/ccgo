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
}

func (p *TerminalParser) Style() TextStyle {
	return p.style
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
		r, size := utf8.DecodeRuneInString(text)
		if r == utf8.RuneError && size == 0 {
			break
		}
		value := text[:size]
		text = text[size:]
		graphemes = append(graphemes, TerminalGrapheme{Value: value, Width: terminalGraphemeWidth(r)})
	}
	return graphemes
}

func terminalGraphemeWidth(r rune) int {
	if isTerminalEmoji(r) || isTerminalEastAsianWide(r) {
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
