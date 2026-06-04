package tui

import (
	"fmt"
	"strings"
)

const (
	CSIPrefix         = "\x1b["
	CursorLeft        = "\x1b[G"
	CursorSave        = "\x1b[s"
	CursorRestore     = "\x1b[u"
	EraseLine         = "\x1b[2K"
	ResetScrollRegion = "\x1b[r"
	PasteStart        = "\x1b[200~"
	PasteEnd          = "\x1b[201~"
	FocusInSequence   = "\x1b[I"
	FocusOutSequence  = "\x1b[O"

	CSIParamStart        = 0x30
	CSIParamEnd          = 0x3f
	CSIIntermediateStart = 0x20
	CSIIntermediateEnd   = 0x2f
	CSIFinalStart        = 0x40
	CSIFinalEnd          = 0x7e

	CSICommandInsertCharacters byte = '@'
	CSICommandCursorUp         byte = 'A'
	CSICommandCursorDown       byte = 'B'
	CSICommandCursorForward    byte = 'C'
	CSICommandCursorBack       byte = 'D'
	CSICommandCursorNextLine   byte = 'E'
	CSICommandCursorPrevLine   byte = 'F'
	CSICommandCursorColumn     byte = 'G'
	CSICommandCursorPosition   byte = 'H'
	CSICommandCursorTab        byte = 'I'
	CSICommandEraseDisplay     byte = 'J'
	CSICommandEraseLine        byte = 'K'
	CSICommandInsertLines      byte = 'L'
	CSICommandDeleteLines      byte = 'M'
	CSICommandDeleteCharacters byte = 'P'
	CSICommandEraseCharacters  byte = 'X'
	CSICommandBackwardTab      byte = 'Z'
	CSICommandScrollUp         byte = 'S'
	CSICommandScrollDown       byte = 'T'
	CSICommandCursorColumnAlt  byte = '`'
	CSICommandCursorForwardAlt byte = 'a'
	CSICommandRepeatPreceding  byte = 'b'
	CSICommandDeviceAttributes byte = 'c'
	CSICommandVerticalPosition byte = 'd'
	CSICommandCursorDownAlt    byte = 'e'
	CSICommandHorizontalVPos   byte = 'f'
	CSICommandTabClear         byte = 'g'
	CSICommandSetMode          byte = 'h'
	CSICommandResetMode        byte = 'l'
	CSICommandSGR              byte = 'm'
	CSICommandDSR              byte = 'n'
	CSICommandSoftReset        byte = 'p'
	CSICommandCursorStyle      byte = 'q'
	CSICommandScrollRegion     byte = 'r'
	CSICommandSaveCursor       byte = 's'
	CSICommandWindowReport     byte = 't'
	CSICommandRestoreCursor    byte = 'u'
	CSICommandTerminalParams   byte = 'x'

	CSIModeInsert   = 4
	CSIModeLineFeed = 20

	DECModeApplicationCursor  = 1
	DECModeColumn             = 3
	DECModeReverseVideo       = 5
	DECModeOrigin             = 6
	DECModeAutoWrap           = 7
	DECModeAutoRepeat         = 8
	DECModeMouseX10           = 9
	DECModeCursorBlink        = 12
	DECModeCursorVisible      = 25
	DECModeAltScreen          = 47
	DECModeReverseWrap        = 45
	DECModeApplicationKeypad  = 66
	DECModeAltScreenBuffer    = 1047
	DECModeSaveRestoreCursor  = 1048
	DECModeAltScreenClear     = 1049
	DECModeMouseNormal        = 1000
	DECModeMouseHighlight     = 1001
	DECModeMouseButton        = 1002
	DECModeMouseAny           = 1003
	DECModeFocusEvents        = 1004
	DECModeMouseUTF8          = 1005
	DECModeMouseSGR           = 1006
	DECModeAlternateScroll    = 1007
	DECModeMouseURXVT         = 1015
	DECModeBracketedPaste     = 2004
	DECModeSynchronizedUpdate = 2026
)

