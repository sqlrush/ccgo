package tui

import (
	"encoding/base64"
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
	HistoryEntries      []session.HistoryEntry
	HistoryIndex        int
	UsePasteReferences  bool
	PastedContents      map[int]session.PastedContent
	NextPastedID        int
	draft               string
	draftPastedContents map[int]session.PastedContent
}

func NewPromptState(history []string) PromptState {
	entries := make([]session.HistoryEntry, 0, len(history))
	for _, display := range history {
		entries = append(entries, session.HistoryEntry{Display: display})
	}
	return NewPromptStateFromEntries(entries)
}

func NewPromptStateFromEntries(entries []session.HistoryEntry) PromptState {
	historyEntries := cloneHistoryEntries(entries)
	history := make([]string, 0, len(historyEntries))
	for _, entry := range historyEntries {
		history = append(history, entry.Display)
	}
	return PromptState{
		History:        history,
		HistoryEntries: historyEntries,
		HistoryIndex:   len(historyEntries),
	}
}

func ParseKey(seq string) Key {
	if text, ok := parseBracketedPaste(seq); ok {
		return Key{Type: KeyPaste, Text: text}
	}
	if image, ok := parseImageHint(seq); ok {
		return Key{
			Type:      KeyImageHint,
			Text:      image.Display,
			Content:   image.Content,
			MediaType: image.MediaType,
			Filename:  image.Filename,
		}
	}
	if key, ok := parseSGRMouse(seq); ok {
		return key
	}
	if key, ok := parseLegacyMouse(seq); ok {
		return key
	}
	if key, ok := parseCSIuKey(seq); ok {
		return key
	}
	if key, ok := parseModifiedNavigationKey(seq); ok {
		return key
	}
	switch seq {
	case "\r", "\n":
		return Key{Type: KeyEnter}
	case "\x1b[13;2u", "\x1b[13;2~", "\x1b[27;2;13~":
		return Key{Type: KeyShiftEnter}
	case "\x7f", "\b":
		return Key{Type: KeyBackspace}
	case "\x1b":
		return Key{Type: KeyEsc}
	case "\x1bb", "\x1bB":
		return Key{Type: KeyAltB}
	case "\x1bd", "\x1bD":
		return Key{Type: KeyAltD}
	case "\x1bf", "\x1bF":
		return Key{Type: KeyAltF}
	case "\x1by", "\x1bY":
		return Key{Type: KeyAltY}
	case "\x1b\x7f", "\x1b\b":
		return Key{Type: KeyAltBS}
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
	case "\x0e":
		return Key{Type: KeyCtrlN}
	case "\x0f":
		return Key{Type: KeyCtrlO}
	case "\x10":
		return Key{Type: KeyCtrlP}
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
	case "\x1b[D", "\x1bOD", "\x1b[d":
		return Key{Type: KeyLeft}
	case "\x1b[C", "\x1bOC", "\x1b[c":
		return Key{Type: KeyRight}
	case "\x1b[1;3D", "\x1b[1;9D":
		return Key{Type: KeyAltLeft}
	case "\x1b[1;3C", "\x1b[1;9C":
		return Key{Type: KeyAltRight}
	case "\x1b[1;5D":
		return Key{Type: KeyCtrlLeft}
	case "\x1b[1;5C":
		return Key{Type: KeyCtrlRight}
	case "\x1b[A", "\x1bOA", "\x1b[a":
		return Key{Type: KeyUp}
	case "\x1b[B", "\x1bOB", "\x1b[b":
		return Key{Type: KeyDown}
	case "\x1b[H", "\x1bOH", "\x1b[1~", "\x1b[7~", "\x1b[7$", "\x1b[7^":
		return Key{Type: KeyHome}
	case "\x1b[F", "\x1bOF", "\x1b[4~", "\x1b[8~", "\x1b[8$", "\x1b[8^":
		return Key{Type: KeyEnd}
	case "\x1b[3~", "\x1b[3$", "\x1b[3^":
		return Key{Type: KeyDelete}
	case "\x1b[5~", "\x1b[[5~", "\x1b[5$", "\x1b[5^":
		return Key{Type: KeyPageUp}
	case "\x1b[6~", "\x1b[[6~", "\x1b[6$", "\x1b[6^":
		return Key{Type: KeyPageDown}
	default:
		r, size := utf8.DecodeRuneInString(seq)
		if r != utf8.RuneError && size == len(seq) && r >= 0x20 {
			return Key{Type: KeyRune, Rune: r}
		}
		return Key{Type: KeyUnknown}
	}
}

