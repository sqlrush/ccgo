package tui

import "unicode/utf8"

const (
	terminalBEL = byte(0x07)
	terminalESC = byte(0x1b)
	terminalSS3 = byte(0x8f)
	terminalDCS = byte(0x90)
	terminalCSI = byte(0x9b)
	terminalST  = byte(0x9c)
	terminalOSC = byte(0x9d)
	terminalPM  = byte(0x9e)
	terminalAPC = byte(0x9f)
	terminalSOS = byte(0x98)
	terminalIND = byte(0x84)
	terminalNEL = byte(0x85)
	terminalHTS = byte(0x88)
	terminalRI  = byte(0x8d)

	escTypeCSI = byte('[')
	escTypeOSC = byte(']')
	escTypeDCS = byte('P')
	escTypeAPC = byte('_')
	escTypePM  = byte('^')
	escTypeSOS = byte('X')
	escTypeSS3 = byte('O')
	escTypeST  = byte('\\')
)

type TerminalTokenType string

const (
	TerminalTokenText     TerminalTokenType = "text"
	TerminalTokenSequence TerminalTokenType = "sequence"
)

type TerminalToken struct {
	Type  TerminalTokenType
	Value string
}

type TerminalTokenizerState string

const (
	terminalTokenizerGround             TerminalTokenizerState = "ground"
	terminalTokenizerEscape             TerminalTokenizerState = "escape"
	terminalTokenizerEscapeIntermediate TerminalTokenizerState = "escapeIntermediate"
	terminalTokenizerCSI                TerminalTokenizerState = "csi"
	terminalTokenizerSS3                TerminalTokenizerState = "ss3"
	terminalTokenizerOSC                TerminalTokenizerState = "osc"
	terminalTokenizerDCS                TerminalTokenizerState = "dcs"
	terminalTokenizerAPC                TerminalTokenizerState = "apc"
	terminalTokenizerPM                 TerminalTokenizerState = "pm"
	terminalTokenizerSOS                TerminalTokenizerState = "sos"
)

type TerminalTokenizerOptions struct {
	X10Mouse bool
}

type TerminalTokenizer struct {
	state    TerminalTokenizerState
	buffer   string
	x10Mouse bool
}

func NewTerminalTokenizer(options TerminalTokenizerOptions) *TerminalTokenizer {
	return &TerminalTokenizer{state: terminalTokenizerGround, x10Mouse: options.X10Mouse}
}

func NewTerminalOutputTokenizer() *TerminalTokenizer {
	return NewTerminalTokenizer(TerminalTokenizerOptions{})
}

func NewTerminalInputTokenizer() *TerminalTokenizer {
	return NewTerminalTokenizer(TerminalTokenizerOptions{X10Mouse: true})
}

func (t *TerminalTokenizer) Feed(input string) []TerminalToken {
	if t.state == "" {
		t.state = terminalTokenizerGround
	}
	tokens, state, buffer := tokenizeTerminal(input, t.state, t.buffer, false, t.x10Mouse)
	t.state = state
	t.buffer = buffer
	return tokens
}

func (t *TerminalTokenizer) Flush() []TerminalToken {
	if t.state == "" {
		t.state = terminalTokenizerGround
	}
	tokens, state, buffer := tokenizeTerminal("", t.state, t.buffer, true, t.x10Mouse)
	t.state = state
	t.buffer = buffer
	return tokens
}

func (t *TerminalTokenizer) Reset() {
	t.state = terminalTokenizerGround
	t.buffer = ""
}

func (t *TerminalTokenizer) Buffer() string {
	return t.buffer
}