type CursorStyle string

const (
	CursorStyleBlock     CursorStyle = "block"
	CursorStyleUnderline CursorStyle = "underline"
	CursorStyleBar       CursorStyle = "bar"
)

type CSIActionType string

const (
	CSIActionCursor  CSIActionType = "cursor"
	CSIActionErase   CSIActionType = "erase"
	CSIActionEdit    CSIActionType = "edit"
	CSIActionReport  CSIActionType = "report"
	CSIActionScroll  CSIActionType = "scroll"
	CSIActionMode    CSIActionType = "mode"
	CSIActionSGR     CSIActionType = "sgr"
	CSIActionReset   CSIActionType = "reset"
	CSIActionUnknown CSIActionType = "unknown"
)

type CSICursorDirection string

const (
	CSICursorUp      CSICursorDirection = "up"
	CSICursorDown    CSICursorDirection = "down"
	CSICursorForward CSICursorDirection = "forward"
	CSICursorBack    CSICursorDirection = "back"
)

type CSICursorActionType string

const (
	CSICursorActionMove     CSICursorActionType = "move"
	CSICursorActionPosition CSICursorActionType = "position"
	CSICursorActionColumn   CSICursorActionType = "column"
	CSICursorActionRow      CSICursorActionType = "row"
	CSICursorActionSave     CSICursorActionType = "save"
	CSICursorActionRestore  CSICursorActionType = "restore"
	CSICursorActionShow     CSICursorActionType = "show"
	CSICursorActionHide     CSICursorActionType = "hide"
	CSICursorActionStyle    CSICursorActionType = "style"
	CSICursorActionNextLine CSICursorActionType = "nextLine"
	CSICursorActionPrevLine CSICursorActionType = "prevLine"
	CSICursorActionTab      CSICursorActionType = "tab"
	CSICursorActionBackTab  CSICursorActionType = "backTab"
	CSICursorActionTabSet   CSICursorActionType = "tabSet"
	CSICursorActionTabClear CSICursorActionType = "tabClear"
)

type CSICursorAction struct {
	Type      CSICursorActionType
	Direction CSICursorDirection
	Count     int
	Row       int
	Column    int
	Style     CursorStyle
	Blinking  bool
}

type CSIEraseActionType string

const (
	CSIEraseActionDisplay CSIEraseActionType = "display"
	CSIEraseActionLine    CSIEraseActionType = "line"
	CSIEraseActionChars   CSIEraseActionType = "chars"
)

type CSIEraseRegion string

const (
	CSIEraseToEnd      CSIEraseRegion = "toEnd"
	CSIEraseToStart    CSIEraseRegion = "toStart"
	CSIEraseAll        CSIEraseRegion = "all"
	CSIEraseScrollback CSIEraseRegion = "scrollback"
)

type CSIEraseAction struct {
	Type   CSIEraseActionType
	Region CSIEraseRegion
	Count  int
}

type CSIEditActionType string

const (
	CSIEditActionInsertChars CSIEditActionType = "insertChars"
	CSIEditActionDeleteChars CSIEditActionType = "deleteChars"
	CSIEditActionInsertLines CSIEditActionType = "insertLines"
	CSIEditActionDeleteLines CSIEditActionType = "deleteLines"
	CSIEditActionRepeatChars CSIEditActionType = "repeatChars"
)

type CSIEditAction struct {
	Type  CSIEditActionType
	Count int
}

type CSIReportActionType string

const (
	CSIReportActionDeviceStatus   CSIReportActionType = "deviceStatus"
	CSIReportActionCursorPosition CSIReportActionType = "cursorPosition"
	CSIReportActionDeviceAttrs    CSIReportActionType = "deviceAttributes"
	CSIReportActionTerminalParams CSIReportActionType = "terminalParameters"
	CSIReportActionWindow         CSIReportActionType = "window"
	CSIReportActionUnknown        CSIReportActionType = "unknown"
)

