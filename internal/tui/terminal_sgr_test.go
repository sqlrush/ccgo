package tui

import "testing"

func TestApplySGRResetsAndTogglesTextStyle(t *testing.T) {
	base := DefaultTextStyle()
	base.Bold = true
	reset := ApplySGR("", base)
	if !TextStylesEqual(reset, DefaultTextStyle()) {
		t.Fatalf("empty SGR reset = %#v", reset)
	}

	style := ApplySGR("1;2;3;4:3;5;7;8;9;53;31;44", DefaultTextStyle())
	if !style.Bold || !style.Dim || !style.Italic || style.Underline != UnderlineCurly || !style.Blink || !style.Inverse || !style.Hidden || !style.Strikethrough || !style.Overline {
		t.Fatalf("style toggles = %#v", style)
	}
	if style.Foreground != namedTerminalColor(NamedColorRed) || style.Background != namedTerminalColor(NamedColorBlue) {
		t.Fatalf("named colors = fg %#v bg %#v", style.Foreground, style.Background)
	}

	style = ApplySGR("22;23;24;25;27;28;29;55;39;49", style)
	if !TextStylesEqual(style, DefaultTextStyle()) {
		t.Fatalf("toggle reset = %#v", style)
	}

	double := ApplySGR("21;4:99", DefaultTextStyle())
	if double.Underline != UnderlineSingle {
		t.Fatalf("unknown underline style should fall back to single: %#v", double)
	}
	double = ApplySGR("21", DefaultTextStyle())
	if double.Underline != UnderlineDouble {
		t.Fatalf("double underline = %#v", double)
	}
}

func TestApplySGRParsesNamedBrightAndExtendedColors(t *testing.T) {
	style := ApplySGR("97;100", DefaultTextStyle())
	if style.Foreground != namedTerminalColor(NamedColorBrightWhite) || style.Background != namedTerminalColor(NamedColorBrightBlack) {
		t.Fatalf("bright colors = fg %#v bg %#v", style.Foreground, style.Background)
	}

	style = ApplySGR("38;5;196;48;2;1;2;3;58;2;4;5;6", DefaultTextStyle())
	if style.Foreground != (TerminalColor{Type: TerminalColorIndexed, Index: 196}) {
		t.Fatalf("indexed foreground = %#v", style.Foreground)
	}
	if style.Background != (TerminalColor{Type: TerminalColorRGB, RGB: RGBColor{R: 1, G: 2, B: 3}}) {
		t.Fatalf("rgb background = %#v", style.Background)
	}
	if style.UnderlineColor != (TerminalColor{Type: TerminalColorRGB, RGB: RGBColor{R: 4, G: 5, B: 6}}) {
		t.Fatalf("rgb underline = %#v", style.UnderlineColor)
	}

	style = ApplySGR("38:5:42;48:2::10:20:30;58:2:1:7:8:9", DefaultTextStyle())
	if style.Foreground != (TerminalColor{Type: TerminalColorIndexed, Index: 42}) {
		t.Fatalf("colon indexed foreground = %#v", style.Foreground)
	}
	if style.Background != (TerminalColor{Type: TerminalColorRGB, RGB: RGBColor{R: 10, G: 20, B: 30}}) {
		t.Fatalf("colon rgb background = %#v", style.Background)
	}
	if style.UnderlineColor != (TerminalColor{Type: TerminalColorRGB, RGB: RGBColor{R: 7, G: 8, B: 9}}) {
		t.Fatalf("colon rgb underline = %#v", style.UnderlineColor)
	}

	style = ApplySGR("59", style)
	if style.UnderlineColor != (TerminalColor{Type: TerminalColorDefault}) {
		t.Fatalf("underline color reset = %#v", style.UnderlineColor)
	}
}

func TestParseSGRSequenceAppliesCSIAction(t *testing.T) {
	style, ok := ParseSGRSequence(CSISequence(1, "m"), DefaultTextStyle())
	if !ok || !style.Bold {
		t.Fatalf("parse sgr sequence = %#v ok=%v", style, ok)
	}
	style, ok = ParseSGRSequence(CSISequence("H"), style)
	if ok || !style.Bold {
		t.Fatalf("non-sgr sequence should not apply = %#v ok=%v", style, ok)
	}
}
