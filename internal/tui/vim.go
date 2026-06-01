package tui

import (
	"unicode"

	"ccgo/internal/session"
)

type VimMode string

const (
	VimInsert VimMode = "insert"
	VimNormal VimMode = "normal"
)

type vimPromptSnapshot struct {
	Text           string
	Cursor         int
	PastedContents map[int]session.PastedContent
	NextPastedID   int
}

func (s *REPLScreen) SetVimEnabled(enabled bool) {
	s.VimEnabled = enabled
	if s.VimMode == "" {
		s.VimMode = VimInsert
	}
}

func (s *REPLScreen) applyVimKey(key Key) (ScreenEvent, bool) {
	if !s.VimEnabled {
		return ScreenEvent{}, false
	}
	if s.VimMode == "" {
		s.VimMode = VimInsert
	}
	if s.VimMode == VimInsert {
		if key.Type == KeyEsc {
			s.VimMode = VimNormal
			s.clearVimPending()
			return ScreenEvent{}, true
		}
		return ScreenEvent{}, false
	}
	switch key.Type {
	case KeyEsc:
		s.clearVimPending()
		return ScreenEvent{}, true
	case KeyLeft:
		return s.applyVimNormalRune('h'), true
	case KeyRight:
		return s.applyVimNormalRune('l'), true
	case KeyUp:
		return s.applyVimNormalRune('k'), true
	case KeyDown:
		return s.applyVimNormalRune('j'), true
	case KeyBackspace:
		if s.VimPendingReplace || s.VimPendingCharMotion != 0 {
			return ScreenEvent{}, true
		}
		return s.applyVimNormalRune('h'), true
	case KeyDelete:
		if s.VimPendingReplace || s.VimPendingCharMotion != 0 {
			return ScreenEvent{}, true
		}
		if s.VimPendingOperator == 0 && s.VimCount == 0 {
			return s.applyVimNormalRune('x'), true
		}
		if s.VimCount > 0 {
			s.VimCount = 0
		}
		return ScreenEvent{}, true
	case KeyRune:
		return s.applyVimNormalRune(key.Rune), true
	}
	return ScreenEvent{}, false
}

