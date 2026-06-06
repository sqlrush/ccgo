package tui

import (
	"reflect"
	"testing"
)

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

func TestTerminalVisibleTextExpandsRepeatPrecedingCharacter(t *testing.T) {
	input := "ab" + CSISequence(3, "b") + "界" + CSISequence(2, "b") + "\n" + CSISequence(4, "b") + "z"
	if got := TerminalVisibleText(input); got != "abbbb界界界\nz" {
		t.Fatalf("visible = %q", got)
	}
	if got := TerminalVisibleWidth(input); got != 12 {
		t.Fatalf("width = %d", got)
	}
	if got := StripANSI(CSISequence(2, "b") + "x" + CSISequence("b")); got != "xx" {
		t.Fatalf("strip = %q", got)
	}
}

func TestTerminalVisibleWidthUsesBaseWidthForCombiningMarks(t *testing.T) {
	input := "e\u0301" + CSISequence(2, "b") + "x"
	if got := TerminalVisibleText(input); got != "e\u0301e\u0301e\u0301x" {
		t.Fatalf("visible = %q", got)
	}
	if got := TerminalVisibleWidth(input); got != 4 {
		t.Fatalf("width = %d", got)
	}

	if got := padOrTrim("e\u0301x", 2); got != "e\u0301x" {
		t.Fatalf("padOrTrim exact = %q", got)
	}
	if got := padOrTrim("e\u0301x", 1); got != "e\u0301" {
		t.Fatalf("padOrTrim trim = %q", got)
	}
}

