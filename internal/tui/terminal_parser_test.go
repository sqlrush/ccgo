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
	actions := parser.Feed(CursorPosition(2, 3) + EraseLine + ScrollUp(2) + EnableBracketedPaste + TerminalTitleSequence("Claude") + ESCIndex + "\x1bOA")
	if len(actions) != 7 {
		t.Fatalf("actions = %#v", actions)
	}
	if actions[0].Type != TerminalActionCursor || actions[0].Cursor.Type != CSICursorActionPosition || actions[0].Cursor.Row != 2 || actions[0].Cursor.Column != 3 {
		t.Fatalf("cursor action = %#v", actions[0])
	}
	if actions[1].Type != TerminalActionErase || actions[1].Erase.Type != CSIEraseActionLine || actions[1].Erase.Region != CSIEraseAll {
		t.Fatalf("erase action = %#v", actions[1])
	}
	if actions[2].Type != TerminalActionScroll || actions[2].Scroll.Type != CSIScrollActionUp || actions[2].Scroll.Count != 2 {
		t.Fatalf("scroll action = %#v", actions[2])
	}
	if actions[3].Type != TerminalActionMode || actions[3].Mode.Type != CSIModeActionBracketedPaste || !actions[3].Mode.Enabled {
		t.Fatalf("mode action = %#v", actions[3])
	}
	if actions[4].Type != TerminalActionTitle || actions[4].OSC.Title.Title != "Claude" {
		t.Fatalf("title action = %#v", actions[4])
	}
	if actions[5].Type != TerminalActionCursor || actions[5].Cursor.Type != CSICursorActionMove || actions[5].Cursor.Direction != CSICursorDown {
		t.Fatalf("esc cursor action = %#v", actions[5])
	}
	if actions[6].Type != TerminalActionUnknown || actions[6].Sequence != "\x1bOA" {
		t.Fatalf("unknown action = %#v", actions[6])
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
}