func tokenizeTerminal(input string, initialState TerminalTokenizerState, initialBuffer string, flush bool, x10Mouse bool) ([]TerminalToken, TerminalTokenizerState, string) {
	tokens := []TerminalToken{}
	state := initialState
	if state == "" {
		state = terminalTokenizerGround
	}
	data := initialBuffer + input
	i := 0
	textStart := 0
	seqStart := 0

	flushText := func() {
		if i > textStart {
			text := data[textStart:i]
			if text != "" {
				tokens = append(tokens, TerminalToken{Type: TerminalTokenText, Value: text})
			}
		}
		textStart = i
	}
	emitSequence := func(sequence string) {
		if sequence != "" {
			tokens = append(tokens, TerminalToken{Type: TerminalTokenSequence, Value: sequence})
		}
		state = terminalTokenizerGround
		textStart = i
	}

	for i < len(data) {
		code := data[i]
		switch state {
		case terminalTokenizerGround:
			if size := terminalUTF8TextRuneSize(data, i); size > 0 {
				i += size
			} else if isC1ESCControlByte(code) {
				flushText()
				seqStart = i
				i++
				emitSequence(data[seqStart:i])
			} else if code == terminalESC {
				flushText()
				seqStart = i
				state = terminalTokenizerEscape
				i++
			} else if code == terminalCSI {
				flushText()
				seqStart = i
				state = terminalTokenizerCSI
				i++
			} else if code == terminalSS3 {
				flushText()
				seqStart = i
				state = terminalTokenizerSS3
				i++
			} else if code == terminalOSC {
				flushText()
				seqStart = i
				state = terminalTokenizerOSC
				i++
			} else if code == terminalDCS {
				flushText()
				seqStart = i
				state = terminalTokenizerDCS
				i++
			} else if code == terminalAPC {
				flushText()
				seqStart = i
				state = terminalTokenizerAPC
				i++
			} else if code == terminalPM {
				flushText()
				seqStart = i
				state = terminalTokenizerPM
				i++
			} else if code == terminalSOS {
				flushText()
				seqStart = i
				state = terminalTokenizerSOS
				i++
			} else {
				i++
			}
		case terminalTokenizerEscape:
			switch {
			case code == escTypeCSI:
				state = terminalTokenizerCSI
				i++
			case code == escTypeOSC:
				state = terminalTokenizerOSC
				i++
			case code == escTypeDCS:
				state = terminalTokenizerDCS
				i++
			case code == escTypeAPC:
				state = terminalTokenizerAPC
				i++
			case code == escTypePM:
				state = terminalTokenizerPM
				i++
			case code == escTypeSOS:
				state = terminalTokenizerSOS
				i++
			case code == escTypeSS3:
				state = terminalTokenizerSS3
				i++
			case IsCSIIntermediate(code):
				state = terminalTokenizerEscapeIntermediate
				i++
			case IsESCFinal(code):
				i++
				emitSequence(data[seqStart:i])
			case code == terminalESC:
				emitSequence(data[seqStart:i])
				seqStart = i
				state = terminalTokenizerEscape
				i++
			default:
				state = terminalTokenizerGround
				textStart = seqStart
			}
		case terminalTokenizerEscapeIntermediate:
			if IsCSIIntermediate(code) {
				i++
			} else if IsESCFinal(code) {
				i++
				emitSequence(data[seqStart:i])
			} else {
				state = terminalTokenizerGround
				textStart = seqStart
			}
		case terminalTokenizerCSI:
			if x10Mouse && code == 'M' && i-seqStart == csiPayloadOffset(data, seqStart) && x10MousePayloadLooksPrintable(data, i) {
				if i+4 <= len(data) {
					i += 4
					emitSequence(data[seqStart:i])
				} else {
					i = len(data)
				}
			} else if IsCSIFinal(code) {
				i++
				emitSequence(data[seqStart:i])
			} else if IsCSIParam(code) || IsCSIIntermediate(code) {
				i++
			} else {
				state = terminalTokenizerGround
				textStart = seqStart
			}
		case terminalTokenizerSS3:
			if IsCSIParam(code) {
				i++
			} else if IsCSIFinal(code) {
				i++
				emitSequence(data[seqStart:i])
			} else {
				state = terminalTokenizerGround
				textStart = seqStart
			}
		case terminalTokenizerOSC:
			if code == terminalBEL {
				i++
				emitSequence(data[seqStart:i])
			} else if code == terminalST {
				i++
				emitSequence(data[seqStart:i])
			} else if code == terminalESC && i+1 < len(data) && data[i+1] == escTypeST {
				i += 2
				emitSequence(data[seqStart:i])
			} else {
				i++
			}
		case terminalTokenizerDCS, terminalTokenizerAPC, terminalTokenizerPM, terminalTokenizerSOS:
			if code == terminalBEL {
				i++
				emitSequence(data[seqStart:i])
			} else if code == terminalST {
				i++
				emitSequence(data[seqStart:i])
			} else if code == terminalESC && i+1 < len(data) && data[i+1] == escTypeST {
				i += 2
				emitSequence(data[seqStart:i])
			} else {
				i++
			}
		}
	}

	if state == terminalTokenizerGround {
		flushText()
		return tokens, state, ""
	}
	if flush {
		remaining := data[seqStart:]
		if remaining != "" {
			tokens = append(tokens, TerminalToken{Type: TerminalTokenSequence, Value: remaining})
		}
		return tokens, terminalTokenizerGround, ""
	}
	return tokens, state, data[seqStart:]
}

func x10MousePayloadLooksPrintable(data string, index int) bool {
	for offset := 1; offset <= 3; offset++ {
		if index+offset < len(data) && data[index+offset] < 0x20 {
			return false
		}
	}
	return true
}

func csiPayloadOffset(data string, seqStart int) int {
	if seqStart >= 0 && seqStart < len(data) && data[seqStart] == terminalCSI {
		return 1
	}
	return 2
}

func terminalUTF8TextRuneSize(data string, index int) int {
	if index < 0 || index >= len(data) || data[index] < utf8.RuneSelf {
		return 0
	}
	_, size := utf8.DecodeRuneInString(data[index:])
	if size <= 1 {
		return 0
	}
	return size
}

func isC1ESCControlByte(code byte) bool {
	switch code {
	case terminalIND, terminalNEL, terminalHTS, terminalRI:
		return true
	default:
		return false
	}
}
