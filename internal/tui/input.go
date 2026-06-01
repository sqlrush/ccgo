package tui

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const ImageHintPlaceholder = "[Image]"

type PromptState struct {
	Text         string
	Cursor       int
	History      []string
	HistoryIndex int
	draft        string
}

func NewPromptState(history []string) PromptState {
	return PromptState{
		History:      append([]string(nil), history...),
		HistoryIndex: len(history),
	}
}

func ParseKey(seq string) Key {
	if text, ok := parseBracketedPaste(seq); ok {
		return Key{Type: KeyPaste, Text: text}
	}
	if text, ok := parseImageHint(seq); ok {
		return Key{Type: KeyImageHint, Text: text}
	}
	if key, ok := parseSGRMouse(seq); ok {
		return key
	}
	switch seq {
	case "\r", "\n":
		return Key{Type: KeyEnter}
	case "\x7f", "\b":
		return Key{Type: KeyBackspace}
	case "\x1b":
		return Key{Type: KeyEsc}
	case "\x01":
		return Key{Type: KeyCtrlA}
	case "\x03":
		return Key{Type: KeyCtrlC}
	case "\x05":
		return Key{Type: KeyCtrlE}
	case "\x12":
		return Key{Type: KeyCtrlR}
	case "\t":
		return Key{Type: KeyTab}
	case "\x1b[Z":
		return Key{Type: KeyShiftTab}
	case "\x1b[D":
		return Key{Type: KeyLeft}
	case "\x1b[C":
		return Key{Type: KeyRight}
	case "\x1b[A":
		return Key{Type: KeyUp}
	case "\x1b[B":
		return Key{Type: KeyDown}
	case "\x1b[H", "\x1b[1~":
		return Key{Type: KeyHome}
	case "\x1b[F", "\x1b[4~":
		return Key{Type: KeyEnd}
	case "\x1b[3~":
		return Key{Type: KeyDelete}
	case "\x1b[5~":
		return Key{Type: KeyPageUp}
	case "\x1b[6~":
		return Key{Type: KeyPageDown}
	default:
		r, size := utf8.DecodeRuneInString(seq)
		if r != utf8.RuneError && size == len(seq) && r >= 0x20 {
			return Key{Type: KeyRune, Rune: r}
		}
		return Key{Type: KeyUnknown}
	}
}

func parseSGRMouse(seq string) (Key, bool) {
	if !strings.HasPrefix(seq, "\x1b[<") {
		return Key{}, false
	}
	final := seq[len(seq)-1]
	if final != 'M' && final != 'm' {
		return Key{}, false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b[<"), string(final))
	parts := strings.Split(body, ";")
	if len(parts) != 3 {
		return Key{}, false
	}
	button, err := strconv.Atoi(parts[0])
	if err != nil {
		return Key{}, false
	}
	x, err := strconv.Atoi(parts[1])
	if err != nil {
		return Key{}, false
	}
	y, err := strconv.Atoi(parts[2])
	if err != nil {
		return Key{}, false
	}
	return Key{Type: KeyMouse, MouseButton: button, MouseX: x, MouseY: y, MouseRelease: final == 'm'}, true
}

func (p *PromptState) Apply(key Key) PromptResult {
	runes := []rune(p.Text)
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor > len(runes) {
		p.Cursor = len(runes)
	}
	switch key.Type {
	case KeyRune:
		runes = append(runes[:p.Cursor], append([]rune{key.Rune}, runes[p.Cursor:]...)...)
		p.Cursor++
		p.Text = string(runes)
		p.resetHistoryCursor()
	case KeyPaste:
		p.insertText(key.Text)
	case KeyImageHint:
		text := key.Text
		if text == "" {
			text = ImageHintPlaceholder
		}
		p.insertText(text)
	case KeyBackspace:
		if p.Cursor > 0 {
			runes = append(runes[:p.Cursor-1], runes[p.Cursor:]...)
			p.Cursor--
			p.Text = string(runes)
			p.resetHistoryCursor()
		}
	case KeyDelete:
		if p.Cursor < len(runes) {
			runes = append(runes[:p.Cursor], runes[p.Cursor+1:]...)
			p.Text = string(runes)
			p.resetHistoryCursor()
		}
	case KeyLeft:
		if p.Cursor > 0 {
			p.Cursor--
		}
	case KeyRight:
		if p.Cursor < len(runes) {
			p.Cursor++
		}
	case KeyHome, KeyCtrlA:
		p.Cursor = 0
	case KeyEnd, KeyCtrlE:
		p.Cursor = len(runes)
	case KeyUp:
		p.historyPrev()
	case KeyDown:
		p.historyNext()
	case KeyEnter:
		submitted := p.Text
		if submitted != "" {
			p.History = append(p.History, submitted)
		}
		p.Text = ""
		p.Cursor = 0
		p.HistoryIndex = len(p.History)
		p.draft = ""
		return PromptResult{Submitted: submitted}
	case KeyEsc:
		return PromptResult{Cancelled: true}
	case KeyCtrlC:
		return PromptResult{Interrupted: true}
	}
	return PromptResult{}
}

func (p *PromptState) insertText(text string) {
	if text == "" {
		return
	}
	runes := []rune(p.Text)
	insert := []rune(text)
	runes = append(runes[:p.Cursor], append(insert, runes[p.Cursor:]...)...)
	p.Cursor += len(insert)
	p.Text = string(runes)
	p.resetHistoryCursor()
}

func (p *PromptState) resetHistoryCursor() {
	p.HistoryIndex = len(p.History)
	p.draft = p.Text
}

func (p *PromptState) historyPrev() {
	if len(p.History) == 0 {
		return
	}
	if p.HistoryIndex == len(p.History) {
		p.draft = p.Text
	}
	if p.HistoryIndex > 0 {
		p.HistoryIndex--
	}
	p.Text = p.History[p.HistoryIndex]
	p.Cursor = len([]rune(p.Text))
}

func (p *PromptState) historyNext() {
	if p.HistoryIndex >= len(p.History) {
		return
	}
	p.HistoryIndex++
	if p.HistoryIndex == len(p.History) {
		p.Text = p.draft
	} else {
		p.Text = p.History[p.HistoryIndex]
	}
	p.Cursor = len([]rune(p.Text))
}

func parseBracketedPaste(seq string) (string, bool) {
	const start = "\x1b[200~"
	const end = "\x1b[201~"
	if strings.HasPrefix(seq, start) && strings.HasSuffix(seq, end) {
		return strings.TrimSuffix(strings.TrimPrefix(seq, start), end), true
	}
	return "", false
}

func parseImageHint(seq string) (string, bool) {
	const prefix = "\x1b]1337;File="
	if !strings.HasPrefix(seq, prefix) {
		return "", false
	}
	payload := strings.TrimPrefix(seq, prefix)
	if before, _, ok := strings.Cut(payload, ":"); ok {
		payload = before
	}
	payload = strings.TrimSuffix(payload, "\a")
	name := ""
	for _, field := range strings.Split(payload, ";") {
		if raw, ok := strings.CutPrefix(field, "name="); ok {
			name = strings.TrimSpace(raw)
			break
		}
	}
	if name == "" {
		return ImageHintPlaceholder, true
	}
	return "[Image: " + name + "]", true
}