type CSIReportAction struct {
	Type        CSIReportActionType
	Code        int
	PrivateMode byte
}

type CSIScrollActionType string

const (
	CSIScrollActionUp        CSIScrollActionType = "up"
	CSIScrollActionDown      CSIScrollActionType = "down"
	CSIScrollActionSetRegion CSIScrollActionType = "setRegion"
)

type CSIScrollAction struct {
	Type   CSIScrollActionType
	Count  int
	Top    int
	Bottom int
}

type CSIModeActionType string

const (
	CSIModeActionApplicationCursor CSIModeActionType = "applicationCursor"
	CSIModeActionAlternateScreen   CSIModeActionType = "alternateScreen"
	CSIModeActionBracketedPaste    CSIModeActionType = "bracketedPaste"
	CSIModeActionInsert            CSIModeActionType = "insertMode"
	CSIModeActionLineFeed          CSIModeActionType = "lineFeedMode"
	CSIModeActionColumn            CSIModeActionType = "columnMode"
	CSIModeActionReverseVideo      CSIModeActionType = "reverseVideo"
	CSIModeActionOrigin            CSIModeActionType = "originMode"
	CSIModeActionAutoWrap          CSIModeActionType = "autoWrap"
	CSIModeActionAutoRepeat        CSIModeActionType = "autoRepeat"
	CSIModeActionReverseWrap       CSIModeActionType = "reverseWrap"
	CSIModeActionCursorBlink       CSIModeActionType = "cursorBlink"
	CSIModeActionApplicationKeypad CSIModeActionType = "applicationKeypad"
	CSIModeActionMouseTracking     CSIModeActionType = "mouseTracking"
	CSIModeActionFocusEvents       CSIModeActionType = "focusEvents"
	CSIModeActionAlternateScroll   CSIModeActionType = "alternateScroll"
	CSIModeActionSynchronized      CSIModeActionType = "synchronizedOutput"
)

type CSIMouseTrackingMode string

const (
	CSIMouseTrackingOff       CSIMouseTrackingMode = "off"
	CSIMouseTrackingX10       CSIMouseTrackingMode = "x10"
	CSIMouseTrackingNormal    CSIMouseTrackingMode = "normal"
	CSIMouseTrackingHighlight CSIMouseTrackingMode = "highlight"
	CSIMouseTrackingButton    CSIMouseTrackingMode = "button"
	CSIMouseTrackingAny       CSIMouseTrackingMode = "any"
	CSIMouseTrackingUTF8      CSIMouseTrackingMode = "utf8"
	CSIMouseTrackingSGR       CSIMouseTrackingMode = "sgr"
	CSIMouseTrackingURXVT     CSIMouseTrackingMode = "urxvt"
)

type CSIModeAction struct {
	Type      CSIModeActionType
	Enabled   bool
	MouseMode CSIMouseTrackingMode
}

type CSIAction struct {
	Type      CSIActionType
	Cursor    CSICursorAction
	Erase     CSIEraseAction
	Edit      CSIEditAction
	Report    CSIReportAction
	Scroll    CSIScrollAction
	Mode      CSIModeAction
	SGRParams string
	Sequence  string
}

func CSISequence(args ...any) string {
	if len(args) == 0 {
		return CSIPrefix
	}
	if len(args) == 1 {
		return CSIPrefix + fmt.Sprint(args[0])
	}
	params := make([]string, 0, len(args)-1)
	for _, arg := range args[:len(args)-1] {
		params = append(params, fmt.Sprint(arg))
	}
	return CSIPrefix + strings.Join(params, ";") + fmt.Sprint(args[len(args)-1])
}

func IsCSIParam(b byte) bool {
	return b >= CSIParamStart && b <= CSIParamEnd
}

func IsCSIIntermediate(b byte) bool {
	return b >= CSIIntermediateStart && b <= CSIIntermediateEnd
}