func parseModifiedNavigationKey(seq string) (Key, bool) {
	switch {
	case isModifiedNavigationCSI(seq, "\x1b[1;", "H"):
		return Key{Type: KeyHome}, true
	case isModifiedNavigationCSI(seq, "\x1b[1;", "F"):
		return Key{Type: KeyEnd}, true
	case isModifiedNavigationCSI(seq, "\x1b[3;", "~"):
		return Key{Type: KeyDelete}, true
	case isModifiedNavigationCSI(seq, "\x1b[5;", "~"):
		return Key{Type: KeyPageUp}, true
	case isModifiedNavigationCSI(seq, "\x1b[6;", "~"):
		return Key{Type: KeyPageDown}, true
	default:
		return Key{}, false
	}
}

func isModifiedNavigationCSI(seq, prefix, suffix string) bool {
	if !strings.HasPrefix(seq, prefix) || !strings.HasSuffix(seq, suffix) {
		return false
	}
	modifier := strings.TrimSuffix(strings.TrimPrefix(seq, prefix), suffix)
	if len(modifier) != 1 {
		return false
	}
	return modifier[0] >= '2' && modifier[0] <= '9'
}

func parseCSIuKey(seq string) (Key, bool) {
	if !strings.HasPrefix(seq, "\x1b[") || !strings.HasSuffix(seq, "u") {
		return Key{}, false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "u")
	parts := strings.Split(body, ";")
	if len(parts) < 2 {
		return Key{}, false
	}
	codepoint, ok := parseCSIuNumber(parts[0])
	if !ok {
		return Key{}, false
	}
	modifier, ok := parseCSIuNumber(parts[1])
	if !ok {
		return Key{}, false
	}
	if modifier < 2 {
		return Key{}, false
	}
	shift := modifier == 2 || modifier == 4 || modifier == 6 || modifier == 8
	alt := modifier == 3 || modifier == 4 || modifier == 7 || modifier == 8
	ctrl := modifier >= 5 && modifier <= 8

	if ctrl {
		if key, ok := ctrlCSIuKey(codepoint); ok {
			return key, true
		}
		return Key{}, false
	}
	if alt {
		if key, ok := altCSIuKey(codepoint); ok {
			return key, true
		}
		return Key{}, false
	}
	if shift {
		if key, ok := shiftCSIuKey(codepoint); ok {
			return key, true
		}
	}
	if r := rune(codepoint); r >= 0x20 {
		return Key{Type: KeyRune, Rune: r}, true
	}
	return Key{}, false
}

