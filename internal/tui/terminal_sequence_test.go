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
		{sequence: "\x9d0;title\x9c", want: TerminalSequenceOSC},
		{sequence: "\x90payload\x9c", want: TerminalSequenceDCS},
		{sequence: "\x9fpayload\x9c", want: TerminalSequenceAPC},
		{sequence: "\x9epayload\x9c", want: TerminalSequencePM},
		{sequence: "\x98payload\x9c", want: TerminalSequenceSOS},
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
	c1OSC, ok := ParseTerminalSequence("\x9d" + OSCSetTitleAndIcon + ";C1\x9c")
	if !ok || c1OSC.Type != TerminalSequenceOSC || c1OSC.OSC.Type != OSCActionTitle || c1OSC.OSC.Title.Title != "C1" {
		t.Fatalf("c1 osc dispatch = %#v ok=%v", c1OSC, ok)
	}

	esc, ok := ParseTerminalSequence(ESCIndex)
	if !ok || esc.Type != TerminalSequenceESC || esc.ESC.Type != ESCActionCursor || esc.ESC.Cursor.Type != CSICursorActionMove || esc.ESC.Cursor.Direction != CSICursorDown {
		t.Fatalf("esc dispatch = %#v ok=%v", esc, ok)
	}

	ss3, ok := ParseTerminalSequence("\x1bOA")
	if !ok || ss3.Type != TerminalSequenceSS3 || ss3.CSI.Type != CSIActionCursor || ss3.CSI.Cursor.Type != CSICursorActionMove || ss3.CSI.Cursor.Direction != CSICursorUp || ss3.CSI.Cursor.Count != 1 {
		t.Fatalf("ss3 dispatch = %#v ok=%v", ss3, ok)
	}
	modifiedSS3Cases := []struct {
		seq       string
		direction CSICursorDirection
	}{
		{seq: "\x1bO1;2A", direction: CSICursorUp},
		{seq: "\x1bO1;5B", direction: CSICursorDown},
		{seq: "\x1bO1;9C", direction: CSICursorForward},
		{seq: "\x1bO1;16D", direction: CSICursorBack},
	}
	for _, tc := range modifiedSS3Cases {
		action, ok := ParseTerminalSequence(tc.seq)
		if !ok || action.Type != TerminalSequenceSS3 || action.CSI.Type != CSIActionCursor || action.CSI.Cursor.Type != CSICursorActionMove || action.CSI.Cursor.Direction != tc.direction || action.CSI.Cursor.Count != 1 {
			t.Fatalf("modified ss3 dispatch %q = %#v ok=%v", tc.seq, action, ok)
		}
	}
	unknownSS3, ok := ParseTerminalSequence("\x1bOP")
	if !ok || unknownSS3.Type != TerminalSequenceUnknown || unknownSS3.Sequence != "\x1bOP" {
		t.Fatalf("unknown ss3 dispatch = %#v ok=%v", unknownSS3, ok)
	}
	unknownModifiedSS3, ok := ParseTerminalSequence("\x1bO2;3A")
	if !ok || unknownModifiedSS3.Type != TerminalSequenceUnknown || unknownModifiedSS3.Sequence != "\x1bO2;3A" {
		t.Fatalf("unknown modified ss3 dispatch = %#v ok=%v", unknownModifiedSS3, ok)
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
	c1DCS, ok := ParseTerminalSequence("\x90tmux;" + EnterAlternateScreen + "\x9c")
	if !ok || c1DCS.Type != TerminalSequenceDCS || c1DCS.StringControl.Type != TerminalSequenceDCS || !c1DCS.StringControl.Complete || c1DCS.StringControl.Terminator != "\x9c" || c1DCS.StringControl.Payload != "tmux;"+EnterAlternateScreen {
		t.Fatalf("c1 dcs dispatch = %#v ok=%v", c1DCS, ok)
	}
	c1APC, ok := ParseTerminalSequence("\x9fpayload\x9c")
	if !ok || c1APC.Type != TerminalSequenceAPC || c1APC.StringControl.Type != TerminalSequenceAPC || !c1APC.StringControl.Complete || c1APC.StringControl.Terminator != "\x9c" || c1APC.StringControl.Payload != "payload" {
		t.Fatalf("c1 apc dispatch = %#v ok=%v", c1APC, ok)
	}
	c1PM, ok := ParseTerminalSequence("\x9epartial")
	if !ok || c1PM.Type != TerminalSequencePM || c1PM.StringControl.Type != TerminalSequencePM || c1PM.StringControl.Complete || c1PM.StringControl.Payload != "partial" {
		t.Fatalf("c1 pm dispatch = %#v ok=%v", c1PM, ok)
	}
	c1SOS, ok := ParseTerminalSequence("\x98sos\x9c")
	if !ok || c1SOS.Type != TerminalSequenceSOS || c1SOS.StringControl.Type != TerminalSequenceSOS || !c1SOS.StringControl.Complete || c1SOS.StringControl.Payload != "sos" {
		t.Fatalf("c1 sos dispatch = %#v ok=%v", c1SOS, ok)
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
	utf8Charset, ok := ParseTerminalSequence("\x1b%G")
	if !ok || utf8Charset.Type != TerminalSequenceESC || utf8Charset.ESC.Type != ESCActionCharset || utf8Charset.ESC.CharsetSlot != '%' || utf8Charset.ESC.CharsetDesignator != 'G' {
		t.Fatalf("utf8 charset esc dispatch = %#v ok=%v", utf8Charset, ok)
	}
	charsetShift, ok := ParseTerminalSequence("\x1bn")
	if !ok || charsetShift.Type != TerminalSequenceESC || charsetShift.ESC.Type != ESCActionCharsetShift || charsetShift.ESC.CharsetShift != "lockingShiftG2" {
		t.Fatalf("charset shift esc dispatch = %#v ok=%v", charsetShift, ok)
	}
	keypadApplication, ok := ParseTerminalSequence("\x1b=")
	if !ok || keypadApplication.Type != TerminalSequenceESC || keypadApplication.ESC.Type != ESCActionMode || keypadApplication.ESC.Mode.Type != CSIModeActionApplicationKeypad || !keypadApplication.ESC.Mode.Enabled {
		t.Fatalf("application keypad esc dispatch = %#v ok=%v", keypadApplication, ok)
	}
	keypadNumeric, ok := ParseTerminalSequence("\x1b>")
	if !ok || keypadNumeric.Type != TerminalSequenceESC || keypadNumeric.ESC.Type != ESCActionMode || keypadNumeric.ESC.Mode.Type != CSIModeActionApplicationKeypad || keypadNumeric.ESC.Mode.Enabled {
		t.Fatalf("numeric keypad esc dispatch = %#v ok=%v", keypadNumeric, ok)
	}
	screen, ok := ParseTerminalSequence("\x1b#8")
	if !ok || screen.Type != TerminalSequenceESC || screen.ESC.Type != ESCActionScreen || screen.ESC.Screen.Type != ESCScreenActionAlignmentTest {
		t.Fatalf("screen esc dispatch = %#v ok=%v", screen, ok)
	}
	report, ok := ParseTerminalSequence(ESCDeviceAttributes)
	if !ok || report.Type != TerminalSequenceESC || report.ESC.Type != ESCActionReport || report.ESC.Report.Type != CSIReportActionDeviceAttrs {
		t.Fatalf("device attributes esc dispatch = %#v ok=%v", report, ok)
	}
	if text, ok := ParseTerminalSequence("plain"); ok || text.Type != "" {
		t.Fatalf("plain dispatch = %#v ok=%v", text, ok)
	}
}
