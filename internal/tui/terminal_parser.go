package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type TerminalActionType string

const (
	TerminalActionText          TerminalActionType = "text"
	TerminalActionCursor        TerminalActionType = "cursor"
	TerminalActionErase         TerminalActionType = "erase"
	TerminalActionEdit          TerminalActionType = "edit"
	TerminalActionReport        TerminalActionType = "report"
	TerminalActionScroll        TerminalActionType = "scroll"
	TerminalActionMode          TerminalActionType = "mode"
	TerminalActionLink          TerminalActionType = "link"
	TerminalActionTitle         TerminalActionType = "title"
	TerminalActionDirectory     TerminalActionType = "directory"
	TerminalActionTabStatus     TerminalActionType = "tabStatus"
	TerminalActionClipboard     TerminalActionType = "clipboard"
	TerminalActionColor         TerminalActionType = "color"
	TerminalActionPalette       TerminalActionType = "palette"
	TerminalActionSpecialColor  TerminalActionType = "specialColor"
	TerminalActionProgress      TerminalActionType = "progress"
	TerminalActionNotification  TerminalActionType = "notification"
	TerminalActionShell         TerminalActionType = "shellIntegration"
	TerminalActionBell          TerminalActionType = "bell"
	TerminalActionReset         TerminalActionType = "reset"
	TerminalActionScreen        TerminalActionType = "screen"
	TerminalActionStringControl TerminalActionType = "stringControl"
	TerminalActionUnknown       TerminalActionType = "unknown"
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
	Edit      CSIEditAction
	Report    CSIReportAction
	Scroll    CSIScrollAction
	Mode      CSIModeAction
	Modes     []CSIModeAction
	Screen    ESCScreenAction
	OSC       OSCAction
	String    TerminalStringControlAction
	Sequence  string
}

type TerminalParser struct {
	tokenizer        *TerminalTokenizer
	style            TextStyle
	inLink           bool
	linkURL          string
	pendingGrapheme  string
	pendingTextStyle TextStyle
}

func NewTerminalParser() *TerminalParser {
	return &TerminalParser{
		tokenizer: NewTerminalOutputTokenizer(),
		style:     DefaultTextStyle(),
	}
}

func (p *TerminalParser) Feed(input string) []TerminalAction {
	if p.tokenizer == nil {
		p.tokenizer = NewTerminalOutputTokenizer()
	}
	return p.processTokens(p.tokenizer.Feed(input))
}

func (p *TerminalParser) Flush() []TerminalAction {
	if p.tokenizer == nil {
		p.tokenizer = NewTerminalOutputTokenizer()
	}
	actions := p.processTokens(p.tokenizer.Flush())
	actions = append(actions, p.flushPendingText()...)
	return actions
}

