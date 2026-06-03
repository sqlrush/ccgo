package tui

import "testing"

func TestTerminalParserTextBellAndGraphemeWidths(t *testing.T) {
	parser := NewTerminalParser()
	actions := parser.Feed("a界😀\x07b")
	if len(actions) != 3 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[0].Type != TerminalActionText {
		t.Fatalf("text action = %#v", actions[0])
	}
	want := []TerminalGrapheme{
		{Value: "a", Width: 1},
		{Value: "界", Width: 2},
		{Value: "😀", Width: 2},
	}
	if len(actions[0].Graphemes) != len(want) {
		t.Fatalf("graphemes = %#v", actions[0].Graphemes)
	}
	for i, got := range actions[0].Graphemes {
		if got != want[i] {
			t.Fatalf("grapheme %d = %#v, want %#v", i, got, want[i])
		}
	}
	if actions[1].Type != TerminalActionBell {
		t.Fatalf("bell action = %#v", actions[1])
	}
	if actions[2].Type != TerminalActionText || len(actions[2].Graphemes) != 1 || actions[2].Graphemes[0].Value != "b" {
		t.Fatalf("trailing text = %#v", actions[2])
	}
}

func TestTerminalVisibleTextUsesParserAndPreservesRawBell(t *testing.T) {
	input := "a" +
		CSISequence(31, "m") + "red" + CSISequence("m") +
		OSCPrefix + OSCSetTitleAndIcon + ";hidden" + OSCStringTerminator +
		"b" +
		"\x1bPignored\x1b\\" +
		"c" +
		"\x1b_ignored\x07" +
		"d" +
		"\x1b^pm\x1b\\" +
		"e" +
		"\x1bXsos\x07" +
		"f\x07"
	if got := TerminalVisibleText(input); got != "aredbcdef\x07" {
		t.Fatalf("visible = %q", got)
	}
	if got := TerminalVisibleText("x" + OSCPrefix + OSCSetTitleAndIcon + ";partial"); got != "x" {
		t.Fatalf("partial visible = %q", got)
	}
}

