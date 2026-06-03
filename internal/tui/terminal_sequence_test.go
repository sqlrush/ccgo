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
	if !ok || ss3.Type != TerminalSequenceUnknown || ss3.Sequence != "\x1bOA" {
		t.Fatalf("ss3 dispatch = %#v ok=%v", ss3, ok)
	}

	unknown, ok := ParseTerminalSequence("\x1b?")
	if !ok || unknown.Type != TerminalSequenceESC || unknown.ESC.Type != ESCActionUnknown || unknown.ESC.Sequence != "\x1b?" {
		t.Fatalf("unknown esc dispatch = %#v ok=%v", unknown, ok)
	}

	if ignored, ok := ParseTerminalSequence("\x1b(B"); ok || ignored.Type != "" {
		t.Fatalf("ignored esc dispatch = %#v ok=%v", ignored, ok)
	}
	if text, ok := ParseTerminalSequence("plain"); ok || text.Type != "" {
		t.Fatalf("plain dispatch = %#v ok=%v", text, ok)
	}
}
