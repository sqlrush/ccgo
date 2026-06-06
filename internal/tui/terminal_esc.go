package tui

import "strings"

const (
	ESCPrefix                    = "\x1b"
	ESCResetSequence             = "\x1bc"
	ESCSaveCursor                = "\x1b7"
	ESCRestoreCursor             = "\x1b8"
	ESCIndex                     = "\x1bD"
	ESCReverseIndex              = "\x1bM"
	ESCNextLine                  = "\x1bE"
	ESCTabSet                    = "\x1bH"
	ESCFinalStart                = 0x30
	ESCFinalEnd                  = 0x7e
	ESCCharsetSingleShift2       = 'N'
	ESCCharsetSingleShift3       = 'O'
	ESCCharsetLockingShift2      = 'n'
	ESCCharsetLockingShift3      = 'o'
	ESCCharsetLockingShift3Right = '|'
	ESCCharsetLockingShift2Right = '}'
	ESCCharsetLockingShift1Right = '~'
	ESCCharsetSelectUTF8         = '%'
	ESCCharsetSelectG0           = '('
	ESCCharsetSelectG1           = ')'
	ESCCharsetSelectG2           = '*'
	ESCCharsetSelectG3           = '+'
	ESCCharsetSelectG1Alt        = '-'
	ESCCharsetSelectG2Alt        = '.'
	ESCCharsetSelectG3Alt        = '/'
)

type ESCActionType string

const (
	ESCActionCursor       ESCActionType = "cursor"
	ESCActionReset        ESCActionType = "reset"
	ESCActionCharset      ESCActionType = "charset"
	ESCActionCharsetShift ESCActionType = "charsetShift"
	ESCActionUnknown      ESCActionType = "unknown"
)

type ESCAction struct {
	Type              ESCActionType
	Cursor            CSICursorAction
	CharsetSlot       byte
	CharsetDesignator byte
	CharsetShift      string
	Sequence          string
}

func IsESCFinal(b byte) bool {
	return b >= ESCFinalStart && b <= ESCFinalEnd
}

func ParseESCSequence(sequence string) (ESCAction, bool) {
	if !strings.HasPrefix(sequence, ESCPrefix) || strings.HasPrefix(sequence, CSIPrefix) || strings.HasPrefix(sequence, OSCPrefix) {
		return ESCAction{}, false
	}
	return ParseESCContent(strings.TrimPrefix(sequence, ESCPrefix))
}

func ParseESCContent(chars string) (ESCAction, bool) {
	if chars == "" {
		return ESCAction{}, false
	}
	if isESCCharsetSelector(chars[0]) && len(chars) >= 2 {
		return ESCAction{Type: ESCActionCharset, CharsetSlot: chars[0], CharsetDesignator: chars[1], Sequence: ESCPrefix + chars}, true
	}
	if shift, ok := escCharsetShift(chars[0]); ok && len(chars) == 1 {
		return ESCAction{Type: ESCActionCharsetShift, CharsetShift: shift, Sequence: ESCPrefix + chars}, true
	}
	switch chars[0] {
	case 'c':
		return ESCAction{Type: ESCActionReset}, true
	case '7':
		return ESCAction{Type: ESCActionCursor, Cursor: CSICursorAction{Type: CSICursorActionSave}}, true
	case '8':
		return ESCAction{Type: ESCActionCursor, Cursor: CSICursorAction{Type: CSICursorActionRestore}}, true
	case 'D':
		return ESCAction{Type: ESCActionCursor, Cursor: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorDown, Count: 1}}, true
	case 'M':
		return ESCAction{Type: ESCActionCursor, Cursor: CSICursorAction{Type: CSICursorActionMove, Direction: CSICursorUp, Count: 1}}, true
	case 'E':
		return ESCAction{Type: ESCActionCursor, Cursor: CSICursorAction{Type: CSICursorActionNextLine, Count: 1}}, true
	case 'H':
		return ESCAction{Type: ESCActionCursor, Cursor: CSICursorAction{Type: CSICursorActionTabSet}}, true
	}
	return ESCAction{Type: ESCActionUnknown, Sequence: ESCPrefix + chars}, true
}

func isESCCharsetSelector(b byte) bool {
	switch b {
	case ESCCharsetSelectUTF8, ESCCharsetSelectG0, ESCCharsetSelectG1, ESCCharsetSelectG2, ESCCharsetSelectG3,
		ESCCharsetSelectG1Alt, ESCCharsetSelectG2Alt, ESCCharsetSelectG3Alt:
		return true
	default:
		return false
	}
}

func escCharsetShift(b byte) (string, bool) {
	switch b {
	case ESCCharsetSingleShift2:
		return "singleShiftG2", true
	case ESCCharsetSingleShift3:
		return "singleShiftG3", true
	case ESCCharsetLockingShift2:
		return "lockingShiftG2", true
	case ESCCharsetLockingShift3:
		return "lockingShiftG3", true
	case ESCCharsetLockingShift3Right:
		return "lockingShiftG3Right", true
	case ESCCharsetLockingShift2Right:
		return "lockingShiftG2Right", true
	case ESCCharsetLockingShift1Right:
		return "lockingShiftG1Right", true
	default:
		return "", false
	}
}