func parseCSIuNumber(field string) (int, bool) {
	value, _, _ := strings.Cut(field, ":")
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func ctrlCSIuKey(codepoint int) (Key, bool) {
	switch asciiLower(codepoint) {
	case 'a':
		return Key{Type: KeyCtrlA}, true
	case 'b':
		return Key{Type: KeyCtrlB}, true
	case 'c':
		return Key{Type: KeyCtrlC}, true
	case 'd':
		return Key{Type: KeyCtrlD}, true
	case 'e':
		return Key{Type: KeyCtrlE}, true
	case 'f':
		return Key{Type: KeyCtrlF}, true
	case 'g':
		return Key{Type: KeyCtrlG}, true
	case 'h':
		return Key{Type: KeyBackspace}, true
	case 'i':
		return Key{Type: KeyTab}, true
	case 'j', 'm':
		return Key{Type: KeyEnter}, true
	case 'k':
		return Key{Type: KeyCtrlK}, true
	case 'l':
		return Key{Type: KeyCtrlL}, true
	case 'n':
		return Key{Type: KeyCtrlN}, true
	case 'o':
		return Key{Type: KeyCtrlO}, true
	case 'p':
		return Key{Type: KeyCtrlP}, true
	case 'r':
		return Key{Type: KeyCtrlR}, true
	case 's':
		return Key{Type: KeyCtrlS}, true
	case 't':
		return Key{Type: KeyCtrlT}, true
	case 'u':
		return Key{Type: KeyCtrlU}, true
	case 'w':
		return Key{Type: KeyCtrlW}, true
	case 'x':
		return Key{Type: KeyCtrlX}, true
	case 'y':
		return Key{Type: KeyCtrlY}, true
	}
	switch codepoint {
	case 27, '[':
		return Key{Type: KeyEsc}, true
	case 8, 127, '?':
		return Key{Type: KeyBackspace}, true
	case 9:
		return Key{Type: KeyTab}, true
	case 10, 13:
		return Key{Type: KeyEnter}, true
	default:
		return Key{}, false
	}
}

func altCSIuKey(codepoint int) (Key, bool) {
	switch asciiLower(codepoint) {
	case 'b':
		return Key{Type: KeyAltB}, true
	case 'd':
		return Key{Type: KeyAltD}, true
	case 'f':
		return Key{Type: KeyAltF}, true
	case 'y':
		return Key{Type: KeyAltY}, true
	}
	switch codepoint {
	case 8, 127:
		return Key{Type: KeyAltBS}, true
	default:
		return Key{}, false
	}
}

func shiftCSIuKey(codepoint int) (Key, bool) {
	switch codepoint {
	case 9:
		return Key{Type: KeyShiftTab}, true
	case 10, 13:
		return Key{Type: KeyShiftEnter}, true
	default:
		return Key{}, false
	}
}

func asciiLower(codepoint int) int {
	if codepoint >= 'A' && codepoint <= 'Z' {
		return codepoint + ('a' - 'A')
	}
	return codepoint
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

func parseLegacyMouse(seq string) (Key, bool) {
	if !strings.HasPrefix(seq, "\x1b[M") || len(seq) != 6 {
		return Key{}, false
	}
	button := int(seq[3]) - 32
	x := int(seq[4]) - 32
	y := int(seq[5]) - 32
	if button < 0 || x < 1 || y < 1 {
		return Key{}, false
	}
	return Key{Type: KeyMouse, MouseButton: button, MouseX: x, MouseY: y, MouseRelease: button&3 == 3}, true
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
	case KeyShiftEnter:
		p.insertText("\n")
	case KeyPaste:
		p.insertPaste(key.Text)
	case KeyImageHint:
		p.insertImageHint(key)
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
	case KeyAltB, KeyAltLeft, KeyCtrlLeft:
		p.moveWordBackward()
	case KeyAltF, KeyAltRight, KeyCtrlRight:
		p.moveWordForward()
	case KeyHome, KeyCtrlA:
		p.moveLineStart()
	case KeyEnd, KeyCtrlE:
		p.moveLineEnd()
	case KeyCtrlK:
		p.deleteToEnd()
	case KeyCtrlU:
		p.deleteToStart()
	case KeyCtrlW, KeyAltBS:
		p.deleteWordBackward()
	case KeyAltD:
		p.deleteWordForward()
	case KeyCtrlY:
		p.yankLastKill()
	case KeyAltY:
		p.yankPop()
	case KeyUp, KeyCtrlP:
		p.historyPrev()
	case KeyDown, KeyCtrlN:
		p.historyNext()
	case KeyEnter:
		display := p.Text
		submitted := p.ExpandedText()
		pastedContents := clonePastedContents(p.PastedContents)
		if display != "" {
			p.History = append(p.History, display)
			p.HistoryEntries = append(p.HistoryEntries, session.HistoryEntry{
				Display:        display,
				PastedContents: clonePastedContents(pastedContents),
			})
		}
		p.Text = ""
		p.Cursor = 0
		p.HistoryIndex = p.historyLength()
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
	return key == KeyCtrlK || key == KeyCtrlU || key == KeyCtrlW || key == KeyAltBS
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

func (p *PromptState) insertImageHint(key Key) {
	text := key.Text
	if text == "" {
		text = ImageHintPlaceholder
	}
	if !p.UsePasteReferences {
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
		ID:        id,
		Type:      session.PastedContentImage,
		Content:   key.Content,
		MediaType: key.MediaType,
		Filename:  key.Filename,
	}
	p.insertText(session.FormatImageRef(id))
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
	p.HistoryIndex = p.historyLength()
	p.draft = p.Text
	p.draftPastedContents = clonePastedContents(p.PastedContents)
}

func (p *PromptState) historyPrev() {
	historyLen := p.historyLength()
	if historyLen == 0 {
		return
	}
	if p.HistoryIndex == historyLen {
		p.draft = p.Text
		p.draftPastedContents = clonePastedContents(p.PastedContents)
	}
	if p.HistoryIndex > 0 {
		p.HistoryIndex--
	}
	entry := p.historyEntryAt(p.HistoryIndex)
	p.Text = entry.Display
	p.replacePastedContents(entry.PastedContents)
	p.Cursor = len([]rune(p.Text))
}

func (p *PromptState) historyNext() {
	historyLen := p.historyLength()
	if p.HistoryIndex >= historyLen {
		return
	}
	p.HistoryIndex++
	if p.HistoryIndex == historyLen {
		p.Text = p.draft
		p.replacePastedContents(p.draftPastedContents)
	} else {
		entry := p.historyEntryAt(p.HistoryIndex)
		p.Text = entry.Display
		p.replacePastedContents(entry.PastedContents)
	}
	p.Cursor = len([]rune(p.Text))
}

func (p PromptState) historyLength() int {
	if len(p.HistoryEntries) > len(p.History) {
		return len(p.HistoryEntries)
	}
	return len(p.History)
}

func (p PromptState) historyEntryAt(index int) session.HistoryEntry {
	if index >= 0 && index < len(p.HistoryEntries) {
		return cloneHistoryEntry(p.HistoryEntries[index])
	}
	if index >= 0 && index < len(p.History) {
		return session.HistoryEntry{Display: p.History[index]}
	}
	return session.HistoryEntry{}
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

func cloneHistoryEntries(in []session.HistoryEntry) []session.HistoryEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]session.HistoryEntry, 0, len(in))
	for _, entry := range in {
		out = append(out, cloneHistoryEntry(entry))
	}
	return out
}

func cloneHistoryEntry(entry session.HistoryEntry) session.HistoryEntry {
	return session.HistoryEntry{
		Display:        entry.Display,
		PastedContents: clonePastedContents(entry.PastedContents),
	}
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

type imageHint struct {
	Display   string
	Content   string
	MediaType string
	Filename  string
}

func parseImageHint(seq string) (imageHint, bool) {
	const prefix = "\x1b]1337;File="
	if !strings.HasPrefix(seq, prefix) {
		return imageHint{}, false
	}
	payload := stripOSCTerminator(strings.TrimPrefix(seq, prefix))
	metadata, content, _ := strings.Cut(payload, ":")
	name := ""
	mediaType := ""
	for _, field := range strings.Split(metadata, ";") {
		if raw, ok := strings.CutPrefix(field, "name="); ok {
			name = decodeImageName(raw)
			continue
		}
		if raw, ok := strings.CutPrefix(field, "type="); ok {
			mediaType = strings.TrimSpace(raw)
			continue
		}
		if raw, ok := strings.CutPrefix(field, "mediaType="); ok {
			mediaType = strings.TrimSpace(raw)
			continue
		}
		if raw, ok := strings.CutPrefix(field, "mime="); ok {
			mediaType = strings.TrimSpace(raw)
		}
	}
	display := ImageHintPlaceholder
	if name == "" {
		return imageHint{Display: display, Content: content, MediaType: mediaType}, true
	}
	display = "[Image: " + name + "]"
	return imageHint{Display: display, Content: content, MediaType: mediaType, Filename: name}, true
}

func stripOSCTerminator(payload string) string {
	if strings.HasSuffix(payload, "\a") {
		return strings.TrimSuffix(payload, "\a")
	}
	if strings.HasSuffix(payload, "\x1b\\") {
		return strings.TrimSuffix(payload, "\x1b\\")
	}
	return payload
}

func decodeImageName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(name)
		if err != nil || len(decoded) == 0 || !utf8.Valid(decoded) {
			continue
		}
		decodedName := strings.TrimSpace(string(decoded))
		if isPrintableImageName(decodedName) {
			return decodedName
		}
	}
	return name
}

func isPrintableImageName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
