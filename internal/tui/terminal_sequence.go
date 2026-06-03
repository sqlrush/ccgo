package tui

import "strings"

type TerminalSequenceType string

const (
	TerminalSequenceCSI     TerminalSequenceType = "csi"
	TerminalSequenceOSC     TerminalSequenceType = "osc"
	TerminalSequenceESC     TerminalSequenceType = "esc"
	TerminalSequenceSS3     TerminalSequenceType = "ss3"
	TerminalSequenceUnknown TerminalSequenceType = "unknown"
)

type TerminalSequenceAction struct {
	Type     TerminalSequenceType
	CSI      CSIAction
	OSC      OSCAction
	ESC      ESCAction
	Sequence string
}

func IdentifyTerminalSequence(sequence string) TerminalSequenceType {
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
			if !strings.HasPrefix(sequence, OSCPrefix) {
				return TerminalSequenceAction{}, false
			}
			action = ParseOSCContent(strings.TrimPrefix(sequence, OSCPrefix))
		}
		return TerminalSequenceAction{Type: TerminalSequenceOSC, OSC: action}, true
	case TerminalSequenceESC:
		action, ok := ParseESCSequence(sequence)
		if !ok {
			return TerminalSequenceAction{}, false
		}
		return TerminalSequenceAction{Type: TerminalSequenceESC, ESC: action}, true
	case TerminalSequenceSS3:
		return TerminalSequenceAction{Type: TerminalSequenceUnknown, Sequence: sequence}, true
	default:
		if len(sequence) > 0 && sequence[0] == ESCPrefix[0] {
			return TerminalSequenceAction{Type: TerminalSequenceUnknown, Sequence: sequence}, true
		}
		return TerminalSequenceAction{}, false
	}
}
