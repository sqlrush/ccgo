package tui

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"ccgo/internal/session"
)

const ImageHintPlaceholder = "[Image]"
const killRingMaxSize = 10

type killRingDirection string

const (
	killRingAppend  killRingDirection = "append"
	killRingPrepend killRingDirection = "prepend"
)

type killRingState struct {
	ring              []string
	index             int
	lastActionWasKill bool
	lastActionWasYank bool
	lastYankStart     int
	lastYankLength    int
}

var sharedKillRing killRingState

type PromptState struct {
	Text                string
	Cursor              int
	History             []string
	HistoryIndex        int
	UsePasteReferences  bool
	PastedContents      map[int]session.PastedContent
	NextPastedID        int
	draft               string
	draftPastedContents map[int]session.PastedContent
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
	case "\x1by", "\x1bY":
		return Key{Type: KeyAltY}
	case "\x01":
		return Key{Type: KeyCtrlA}
	case "\x02":
		return Key{Type: KeyCtrlB}
	case "\x03":
		return Key{Type: KeyCtrlC}
	case "\x04":
		return Key{Type: KeyCtrlD}
	case "\x05":
		return Key{Type: KeyCtrlE}
	case "\x06":
		return Key{Type: KeyCtrlF}
	case "\x07":
		return Key{Type: KeyCtrlG}
	case "\x0b":
		return Key{Type: KeyCtrlK}
	case "\x0c":
		return Key{Type: KeyCtrlL}
	case "\x0f":
		return Key{Type: KeyCtrlO}
	case "\x12":
		return Key{Type: KeyCtrlR}
	case "\x13":
		return Key{Type: KeyCtrlS}
	case "\x14":
		return Key{Type: KeyCtrlT}
	case "\x15":
		return Key{Type: KeyCtrlU}
	case "\x17":
		return Key{Type: KeyCtrlW}
	case "\x18":
		return Key{Type: KeyCtrlX}
	case "\x19":
		return Key{Type: KeyCtrlY}
	case "\t":
		return Key{Type: KeyTab}
	case "\x1b[Z":
		return Key{Type: KeyShiftTab}
	case "\x1b[I":
		return Key{Type: KeyFocusIn}
	case "\x1b[O":
		return Key{Type: KeyFocusOut}
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
	sharedKillRing.trackKey(key.Type)
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
		p.insertPaste(key.Text)
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
	case KeyDelete, KeyCtrlD:
		if p.Cursor < len(runes) {
			runes = append(runes[:p.Cursor], runes[p.Cursor+1:]...)
			p.Text = string(runes)
			p.resetHistoryCursor()
		}
	case KeyLeft, KeyCtrlB:
		if p.Cursor > 0 {
			p.Cursor--
		}
	case KeyRight, KeyCtrlF:
		if p.Cursor < len(runes) {
			p.Cursor++
		}
	case KeyHome, KeyCtrlA:
		p.Cursor = 0
	case KeyEnd, KeyCtrlE:
		p.Cursor = len(runes)
	case KeyCtrlK:
		p.deleteToEnd()
	case KeyCtrlU:
		p.deleteToStart()
	case KeyCtrlW:
		p.deleteWordBackward()
	case KeyCtrlY:
		p.yankLastKill()
	case KeyAltY:
		p.yankPop()
	case KeyUp:
		p.historyPrev()
	case KeyDown:
		p.historyNext()
	case KeyEnter:
		display := p.Text
		submitted := p.ExpandedText()
		pastedContents := clonePastedContents(p.PastedContents)
		if display != "" {
			p.History = append(p.History, display)
		}
		p.Text = ""
		p.Cursor = 0
		p.HistoryIndex = len(p.History)
		p.draft = ""
		p.draftPastedContents = nil
		p.resetPastedContents()
		return PromptResult{Submitted: submitted, Display: display, PastedContents: pastedContents}
	case KeyEsc:
		return PromptResult{Cancelled: true}
	case KeyCtrlC:
		return PromptResult{Interrupted: true}
	}
	return PromptResult{}
}

func isPromptKillKey(key KeyType) bool {
	return key == KeyCtrlK || key == KeyCtrlU || key == KeyCtrlW
}

func isPromptYankKey(key KeyType) bool {
	return key == KeyCtrlY || key == KeyAltY
}

func resetSharedKillRingForTesting() {
	sharedKillRing = killRingState{}
}

func (p *PromptState) EnablePasteReferences() {
	p.UsePasteReferences = true
	p.resetPastedContents()
}

func (p PromptState) ExpandedText() string {
	if len(p.PastedContents) == 0 {
		return p.Text
	}
	return session.ExpandPastedTextRefs(p.Text, p.PastedContents)
}

func (p PromptState) HistoryEntry() session.HistoryEntry {
	return session.HistoryEntry{
		Display:        p.Text,
		PastedContents: clonePastedContents(p.PastedContents),
	}
}