func TestTerminalParserSegmentsSpacingAndPrependMarks(t *testing.T) {
	parser := NewTerminalParser()
	actions := parser.Feed("\u0915\u093e!\u0600A")
	if len(actions) != 1 || actions[0].Type != TerminalActionText {
		t.Fatalf("actions = %#v", actions)
	}
	want := []TerminalGrapheme{
		{Value: "\u0915\u093e", Width: 1},
		{Value: "!", Width: 1},
		{Value: "\u0600A", Width: 1},
	}
	if len(actions[0].Graphemes) != len(want) {
		t.Fatalf("graphemes = %#v", actions[0].Graphemes)
	}
	for i, got := range actions[0].Graphemes {
		if got != want[i] {
			t.Fatalf("grapheme %d = %#v, want %#v", i, got, want[i])
		}
	}
	if got := TerminalActionsVisibleWidth(actions); got != 3 {
		t.Fatalf("width = %d", got)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("\u0600"); len(actions) != 0 {
		t.Fatalf("partial prepend actions = %#v", actions)
	}
	actions = parser.Feed("B!")
	if len(actions) != 1 || len(actions[0].Graphemes) != 2 {
		t.Fatalf("chunked prepend actions = %#v", actions)
	}
	if got := actions[0].Graphemes[0]; got.Value != "\u0600B" || got.Width != 1 {
		t.Fatalf("chunked prepend grapheme = %#v", got)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("\u0600"); len(actions) != 0 {
		t.Fatalf("flush prepend feed actions = %#v", actions)
	}
	actions = parser.Flush()
	if len(actions) != 1 || len(actions[0].Graphemes) != 1 || actions[0].Graphemes[0].Value != "\u0600" || actions[0].Graphemes[0].Width != 0 {
		t.Fatalf("flush prepend actions = %#v", actions)
	}
}

func TestTerminalParserKeepsCRLFGraphemeTogether(t *testing.T) {
	parser := NewTerminalParser()
	actions := parser.Feed("a\r\nb")
	if len(actions) != 1 || actions[0].Type != TerminalActionText {
		t.Fatalf("actions = %#v", actions)
	}
	want := []TerminalGrapheme{
		{Value: "a", Width: 1},
		{Value: "\r\n", Width: 0},
		{Value: "b", Width: 1},
	}
	if len(actions[0].Graphemes) != len(want) {
		t.Fatalf("graphemes = %#v", actions[0].Graphemes)
	}
	for i, got := range actions[0].Graphemes {
		if got != want[i] {
			t.Fatalf("grapheme %d = %#v, want %#v", i, got, want[i])
		}
	}
	if got := TerminalActionsVisibleText(actions); got != "a\r\nb" {
		t.Fatalf("visible = %q", got)
	}
	if got := TerminalActionsVisibleWidth(actions); got != 2 {
		t.Fatalf("width = %d", got)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("\r"); len(actions) != 0 {
		t.Fatalf("partial cr actions = %#v", actions)
	}
	actions = parser.Feed("\nb")
	if len(actions) != 1 || len(actions[0].Graphemes) != 2 {
		t.Fatalf("chunked crlf actions = %#v", actions)
	}
	if got := actions[0].Graphemes[0]; got.Value != "\r\n" || got.Width != 0 {
		t.Fatalf("chunked crlf grapheme = %#v", got)
	}
	if got := TerminalActionsVisibleWidth(actions); got != 1 {
		t.Fatalf("chunked crlf width = %d", got)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("\r"); len(actions) != 0 {
		t.Fatalf("flush cr feed actions = %#v", actions)
	}
	actions = parser.Flush()
	if len(actions) != 1 || len(actions[0].Graphemes) != 1 || actions[0].Graphemes[0].Value != "\r" || actions[0].Graphemes[0].Width != 0 {
		t.Fatalf("flush cr actions = %#v", actions)
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
	actions := parser.Feed("e\u0301 \u2764\ufe0f 1\ufe0f\u20e3 2\u20e3 \u1112\u1161\u11ab \U0001f44b\U0001f3fd \U0001f469\u200d\U0001f4bb \U0001f1fa\U0001f1f8 \U0001f3f4\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f")
	if len(actions) != 1 || actions[0].Type != TerminalActionText {
		t.Fatalf("actions = %#v", actions)
	}
	want := []TerminalGrapheme{
		{Value: "e\u0301", Width: 1},
		{Value: " ", Width: 1},
		{Value: "\u2764\ufe0f", Width: 2},
		{Value: " ", Width: 1},
		{Value: "1\ufe0f\u20e3", Width: 2},
		{Value: " ", Width: 1},
		{Value: "2\u20e3", Width: 2},
		{Value: " ", Width: 1},
		{Value: "\u1112\u1161\u11ab", Width: 2},
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

func TestTerminalParserKeepsChunkedGraphemeClustersTogether(t *testing.T) {
	parser := NewTerminalParser()
	if actions := parser.Feed("\U0001f469\u200d"); len(actions) != 0 {
		t.Fatalf("partial zwj actions = %#v", actions)
	}
	actions := parser.Feed("\U0001f4bb ok")
	if len(actions) != 1 || actions[0].Type != TerminalActionText || len(actions[0].Graphemes) != 4 {
		t.Fatalf("zwj actions = %#v", actions)
	}
	if got := actions[0].Graphemes[0]; got.Value != "\U0001f469\u200d\U0001f4bb" || got.Width != 2 {
		t.Fatalf("zwj grapheme = %#v", got)
	}
	if width := TerminalActionsVisibleWidth(actions); width != 5 {
		t.Fatalf("zwj width = %d", width)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("\U0001f469\U0001f3fd"); len(actions) != 0 {
		t.Fatalf("partial modified emoji actions = %#v", actions)
	}
	actions = parser.Feed("\u200d\U0001f4bb!")
	if len(actions) != 1 || actions[0].Type != TerminalActionText || len(actions[0].Graphemes) != 2 {
		t.Fatalf("modified zwj actions = %#v", actions)
	}
	if got := actions[0].Graphemes[0]; got.Value != "\U0001f469\U0001f3fd\u200d\U0001f4bb" || got.Width != 2 {
		t.Fatalf("modified zwj grapheme = %#v", got)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("\U0001f1fa"); len(actions) != 0 {
		t.Fatalf("partial regional actions = %#v", actions)
	}
	actions = parser.Feed("\U0001f1f8!")
	if len(actions) != 1 || len(actions[0].Graphemes) != 2 {
		t.Fatalf("regional actions = %#v", actions)
	}
	if got := actions[0].Graphemes[0]; got.Value != "\U0001f1fa\U0001f1f8" || got.Width != 2 {
		t.Fatalf("regional grapheme = %#v", got)
	}
	if width := TerminalActionsVisibleWidth(actions); width != 3 {
		t.Fatalf("regional width = %d", width)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("1"); len(actions) != 0 {
		t.Fatalf("partial keycap base actions = %#v", actions)
	}
	actions = parser.Feed("\ufe0f\u20e3!")
	if len(actions) != 1 || len(actions[0].Graphemes) != 2 {
		t.Fatalf("keycap actions = %#v", actions)
	}
	if got := actions[0].Graphemes[0]; got.Value != "1\ufe0f\u20e3" || got.Width != 2 {
		t.Fatalf("keycap grapheme = %#v", got)
	}
	if width := TerminalActionsVisibleWidth(actions); width != 3 {
		t.Fatalf("keycap width = %d", width)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("2\ufe0f"); len(actions) != 0 {
		t.Fatalf("partial keycap variation actions = %#v", actions)
	}
	actions = parser.Feed("\u20e3")
	if len(actions) != 1 || len(actions[0].Graphemes) != 1 {
		t.Fatalf("variation keycap actions = %#v", actions)
	}
	if got := actions[0].Graphemes[0]; got.Value != "2\ufe0f\u20e3" || got.Width != 2 {
		t.Fatalf("variation keycap grapheme = %#v", got)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("#"); len(actions) != 0 {
		t.Fatalf("partial keycap flush feed actions = %#v", actions)
	}
	actions = parser.Flush()
	if len(actions) != 1 || actions[0].Type != TerminalActionText || actions[0].Graphemes[0].Value != "#" || actions[0].Graphemes[0].Width != 1 {
		t.Fatalf("flush keycap base actions = %#v", actions)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("\u1112"); len(actions) != 0 {
		t.Fatalf("partial hangul leading actions = %#v", actions)
	}
	actions = parser.Feed("\u1161\u11ab!")
	if len(actions) != 1 || len(actions[0].Graphemes) != 2 {
		t.Fatalf("hangul actions = %#v", actions)
	}
	if got := actions[0].Graphemes[0]; got.Value != "\u1112\u1161\u11ab" || got.Width != 2 {
		t.Fatalf("hangul grapheme = %#v", got)
	}
	if width := TerminalActionsVisibleWidth(actions); width != 3 {
		t.Fatalf("hangul width = %d", width)
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("\u1112\u1161"); len(actions) != 0 {
		t.Fatalf("partial hangul vowel actions = %#v", actions)
	}
	actions = parser.Feed("\u11ab!")
	if len(actions) != 1 || len(actions[0].Graphemes) != 2 {
		t.Fatalf("hangul trailing actions = %#v", actions)
	}
	if got := actions[0].Graphemes[0]; got.Value != "\u1112\u1161\u11ab" || got.Width != 2 {
		t.Fatalf("hangul trailing grapheme = %#v", got)
	}

	parser = NewTerminalParser()
	actions = parser.Feed("\U0001f1fa" + CSISequence(31, "m") + "red")
	if len(actions) != 2 {
		t.Fatalf("sequence boundary actions = %#v", actions)
	}
	if actions[0].Type != TerminalActionText || actions[0].Graphemes[0].Value != "\U0001f1fa" || actions[0].Graphemes[0].Width != 2 {
		t.Fatalf("flushed regional action = %#v", actions[0])
	}
	if actions[1].Type != TerminalActionText || actions[1].Style.Foreground != namedTerminalColor(NamedColorRed) {
		t.Fatalf("styled text action = %#v", actions[1])
	}

	parser = NewTerminalParser()
	if actions := parser.Feed("\U0001f1fa"); len(actions) != 0 {
		t.Fatalf("flush partial feed actions = %#v", actions)
	}
	actions = parser.Flush()
	if len(actions) != 1 || actions[0].Type != TerminalActionText || actions[0].Graphemes[0].Value != "\U0001f1fa" {
		t.Fatalf("flush partial actions = %#v", actions)
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
	if actions[9].Type != TerminalActionCursor || actions[9].Cursor.Type != CSICursorActionMove || actions[9].Cursor.Direction != CSICursorUp || actions[9].Cursor.Count != 1 {
		t.Fatalf("ss3 cursor action = %#v", actions[9])
	}
}

func TestTerminalParserDispatchesWindowResizeReports(t *testing.T) {
	parser := NewTerminalParser()
	input := "a" + CSISequence(8, 24, 80, "t") + "b"
	actions := parser.Feed(input)
	if len(actions) != 3 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[1].Type != TerminalActionReport || actions[1].Report.Type != CSIReportActionWindow || actions[1].Report.Code != 8 || actions[1].Report.Rows != 24 || actions[1].Report.Columns != 80 || !reflect.DeepEqual(actions[1].Report.Params, []int{8, 24, 80}) {
		t.Fatalf("window report action = %#v", actions[1])
	}
	if got := TerminalVisibleText(input); got != "ab" {
		t.Fatalf("visible = %q", got)
	}
}

func TestTerminalParserDispatchesDeviceAttributeReports(t *testing.T) {
	parser := NewTerminalParser()
	input := "a" + CSISequence("?62;1;2;6c") + "b"
	actions := parser.Feed(input)
	if len(actions) != 3 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[1].Type != TerminalActionReport || actions[1].Report.Type != CSIReportActionDeviceAttrs || actions[1].Report.PrivateMode != '?' || actions[1].Report.Code != 62 || !reflect.DeepEqual(actions[1].Report.Codes, []int{62, 1, 2, 6}) {
		t.Fatalf("device attribute report action = %#v", actions[1])
	}
	if got := TerminalVisibleText(input); got != "ab" {
		t.Fatalf("visible = %q", got)
	}
}

func TestTerminalParserDispatchesTerminalParameterReports(t *testing.T) {
	parser := NewTerminalParser()
	input := "a" + CSISequence("2;1;1;112;112;1;0x") + "b"
	actions := parser.Feed(input)
	if len(actions) != 3 {
		t.Fatalf("actions = %#v", actions)
	}
	wantParams := []int{2, 1, 1, 112, 112, 1, 0}
	if actions[1].Type != TerminalActionReport || actions[1].Report.Type != CSIReportActionTerminalParams || actions[1].Report.Code != 2 || !reflect.DeepEqual(actions[1].Report.Params, wantParams) {
		t.Fatalf("terminal parameter report action = %#v", actions[1])
	}
	if got := TerminalVisibleText(input); got != "ab" {
		t.Fatalf("visible = %q", got)
	}
}

func TestTerminalParserDispatchesCursorPositionReports(t *testing.T) {
	parser := NewTerminalParser()
	input := "a" + CSISequence("?12;34;2R") + "b"
	actions := parser.Feed(input)
	if len(actions) != 3 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[1].Type != TerminalActionReport || actions[1].Report.Type != CSIReportActionCursorPosition || actions[1].Report.PrivateMode != '?' || actions[1].Report.Row != 12 || actions[1].Report.Column != 34 || actions[1].Report.Page != 2 {
		t.Fatalf("cursor position report action = %#v", actions[1])
	}
	if got := TerminalVisibleText(input); got != "ab" {
		t.Fatalf("visible = %q", got)
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

func TestTerminalParserDispatchesStructuredOSCActions(t *testing.T) {
	parser := NewTerminalParser()
	input := "a" +
		TerminalClipboardSequence("copy me") +
		"b" +
		TerminalProgressSequence(TerminalProgressRunning, 40) +
		OSCSequence(OSCForegroundColor, "rgb:f/0/8") +
		OSCSequence(OSCResetCursor) +
		OSCSequence(OSCPaletteColor, "1", "#112233") +
		"c" +
		GhosttyNotificationSequence("Build complete", "Claude") +
		"d"
	actions := parser.Feed(input)
	if len(actions) != 10 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[1].Type != TerminalActionClipboard || actions[1].OSC.Clipboard.Text != "copy me" {
		t.Fatalf("clipboard action = %#v", actions[1])
	}
	if actions[3].Type != TerminalActionProgress || actions[3].OSC.Progress.State != TerminalProgressRunning || actions[3].OSC.Progress.Percent != 40 {
		t.Fatalf("progress action = %#v", actions[3])
	}
	if actions[4].Type != TerminalActionColor || actions[4].OSC.Color.Target != "foreground" || !actions[4].OSC.Color.Valid || actions[4].OSC.Color.Color == nil || *actions[4].OSC.Color.Color != (RGBColor{R: 255, G: 0, B: 136}) {
		t.Fatalf("color action = %#v", actions[4])
	}
	if actions[5].Type != TerminalActionColor || actions[5].OSC.Color.Target != "cursor" || !actions[5].OSC.Color.Valid || !actions[5].OSC.Color.Reset || actions[5].OSC.Color.Color != nil {
		t.Fatalf("color reset action = %#v", actions[5])
	}
	if actions[6].Type != TerminalActionPalette || !actions[6].OSC.Palette.Valid || len(actions[6].OSC.Palette.Entries) != 1 || actions[6].OSC.Palette.Entries[0].Index != 1 || actions[6].OSC.Palette.Entries[0].Color == nil || *actions[6].OSC.Palette.Entries[0].Color != (RGBColor{R: 17, G: 34, B: 51}) {
		t.Fatalf("palette action = %#v", actions[6])
	}
	if actions[8].Type != TerminalActionNotification || actions[8].OSC.Notification.Provider != "ghostty" || actions[8].OSC.Notification.Title != "Claude" || actions[8].OSC.Notification.Message != "Build complete" {
		t.Fatalf("notification action = %#v", actions[8])
	}
	if got := TerminalVisibleText(input); got != "abcd" {
		t.Fatalf("visible = %q", got)
	}

	shellInput := "x" + OSCSequence(OSCShellIntegration, "A") + "y" + OSCSequence(OSCShellIntegration, "D", "2") + "z"
	shellActions := parser.Feed(shellInput)
	if len(shellActions) != 5 || shellActions[1].Type != TerminalActionShell || shellActions[1].OSC.Shell.Marker != "promptStart" || shellActions[3].Type != TerminalActionShell || shellActions[3].OSC.Shell.Marker != "commandEnd" || shellActions[3].OSC.Shell.ExitCode != 2 || !shellActions[3].OSC.Shell.HasExitCode {
		t.Fatalf("shell actions = %#v", shellActions)
	}
	if got := TerminalVisibleText(shellInput); got != "xyz" {
		t.Fatalf("shell visible = %q", got)
	}
	vsCodeShellInput := "m" +
		OSCSequence(OSCVSShellIntegration, "C") +
		OSCSequence(OSCVSShellIntegration, "E", "go test ./...") +
		OSCSequence(OSCVSShellIntegration, "P", "Cwd=/tmp/ccgo", "IsWindows=False") +
		"n"
	vsCodeShellActions := parser.Feed(vsCodeShellInput)
	if len(vsCodeShellActions) != 5 || vsCodeShellActions[1].Type != TerminalActionShell || vsCodeShellActions[1].OSC.Shell.Marker != "commandStart" {
		t.Fatalf("vscode shell actions = %#v", vsCodeShellActions)
	}
	if vsCodeShellActions[2].Type != TerminalActionShell || vsCodeShellActions[2].OSC.Shell.Marker != "commandLine" || vsCodeShellActions[2].OSC.Shell.Value != "go test ./..." {
		t.Fatalf("vscode shell command line action = %#v", vsCodeShellActions[2])
	}
	if vsCodeShellActions[3].Type != TerminalActionShell || vsCodeShellActions[3].OSC.Shell.Marker != "property" || vsCodeShellActions[3].OSC.Shell.Properties["Cwd"] != "/tmp/ccgo" || vsCodeShellActions[3].OSC.Shell.Properties["IsWindows"] != "False" {
		t.Fatalf("vscode shell property action = %#v", vsCodeShellActions[3])
	}
	if got := TerminalVisibleText(vsCodeShellInput); got != "mn" {
		t.Fatalf("vscode shell visible = %q", got)
	}
}

func TestTerminalParserDispatchesMultiDynamicOSCColorActions(t *testing.T) {
	parser := NewTerminalParser()
	input := "a" + OSCSequence(OSCBackgroundColor, "#112233", "rgb:f/0/8") + "b" + OSCSequence(OSCResetHighlightForeground) + "c"
	actions := parser.Feed(input)
	if len(actions) != 5 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[1].Type != TerminalActionColor || !actions[1].OSC.Color.Valid || len(actions[1].OSC.Color.Entries) != 2 {
		t.Fatalf("dynamic color action = %#v", actions[1])
	}
	if actions[1].OSC.Color.Target != "background" || actions[1].OSC.Color.Color == nil || *actions[1].OSC.Color.Color != (RGBColor{R: 17, G: 34, B: 51}) {
		t.Fatalf("dynamic first color = %#v", actions[1].OSC.Color)
	}
	if actions[1].OSC.Color.Entries[1].Target != "cursor" || actions[1].OSC.Color.Entries[1].Color == nil || *actions[1].OSC.Color.Entries[1].Color != (RGBColor{R: 255, G: 0, B: 136}) {
		t.Fatalf("dynamic second color = %#v", actions[1].OSC.Color.Entries[1])
	}
	if actions[3].Type != TerminalActionColor || actions[3].OSC.Color.Target != "highlightForeground" || !actions[3].OSC.Color.Valid || !actions[3].OSC.Color.Reset {
		t.Fatalf("dynamic reset action = %#v", actions[3])
	}
	if got := TerminalVisibleText(input); got != "abc" {
		t.Fatalf("visible = %q", got)
	}
}

func TestTerminalParserDispatchesSpecialOSCColorActions(t *testing.T) {
	parser := NewTerminalParser()
	input := "a" + OSCSequence(OSCSpecialColor, "0", "#112233", "4", "?") + "b" + OSCSequence(OSCResetSpecialColor, "4") + "c"
	actions := parser.Feed(input)
	if len(actions) != 5 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[1].Type != TerminalActionSpecialColor || !actions[1].OSC.SpecialColor.Valid || len(actions[1].OSC.SpecialColor.Entries) != 2 {
		t.Fatalf("special color action = %#v", actions[1])
	}
	if actions[1].OSC.SpecialColor.Entries[0].Index != 0 || actions[1].OSC.SpecialColor.Entries[0].Color == nil || *actions[1].OSC.SpecialColor.Entries[0].Color != (RGBColor{R: 17, G: 34, B: 51}) {
		t.Fatalf("special color set = %#v", actions[1].OSC.SpecialColor.Entries[0])
	}
	if actions[1].OSC.SpecialColor.Entries[1].Index != 4 || !actions[1].OSC.SpecialColor.Entries[1].Query {
		t.Fatalf("special color query = %#v", actions[1].OSC.SpecialColor.Entries[1])
	}
	if actions[3].Type != TerminalActionSpecialColor || !actions[3].OSC.SpecialColor.Valid || len(actions[3].OSC.SpecialColor.Entries) != 1 || !actions[3].OSC.SpecialColor.Entries[0].Reset || actions[3].OSC.SpecialColor.Entries[0].Index != 4 {
		t.Fatalf("special color reset = %#v", actions[3])
	}
	if got := TerminalVisibleText(input); got != "abc" {
		t.Fatalf("visible = %q", got)
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