func (s *REPLScreen) applyVimNormalRune(r rune) ScreenEvent {
	if s.VimPendingCharMotion != 0 {
		return s.applyVimCharMotion(r)
	}
	if s.VimPendingReplace {
		return s.applyVimReplace(r)
	}
	if s.VimPendingTextObject != 0 {
		return s.applyVimTextObject(r)
	}
	if s.VimPendingOperator != 0 {
		return s.applyVimOperator(r)
	}
	if isVimCountRune(r) {
		if r == '0' && s.VimCount == 0 {
			s.Prompt.Apply(Key{Type: KeyHome})
			return ScreenEvent{}
		}
		s.VimCount = s.VimCount*10 + int(r-'0')
		return ScreenEvent{}
	}
	count := s.takeVimCount()
	switch r {
	case 'i':
		s.VimMode = VimInsert
		s.clearVimPending()
	case 'u':
		s.undoVimPrompt()
	case 'r':
		s.VimPendingReplace = true
		s.VimPendingCount = count
	case 'f', 't', 'F', 'T':
		s.VimPendingCharMotion = r
		s.VimPendingCount = count
	case ';', ',':
		s.applyVimRepeatedCharMotion(count, r == ',')
	case 'd', 'c':
		s.VimPendingOperator = r
		s.VimPendingCount = count
	case 'a':
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyRight}) })
		s.VimMode = VimInsert
	case 'I':
		s.Prompt.Apply(Key{Type: KeyHome})
		s.VimMode = VimInsert
	case 'A':
		s.Prompt.Apply(Key{Type: KeyEnd})
		s.VimMode = VimInsert
	case 'h':
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyLeft}) })
	case 'l':
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyRight}) })
	case 'w':
		applyN(count, func() { s.Prompt.moveWordForward() })
	case 'b':
		applyN(count, func() { s.Prompt.moveWordBackward() })
	case 'e':
		applyN(count, func() { s.Prompt.moveWordEnd() })
	case 'W':
		applyN(count, func() { s.Prompt.moveWORDForward() })
	case 'B':
		applyN(count, func() { s.Prompt.moveWORDBackward() })
	case 'E':
		applyN(count, func() { s.Prompt.moveWORDEnd() })
	case '^':
		s.Prompt.moveFirstNonBlank()
	case '$':
		s.Prompt.Apply(Key{Type: KeyEnd})
	case 'x':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyDelete}) })
	case 'X':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyBackspace}) })
	case 'D':
		s.recordVimUndo()
		s.Prompt.deleteToEnd()
	case 'C':
		s.recordVimUndo()
		s.Prompt.deleteToEnd()
		s.VimMode = VimInsert
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyVimOperator(r rune) ScreenEvent {
	if isVimCountRune(r) && (r != '0' || s.VimCount > 0) {
		s.VimCount = s.VimCount*10 + int(r-'0')
		return ScreenEvent{}
	}
	operator := s.VimPendingOperator
	count := s.takeVimOperatorCount()
	s.clearVimPending()
	change := operator == 'c'
	switch r {
	case 'd', 'c':
		if r == operator {
			s.recordVimUndo()
			s.Prompt.deleteAll()
			if change {
				s.VimMode = VimInsert
			}
		}
	case 'f', 't', 'F', 'T':
		s.VimPendingOperator = operator
		s.VimPendingCharMotion = r
		s.VimPendingCount = count
	case 'i', 'a':
		s.VimPendingOperator = operator
		s.VimPendingTextObject = r
		s.VimPendingCount = count
	case ';', ',':
		s.VimPendingOperator = operator
		s.VimPendingCharMotion = s.repeatedVimCharMotion(r == ',')
		s.VimPendingCount = count
		if s.VimPendingCharMotion == 0 || s.VimLastCharTarget == 0 {
			s.clearVimPending()
			return ScreenEvent{}
		}
		s.VimRepeatingChar = true
		return s.applyVimCharMotion(s.VimLastCharTarget)
	case 'w':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.deleteWordForward() })
		if change {
			s.VimMode = VimInsert
		}
	case 'W':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.deleteWORDForward() })
		if change {
			s.VimMode = VimInsert
		}
	case 'e':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.deleteToWordEnd(false) })
		if change {
			s.VimMode = VimInsert
		}
	case 'E':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.deleteToWordEnd(true) })
		if change {
			s.VimMode = VimInsert
		}
	case '$':
		s.recordVimUndo()
		s.Prompt.deleteToEnd()
		if change {
			s.VimMode = VimInsert
		}
	case '0':
		s.recordVimUndo()
		s.Prompt.deleteToStart()
		if change {
			s.VimMode = VimInsert
		}
	case 'b':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.deleteWordBackward() })
		if change {
			s.VimMode = VimInsert
		}
	case 'B':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.deleteWORDBackward() })
		if change {
			s.VimMode = VimInsert
		}
	case '^':
		s.recordVimUndo()
		s.Prompt.deleteToFirstNonBlank()
		if change {
			s.VimMode = VimInsert
		}
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyVimTextObject(obj rune) ScreenEvent {
	operator := s.VimPendingOperator
	scope := s.VimPendingTextObject
	count := s.takeVimOperatorCount()
	s.clearVimPending()
	if obj != 'w' && obj != 'W' {
		return ScreenEvent{}
	}
	start, end, ok := s.Prompt.findTextObjectRange(scope, obj, count)
	if !ok {
		return ScreenEvent{}
	}
	s.recordVimUndo()
	s.Prompt.deleteRange(start, end)
	if operator == 'c' {
		s.VimMode = VimInsert
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyVimCharMotion(target rune) ScreenEvent {
	motion := s.VimPendingCharMotion
	operator := s.VimPendingOperator
	count := s.VimPendingCount
	if count <= 0 {
		count = 1
	}
	start := s.Prompt.Cursor
	end, ok := s.Prompt.findCharMotion(motion, target, count)
	repeating := s.VimRepeatingChar
	s.clearVimPending()
	if !ok {
		return ScreenEvent{}
	}
	if !repeating {
		s.VimLastCharMotion = motion
		s.VimLastCharTarget = target
	}
	if operator == 0 {
		s.Prompt.Cursor = end
		return ScreenEvent{}
	}
	s.recordVimUndo()
	s.Prompt.deleteCharMotionRange(start, end, motion)
	if operator == 'c' {
		s.VimMode = VimInsert
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyVimRepeatedCharMotion(count int, reverse bool) {
	motion := s.repeatedVimCharMotion(reverse)
	if motion == 0 || s.VimLastCharTarget == 0 {
		return
	}
	s.VimPendingCharMotion = motion
	s.VimPendingCount = count
	s.VimRepeatingChar = true
	s.applyVimCharMotion(s.VimLastCharTarget)
}

func (s *REPLScreen) repeatedVimCharMotion(reverse bool) rune {
	if !reverse {
		return s.VimLastCharMotion
	}
	switch s.VimLastCharMotion {
	case 'f':
		return 'F'
	case 'F':
		return 'f'
	case 't':
		return 'T'
	case 'T':
		return 't'
	default:
		return 0
	}
}

func (s *REPLScreen) applyVimReplace(r rune) ScreenEvent {
	count := s.takeVimOperatorCount()
	s.clearVimPending()
	s.recordVimUndo()
	s.Prompt.replaceRunes(count, r)
	return ScreenEvent{}
}

func (s *REPLScreen) takeVimCount() int {
	if s.VimCount <= 0 {
		return 1
	}
	count := s.VimCount
	s.VimCount = 0
	return count
}

func (s *REPLScreen) takeVimOperatorCount() int {
	count := s.VimPendingCount
	if count <= 0 {
		count = 1
	}
	if s.VimCount > 0 {
		count *= s.VimCount
	}
	return count
}

func (s *REPLScreen) clearVimPending() {
	s.VimPendingOperator = 0
	s.VimPendingCharMotion = 0
	s.VimPendingTextObject = 0
	s.VimPendingCount = 0
	s.VimCount = 0
	s.VimPendingReplace = false
	s.VimRepeatingChar = false
}

func (s *REPLScreen) recordVimUndo() {
	snapshot := vimPromptSnapshot{
		Text:           s.Prompt.Text,
		Cursor:         s.Prompt.Cursor,
		PastedContents: clonePastedContents(s.Prompt.PastedContents),
		NextPastedID:   s.Prompt.NextPastedID,
	}
	if len(s.VimUndoStack) > 0 {
		last := s.VimUndoStack[len(s.VimUndoStack)-1]
		if last.Text == snapshot.Text && last.Cursor == snapshot.Cursor {
			return
		}
	}
	s.VimUndoStack = append(s.VimUndoStack, snapshot)
	if len(s.VimUndoStack) > 50 {
		s.VimUndoStack = append([]vimPromptSnapshot(nil), s.VimUndoStack[len(s.VimUndoStack)-50:]...)
	}
}

func (s *REPLScreen) undoVimPrompt() {
	if len(s.VimUndoStack) == 0 {
		return
	}
	last := s.VimUndoStack[len(s.VimUndoStack)-1]
	s.VimUndoStack = s.VimUndoStack[:len(s.VimUndoStack)-1]
	s.Prompt.Text = last.Text
	s.Prompt.Cursor = last.Cursor
	s.Prompt.replacePastedContents(last.PastedContents)
	if s.Prompt.UsePasteReferences && last.NextPastedID > 0 {
		s.Prompt.NextPastedID = last.NextPastedID
	}
	s.Prompt.resetHistoryCursor()
}

func isVimCountRune(r rune) bool {
	return r >= '0' && r <= '9'
}

func applyN(count int, fn func()) {
	if count <= 0 {
		count = 1
	}
	for i := 0; i < count; i++ {
		fn()
	}
}

func (p *PromptState) moveWordForward() {
	runes := []rune(p.Text)
	i := p.Cursor
	for i < len(runes) && !isWordRune(runes[i]) {
		i++
	}
	for i < len(runes) && isWordRune(runes[i]) {
		i++
	}
	for i < len(runes) && !isWordRune(runes[i]) {
		i++
	}
	p.Cursor = i
}

func (p *PromptState) moveWordBackward() {
	runes := []rune(p.Text)
	i := p.Cursor
	if i > len(runes) {
		i = len(runes)
	}
	for i > 0 && !isWordRune(runes[i-1]) {
		i--
	}
	for i > 0 && isWordRune(runes[i-1]) {
		i--
	}
	p.Cursor = i
}

func (p *PromptState) moveWordEnd() {
	runes := []rune(p.Text)
	i := p.Cursor
	if i < len(runes) && isWordRune(runes[i]) {
		i++
	}
	for i < len(runes) && !isWordRune(runes[i]) {
		i++
	}
	for i < len(runes) && isWordRune(runes[i]) {
		i++
	}
	if i > 0 {
		i--
	}
	p.Cursor = i
}

func (p *PromptState) moveWORDForward() {
	runes := []rune(p.Text)
	i := p.Cursor
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	for i < len(runes) && !unicode.IsSpace(runes[i]) {
		i++
	}
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	p.Cursor = i
}

func (p *PromptState) moveWORDBackward() {
	runes := []rune(p.Text)
	i := p.Cursor
	if i > len(runes) {
		i = len(runes)
	}
	for i > 0 && unicode.IsSpace(runes[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(runes[i-1]) {
		i--
	}
	p.Cursor = i
}

func (p *PromptState) moveWORDEnd() {
	runes := []rune(p.Text)
	i := p.Cursor
	if i < len(runes) && !unicode.IsSpace(runes[i]) {
		i++
	}
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	for i < len(runes) && !unicode.IsSpace(runes[i]) {
		i++
	}
	if i > 0 {
		i--
	}
	p.Cursor = i
}

func (p *PromptState) moveFirstNonBlank() {
	runes := []rune(p.Text)
	for i, r := range runes {
		if !unicode.IsSpace(r) {
			p.Cursor = i
			return
		}
	}
	p.Cursor = 0
}

func (p *PromptState) deleteAll() {
	p.Text = ""
	p.Cursor = 0
	p.resetHistoryCursor()
	p.resetPastedContents()
}

func (p *PromptState) deleteToEnd() {
	runes := []rune(p.Text)
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor > len(runes) {
		p.Cursor = len(runes)
	}
	p.Text = string(runes[:p.Cursor])
	p.resetHistoryCursor()
}

func (p *PromptState) deleteToStart() {
	runes := []rune(p.Text)
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor > len(runes) {
		p.Cursor = len(runes)
	}
	p.Text = string(runes[p.Cursor:])
	p.Cursor = 0
	p.resetHistoryCursor()
}

func (p *PromptState) deleteWordForward() {
	start := p.Cursor
	p.moveWordForward()
	end := p.Cursor
	p.deleteRange(start, end)
}

func (p *PromptState) deleteWordBackward() {
	end := p.Cursor
	p.moveWordBackward()
	start := p.Cursor
	p.deleteRange(start, end)
}

func (p *PromptState) deleteWORDForward() {
	start := p.Cursor
	p.moveWORDForward()
	end := p.Cursor
	p.deleteRange(start, end)
}

func (p *PromptState) deleteWORDBackward() {
	end := p.Cursor
	p.moveWORDBackward()
	start := p.Cursor
	p.deleteRange(start, end)
}

func (p *PromptState) deleteToWordEnd(big bool) {
	start := p.Cursor
	if big {
		p.moveWORDEnd()
	} else {
		p.moveWordEnd()
	}
	end := p.Cursor
	runes := []rune(p.Text)
	if end < len(runes) {
		end++
	}
	p.deleteRange(start, end)
}

func (p *PromptState) deleteToFirstNonBlank() {
	end := p.Cursor
	p.moveFirstNonBlank()
	start := p.Cursor
	p.deleteRange(start, end)
}

func (p *PromptState) findTextObjectRange(scope rune, obj rune, count int) (int, int, bool) {
	if count <= 0 {
		count = 1
	}
	runes := []rune(p.Text)
	if len(runes) == 0 {
		return 0, 0, false
	}
	idx := p.Cursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(runes) {
		idx = len(runes) - 1
	}
	isWord := isWordRune
	if obj == 'W' {
		isWord = func(r rune) bool { return !unicode.IsSpace(r) }
	}
	start, end := textObjectCoreRange(runes, idx, isWord)
	for i := 1; i < count && end < len(runes); i++ {
		next := end
		for next < len(runes) && unicode.IsSpace(runes[next]) {
			next++
		}
		if next >= len(runes) {
			break
		}
		_, end = textObjectCoreRange(runes, next, isWord)
	}
	if scope == 'a' {
		start, end = expandAroundTextObject(runes, start, end)
	}
	return start, end, true
}

func textObjectCoreRange(runes []rune, idx int, isWord func(rune) bool) (int, int) {
	start := idx
	end := idx + 1
	switch {
	case isWord(runes[idx]):
		for start > 0 && isWord(runes[start-1]) {
			start--
		}
		for end < len(runes) && isWord(runes[end]) {
			end++
		}
	case unicode.IsSpace(runes[idx]):
		for start > 0 && unicode.IsSpace(runes[start-1]) {
			start--
		}
		for end < len(runes) && unicode.IsSpace(runes[end]) {
			end++
		}
	default:
		for start > 0 && isVimPunctuation(runes[start-1], isWord) {
			start--
		}
		for end < len(runes) && isVimPunctuation(runes[end], isWord) {
			end++
		}
	}
	return start, end
}

func expandAroundTextObject(runes []rune, start int, end int) (int, int) {
	if end < len(runes) && unicode.IsSpace(runes[end]) {
		for end < len(runes) && unicode.IsSpace(runes[end]) {
			end++
		}
		return start, end
	}
	for start > 0 && unicode.IsSpace(runes[start-1]) {
		start--
	}
	return start, end
}

func isVimPunctuation(r rune, isWord func(rune) bool) bool {
	return !unicode.IsSpace(r) && !isWord(r)
}

func (p *PromptState) deleteRange(start int, end int) {
	runes := []rune(p.Text)
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if end < start {
		start, end = end, start
	}
	p.Text = string(append(runes[:start], runes[end:]...))
	p.Cursor = start
	p.resetHistoryCursor()
}

func (p *PromptState) replaceRunes(count int, r rune) {
	if count <= 0 {
		count = 1
	}
	runes := []rune(p.Text)
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor >= len(runes) {
		return
	}
	end := p.Cursor + count
	if end > len(runes) {
		end = len(runes)
	}
	for i := p.Cursor; i < end; i++ {
		runes[i] = r
	}
	p.Text = string(runes)
	p.resetHistoryCursor()
}

func (p *PromptState) findCharMotion(motion rune, target rune, count int) (int, bool) {
	if count <= 0 {
		count = 1
	}
	runes := []rune(p.Text)
	cursor := p.Cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	switch motion {
	case 'f', 't':
		match := -1
		for i := cursor + 1; i < len(runes); i++ {
			if runes[i] != target {
				continue
			}
			count--
			if count == 0 {
				match = i
				break
			}
		}
		if match < 0 {
			return cursor, false
		}
		if motion == 't' {
			if match > cursor {
				return match - 1, true
			}
			return cursor, true
		}
		return match, true
	case 'F', 'T':
		match := -1
		for i := cursor - 1; i >= 0; i-- {
			if runes[i] != target {
				continue
			}
			count--
			if count == 0 {
				match = i
				break
			}
		}
		if match < 0 {
			return cursor, false
		}
		if motion == 'T' {
			if match < len(runes)-1 {
				return match + 1, true
			}
			return cursor, true
		}
		return match, true
	default:
		return cursor, false
	}
}

func (p *PromptState) deleteCharMotionRange(start int, end int, motion rune) {
	switch motion {
	case 'f':
		p.deleteRange(start, end+1)
	case 't':
		p.deleteRange(start, end+1)
	case 'F':
		p.deleteRange(end, start+1)
	case 'T':
		p.deleteRange(end+1, start+1)
	}
}

func isWordRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}