func TestTerminalParserDispatchesStringControlActions(t *testing.T) {
	parser := NewTerminalParser()
	input := "a" +
		"\x1bPtmux;" + EnterAlternateScreen + OSCStringTerminator +
		"b" +
		"\x1b_payload" + OSCTerminator +
		"c" +
		"\x1b^pm" + OSCStringTerminator +
		"d" +
		"\x1bXsos" + OSCTerminator +
		"e"
	actions := parser.Feed(input)
	if len(actions) != 9 {
		t.Fatalf("actions = %#v", actions)
	}
	want := []struct {
		index      int
		kind       TerminalSequenceType
		payload    string
		terminator string
	}{
		{index: 1, kind: TerminalSequenceDCS, payload: "tmux;" + EnterAlternateScreen, terminator: OSCStringTerminator},
		{index: 3, kind: TerminalSequenceAPC, payload: "payload", terminator: OSCTerminator},
		{index: 5, kind: TerminalSequencePM, payload: "pm", terminator: OSCStringTerminator},
		{index: 7, kind: TerminalSequenceSOS, payload: "sos", terminator: OSCTerminator},
	}
	for _, tc := range want {
		action := actions[tc.index]
		if action.Type != TerminalActionStringControl || action.String.Type != tc.kind || !action.String.Complete || action.String.Payload != tc.payload || action.String.Terminator != tc.terminator {
			t.Fatalf("string control %d = %#v", tc.index, action)
		}
	}
	if got := TerminalVisibleText(input); got != "abcde" {
		t.Fatalf("visible = %q", got)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("\x1bPpartial"); len(actions) != 0 {
		t.Fatalf("partial feed actions = %#v", actions)
	}
	actions = parser.Flush()
	if len(actions) != 1 || actions[0].Type != TerminalActionStringControl || actions[0].String.Type != TerminalSequenceDCS || actions[0].String.Complete || actions[0].String.Payload != "partial" {
		t.Fatalf("partial flush actions = %#v", actions)
	}
}

func TestTerminalParserSegmentsCommonGraphemeClusters(t *testing.T) {
	parser := NewTerminalParser()
	actions := parser.Feed("e\u0301 \u2764\ufe0f \U0001f44b\U0001f3fd \U0001f469\u200d\U0001f4bb \U0001f1fa\U0001f1f8 \U0001f3f4\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f")
	if len(actions) != 1 || actions[0].Type != TerminalActionText {
		t.Fatalf("actions = %#v", actions)
	}
	want := []TerminalGrapheme{
		{Value: "e\u0301", Width: 2},
		{Value: " ", Width: 1},
		{Value: "\u2764\ufe0f", Width: 2},
		{Value: " ", Width: 1},
		{Value: "\U0001f44b\U0001f3fd", Width: 2},
		{Value: " ", Width: 1},
		{Value: "\U0001f469\u200d\U0001f4bb", Width: 2},
		{Value: " ", Width: 1},
		{Value: "\U0001f1fa\U0001f1f8", Width: 2},
		{Value: " ", Width: 1},
		{Value: "\U0001f3f4\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f", Width: 2},
	}
	if len(actions[0].Graphemes) != len(want) {
		t.Fatalf("graphemes = %#v", actions[0].Graphemes)
	}
	for i, got := range actions[0].Graphemes {
		if got != want[i] {
			t.Fatalf("grapheme %d = %#v, want %#v", i, got, want[i])
		}
	}
}

func TestTerminalParserAppliesSGRToFollowingText(t *testing.T) {
	parser := NewTerminalParser()
	actions := parser.Feed("plain" + CSISequence(31, "m") + "red" + CSISequence("m") + "normal")
	if len(actions) != 3 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[0].Type != TerminalActionText || actions[0].Style.Foreground.Type != TerminalColorDefault {
		t.Fatalf("plain text = %#v", actions[0])
	}
	if actions[1].Type != TerminalActionText || actions[1].Style.Foreground != namedTerminalColor(NamedColorRed) {
		t.Fatalf("red text = %#v", actions[1])
	}
	if actions[2].Type != TerminalActionText || !TextStylesEqual(actions[2].Style, DefaultTextStyle()) {
		t.Fatalf("normal text = %#v", actions[2])
	}
	if !TextStylesEqual(parser.Style(), DefaultTextStyle()) {
		t.Fatalf("parser style = %#v", parser.Style())
	}
}

func TestTerminalParserDispatchesSequenceActions(t *testing.T) {
	parser := NewTerminalParser()
	actions := parser.Feed(CursorPosition(2, 3) + EraseLine + CSISequence(2, "@") + CSISequence(6, "n") + CSISequence(">1c") + ScrollUp(2) + EnableBracketedPaste + TerminalTitleSequence("Claude") + ESCIndex + "\x1bOA")
	if len(actions) != 10 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[0].Type != TerminalActionCursor || actions[0].Cursor.Type != CSICursorActionPosition || actions[0].Cursor.Row != 2 || actions[0].Cursor.Column != 3 {
		t.Fatalf("cursor action = %#v", actions[0])
	}
	if actions[1].Type != TerminalActionErase || actions[1].Erase.Type != CSIEraseActionLine || actions[1].Erase.Region != CSIEraseAll {
		t.Fatalf("erase action = %#v", actions[1])
	}
	if actions[2].Type != TerminalActionEdit || actions[2].Edit.Type != CSIEditActionInsertChars || actions[2].Edit.Count != 2 {
		t.Fatalf("edit action = %#v", actions[2])
	}
	if actions[3].Type != TerminalActionReport || actions[3].Report.Type != CSIReportActionCursorPosition || actions[3].Report.Code != 6 {
		t.Fatalf("report action = %#v", actions[3])
	}
	if actions[4].Type != TerminalActionReport || actions[4].Report.Type != CSIReportActionDeviceAttrs || actions[4].Report.Code != 1 || actions[4].Report.PrivateMode != '>' {
		t.Fatalf("device attributes action = %#v", actions[4])
	}
	if actions[5].Type != TerminalActionScroll || actions[5].Scroll.Type != CSIScrollActionUp || actions[5].Scroll.Count != 2 {
		t.Fatalf("scroll action = %#v", actions[5])
	}
	if actions[6].Type != TerminalActionMode || actions[6].Mode.Type != CSIModeActionBracketedPaste || !actions[6].Mode.Enabled {
		t.Fatalf("mode action = %#v", actions[6])
	}
	if actions[7].Type != TerminalActionTitle || actions[7].OSC.Title.Title != "Claude" {
		t.Fatalf("title action = %#v", actions[7])
	}
	if actions[8].Type != TerminalActionCursor || actions[8].Cursor.Type != CSICursorActionMove || actions[8].Cursor.Direction != CSICursorDown {
		t.Fatalf("esc cursor action = %#v", actions[8])
	}
	if actions[9].Type != TerminalActionUnknown || actions[9].Sequence != "\x1bOA" {
		t.Fatalf("unknown action = %#v", actions[9])
	}
}

func TestTerminalParserUsesOutputTokenizerForCSIM(t *testing.T) {
	parser := NewTerminalParser()
	actions := parser.Feed("\x1b[M`rK")
	if len(actions) != 2 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[0].Type != TerminalActionEdit || actions[0].Edit.Type != CSIEditActionDeleteLines || actions[0].Edit.Count != 1 {
		t.Fatalf("csi m action = %#v", actions[0])
	}
	if actions[1].Type != TerminalActionText || len(actions[1].Graphemes) != 3 {
		t.Fatalf("payload text = %#v", actions[1])
	}
}

func TestTerminalParserTracksHyperlinkState(t *testing.T) {
	parser := NewTerminalParser()
	start := TerminalHyperlinkSequence("https://example.com/docs", nil)
	actions := parser.Feed(start + "docs")
	if len(actions) != 2 {
		t.Fatalf("start actions = %#v", actions)
	}
	if actions[0].Type != TerminalActionLink || actions[0].OSC.Hyperlink.URL != "https://example.com/docs" {
		t.Fatalf("link start action = %#v", actions[0])
	}
	if !parser.InLink() || parser.LinkURL() != "https://example.com/docs" {
		t.Fatalf("parser link state = inLink %v url %q", parser.InLink(), parser.LinkURL())
	}
	if actions[1].Type != TerminalActionText || actions[1].Graphemes[0].Value != "d" {
		t.Fatalf("link text action = %#v", actions[1])
	}

	actions = parser.Feed(EndTerminalHyperlinkSequence())
	if len(actions) != 1 || actions[0].Type != TerminalActionLink || !actions[0].OSC.Hyperlink.End {
		t.Fatalf("link end actions = %#v", actions)
	}
	if parser.InLink() || parser.LinkURL() != "" {
		t.Fatalf("parser link end state = inLink %v url %q", parser.InLink(), parser.LinkURL())
	}

	parser.Feed(start)
	parser.Reset()
	if parser.InLink() || parser.LinkURL() != "" {
		t.Fatalf("parser reset link state = inLink %v url %q", parser.InLink(), parser.LinkURL())
	}
}

func TestTerminalParserResetClearsStyle(t *testing.T) {
	parser := NewTerminalParser()
	actions := parser.Feed(CSISequence(1, "m") + "bold" + ESCResetSequence + "plain")
	if len(actions) != 3 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[0].Type != TerminalActionText || !actions[0].Style.Bold {
		t.Fatalf("bold text = %#v", actions[0])
	}
	if actions[1].Type != TerminalActionReset {
		t.Fatalf("reset action = %#v", actions[1])
	}
	if actions[2].Type != TerminalActionText || !TextStylesEqual(actions[2].Style, DefaultTextStyle()) {
		t.Fatalf("plain text = %#v", actions[2])
	}

	parser = NewTerminalParser()
	actions = parser.Feed(TerminalHyperlinkSequence("https://example.com", nil) + CSISequence(31, "m") + "red" + CSISequence("!p") + "plain")
	if len(actions) != 4 {
		t.Fatalf("soft reset actions = %#v", actions)
	}
	if actions[1].Type != TerminalActionText || actions[1].Style.Foreground != namedTerminalColor(NamedColorRed) {
		t.Fatalf("red text before soft reset = %#v", actions[1])
	}
	if actions[2].Type != TerminalActionReset {
		t.Fatalf("soft reset action = %#v", actions[2])
	}
	if actions[3].Type != TerminalActionText || !TextStylesEqual(actions[3].Style, DefaultTextStyle()) || parser.InLink() {
		t.Fatalf("plain text after soft reset = %#v inLink=%v", actions[3], parser.InLink())
	}
}

func TestTerminalParserBuffersIncompleteSequences(t *testing.T) {
	parser := NewTerminalParser()
	actions := parser.Feed("a\x1b[")
	if len(actions) != 1 || actions[0].Type != TerminalActionText {
		t.Fatalf("first actions = %#v", actions)
	}
	actions = parser.Feed("31mred")
	if len(actions) != 1 || actions[0].Type != TerminalActionText || actions[0].Style.Foreground != namedTerminalColor(NamedColorRed) {
		t.Fatalf("second actions = %#v", actions)
	}
	actions = parser.Flush()
	if len(actions) != 0 {
		t.Fatalf("flush actions = %#v", actions)
	}

	parser = NewTerminalParser()
	actions = parser.Feed("\x1b[?")
	if len(actions) != 0 {
		t.Fatalf("incomplete csi feed actions = %#v", actions)
	}
	actions = parser.Flush()
	if len(actions) != 1 || actions[0].Type != TerminalActionUnknown || actions[0].Sequence != "\x1b[?" {
		t.Fatalf("incomplete csi flush actions = %#v", actions)
	}

	parser = NewTerminalParser()
	actions = parser.Feed(OSCPrefix + OSCSetTitleAndIcon + ";Partial")
	if len(actions) != 0 {
		t.Fatalf("incomplete osc feed actions = %#v", actions)
	}
	actions = parser.Flush()
	if len(actions) != 1 || actions[0].Type != TerminalActionTitle || actions[0].OSC.Title.Title != "Partial" {
		t.Fatalf("incomplete osc flush actions = %#v", actions)
	}
}