func (p *PromptState) insertPaste(text string) {
	if !p.UsePasteReferences || text == "" {
		p.insertText(text)
		return
	}
	if p.PastedContents == nil {
		p.resetPastedContents()
	}
	id := p.NextPastedID
	if id <= 0 {
		id = nextPastedID(p.PastedContents)
	}
	p.NextPastedID = id + 1
	p.PastedContents[id] = session.PastedContent{
		ID:      id,
		Type:    session.PastedContentText,
		Content: text,
	}
	p.insertText(session.FormatPastedTextRef(id, session.PastedTextRefNumLines(text)))
}

func (p *PromptState) resetPastedContents() {
	if !p.UsePasteReferences {
		return
	}
	p.PastedContents = map[int]session.PastedContent{}
	p.NextPastedID = 1
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

func (p *PromptState) pushToKillRing(text string, direction killRingDirection) {
	sharedKillRing.push(text, direction)
}

func (r *killRingState) trackKey(key KeyType) {
	if !isPromptKillKey(key) {
		r.lastActionWasKill = false
	}
	if !isPromptYankKey(key) {
		r.lastActionWasYank = false
	}
}

func (r *killRingState) push(text string, direction killRingDirection) {
	if text == "" {
		return
	}
	if r.lastActionWasKill && len(r.ring) > 0 {
		if direction == killRingPrepend {
			r.ring[0] = text + r.ring[0]
		} else {
			r.ring[0] += text
		}
	} else {
		r.ring = append([]string{text}, r.ring...)
		if len(r.ring) > killRingMaxSize {
			r.ring = r.ring[:killRingMaxSize]
		}
	}
	r.index = 0
	r.lastActionWasKill = true
	r.lastActionWasYank = false
}

func (r killRingState) lastKill() string {
	if len(r.ring) == 0 {
		return ""
	}
	return r.ring[0]
}

func (r *killRingState) recordYank(start int, length int) {
	r.index = 0
	r.lastYankStart = start
	r.lastYankLength = length
	r.lastActionWasYank = true
}

func (r *killRingState) nextYankPop() (text string, start int, length int, ok bool) {
	if !r.lastActionWasYank || len(r.ring) <= 1 {
		return "", 0, 0, false
	}
	r.index = (r.index + 1) % len(r.ring)
	return r.ring[r.index], r.lastYankStart, r.lastYankLength, true
}

func (r *killRingState) updateYankLength(length int) {
	r.lastYankLength = length
	r.lastActionWasYank = true
}

func (p *PromptState) yankLastKill() {
	text := sharedKillRing.lastKill()
	if text == "" {
		return
	}
	start := p.Cursor
	p.insertText(text)
	sharedKillRing.recordYank(start, len([]rune(text)))
}

func (p *PromptState) yankPop() {
	text, start, length, ok := sharedKillRing.nextYankPop()
	if !ok {
		return
	}
	runes := []rune(p.Text)
	if start < 0 {
		start = 0
	}
	if start > len(runes) {
		start = len(runes)
	}
	end := start + length
	if end > len(runes) {
		end = len(runes)
	}
	insert := []rune(text)
	runes = append(runes[:start], append(insert, runes[end:]...)...)
	p.Text = string(runes)
	p.Cursor = start + len(insert)
	sharedKillRing.updateYankLength(len(insert))
	p.resetHistoryCursor()
}

func (p *PromptState) resetHistoryCursor() {
	p.HistoryIndex = len(p.History)
	p.draft = p.Text
	p.draftPastedContents = clonePastedContents(p.PastedContents)
}

func (p *PromptState) historyPrev() {
	if len(p.History) == 0 {
		return
	}
	if p.HistoryIndex == len(p.History) {
		p.draft = p.Text
		p.draftPastedContents = clonePastedContents(p.PastedContents)
	}
	if p.HistoryIndex > 0 {
		p.HistoryIndex--
	}
	p.Text = p.History[p.HistoryIndex]
	p.replacePastedContents(nil)
	p.Cursor = len([]rune(p.Text))
}

func (p *PromptState) historyNext() {
	if p.HistoryIndex >= len(p.History) {
		return
	}
	p.HistoryIndex++
	if p.HistoryIndex == len(p.History) {
		p.Text = p.draft
		p.replacePastedContents(p.draftPastedContents)
	} else {
		p.Text = p.History[p.HistoryIndex]
		p.replacePastedContents(nil)
	}
	p.Cursor = len([]rune(p.Text))
}

func (p *PromptState) replacePastedContents(contents map[int]session.PastedContent) {
	if !p.UsePasteReferences {
		return
	}
	p.PastedContents = clonePastedContents(contents)
	if p.PastedContents == nil {
		p.PastedContents = map[int]session.PastedContent{}
	}
	p.NextPastedID = nextPastedID(p.PastedContents)
}

func clonePastedContents(in map[int]session.PastedContent) map[int]session.PastedContent {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int]session.PastedContent, len(in))
	for id, content := range in {
		out[id] = content
	}
	return out
}

func nextPastedID(contents map[int]session.PastedContent) int {
	next := 1
	for id := range contents {
		if id >= next {
			next = id + 1
		}
	}
	return next
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
