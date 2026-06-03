package tui

import (
	"strings"
	"unicode"

	"ccgo/internal/session"
)

type VimMode string

const (
	VimInsert VimMode = "insert"
	VimNormal VimMode = "normal"
)

type vimPromptSnapshot struct {
	Text                   string
	Cursor                 int
	PastedContents         map[int]session.PastedContent
	NextPastedID           int
	PendingSpaceAfterImage bool
}

type vimRecordedChange struct {
	Kind     string
	Text     string
	Operator rune
	Motion   rune
	Count    int
	Target   rune
	Scope    rune
	Object   rune
	Dir      rune
	Below    bool
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
			if s.VimInsertedText != "" {
				s.recordVimChange(vimRecordedChange{Kind: "insert", Text: s.VimInsertedText})
			}
			s.VimMode = VimNormal
			s.clearVimPending()
			s.VimInsertedText = ""
			return ScreenEvent{}, true
		}
		s.trackVimInsertedText(key)
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
	if s.VimPendingG {
		return s.applyVimG(r)
	}
	if s.VimPendingIndent != 0 {
		return s.applyVimIndent(r)
	}
	if s.VimPendingOperator != 0 {
		return s.applyVimOperator(r)
	}
	if isVimCountRune(r) {
		if r == '0' && s.VimCount == 0 {
			s.Prompt.moveLineStart()
			return ScreenEvent{}
		}
		s.VimCount = s.VimCount*10 + int(r-'0')
		return ScreenEvent{}
	}
	count := s.takeVimCount()
	switch r {
	case 'i':
		s.enterVimInsert()
		s.clearVimPending()
	case 'u':
		s.undoVimPrompt()
	case '.':
		s.replayVimLastChange()
	case 'r':
		s.VimPendingReplace = true
		s.VimPendingCount = count
	case 'f', 't', 'F', 'T':
		s.VimPendingCharMotion = r
		s.VimPendingCount = count
	case ';', ',':
		s.applyVimRepeatedCharMotion(count, r == ',')
	case 'g':
		s.VimPendingG = true
		s.VimPendingCount = count
	case 'G':
		s.Prompt.goToLine(vimGTargetLine(s.Prompt.lineCount(), count))
	case '>', '<':
		s.VimPendingIndent = r
		s.VimPendingCount = count
	case '~':
		s.recordVimUndo()
		s.Prompt.toggleCase(count)
		s.recordVimChange(vimRecordedChange{Kind: "toggleCase", Count: count})
	case 'J':
		s.recordVimUndo()
		s.Prompt.joinLines(count)
		s.recordVimChange(vimRecordedChange{Kind: "join", Count: count})
	case 'd', 'c', 'y':
		s.VimPendingOperator = r
		s.VimPendingCount = count
	case 'Y':
		s.applyVimLineOperator('y', count)
	case 'p', 'P':
		s.applyVimPaste(r == 'p', count)
	case 'a':
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyRight}) })
		s.enterVimInsert()
	case 'I':
		s.Prompt.moveFirstNonBlank()
		s.enterVimInsert()
	case 'A':
		s.Prompt.moveLineEnd()
		s.enterVimInsert()
	case 'o':
		s.recordVimUndo()
		s.Prompt.openLine(true)
		s.recordVimChange(vimRecordedChange{Kind: "openLine", Below: true})
		s.enterVimInsert()
	case 'O':
		s.recordVimUndo()
		s.Prompt.openLine(false)
		s.recordVimChange(vimRecordedChange{Kind: "openLine", Below: false})
		s.enterVimInsert()
	case 'h':
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyLeft}) })
	case 'l':
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyRight}) })
	case 'j':
		s.Prompt.moveLogicalLine(count)
	case 'k':
		s.Prompt.moveLogicalLine(-count)
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
		s.Prompt.moveLineEnd()
	case 'x':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyDelete}) })
		s.recordVimChange(vimRecordedChange{Kind: "x", Count: count})
	case 'X':
		s.recordVimUndo()
		applyN(count, func() { s.Prompt.Apply(Key{Type: KeyBackspace}) })
		s.recordVimChange(vimRecordedChange{Kind: "X", Count: count})
	case 's':
		s.applyVimSubstitute(count)
	case 'S':
		s.applyVimLineOperator('c', count)
	case 'D':
		s.applyVimMotionOperator('d', '$', 1)
	case 'C':
		s.applyVimMotionOperator('c', '$', 1)
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
	switch r {
	case 'd', 'c', 'y':
		if r == operator {
			s.applyVimLineOperator(operator, count)
		}
	case 'f', 't', 'F', 'T':
		s.VimPendingOperator = operator
		s.VimPendingCharMotion = r
		s.VimPendingCount = count
	case 'i', 'a':
		s.VimPendingOperator = operator
		s.VimPendingTextObject = r
		s.VimPendingCount = count
	case 'G':
		s.applyVimLineMotionOperator(operator, vimGTargetLine(s.Prompt.lineCount(), count), 'G', count)
	case 'g':
		s.VimPendingOperator = operator
		s.VimPendingG = true
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
	case 'h', 'l', 'j', 'k', 'w', 'W', 'e', 'E', '$', '0', 'b', 'B', '^':
		s.applyVimMotionOperator(operator, r, count)
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyVimG(r rune) ScreenEvent {
	operator := s.VimPendingOperator
	count := s.VimPendingCount
	s.clearVimPending()
	switch r {
	case 'g':
		targetLine := 1
		if count > 1 {
			targetLine = count
		}
		if operator == 0 {
			s.Prompt.goToLine(targetLine)
			return ScreenEvent{}
		}
		s.applyVimLineMotionOperator(operator, targetLine, 'g', count)
	case 'j':
		if operator == 0 {
			s.Prompt.moveLogicalLine(count)
		} else {
			s.applyVimLineMotionOperator(operator, s.Prompt.currentLogicalLine()+count+1, 'j', count)
		}
	case 'k':
		if operator == 0 {
			s.Prompt.moveLogicalLine(-count)
		} else {
			s.applyVimLineMotionOperator(operator, s.Prompt.currentLogicalLine()-count+1, 'k', count)
		}
	case 'e':
		if operator == 0 {
			applyN(count, func() { s.Prompt.moveWordBackwardEnd() })
		} else {
			s.applyVimBackwardEndMotionOperator(operator, r, count)
		}
	case 'E':
		if operator == 0 {
			applyN(count, func() { s.Prompt.moveWORDBackwardEnd() })
		} else {
			s.applyVimBackwardEndMotionOperator(operator, r, count)
		}
	}
	return ScreenEvent{}
}

func (s *REPLScreen) applyVimIndent(r rune) ScreenEvent {
	dir := s.VimPendingIndent
	count := s.VimPendingCount
	s.clearVimPending()
	if r != dir {
		return ScreenEvent{}
	}
	s.recordVimUndo()
	s.Prompt.indentLines(dir, count)
	s.recordVimChange(vimRecordedChange{Kind: "indent", Dir: dir, Count: count})
	return ScreenEvent{}
}

func (s *REPLScreen) applyVimTextObject(obj rune) ScreenEvent {
	operator := s.VimPendingOperator
	scope := s.VimPendingTextObject
	count := s.takeVimOperatorCount()
	s.clearVimPending()
	if obj != 'w' && obj != 'W' {
		if _, _, ok := textObjectPair(obj); !ok {
			return ScreenEvent{}
		}
	}
	start, end, ok := s.Prompt.findTextObjectRange(scope, obj, count)
	if !ok {
		return ScreenEvent{}
	}
	s.applyVimRangeOperator(operator, start, end, false)
	s.recordVimChange(vimRecordedChange{Kind: "operatorTextObj", Operator: operator, Scope: scope, Object: obj, Count: count})
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
	from, to := vimCharMotionRange(start, end, motion)
	s.applyVimRangeOperator(operator, from, to, false)
	s.recordVimChange(vimRecordedChange{Kind: "operatorFind", Operator: operator, Motion: motion, Target: target, Count: count})
	return ScreenEvent{}
}

func vimCharMotionRange(start int, end int, motion rune) (int, int) {
	switch motion {
	case 'f', 't':
		return start, end + 1
	case 'F':
		return end, start + 1
	case 'T':
		return end + 1, start + 1
	default:
		return start, end
	}
}

func (s *REPLScreen) applyVimMotionOperator(operator rune, motion rune, count int) {
	start, end, linewise, ok := s.Prompt.operatorMotionRange(operator, motion, count)
	if !ok {
		return
	}
	s.applyVimRangeOperator(operator, start, end, linewise)
	s.recordVimChange(vimRecordedChange{Kind: "operator", Operator: operator, Motion: motion, Count: count})
}

func (s *REPLScreen) applyVimLineMotionOperator(operator rune, targetLine int, motion rune, count int) {
	start, end, ok := s.Prompt.lineMotionRange(targetLine)
	if !ok {
		return
	}
	s.applyVimRangeOperator(operator, start, end, true)
	s.recordVimChange(vimRecordedChange{Kind: "operatorLineMotion", Operator: operator, Motion: motion, Count: count})
}

func (s *REPLScreen) applyVimBackwardEndMotionOperator(operator rune, motion rune, count int) {
	start, end, ok := s.Prompt.backwardEndMotionRange(motion, count)
	if !ok {
		return
	}
	s.applyVimRangeOperator(operator, start, end, false)
	s.recordVimChange(vimRecordedChange{Kind: "operatorBackwardEnd", Operator: operator, Motion: motion, Count: count})
}

func (s *REPLScreen) applyVimLineOperator(operator rune, count int) {
	start, end := s.Prompt.lineRange(count)
	s.setVimRegister(s.Prompt.rangeText(start, end), true)
	switch operator {
	case 'y':
		s.Prompt.Cursor = s.Prompt.clampCursor(start)
	case 'd', 'c':
		s.recordVimUndo()
		deleteStart := start
		runes := []rune(s.Prompt.Text)
		if operator == 'd' && end == len(runes) && deleteStart > 0 && runes[deleteStart-1] == '\n' {
			deleteStart--
		}
		s.Prompt.deleteRange(deleteStart, end)
		if operator == 'c' {
			s.enterVimInsert()
		}
	}
	s.recordVimChange(vimRecordedChange{Kind: "lineOperator", Operator: operator, Count: count})
}

func (s *REPLScreen) applyVimSubstitute(count int) {
	if count <= 0 {
		count = 1
	}
	runes := []rune(s.Prompt.Text)
	start := s.Prompt.clampCursor(s.Prompt.Cursor)
	end := start + count
	if end > len(runes) {
		end = len(runes)
	}
	s.setVimRegister(s.Prompt.rangeText(start, end), false)
	s.recordVimUndo()
	s.Prompt.deleteRange(start, end)
	s.recordVimChange(vimRecordedChange{Kind: "substitute", Count: count})
	s.enterVimInsert()
}

func (s *REPLScreen) applyVimRangeOperator(operator rune, start int, end int, linewise bool) {
	s.setVimRegister(s.Prompt.rangeText(start, end), linewise)
	switch operator {
	case 'y':
		s.Prompt.Cursor = s.Prompt.clampCursor(start)
	case 'd', 'c':
		s.recordVimUndo()
		deleteStart := start
		if linewise && operator == 'd' {
			runes := []rune(s.Prompt.Text)
			if end == len(runes) && deleteStart > 0 && runes[deleteStart-1] == '\n' {
				deleteStart--
			}
		}
		s.Prompt.deleteRange(deleteStart, end)
		if operator == 'c' {
			s.enterVimInsert()
		}
	}
}

func (s *REPLScreen) setVimRegister(content string, linewise bool) {
	if linewise && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	s.VimRegister = content
	s.VimRegisterLinewise = linewise
}

func (s *REPLScreen) applyVimPaste(after bool, count int) {
	if s.VimRegister == "" {
		return
	}
	if count <= 0 {
		count = 1
	}
	s.recordVimUndo()
	if s.VimRegisterLinewise || strings.HasSuffix(s.VimRegister, "\n") {
		s.Prompt.pasteLinewise(s.VimRegister, after, count)
		return
	}
	s.Prompt.pasteCharacterwise(s.VimRegister, after, count)
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
	s.recordVimChange(vimRecordedChange{Kind: "replace", Target: r, Count: count})
	return ScreenEvent{}
}

func (s *REPLScreen) enterVimInsert() {
	s.VimMode = VimInsert
	s.VimInsertedText = ""
}

func (s *REPLScreen) trackVimInsertedText(key Key) {
	switch key.Type {
	case KeyRune:
		s.VimInsertedText += string(key.Rune)
	case KeyPaste:
		s.VimInsertedText += key.Text
	case KeyBackspace:
		runes := []rune(s.VimInsertedText)
		if len(runes) > 0 {
			s.VimInsertedText = string(runes[:len(runes)-1])
		}
	}
}

func (s *REPLScreen) recordVimChange(change vimRecordedChange) {
	if s.VimReplayingChange || change.Kind == "" {
		return
	}
	if change.Count <= 0 {
		change.Count = 1
	}
	s.VimLastChange = change
}

func (s *REPLScreen) replayVimLastChange() {
	change := s.VimLastChange
	if change.Kind == "" {
		return
	}
	s.VimReplayingChange = true
	defer func() { s.VimReplayingChange = false }()
	switch change.Kind {
	case "insert":
		if change.Text == "" {
			return
		}
		s.recordVimUndo()
		s.Prompt.insertText(change.Text)
	case "x":
		s.recordVimUndo()
		applyN(change.Count, func() { s.Prompt.Apply(Key{Type: KeyDelete}) })
	case "X":
		s.recordVimUndo()
		applyN(change.Count, func() { s.Prompt.Apply(Key{Type: KeyBackspace}) })
	case "replace":
		s.recordVimUndo()
		s.Prompt.replaceRunes(change.Count, change.Target)
	case "toggleCase":
		s.recordVimUndo()
		s.Prompt.toggleCase(change.Count)
	case "join":
		s.recordVimUndo()
		s.Prompt.joinLines(change.Count)
	case "indent":
		s.recordVimUndo()
		s.Prompt.indentLines(change.Dir, change.Count)
	case "openLine":
		s.recordVimUndo()
		s.Prompt.openLine(change.Below)
		s.enterVimInsert()
	case "operator":
		s.applyVimMotionOperator(change.Operator, change.Motion, change.Count)
	case "operatorBackwardEnd":
		s.applyVimBackwardEndMotionOperator(change.Operator, change.Motion, change.Count)
	case "operatorLineMotion":
		switch change.Motion {
		case 'G':
			s.applyVimLineMotionOperator(change.Operator, vimGTargetLine(s.Prompt.lineCount(), change.Count), change.Motion, change.Count)
		case 'g':
			targetLine := 1
			if change.Count > 1 {
				targetLine = change.Count
			}
			s.applyVimLineMotionOperator(change.Operator, targetLine, change.Motion, change.Count)
		case 'j':
			s.applyVimLineMotionOperator(change.Operator, s.Prompt.currentLogicalLine()+change.Count+1, change.Motion, change.Count)
		case 'k':
			s.applyVimLineMotionOperator(change.Operator, s.Prompt.currentLogicalLine()-change.Count+1, change.Motion, change.Count)
		}
	case "lineOperator":
		s.applyVimLineOperator(change.Operator, change.Count)
	case "substitute":
		s.applyVimSubstitute(change.Count)
	case "operatorFind":
		start := s.Prompt.Cursor
		end, ok := s.Prompt.findCharMotion(change.Motion, change.Target, change.Count)
		if !ok {
			return
		}
		from, to := vimCharMotionRange(start, end, change.Motion)
		s.applyVimRangeOperator(change.Operator, from, to, false)
	case "operatorTextObj":
		start, end, ok := s.Prompt.findTextObjectRange(change.Scope, change.Object, change.Count)
		if !ok {
			return
		}
		s.applyVimRangeOperator(change.Operator, start, end, false)
	}
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
	s.VimPendingG = false
	s.VimPendingIndent = 0
	s.VimPendingCount = 0
	s.VimCount = 0
	s.VimPendingReplace = false
	s.VimRepeatingChar = false
}

func (s *REPLScreen) recordVimUndo() {
	snapshot := vimPromptSnapshot{
		Text:                   s.Prompt.Text,
		Cursor:                 s.Prompt.Cursor,
		PastedContents:         clonePastedContents(s.Prompt.PastedContents),
		NextPastedID:           s.Prompt.NextPastedID,
		PendingSpaceAfterImage: s.Prompt.pendingSpaceAfterImage,
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
	s.Prompt.pendingSpaceAfterImage = last.PendingSpaceAfterImage
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

func vimGTargetLine(lineCount int, count int) int {
	if count <= 1 {
		return lineCount
	}
	return count
}

func (p *PromptState) operatorMotionRange(operator rune, motion rune, count int) (int, int, bool, bool) {
	if count <= 0 {
		count = 1
	}
	runes := []rune(p.Text)
	start := p.clampCursor(p.Cursor)
	cursor := *p
	switch motion {
	case 'j':
		start, end, ok := p.lineMotionRange(p.currentLogicalLine() + count + 1)
		return start, end, true, ok
	case 'k':
		start, end, ok := p.lineMotionRange(p.currentLogicalLine() - count + 1)
		return start, end, true, ok
	}
	if operator == 'c' && (motion == 'w' || motion == 'W') {
		for i := 0; i < count-1; i++ {
			if motion == 'w' {
				cursor.moveWordForward()
			} else {
				cursor.moveWORDForward()
			}
		}
		if motion == 'w' {
			cursor.moveWordEnd()
		} else {
			cursor.moveWORDEnd()
		}
		end := cursor.Cursor
		if end < len(runes) {
			end++
		}
		return orderedRange(start, end, false)
	}
	for i := 0; i < count; i++ {
		switch motion {
		case 'h':
			cursor.Apply(Key{Type: KeyLeft})
		case 'l':
			cursor.Apply(Key{Type: KeyRight})
		case 'w':
			cursor.moveWordForward()
		case 'W':
			cursor.moveWORDForward()
		case 'e':
			cursor.moveWordEnd()
		case 'E':
			cursor.moveWORDEnd()
		case 'b':
			cursor.moveWordBackward()
		case 'B':
			cursor.moveWORDBackward()
		case '$':
			cursor.moveLineEnd()
		case '0':
			cursor.moveLineStart()
		case '^':
			cursor.moveFirstNonBlank()
		default:
			return 0, 0, false, false
		}
	}
	end := cursor.Cursor
	if (motion == 'e' || motion == 'E') && start <= end && end < len(runes) {
		end++
	}
	return orderedRange(start, end, false)
}

func (p *PromptState) backwardEndMotionRange(motion rune, count int) (int, int, bool) {
	if count <= 0 {
		count = 1
	}
	start := p.clampCursor(p.Cursor)
	cursor := *p
	for i := 0; i < count; i++ {
		switch motion {
		case 'e':
			cursor.moveWordBackwardEnd()
		case 'E':
			cursor.moveWORDBackwardEnd()
		default:
			return 0, 0, false
		}
	}
	end := cursor.Cursor
	start, end, _, ok := orderedRange(start, end, false)
	return start, end, ok
}

func orderedRange(start int, end int, linewise bool) (int, int, bool, bool) {
	if start == end {
		return 0, 0, false, false
	}
	if end < start {
		start, end = end, start
	}
	return start, end, linewise, true
}

func (p *PromptState) lineCount() int {
	return len(strings.Split(p.Text, "\n"))
}

func (p *PromptState) clampCursor(cursor int) int {
	runes := []rune(p.Text)
	if cursor < 0 {
		return 0
	}
	if cursor > len(runes) {
		return len(runes)
	}
	return cursor
}

func (p *PromptState) rangeText(start int, end int) string {
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
	return string(runes[start:end])
}

func (p *PromptState) goToLine(line int) {
	lines := strings.Split(p.Text, "\n")
	if line < 1 {
		line = 1
	}
	if line > len(lines) {
		line = len(lines)
	}
	p.Cursor = lineStartOffset(lines, line-1)
}

func (p *PromptState) lineMotionRange(targetLine int) (int, int, bool) {
	lines := strings.Split(p.Text, "\n")
	if len(lines) == 0 {
		return 0, 0, false
	}
	current := p.currentLogicalLine()
	if targetLine < 1 {
		targetLine = 1
	}
	if targetLine > len(lines) {
		targetLine = len(lines)
	}
	target := targetLine - 1
	startLine := current
	endLine := target
	if endLine < startLine {
		startLine, endLine = endLine, startLine
	}
	start := lineStartOffset(lines, startLine)
	end := len([]rune(p.Text))
	if endLine+1 < len(lines) {
		end = lineStartOffset(lines, endLine+1)
	}
	if start == end {
		return 0, 0, false
	}
	return start, end, true
}

func (p *PromptState) lineRange(count int) (int, int) {
	if count <= 0 {
		count = 1
	}
	runes := []rune(p.Text)
	cursor := p.clampCursor(p.Cursor)
	start := 0
	for i := cursor - 1; i >= 0; i-- {
		if runes[i] == '\n' {
			start = i + 1
			break
		}
	}
	end := start
	for i := 0; i < count && end < len(runes); i++ {
		for end < len(runes) && runes[end] != '\n' {
			end++
		}
		if end < len(runes) {
			end++
		}
	}
	return start, end
}

func (p *PromptState) currentLogicalLine() int {
	line := 0
	for i, r := range []rune(p.Text) {
		if i >= p.Cursor {
			break
		}
		if r == '\n' {
			line++
		}
	}
	return line
}

func (p *PromptState) moveLogicalLine(delta int) {
	line := p.currentLogicalLine() + delta + 1
	p.goToLine(line)
}

func (p *PromptState) toggleCase(count int) {
	if count <= 0 {
		count = 1
	}
	runes := []rune(p.Text)
	cursor := p.clampCursor(p.Cursor)
	for i := 0; i < count && cursor+i < len(runes); i++ {
		idx := cursor + i
		r := runes[idx]
		switch {
		case unicode.IsUpper(r):
			runes[idx] = unicode.ToLower(r)
		case unicode.IsLower(r):
			runes[idx] = unicode.ToUpper(r)
		default:
			runes[idx] = unicode.ToUpper(r)
		}
	}
	p.Text = string(runes)
	if cursor+count <= len(runes) {
		p.Cursor = cursor + count
	} else {
		p.Cursor = len(runes)
	}
	p.resetHistoryCursor()
}

func (p *PromptState) joinLines(count int) {
	if count <= 0 {
		count = 1
	}
	lines := strings.Split(p.Text, "\n")
	current := p.currentLogicalLine()
	if current >= len(lines)-1 {
		return
	}
	linesToJoin := count
	if linesToJoin > len(lines)-current-1 {
		linesToJoin = len(lines) - current - 1
	}
	joined := lines[current]
	cursorPos := len([]rune(joined))
	for i := 1; i <= linesToJoin; i++ {
		next := strings.TrimLeftFunc(lines[current+i], unicode.IsSpace)
		if next == "" {
			continue
		}
		if joined != "" && !strings.HasSuffix(joined, " ") {
			joined += " "
		}
		joined += next
	}
	newLines := make([]string, 0, len(lines)-linesToJoin)
	newLines = append(newLines, lines[:current]...)
	newLines = append(newLines, joined)
	newLines = append(newLines, lines[current+linesToJoin+1:]...)
	p.Text = strings.Join(newLines, "\n")
	p.Cursor = lineStartOffset(newLines, current) + cursorPos
	p.resetHistoryCursor()
}

func (p *PromptState) indentLines(dir rune, count int) {
	if count <= 0 {
		count = 1
	}
	lines := strings.Split(p.Text, "\n")
	current := p.currentLogicalLine()
	linesToAffect := count
	if linesToAffect > len(lines)-current {
		linesToAffect = len(lines) - current
	}
	for i := 0; i < linesToAffect; i++ {
		idx := current + i
		line := lines[idx]
		if dir == '>' {
			lines[idx] = "  " + line
			continue
		}
		switch {
		case strings.HasPrefix(line, "  "):
			lines[idx] = line[2:]
		case strings.HasPrefix(line, "\t"):
			lines[idx] = line[1:]
		default:
			removed := 0
			runes := []rune(line)
			cut := 0
			for cut < len(runes) && removed < 2 && unicode.IsSpace(runes[cut]) {
				cut++
				removed++
			}
			lines[idx] = string(runes[cut:])
		}
	}
	p.Text = strings.Join(lines, "\n")
	p.Cursor = lineStartOffset(lines, current) + firstNonBlankOffset(lines[current])
	p.resetHistoryCursor()
}

func (p *PromptState) openLine(below bool) {
	lines := strings.Split(p.Text, "\n")
	insertLine := p.currentLogicalLine()
	if below {
		insertLine++
	}
	newLines := make([]string, 0, len(lines)+1)
	newLines = append(newLines, lines[:insertLine]...)
	newLines = append(newLines, "")
	newLines = append(newLines, lines[insertLine:]...)
	p.Text = strings.Join(newLines, "\n")
	p.Cursor = lineStartOffset(newLines, insertLine)
	p.resetHistoryCursor()
}

func (p *PromptState) pasteCharacterwise(content string, after bool, count int) {
	runes := []rune(p.Text)
	insert := p.clampCursor(p.Cursor)
	if after && insert < len(runes) {
		insert++
	}
	insertRunes := []rune(strings.Repeat(content, count))
	runes = append(runes[:insert], append(insertRunes, runes[insert:]...)...)
	p.Text = string(runes)
	if len(insertRunes) > 0 {
		p.Cursor = insert + len(insertRunes) - 1
	} else {
		p.Cursor = insert
	}
	p.resetHistoryCursor()
}

func (p *PromptState) pasteLinewise(content string, after bool, count int) {
	content = strings.TrimSuffix(content, "\n")
	lines := strings.Split(p.Text, "\n")
	insertLine := p.currentLogicalLine()
	if after {
		insertLine++
	}
	contentLines := strings.Split(content, "\n")
	repeated := make([]string, 0, len(contentLines)*count)
	for i := 0; i < count; i++ {
		repeated = append(repeated, contentLines...)
	}
	newLines := make([]string, 0, len(lines)+len(repeated))
	newLines = append(newLines, lines[:insertLine]...)
	newLines = append(newLines, repeated...)
	newLines = append(newLines, lines[insertLine:]...)
	p.Text = strings.Join(newLines, "\n")
	p.Cursor = lineStartOffset(newLines, insertLine)
	p.resetHistoryCursor()
}

func lineStartOffset(lines []string, lineIndex int) int {
	if lineIndex <= 0 {
		return 0
	}
	if lineIndex > len(lines) {
		lineIndex = len(lines)
	}
	offset := 0
	for i := 0; i < lineIndex; i++ {
		offset += len([]rune(lines[i]))
	}
	return offset + lineIndex
}

func firstNonBlankOffset(line string) int {
	for i, r := range []rune(line) {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return 0
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

func (p *PromptState) moveWordBackwardEnd() {
	runes := []rune(p.Text)
	i := p.clampCursor(p.Cursor)
	if i < len(runes) && isWordRune(runes[i]) {
		for i >= 0 && isWordRune(runes[i]) {
			i--
		}
	} else {
		i--
	}
	for i >= 0 && !isWordRune(runes[i]) {
		i--
	}
	if i < 0 {
		i = 0
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

func (p *PromptState) moveWORDBackwardEnd() {
	runes := []rune(p.Text)
	i := p.clampCursor(p.Cursor)
	if i < len(runes) && !unicode.IsSpace(runes[i]) {
		for i >= 0 && !unicode.IsSpace(runes[i]) {
			i--
		}
	} else {
		i--
	}
	for i >= 0 && unicode.IsSpace(runes[i]) {
		i--
	}
	if i < 0 {
		i = 0
	}
	p.Cursor = i
}

func (p *PromptState) moveFirstNonBlank() {
	runes := []rune(p.Text)
	cursor := p.clampCursor(p.Cursor)
	start := 0
	for i := cursor - 1; i >= 0; i-- {
		if runes[i] == '\n' {
			start = i + 1
			break
		}
	}
	end := len(runes)
	for i := start; i < len(runes); i++ {
		if runes[i] == '\n' {
			end = i
			break
		}
	}
	for i := start; i < end; i++ {
		r := runes[i]
		if !unicode.IsSpace(r) {
			p.Cursor = i
			return
		}
	}
	p.Cursor = start
}

func (p *PromptState) moveLineEnd() {
	runes := []rune(p.Text)
	cursor := p.clampCursor(p.Cursor)
	for cursor < len(runes) && runes[cursor] != '\n' {
		cursor++
	}
	p.Cursor = cursor
}

func (p *PromptState) moveLineStart() {
	runes := []rune(p.Text)
	cursor := p.clampCursor(p.Cursor)
	for cursor > 0 && runes[cursor-1] != '\n' {
		cursor--
	}
	p.Cursor = cursor
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
	end := p.Cursor
	for end < len(runes) && runes[end] != '\n' {
		end++
	}
	if end == p.Cursor && end < len(runes) && runes[end] == '\n' {
		end++
	}
	killed := string(runes[p.Cursor:end])
	p.Text = string(runes[:p.Cursor]) + string(runes[end:])
	p.pushToKillRing(killed, killRingAppend)
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
	start := p.Cursor
	for start > 0 && runes[start-1] != '\n' {
		start--
	}
	killed := string(runes[start:p.Cursor])
	p.Text = string(runes[:start]) + string(runes[p.Cursor:])
	p.Cursor = start
	p.pushToKillRing(killed, killRingPrepend)
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
	killed := p.rangeText(start, end)
	p.deleteRange(start, end)
	p.pushToKillRing(killed, killRingPrepend)
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
	if obj == 'w' || obj == 'W' {
		return findWordTextObjectRange(runes, idx, scope, obj, count)
	}
	open, close, ok := textObjectPair(obj)
	if !ok {
		return 0, 0, false
	}
	if open == close {
		return findQuoteTextObjectRange(runes, idx, open, scope)
	}
	return findBracketTextObjectRange(runes, idx, open, close, scope)
}

func findWordTextObjectRange(runes []rune, idx int, scope rune, obj rune, count int) (int, int, bool) {
	if count <= 0 {
		count = 1
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

func textObjectPair(obj rune) (rune, rune, bool) {
	switch obj {
	case '(', ')', 'b':
		return '(', ')', true
	case '[', ']':
		return '[', ']', true
	case '{', '}', 'B':
		return '{', '}', true
	case '<', '>':
		return '<', '>', true
	case '"', '\'', '`':
		return obj, obj, true
	default:
		return 0, 0, false
	}
}

func findQuoteTextObjectRange(runes []rune, idx int, quote rune, scope rune) (int, int, bool) {
	lineStart := 0
	for i := idx - 1; i >= 0; i-- {
		if runes[i] == '\n' {
			lineStart = i + 1
			break
		}
	}
	lineEnd := len(runes)
	for i := idx; i < len(runes); i++ {
		if runes[i] == '\n' {
			lineEnd = i
			break
		}
	}
	var positions []int
	for i := lineStart; i < lineEnd; i++ {
		if runes[i] == quote {
			positions = append(positions, i)
		}
	}
	for i := 0; i < len(positions)-1; i += 2 {
		start := positions[i]
		end := positions[i+1]
		if start <= idx && idx <= end {
			if scope == 'i' {
				return start + 1, end, true
			}
			return start, end + 1, true
		}
	}
	return 0, 0, false
}

func findBracketTextObjectRange(runes []rune, idx int, open rune, close rune, scope rune) (int, int, bool) {
	depth := 0
	start := -1
	for i := idx; i >= 0; i-- {
		if runes[i] == close && i != idx {
			depth++
			continue
		}
		if runes[i] != open {
			continue
		}
		if depth == 0 {
			start = i
			break
		}
		depth--
	}
	if start == -1 {
		return 0, 0, false
	}
	depth = 0
	end := -1
	for i := start + 1; i < len(runes); i++ {
		switch runes[i] {
		case open:
			depth++
		case close:
			if depth == 0 {
				end = i
				break
			}
			depth--
		}
	}
	if end == -1 {
		return 0, 0, false
	}
	if scope == 'i' {
		return start + 1, end, true
	}
	return start, end + 1, true
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