func IsCSIFinal(b byte) bool {
	return b >= CSIFinalStart && b <= CSIFinalEnd
}

func ParseCSISequence(sequence string) (CSIAction, bool) {
	if !strings.HasPrefix(sequence, CSIPrefix) {
		return CSIAction{}, false
	}
	inner := strings.TrimPrefix(sequence, CSIPrefix)
	if len(inner) == 0 {
		return CSIAction{}, false
	}
	final := inner[len(inner)-1]
	if !IsCSIFinal(final) {
		return CSIAction{Type: CSIActionUnknown, Sequence: sequence}, true
	}
	beforeFinal := inner[:len(inner)-1]
	privateMode := byte(0)
	paramStr := beforeFinal
	if len(beforeFinal) > 0 {
		switch beforeFinal[0] {
		case '?', '>', '=':
			privateMode = beforeFinal[0]
			paramStr = beforeFinal[1:]
		}
	}
	intermediate := ""
	for len(paramStr) > 0 {
		b := paramStr[len(paramStr)-1]
		if (b >= '0' && b <= '9') || b == ';' || b == ':' {
			break
		}
		intermediate = string(b) + intermediate
		paramStr = paramStr[:len(paramStr)-1]
	}
	params := parseCSIParams(paramStr)
	p0 := csiParamDefault(params, 0, 1)
	p1 := csiParamDefault(params, 1, 1)

	if final == CSICommandSGR && privateMode == 0 {
		return CSIAction{Type: CSIActionSGR, SGRParams: paramStr}, true
	}

	switch final {
	case CSICommandCursorUp:
		return csiCursorMove(CSICursorUp, p0), true
	case CSICommandCursorDown:
		return csiCursorMove(CSICursorDown, p0), true
	case CSICommandCursorForward:
		return csiCursorMove(CSICursorForward, p0), true
	case CSICommandCursorForwardAlt:
		return csiCursorMove(CSICursorForward, p0), true
	case CSICommandRepeatPreceding:
		return csiEdit(CSIEditActionRepeatChars, p0), true
	case CSICommandCursorBack:
		return csiCursorMove(CSICursorBack, p0), true
	case CSICommandCursorNextLine:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionNextLine, Count: p0}}, true
	case CSICommandCursorPrevLine:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionPrevLine, Count: p0}}, true
	case CSICommandCursorTab:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionTab, Count: p0}}, true
	case CSICommandBackwardTab:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionBackTab, Count: p0}}, true
	case CSICommandCursorColumn:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionColumn, Column: p0}}, true
	case CSICommandCursorColumnAlt:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionColumn, Column: p0}}, true
	case CSICommandCursorPosition, CSICommandHorizontalVPos:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionPosition, Row: p0, Column: p1}}, true
	case CSICommandDeviceAttributes:
		return csiDeviceAttributes(csiParamDefault(params, 0, 0), privateMode), true
	case CSICommandCursorDownAlt:
		return csiCursorMove(CSICursorDown, p0), true
	case CSICommandVerticalPosition:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionRow, Row: p0}}, true
	case CSICommandTabClear:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionTabClear, Count: csiParamDefault(params, 0, 0)}}, true
	case CSICommandEraseDisplay:
		return CSIAction{Type: CSIActionErase, Erase: CSIEraseAction{Type: CSIEraseActionDisplay, Region: csiEraseDisplayRegion(csiParamDefault(params, 0, 0))}}, true
	case CSICommandEraseLine:
		return CSIAction{Type: CSIActionErase, Erase: CSIEraseAction{Type: CSIEraseActionLine, Region: csiEraseLineRegion(csiParamDefault(params, 0, 0))}}, true
	case CSICommandEraseCharacters:
		return CSIAction{Type: CSIActionErase, Erase: CSIEraseAction{Type: CSIEraseActionChars, Count: p0}}, true
	case CSICommandInsertCharacters:
		return csiEdit(CSIEditActionInsertChars, p0), true
	case CSICommandDeleteCharacters:
		return csiEdit(CSIEditActionDeleteChars, p0), true
	case CSICommandInsertLines:
		return csiEdit(CSIEditActionInsertLines, p0), true
	case CSICommandDeleteLines:
		return csiEdit(CSIEditActionDeleteLines, p0), true
	case CSICommandDSR:
		return csiReport(p0, privateMode), true
	case CSICommandSoftReset:
		if intermediate == "!" && privateMode == 0 && len(params) == 0 {
			return CSIAction{Type: CSIActionReset}, true
		}
	case CSICommandTerminalParams:
		return csiTerminalParameters(csiParamDefault(params, 0, 0), privateMode), true
	case CSICommandScrollUp:
		return CSIAction{Type: CSIActionScroll, Scroll: CSIScrollAction{Type: CSIScrollActionUp, Count: p0}}, true
	case CSICommandScrollDown:
		return CSIAction{Type: CSIActionScroll, Scroll: CSIScrollAction{Type: CSIScrollActionDown, Count: p0}}, true
	case CSICommandScrollRegion:
		return csiScrollRegion(params), true
	case CSICommandSaveCursor:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionSave}}, true
	case CSICommandWindowReport:
		return csiWindowReport(csiParamDefault(params, 0, 0), privateMode), true
	case CSICommandRestoreCursor:
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionRestore}}, true
	case CSICommandCursorStyle:
		if intermediate == " " {
			style, blinking := csiCursorStyle(p0)
			return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: CSICursorActionStyle, Style: style, Blinking: blinking}}, true
		}
	}

	if privateMode == '?' && (final == CSICommandSetMode || final == CSICommandResetMode) {
		if action, ok := csiPrivateModeAction(p0, final == CSICommandSetMode); ok {
			return action, true
		}
	}
	if privateMode == 0 && (final == CSICommandSetMode || final == CSICommandResetMode) {
		if action, ok := csiModeAction(p0, final == CSICommandSetMode); ok {
			return action, true
		}
	}

	return CSIAction{Type: CSIActionUnknown, Sequence: sequence}, true
}