func (p *TerminalParser) Reset() {
	if p.tokenizer == nil {
		p.tokenizer = NewTerminalOutputTokenizer()
	} else {
		p.tokenizer.Reset()
	}
	p.style = DefaultTextStyle()
	p.inLink = false
	p.linkURL = ""
	p.pendingGrapheme = ""
	p.pendingTextStyle = TextStyle{}
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

func TerminalVisibleText(input string) string {
	parser := NewTerminalParser()
	actions := parser.Feed(input)
	actions = append(actions, parser.Flush()...)
	return TerminalActionsVisibleText(actions)
}

func TerminalVisibleWidth(input string) int {
	parser := NewTerminalParser()
	actions := parser.Feed(input)
	actions = append(actions, parser.Flush()...)
	return TerminalActionsVisibleWidth(actions)
}

func TerminalActionsVisibleText(actions []TerminalAction) string {
	var out strings.Builder
	var last TerminalGrapheme
	hasLast := false
	for _, action := range actions {
		switch action.Type {
		case TerminalActionText:
			for _, grapheme := range action.Graphemes {
				out.WriteString(grapheme.Value)
				if isRepeatableTerminalGrapheme(grapheme) {
					last = grapheme
					hasLast = true
				} else {
					hasLast = false
				}
			}
		case TerminalActionBell:
			out.WriteByte(terminalBEL)
			hasLast = false
		case TerminalActionEdit:
			if action.Edit.Type == CSIEditActionRepeatChars && hasLast && action.Edit.Count > 0 {
				out.WriteString(strings.Repeat(last.Value, action.Edit.Count))
			}
		}
	}
	return out.String()
}

func TerminalActionsVisibleWidth(actions []TerminalAction) int {
	width := 0
	var last TerminalGrapheme
	hasLast := false
	for _, action := range actions {
		switch action.Type {
		case TerminalActionText:
			for _, grapheme := range action.Graphemes {
				if !isTerminalLineBreakGrapheme(grapheme.Value) {
					width += grapheme.Width
				}
				if isRepeatableTerminalGrapheme(grapheme) {
					last = grapheme
					hasLast = true
				} else {
					hasLast = false
				}
			}
		case TerminalActionBell:
			hasLast = false
		case TerminalActionEdit:
			if action.Edit.Type == CSIEditActionRepeatChars && hasLast && action.Edit.Count > 0 {
				width += last.Width * action.Edit.Count
			}
		}
	}
	return width
}

func isRepeatableTerminalGrapheme(grapheme TerminalGrapheme) bool {
	if grapheme.Value == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(grapheme.Value)
	return r >= 0x20 && r != 0x7f
}

func (p *TerminalParser) processTokens(tokens []TerminalToken) []TerminalAction {
	actions := []TerminalAction{}
	for _, token := range tokens {
		switch token.Type {
		case TerminalTokenText:
			actions = append(actions, p.processText(token.Value)...)
		case TerminalTokenSequence:
			actions = append(actions, p.flushPendingText()...)
			if action, ok := p.processSequence(token.Value); ok {
				actions = append(actions, action)
			}
		}
	}
	return actions
}

func (p *TerminalParser) processText(text string) []TerminalAction {
	actions := []TerminalAction{}
	for len(text) > 0 {
		index := strings.IndexByte(text, terminalBEL)
		if index < 0 {
			actions = append(actions, p.processPlainText(text)...)
			break
		}
		if index > 0 {
			actions = append(actions, p.processPlainText(text[:index])...)
		}
		actions = append(actions, p.flushPendingText()...)
		actions = append(actions, TerminalAction{Type: TerminalActionBell})
		text = text[index+1:]
	}
	return actions
}

func (p *TerminalParser) processPlainText(text string) []TerminalAction {
	if text == "" {
		return nil
	}
	style := p.style
	if p.pendingGrapheme != "" {
		text = p.pendingGrapheme + text
		style = p.pendingTextStyle
		p.pendingGrapheme = ""
		p.pendingTextStyle = TextStyle{}
	}
	graphemes := terminalGraphemes(text)
	if len(graphemes) == 0 {
		return nil
	}
	if last := graphemes[len(graphemes)-1]; terminalGraphemeMayContinueAtChunkBoundary(last.Value) {
		p.pendingGrapheme = last.Value
		p.pendingTextStyle = style
		graphemes = graphemes[:len(graphemes)-1]
	}
	if len(graphemes) == 0 {
		return nil
	}
	return []TerminalAction{{Type: TerminalActionText, Graphemes: graphemes, Style: style}}
}

func (p *TerminalParser) flushPendingText() []TerminalAction {
	if p.pendingGrapheme == "" {
		return nil
	}
	grapheme := p.pendingGrapheme
	style := p.pendingTextStyle
	p.pendingGrapheme = ""
	p.pendingTextStyle = TextStyle{}
	return []TerminalAction{{Type: TerminalActionText, Graphemes: terminalGraphemes(grapheme), Style: style}}
}

func terminalGraphemeMayContinueAtChunkBoundary(value string) bool {
	if value == "" {
		return false
	}
	if value == "\r" {
		return true
	}
	if terminalGraphemeIsOnlyPrepend(value) {
		return true
	}
	last, _ := utf8.DecodeLastRuneInString(value)
	if last == 0x200d || isTerminalEmojiModifier(last) || terminalGraphemeCanStartKeycapSequence(value) || terminalGraphemeMayContinueHangul(value) {
		return true
	}
	regionalCount := 0
	hasEmojiTag := false
	for _, r := range value {
		if isTerminalRegionalIndicator(r) {
			regionalCount++
		}
		if r >= 0xe0020 && r <= 0xe007e {
			hasEmojiTag = true
		}
		if r == 0xe007f {
			hasEmojiTag = false
		}
	}
	return regionalCount%2 == 1 || hasEmojiTag
}

func (p *TerminalParser) processSequence(sequence string) (TerminalAction, bool) {
	action, ok := ParseTerminalSequence(sequence)
	if !ok {
		return TerminalAction{}, false
	}
	switch action.Type {
	case TerminalSequenceCSI, TerminalSequenceSS3:
		switch action.CSI.Type {
		case CSIActionSGR:
			p.style = ApplySGR(action.CSI.SGRParams, p.style)
			return TerminalAction{}, false
		case CSIActionCursor:
			return TerminalAction{Type: TerminalActionCursor, Cursor: action.CSI.Cursor}, true
		case CSIActionErase:
			return TerminalAction{Type: TerminalActionErase, Erase: action.CSI.Erase}, true
		case CSIActionEdit:
			return TerminalAction{Type: TerminalActionEdit, Edit: action.CSI.Edit}, true
		case CSIActionReport:
			return TerminalAction{Type: TerminalActionReport, Report: action.CSI.Report}, true
		case CSIActionScroll:
			return TerminalAction{Type: TerminalActionScroll, Scroll: action.CSI.Scroll}, true
		case CSIActionMode:
			return TerminalAction{Type: TerminalActionMode, Mode: action.CSI.Mode, Modes: action.CSI.Modes}, true
		case CSIActionReset:
			p.Reset()
			return TerminalAction{Type: TerminalActionReset}, true
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
		case OSCActionDirectory:
			return TerminalAction{Type: TerminalActionDirectory, OSC: action.OSC}, true
		case OSCActionTabStatus:
			return TerminalAction{Type: TerminalActionTabStatus, OSC: action.OSC}, true
		case OSCActionClipboard:
			return TerminalAction{Type: TerminalActionClipboard, OSC: action.OSC}, true
		case OSCActionColor:
			return TerminalAction{Type: TerminalActionColor, OSC: action.OSC}, true
		case OSCActionPalette:
			return TerminalAction{Type: TerminalActionPalette, OSC: action.OSC}, true
		case OSCActionSpecialColor:
			return TerminalAction{Type: TerminalActionSpecialColor, OSC: action.OSC}, true
		case OSCActionProgress:
			return TerminalAction{Type: TerminalActionProgress, OSC: action.OSC}, true
		case OSCActionNotification:
			return TerminalAction{Type: TerminalActionNotification, OSC: action.OSC}, true
		case OSCActionShell:
			return TerminalAction{Type: TerminalActionShell, OSC: action.OSC}, true
		default:
			return TerminalAction{Type: TerminalActionUnknown, Sequence: action.OSC.Sequence}, true
		}
	case TerminalSequenceESC:
		switch action.ESC.Type {
		case ESCActionCursor:
			return TerminalAction{Type: TerminalActionCursor, Cursor: action.ESC.Cursor}, true
		case ESCActionMode:
			return TerminalAction{Type: TerminalActionMode, Mode: action.ESC.Mode}, true
		case ESCActionReport:
			return TerminalAction{Type: TerminalActionReport, Report: action.ESC.Report}, true
		case ESCActionReset:
			p.Reset()
			return TerminalAction{Type: TerminalActionReset}, true
		case ESCActionScreen:
			return TerminalAction{Type: TerminalActionScreen, Screen: action.ESC.Screen}, true
		case ESCActionCharset, ESCActionCharsetShift:
			return TerminalAction{}, false
		default:
			return TerminalAction{Type: TerminalActionUnknown, Sequence: action.ESC.Sequence}, true
		}
	case TerminalSequenceDCS, TerminalSequenceAPC, TerminalSequencePM, TerminalSequenceSOS:
		return TerminalAction{Type: TerminalActionStringControl, String: action.StringControl}, true
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
	if first == '\r' && end < len(text) {
		if next, nextSize := utf8.DecodeRuneInString(text[end:]); next == '\n' {
			return text[:end+nextSize], end + nextSize
		}
	}
	prependPrefix := isTerminalPrepend(first)
	previousWasZWJ := false
	regionalCount := 0
	if isTerminalRegionalIndicator(first) {
		regionalCount = 1
	}
	hangulClass := terminalHangulClassOf(first)
	for end < len(text) {
		r, nextSize := utf8.DecodeRuneInString(text[end:])
		if r == utf8.RuneError && nextSize == 0 {
			break
		}
		if prependPrefix {
			end += nextSize
			prependPrefix = isTerminalPrepend(r)
			if !prependPrefix {
				hangulClass = terminalHangulClassOf(r)
				if isTerminalRegionalIndicator(r) {
					regionalCount = 1
				}
			}
			previousWasZWJ = false
			continue
		}
		if isTerminalCombiningMark(r) || isTerminalSpacingMark(r) || isTerminalVariationSelector(r) || isTerminalEmojiModifier(r) || isTerminalEmojiTag(r) {
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
		if nextHangulClass := terminalHangulClassOf(r); terminalHangulCanJoin(hangulClass, nextHangulClass) {
			end += nextSize
			hangulClass = terminalHangulJoinedClass(hangulClass, nextHangulClass)
			previousWasZWJ = false
			continue
		}
		break
	}
	return text[:end], end
}

func terminalGraphemeStringWidth(grapheme string) int {
	if isTerminalLineBreakGrapheme(grapheme) {
		return 0
	}
	baseWidth := 1
	hasBase := false
	hasWidePresentation := false
	for _, r := range grapheme {
		if !hasBase && isTerminalPrepend(r) {
			continue
		}
		if !hasBase {
			hasBase = true
			if isTerminalEmoji(r) || isTerminalEastAsianWide(r) || isTerminalHangulWideBase(r) {
				baseWidth = 2
			}
			continue
		}
		if r == 0x200d || isTerminalRegionalIndicator(r) || isTerminalEmojiModifier(r) || isTerminalEmojiTag(r) {
			hasWidePresentation = true
			continue
		}
		if isTerminalVariationSelector(r) {
			if r == 0xfe0f {
				hasWidePresentation = true
			}
			continue
		}
		if isTerminalEmojiKeycapMark(r) {
			hasWidePresentation = true
			continue
		}
		if isTerminalCombiningMark(r) || isTerminalSpacingMark(r) {
			continue
		}
		if isTerminalEmoji(r) || isTerminalEastAsianWide(r) {
			hasWidePresentation = true
		}
	}
	if !hasBase {
		return 0
	}
	if hasWidePresentation {
		return 2
	}
	return baseWidth
}

func terminalGraphemeCanStartKeycapSequence(value string) bool {
	runes := []rune(value)
	if len(runes) == 1 {
		return isTerminalEmojiKeycapBase(runes[0])
	}
	return len(runes) == 2 && isTerminalEmojiKeycapBase(runes[0]) && runes[1] == 0xfe0f
}

func isTerminalLineBreakGrapheme(value string) bool {
	return value == "\n" || value == "\r" || value == "\r\n"
}

func terminalGraphemeIsOnlyPrepend(value string) bool {
	seen := false
	for _, r := range value {
		if !isTerminalPrepend(r) {
			return false
		}
		seen = true
	}
	return seen
}

func terminalGraphemeMayContinueHangul(value string) bool {
	class := terminalHangulNone
	for _, r := range value {
		if next := terminalHangulClassOf(r); next != terminalHangulNone {
			class = next
		}
	}
	return class != terminalHangulNone
}

type terminalHangulClass uint8

const (
	terminalHangulNone terminalHangulClass = iota
	terminalHangulL
	terminalHangulV
	terminalHangulT
	terminalHangulLV
	terminalHangulLVT
)

func terminalHangulClassOf(r rune) terminalHangulClass {
	switch {
	case (r >= 0x1100 && r <= 0x115f) || (r >= 0xa960 && r <= 0xa97c):
		return terminalHangulL
	case (r >= 0x1160 && r <= 0x11a7) || (r >= 0xd7b0 && r <= 0xd7c6):
		return terminalHangulV
	case (r >= 0x11a8 && r <= 0x11ff) || (r >= 0xd7cb && r <= 0xd7fb):
		return terminalHangulT
	case r >= 0xac00 && r <= 0xd7a3:
		if (r-0xac00)%28 == 0 {
			return terminalHangulLV
		}
		return terminalHangulLVT
	default:
		return terminalHangulNone
	}
}

func terminalHangulCanJoin(left, right terminalHangulClass) bool {
	switch left {
	case terminalHangulL:
		return right == terminalHangulL || right == terminalHangulV || right == terminalHangulLV || right == terminalHangulLVT
	case terminalHangulV, terminalHangulLV:
		return right == terminalHangulV || right == terminalHangulT
	case terminalHangulT, terminalHangulLVT:
		return right == terminalHangulT
	default:
		return false
	}
}

func terminalHangulJoinedClass(left, right terminalHangulClass) terminalHangulClass {
	switch {
	case left == terminalHangulL && right == terminalHangulL:
		return terminalHangulL
	case left == terminalHangulL && right == terminalHangulV:
		return terminalHangulV
	case left == terminalHangulL && right == terminalHangulLV:
		return terminalHangulLV
	case left == terminalHangulL && right == terminalHangulLVT:
		return terminalHangulLVT
	case (left == terminalHangulV || left == terminalHangulLV) && right == terminalHangulV:
		return terminalHangulV
	case (left == terminalHangulV || left == terminalHangulLV) && right == terminalHangulT:
		return terminalHangulT
	case (left == terminalHangulT || left == terminalHangulLVT) && right == terminalHangulT:
		return terminalHangulT
	case right != terminalHangulNone:
		return right
	default:
		return left
	}
}

func isTerminalHangulWideBase(r rune) bool {
	class := terminalHangulClassOf(r)
	return class == terminalHangulL || class == terminalHangulLV || class == terminalHangulLVT
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
	return unicode.Is(unicode.Mn, r) ||
		unicode.Is(unicode.Me, r) ||
		(r >= 0x0300 && r <= 0x036f) ||
		(r >= 0x1ab0 && r <= 0x1aff) ||
		(r >= 0x1dc0 && r <= 0x1dff) ||
		(r >= 0x20d0 && r <= 0x20ff) ||
		(r >= 0xfe20 && r <= 0xfe2f)
}

func isTerminalSpacingMark(r rune) bool {
	return unicode.Is(unicode.Mc, r)
}

func isTerminalPrepend(r rune) bool {
	return (r >= 0x0600 && r <= 0x0605) ||
		r == 0x06dd ||
		r == 0x070f ||
		(r >= 0x0890 && r <= 0x0891) ||
		r == 0x08e2 ||
		r == 0x110bd ||
		r == 0x110cd
}

func isTerminalVariationSelector(r rune) bool {
	return (r >= 0xfe00 && r <= 0xfe0f) ||
		(r >= 0xe0100 && r <= 0xe01ef)
}

func isTerminalEmojiModifier(r rune) bool {
	return r >= 0x1f3fb && r <= 0x1f3ff
}

func isTerminalEmojiKeycapBase(r rune) bool {
	return r == '#' || r == '*' || (r >= '0' && r <= '9')
}

func isTerminalEmojiKeycapMark(r rune) bool {
	return r == 0x20e3
}

func isTerminalEmojiTag(r rune) bool {
	return (r >= 0xe0020 && r <= 0xe007e) || r == 0xe007f
}

func isTerminalRegionalIndicator(r rune) bool {
	return r >= 0x1f1e0 && r <= 0x1f1ff
}
