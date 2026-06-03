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

func ParseTerminalStringControl(sequence string) TerminalStringControlAction {
	control := TerminalStringControlAction{
		Type:     IdentifyTerminalSequence(sequence),
		Sequence: sequence,
	}
	if len(sequence) < 2 || sequence[0] != ESCPrefix[0] {
		return control
	}
	payload := sequence[2:]
	switch {
	case strings.HasSuffix(payload, OSCTerminator):
		control.Payload = strings.TrimSuffix(payload, OSCTerminator)
		control.Terminator = OSCTerminator
		control.Complete = true
	case strings.HasSuffix(payload, OSCStringTerminator):
		control.Payload = strings.TrimSuffix(payload, OSCStringTerminator)
		control.Terminator = OSCStringTerminator
		control.Complete = true
	default:
		control.Payload = payload
	}
	return control
}
