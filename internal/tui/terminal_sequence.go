package tui

import "strings"

type TerminalSequenceType string

const (
	TerminalSequenceCSI     TerminalSequenceType = "csi"
	TerminalSequenceOSC     TerminalSequenceType = "osc"
	TerminalSequenceESC     TerminalSequenceType = "esc"
	TerminalSequenceSS3     TerminalSequenceType = "ss3"
	TerminalSequenceDCS     TerminalSequenceType = "dcs"
	TerminalSequenceAPC     TerminalSequenceType = "apc"
	TerminalSequencePM      TerminalSequenceType = "pm"
	TerminalSequenceSOS     TerminalSequenceType = "sos"
	TerminalSequenceUnknown TerminalSequenceType = "unknown"
)

const (
	C1SS3Prefix        = "\x8f"
	C1DCSPrefix        = "\x90"
	C1SOSPrefix        = "\x98"
	C1StringTerminator = "\x9c"
	C1OSCPrefix        = "\x9d"
	C1PMPrefix         = "\x9e"
	C1APCPrefix        = "\x9f"
)

type TerminalStringControlAction struct {
	Type       TerminalSequenceType
	Payload    string
	Terminator string
	Complete   bool
	Sequence   string
}

type TerminalSequenceAction struct {
	Type          TerminalSequenceType
	CSI           CSIAction
	OSC           OSCAction
	ESC           ESCAction
	StringControl TerminalStringControlAction
	Sequence      string
}

func IdentifyTerminalSequence(sequence string) TerminalSequenceType {
	if strings.HasPrefix(sequence, C1CSIPrefix) {
		return TerminalSequenceCSI
	}
	if strings.HasPrefix(sequence, C1OSCPrefix) {
		return TerminalSequenceOSC
	}
	if strings.HasPrefix(sequence, C1SS3Prefix) {
		return TerminalSequenceSS3
	}
	if strings.HasPrefix(sequence, C1DCSPrefix) {
		return TerminalSequenceDCS
	}
	if strings.HasPrefix(sequence, C1APCPrefix) {
		return TerminalSequenceAPC
	}
	if strings.HasPrefix(sequence, C1PMPrefix) {
		return TerminalSequencePM
	}
	if strings.HasPrefix(sequence, C1SOSPrefix) {
		return TerminalSequenceSOS
	}
	if len(sequence) < 2 || sequence[0] != ESCPrefix[0] {
		return TerminalSequenceUnknown
	}
	switch sequence[1] {
	case '[':
		return TerminalSequenceCSI
	case ']':
		return TerminalSequenceOSC
	case 'O':
		return TerminalSequenceSS3
	case 'P':
		return TerminalSequenceDCS
	case '_':
		return TerminalSequenceAPC
	case '^':
		return TerminalSequencePM
	case 'X':
		return TerminalSequenceSOS
	default:
		return TerminalSequenceESC
	}
}

func ParseTerminalSequence(sequence string) (TerminalSequenceAction, bool) {
	switch IdentifyTerminalSequence(sequence) {
	case TerminalSequenceCSI:
		action, ok := ParseCSISequence(sequence)
		if !ok {
			return TerminalSequenceAction{}, false
		}
		return TerminalSequenceAction{Type: TerminalSequenceCSI, CSI: action}, true
	case TerminalSequenceOSC:
		action, ok := ParseOSCSequence(sequence)
		if !ok {
			content, hasPrefix := trimOSCPrefix(sequence)
			if !hasPrefix {
				return TerminalSequenceAction{}, false
			}
			action = ParseOSCContent(content)
		}
		return TerminalSequenceAction{Type: TerminalSequenceOSC, OSC: action}, true
	case TerminalSequenceESC:
		action, ok := ParseESCSequence(sequence)
		if !ok {
			return TerminalSequenceAction{}, false
		}
		return TerminalSequenceAction{Type: TerminalSequenceESC, ESC: action}, true
	case TerminalSequenceSS3:
		action, ok := ParseSS3Sequence(sequence)
		if !ok {
			return TerminalSequenceAction{Type: TerminalSequenceUnknown, Sequence: sequence}, true
		}
		return TerminalSequenceAction{Type: TerminalSequenceSS3, CSI: action}, true
	case TerminalSequenceDCS, TerminalSequenceAPC, TerminalSequencePM, TerminalSequenceSOS:
		return TerminalSequenceAction{
			Type:          IdentifyTerminalSequence(sequence),
			StringControl: ParseTerminalStringControl(sequence),
		}, true
	default:
		if len(sequence) > 0 && sequence[0] == ESCPrefix[0] {
			return TerminalSequenceAction{Type: TerminalSequenceUnknown, Sequence: sequence}, true
		}
		return TerminalSequenceAction{}, false
	}
}