func parseCSIParams(paramStr string) []int {
	if paramStr == "" {
		return nil
	}
	fields := strings.Split(strings.ReplaceAll(paramStr, ":", ";"), ";")
	params := make([]int, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			params = append(params, 0)
			continue
		}
		value := 0
		for _, r := range field {
			if r < '0' || r > '9' {
				value = 0
				break
			}
			value = value*10 + int(r-'0')
		}
		params = append(params, value)
	}
	return params
}

func csiParamDefault(params []int, index int, defaultValue int) int {
	if index >= len(params) {
		return defaultValue
	}
	return params[index]
}

func csiCursorMove(direction CSICursorDirection, count int) CSIAction {
	return CSIAction{
		Type:   CSIActionCursor,
		Cursor: CSICursorAction{Type: CSICursorActionMove, Direction: direction, Count: count},
	}
}

func csiEdit(actionType CSIEditActionType, count int) CSIAction {
	return CSIAction{
		Type: CSIActionEdit,
		Edit: CSIEditAction{Type: actionType, Count: count},
	}
}

func csiReport(code int, privateMode byte) CSIAction {
	actionType := CSIReportActionUnknown
	if privateMode == 0 {
		switch code {
		case 5:
			actionType = CSIReportActionDeviceStatus
		case 6:
			actionType = CSIReportActionCursorPosition
		}
	}
	return CSIAction{
		Type:   CSIActionReport,
		Report: CSIReportAction{Type: actionType, Code: code, PrivateMode: privateMode},
	}
}

func csiDeviceAttributes(code int, privateMode byte) CSIAction {
	return CSIAction{
		Type:   CSIActionReport,
		Report: CSIReportAction{Type: CSIReportActionDeviceAttrs, Code: code, PrivateMode: privateMode},
	}
}

