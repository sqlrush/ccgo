package tui

import "testing"

func TestIdentifyTerminalSequence(t *testing.T) {
	cases := []struct {
		sequence string
		want     TerminalSequenceType
	}{
		{sequence: "text", want: TerminalSequenceUnknown},
		{sequence: CSISequence("H"), want: TerminalSequenceCSI},
		{sequence: TerminalTitleSequence("Claude"), want: TerminalSequenceOSC},
		{sequence: ESCSaveCursor, want: TerminalSequenceESC},
		{sequence: "\x1bOA", want: TerminalSequenceSS3},
		{sequence: "\x1bPpayload" + OSCStringTerminator, want: TerminalSequenceDCS},
		{sequence: "\x1b_payload" + OSCTerminator, want: TerminalSequenceAPC},
		{sequence: "\x1b^payload" + OSCStringTerminator, want: TerminalSequencePM},
		{sequence: "\x1bXpayload" + OSCTerminator, want: TerminalSequenceSOS},
	}
	for _, tc := range cases {
		if got := IdentifyTerminalSequence(tc.sequence); got != tc.want {
			t.Fatalf("identify %q = %q, want %q", tc.sequence, got, tc.want)
		}
	}
}

func TestParseTerminalSequenceDispatchesActions(t *testing.T) {
	csi, ok := ParseTerminalSequence(CursorPosition(2, 3))
	if !ok || csi.Type != TerminalSequenceCSI || csi.CSI.Type != CSIActionCursor || csi.CSI.Cursor.Type != CSICursorActionPosition || csi.CSI.Cursor.Row != 2 || csi.CSI.Cursor.Column != 3 {
		t.Fatalf("csi dispatch = %#v ok=%v", csi, ok)
	}

	osc, ok := ParseTerminalSequence(TerminalTitleSequence("Claude"))
	if !ok || osc.Type != TerminalSequenceOSC || osc.OSC.Type != OSCActionTitle || osc.OSC.Title.Title != "Claude" {
		t.Fatalf("osc dispatch = %#v ok=%v", osc, ok)
	}
	partialOSC, ok := ParseTerminalSequence(OSCPrefix + OSCSetTitleAndIcon + ";Partial")
	if !ok || partialOSC.Type != TerminalSequenceOSC || partialOSC.OSC.Type != OSCActionTitle || partialOSC.OSC.Title.Title != "Partial" {
		t.Fatalf("partial osc dispatch = %#v ok=%v", partialOSC, ok)
	}

	esc, ok := ParseTerminalSequence(ESCIndex)
	if !ok || esc.Type != TerminalSequenceESC || esc.ESC.Type != ESCActionCursor || esc.ESC.Cursor.Type != CSICursorActionMove || esc.ESC.Cursor.Direction != CSICursorDown {
		t.Fatalf("esc dispatch = %#v ok=%v", esc, ok)
	}

	ss3, ok := ParseTerminalSequence("\x1bOA")
	if !ok || ss3.Type != TerminalSequenceSS3 || ss3.CSI.Type != CSIActionCursor || ss3.CSI.Cursor.Type != CSICursorActionMove || ss3.CSI.Cursor.Direction != CSICursorUp || ss3.CSI.Cursor.Count != 1 {
		t.Fatalf("ss3 dispatch = %#v ok=%v", ss3, ok)
	}
	unknownSS3, ok := ParseTerminalSequence("\x1bOP")
	if !ok || unknownSS3.Type != TerminalSequenceUnknown || unknownSS3.Sequence != "\x1bOP" {
		t.Fatalf("unknown ss3 dispatch = %#v ok=%v", unknownSS3, ok)
	}

	dcs, ok := ParseTerminalSequence("\x1bPtmux;" + EnterAlternateScreen + OSCStringTerminator)
	if !ok || dcs.Type != TerminalSequenceDCS || dcs.StringControl.Type != TerminalSequenceDCS || !dcs.StringControl.Complete || dcs.StringControl.Terminator != OSCStringTerminator || dcs.StringControl.Payload != "tmux;"+EnterAlternateScreen {
		t.Fatalf("dcs dispatch = %#v ok=%v", dcs, ok)
	}
	apc, ok := ParseTerminalSequence("\x1b_payload" + OSCTerminator)
	if !ok || apc.Type != TerminalSequenceAPC || apc.StringControl.Type != TerminalSequenceAPC || !apc.StringControl.Complete || apc.StringControl.Terminator != OSCTerminator || apc.StringControl.Payload != "payload" {
		t.Fatalf("apc dispatch = %#v ok=%v", apc, ok)
	}
	pm, ok := ParseTerminalSequence("\x1b^partial")
	if !ok || pm.Type != TerminalSequencePM || pm.StringControl.Type != TerminalSequencePM || pm.StringControl.Complete || pm.StringControl.Payload != "partial" {
		t.Fatalf("pm dispatch = %#v ok=%v", pm, ok)
	}
	sos, ok := ParseTerminalSequence("\x1bXsos" + OSCTerminator)
	if !ok || sos.Type != TerminalSequenceSOS || sos.StringControl.Type != TerminalSequenceSOS || !sos.StringControl.Complete || sos.StringControl.Payload != "sos" {
		t.Fatalf("sos dispatch = %#v ok=%v", sos, ok)
	}

	unknown, ok := ParseTerminalSequence("\x1b?")
	if !ok || unknown.Type != TerminalSequenceESC || unknown.ESC.Type != ESCActionUnknown || unknown.ESC.Sequence != "\x1b?" {
		t.Fatalf("unknown esc dispatch = %#v ok=%v", unknown, ok)
	}

	charset, ok := ParseTerminalSequence("\x1b(B")
	if !ok || charset.Type != TerminalSequenceESC || charset.ESC.Type != ESCActionCharset || charset.ESC.CharsetSlot != '(' || charset.ESC.CharsetDesignator != 'B' {
		t.Fatalf("charset esc dispatch = %#v ok=%v", charset, ok)
	}
	extendedCharset, ok := ParseTerminalSequence("\x1b*B")
	if !ok || extendedCharset.Type != TerminalSequenceESC || extendedCharset.ESC.Type != ESCActionCharset || extendedCharset.ESC.CharsetSlot != '*' || extendedCharset.ESC.CharsetDesignator != 'B' {
		t.Fatalf("extended charset esc dispatch = %#v ok=%v", extendedCharset, ok)
	}
	if text, ok := ParseTerminalSequence("plain"); ok || text.Type != "" {
		t.Fatalf("plain dispatch = %#v ok=%v", text, ok)
	}
}
