package tui

import "unicode/utf8"

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
	default:
		r, size := utf8.DecodeRuneInString(seq)
		if r != utf8.RuneError && size == len(seq) && r >= 0x20 {
			return Key{Type: KeyRune, Rune: r}
		}
		return Key{Type: KeyUnknown}
	}
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