func csiTerminalParameters(code int, privateMode byte) CSIAction {
	return CSIAction{
		Type:   CSIActionReport,
		Report: CSIReportAction{Type: CSIReportActionTerminalParams, Code: code, PrivateMode: privateMode},
	}
}

func csiWindowReport(code int, privateMode byte) CSIAction {
	return CSIAction{
		Type:   CSIActionReport,
		Report: CSIReportAction{Type: CSIReportActionWindow, Code: code, PrivateMode: privateMode},
	}
}

func csiScrollRegion(params []int) CSIAction {
	top := 1
	if len(params) > 0 && params[0] > 0 {
		top = params[0]
	}
	bottom := 0
	if len(params) > 1 && params[1] > 0 {
		bottom = params[1]
	}
	return CSIAction{Type: CSIActionScroll, Scroll: CSIScrollAction{Type: CSIScrollActionSetRegion, Top: top, Bottom: bottom}}
}

func csiEraseDisplayRegion(index int) CSIEraseRegion {
	switch index {
	case 1:
		return CSIEraseToStart
	case 2:
		return CSIEraseAll
	case 3:
		return CSIEraseScrollback
	default:
		return CSIEraseToEnd
	}
}

func csiEraseLineRegion(index int) CSIEraseRegion {
	switch index {
	case 1:
		return CSIEraseToStart
	case 2:
		return CSIEraseAll
	default:
		return CSIEraseToEnd
	}
}

func csiCursorStyle(index int) (CursorStyle, bool) {
	switch index {
	case 0, 1:
		return CursorStyleBlock, true
	case 2:
		return CursorStyleBlock, false
	case 3:
		return CursorStyleUnderline, true
	case 4:
		return CursorStyleUnderline, false
	case 5:
		return CursorStyleBar, true
	case 6:
		return CursorStyleBar, false
	default:
		return CursorStyleBlock, true
	}
}

func csiPrivateModeAction(mode int, enabled bool) (CSIAction, bool) {
	switch mode {
	case DECModeApplicationCursor:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionApplicationCursor, Enabled: enabled}}, true
	case DECModeColumn:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionColumn, Enabled: enabled}}, true
	case DECModeReverseVideo:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionReverseVideo, Enabled: enabled}}, true
	case DECModeOrigin:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionOrigin, Enabled: enabled}}, true
	case DECModeAutoWrap:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionAutoWrap, Enabled: enabled}}, true
	case DECModeAutoRepeat:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionAutoRepeat, Enabled: enabled}}, true
	case DECModeReverseWrap:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionReverseWrap, Enabled: enabled}}, true
	case DECModeMouseX10:
		return csiMouseModeAction(CSIMouseTrackingX10, enabled), true
	case DECModeCursorBlink:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionCursorBlink, Enabled: enabled}}, true
	case DECModeCursorVisible:
		cursorType := CSICursorActionHide
		if enabled {
			cursorType = CSICursorActionShow
		}
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: cursorType}}, true
	case DECModeAltScreen, DECModeAltScreenBuffer, DECModeAltScreenClear:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionAlternateScreen, Enabled: enabled}}, true
	case DECModeApplicationKeypad:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionApplicationKeypad, Enabled: enabled}}, true
	case DECModeSaveRestoreCursor:
		cursorType := CSICursorActionRestore
		if enabled {
			cursorType = CSICursorActionSave
		}
		return CSIAction{Type: CSIActionCursor, Cursor: CSICursorAction{Type: cursorType}}, true
	case DECModeBracketedPaste:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionBracketedPaste, Enabled: enabled}}, true
	case DECModeMouseNormal:
		return csiMouseModeAction(CSIMouseTrackingNormal, enabled), true
	case DECModeMouseHighlight:
		return csiMouseModeAction(CSIMouseTrackingHighlight, enabled), true
	case DECModeMouseButton:
		return csiMouseModeAction(CSIMouseTrackingButton, enabled), true
	case DECModeMouseAny:
		return csiMouseModeAction(CSIMouseTrackingAny, enabled), true
	case DECModeMouseUTF8:
		return csiMouseModeAction(CSIMouseTrackingUTF8, enabled), true
	case DECModeMouseSGR:
		return csiMouseModeAction(CSIMouseTrackingSGR, enabled), true
	case DECModeAlternateScroll:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionAlternateScroll, Enabled: enabled}}, true
	case DECModeMouseURXVT:
		return csiMouseModeAction(CSIMouseTrackingURXVT, enabled), true
	case DECModeFocusEvents:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionFocusEvents, Enabled: enabled}}, true
	case DECModeSynchronizedUpdate:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionSynchronized, Enabled: enabled}}, true
	default:
		return CSIAction{}, false
	}
}