func ParseSS3Sequence(sequence string) (CSIAction, bool) {
	inner, ok := trimSS3Prefix(sequence)
	if !ok {
		return CSIAction{}, false
	}
	if len(inner) > 1 {
		return parseModifiedSS3CursorSequence(sequence)
	}
	if len(inner) != 1 {
		return CSIAction{}, false
	}
	switch inner[0] {
	case 'A':
		return csiCursorMove(CSICursorUp, 1), true
	case 'B':
		return csiCursorMove(CSICursorDown, 1), true
	case 'C':
		return csiCursorMove(CSICursorForward, 1), true
	case 'D':
		return csiCursorMove(CSICursorBack, 1), true
	default:
		return CSIAction{}, false
	}
}

func trimSS3Prefix(sequence string) (string, bool) {
	switch {
	case strings.HasPrefix(sequence, ESCPrefix+"O"):
		return strings.TrimPrefix(sequence, ESCPrefix+"O"), true
	case strings.HasPrefix(sequence, C1SS3Prefix):
		return strings.TrimPrefix(sequence, C1SS3Prefix), true
	default:
		return "", false
	}
}

func parseModifiedSS3CursorSequence(sequence string) (CSIAction, bool) {
	switch {
	case isModifiedNavigationSS3(sequence, "A"):
		return csiCursorMove(CSICursorUp, 1), true
	case isModifiedNavigationSS3(sequence, "B"):
		return csiCursorMove(CSICursorDown, 1), true
	case isModifiedNavigationSS3(sequence, "C"):
		return csiCursorMove(CSICursorForward, 1), true
	case isModifiedNavigationSS3(sequence, "D"):
		return csiCursorMove(CSICursorBack, 1), true
	default:
		return CSIAction{}, false
	}
}

func ParseTerminalStringControl(sequence string) TerminalStringControlAction {
	control := TerminalStringControlAction{
		Type:     IdentifyTerminalSequence(sequence),
		Sequence: sequence,
	}
	prefixLen, ok := terminalStringControlPrefixLen(sequence)
	if !ok {
		return control
	}
	payload := sequence[prefixLen:]
	switch {
	case strings.HasSuffix(payload, OSCTerminator):
		control.Payload = strings.TrimSuffix(payload, OSCTerminator)
		control.Terminator = OSCTerminator
		control.Complete = true
	case strings.HasSuffix(payload, OSCStringTerminator):
		control.Payload = strings.TrimSuffix(payload, OSCStringTerminator)
		control.Terminator = OSCStringTerminator
		control.Complete = true
	case strings.HasSuffix(payload, C1StringTerminator):
		control.Payload = strings.TrimSuffix(payload, C1StringTerminator)
		control.Terminator = C1StringTerminator
		control.Complete = true
	default:
		control.Payload = payload
	}
	return control
}

func terminalStringControlPrefixLen(sequence string) (int, bool) {
	if strings.HasPrefix(sequence, C1DCSPrefix) ||
		strings.HasPrefix(sequence, C1APCPrefix) ||
		strings.HasPrefix(sequence, C1PMPrefix) ||
		strings.HasPrefix(sequence, C1SOSPrefix) {
		return 1, true
	}
	if len(sequence) < 2 || sequence[0] != ESCPrefix[0] {
		return 0, false
	}
	switch sequence[1] {
	case 'P', '_', '^', 'X':
		return 2, true
	default:
		return 0, false
	}
}