func csiModeAction(mode int, enabled bool) (CSIAction, bool) {
	switch mode {
	case CSIModeInsert:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionInsert, Enabled: enabled}}, true
	case CSIModeLineFeed:
		return CSIAction{Type: CSIActionMode, Mode: CSIModeAction{Type: CSIModeActionLineFeed, Enabled: enabled}}, true
	default:
		return CSIAction{}, false
	}
}

func csiMouseModeAction(mode CSIMouseTrackingMode, enabled bool) CSIAction {
	if !enabled {
		mode = CSIMouseTrackingOff
	}
	return CSIAction{
		Type: CSIActionMode,
		Mode: CSIModeAction{Type: CSIModeActionMouseTracking, MouseMode: mode, Enabled: enabled},
	}
}

func CursorUp(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "A")
}

func CursorDown(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "B")
}

func CursorForward(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "C")
}

func CursorBack(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "D")
}

func CursorToColumn(column int) string {
	return CSISequence(column, "G")
}

func CursorPosition(row int, column int) string {
	return CSISequence(row, column, "H")
}

func CursorMove(x int, y int) string {
	var out strings.Builder
	if x < 0 {
		out.WriteString(CursorBack(-x))
	} else if x > 0 {
		out.WriteString(CursorForward(x))
	}
	if y < 0 {
		out.WriteString(CursorUp(-y))
	} else if y > 0 {
		out.WriteString(CursorDown(y))
	}
	return out.String()
}

func SetCursorStyleSequence(style CursorStyle, blinking bool) string {
	code := 0
	switch style {
	case CursorStyleBlock:
		if blinking {
			code = 1
		} else {
			code = 2
		}
	case CursorStyleUnderline:
		if blinking {
			code = 3
		} else {
			code = 4
		}
	case CursorStyleBar:
		if blinking {
			code = 5
		} else {
			code = 6
		}
	default:
		code = 0
	}
	return CSISequence(fmt.Sprintf("%d q", code))
}

func EraseToEndOfLine() string {
	return CSISequence("K")
}

func EraseToStartOfLine() string {
	return CSISequence(1, "K")
}

func EraseLineSequence() string {
	return EraseLine
}

func EraseLinesSequence(n int) string {
	if n <= 0 {
		return ""
	}
	var out strings.Builder
	for i := 0; i < n; i++ {
		out.WriteString(EraseLine)
		if i < n-1 {
			out.WriteString(CursorUp(1))
		}
	}
	out.WriteString(CursorLeft)
	return out.String()
}

func EraseToEndOfScreen() string {
	return CSISequence("J")
}

func EraseToStartOfScreen() string {
	return CSISequence(1, "J")
}

func EraseScreenSequence() string {
	return ClearScreen
}

func ScrollUp(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "S")
}

func ScrollDown(n int) string {
	if n == 0 {
		return ""
	}
	return CSISequence(n, "T")
}

func SetScrollRegion(top int, bottom int) string {
	return CSISequence(top, bottom, "r")
}
