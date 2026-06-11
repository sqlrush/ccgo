package tui

import (
	"reflect"
	"testing"
)

func TestTerminalTokenizerFeedsTextAndSequencesAcrossChunks(t *testing.T) {
	tokenizer := NewTerminalTokenizer(TerminalTokenizerOptions{})
	tokens := tokenizer.Feed("hi \x1b[")
	want := []TerminalToken{{Type: TerminalTokenText, Value: "hi "}}
	if !reflect.DeepEqual(tokens, want) || tokenizer.Buffer() != "\x1b[" {
		t.Fatalf("first feed tokens=%#v buffer=%q", tokens, tokenizer.Buffer())
	}

	tokens = tokenizer.Feed("31mred")
	want = []TerminalToken{
		{Type: TerminalTokenSequence, Value: "\x1b[31m"},
		{Type: TerminalTokenText, Value: "red"},
	}
	if !reflect.DeepEqual(tokens, want) || tokenizer.Buffer() != "" {
		t.Fatalf("second feed tokens=%#v buffer=%q", tokens, tokenizer.Buffer())
	}
}

func TestTerminalTokenizerFeedsC1CSISequencesAcrossChunks(t *testing.T) {
	tokenizer := NewTerminalTokenizer(TerminalTokenizerOptions{})
	tokens := tokenizer.Feed("hi \x9b")
	want := []TerminalToken{{Type: TerminalTokenText, Value: "hi "}}
	if !reflect.DeepEqual(tokens, want) || tokenizer.Buffer() != "\x9b" {
		t.Fatalf("first c1 feed tokens=%#v buffer=%q", tokens, tokenizer.Buffer())
	}

	tokens = tokenizer.Feed("31mred")
	want = []TerminalToken{
		{Type: TerminalTokenSequence, Value: "\x9b31m"},
		{Type: TerminalTokenText, Value: "red"},
	}
	if !reflect.DeepEqual(tokens, want) || tokenizer.Buffer() != "" {
		t.Fatalf("second c1 feed tokens=%#v buffer=%q", tokens, tokenizer.Buffer())
	}
}

func TestTerminalTokenizerEmitsC1ESCControlBytes(t *testing.T) {
	tokenizer := NewTerminalTokenizer(TerminalTokenizerOptions{})
	tokens := tokenizer.Feed("a\x84b\x85c\x8dd\x88e")
	want := []TerminalToken{
		{Type: TerminalTokenText, Value: "a"},
		{Type: TerminalTokenSequence, Value: "\x84"},
		{Type: TerminalTokenText, Value: "b"},
		{Type: TerminalTokenSequence, Value: "\x85"},
		{Type: TerminalTokenText, Value: "c"},
		{Type: TerminalTokenSequence, Value: "\x8d"},
		{Type: TerminalTokenText, Value: "d"},
		{Type: TerminalTokenSequence, Value: "\x88"},
		{Type: TerminalTokenText, Value: "e"},
	}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("c1 esc tokens=%#v", tokens)
	}
}

func TestTerminalTokenizerFlushesIncompleteSequences(t *testing.T) {
	tokenizer := NewTerminalTokenizer(TerminalTokenizerOptions{})
	if tokens := tokenizer.Feed("a\x1b[?"); !reflect.DeepEqual(tokens, []TerminalToken{{Type: TerminalTokenText, Value: "a"}}) {
		t.Fatalf("feed tokens = %#v", tokens)
	}
	if tokenizer.Buffer() != "\x1b[?" {
		t.Fatalf("buffer = %q", tokenizer.Buffer())
	}
	tokens := tokenizer.Flush()
	want := []TerminalToken{{Type: TerminalTokenSequence, Value: "\x1b[?"}}
	if !reflect.DeepEqual(tokens, want) || tokenizer.Buffer() != "" {
		t.Fatalf("flush tokens=%#v buffer=%q", tokens, tokenizer.Buffer())
	}

	tokenizer.Feed("\x1b[")
	tokenizer.Reset()
	if tokenizer.Buffer() != "" {
		t.Fatalf("reset buffer = %q", tokenizer.Buffer())
	}
	if tokens := tokenizer.Feed("x"); !reflect.DeepEqual(tokens, []TerminalToken{{Type: TerminalTokenText, Value: "x"}}) {
		t.Fatalf("tokens after reset = %#v", tokens)
	}
}

func TestTerminalTokenizerHandlesOSCAndStringControls(t *testing.T) {
	tokenizer := NewTerminalTokenizer(TerminalTokenizerOptions{})
	input := "a" + TerminalTitleSequence("Claude") + "b" + OSCSequenceWithStringTerminator(OSCSetTitleAndIcon, "ST") + "c"
	tokens := tokenizer.Feed(input)
	want := []TerminalToken{
		{Type: TerminalTokenText, Value: "a"},
		{Type: TerminalTokenSequence, Value: TerminalTitleSequence("Claude")},
		{Type: TerminalTokenText, Value: "b"},
		{Type: TerminalTokenSequence, Value: OSCSequenceWithStringTerminator(OSCSetTitleAndIcon, "ST")},
		{Type: TerminalTokenText, Value: "c"},
	}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("osc tokens = %#v", tokens)
	}

	dcs := "\x1bPpayload\x1b\\"
	apc := "\x1b_payload\x07"
	pm := "\x1b^private\x07"
	sos := "\x1bXstart\x1b\\"
	tokens = tokenizer.Feed(dcs + "x" + apc + pm + sos)
	want = []TerminalToken{
		{Type: TerminalTokenSequence, Value: dcs},
		{Type: TerminalTokenText, Value: "x"},
		{Type: TerminalTokenSequence, Value: apc},
		{Type: TerminalTokenSequence, Value: pm},
		{Type: TerminalTokenSequence, Value: sos},
	}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("string-control tokens = %#v", tokens)
	}
}

func TestTerminalTokenizerHandlesC1OSCAndStringControls(t *testing.T) {
	tokenizer := NewTerminalTokenizer(TerminalTokenizerOptions{})
	input := "a\x9d0;Claude\x9cb\x90dcs\x9cc\x9fapc\x9cd\x9epm\x9ce\x98sos\x9cf"
	tokens := tokenizer.Feed(input)
	want := []TerminalToken{
		{Type: TerminalTokenText, Value: "a"},
		{Type: TerminalTokenSequence, Value: "\x9d0;Claude\x9c"},
		{Type: TerminalTokenText, Value: "b"},
		{Type: TerminalTokenSequence, Value: "\x90dcs\x9c"},
		{Type: TerminalTokenText, Value: "c"},
		{Type: TerminalTokenSequence, Value: "\x9fapc\x9c"},
		{Type: TerminalTokenText, Value: "d"},
		{Type: TerminalTokenSequence, Value: "\x9epm\x9c"},
		{Type: TerminalTokenText, Value: "e"},
		{Type: TerminalTokenSequence, Value: "\x98sos\x9c"},
		{Type: TerminalTokenText, Value: "f"},
	}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("c1 osc/string-control tokens = %#v", tokens)
	}
}

func TestTerminalTokenizerHandlesESCIntermediateSS3AndInvalidSequences(t *testing.T) {
	tokenizer := NewTerminalTokenizer(TerminalTokenizerOptions{})
	tokens := tokenizer.Feed("\x1b(B\x1bOA\x1bO1;2A\x1b[31x")
	want := []TerminalToken{
		{Type: TerminalTokenSequence, Value: "\x1b(B"},
		{Type: TerminalTokenSequence, Value: "\x1bOA"},
		{Type: TerminalTokenSequence, Value: "\x1bO1;2A"},
		{Type: TerminalTokenSequence, Value: "\x1b[31x"},
	}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("sequence tokens = %#v", tokens)
	}

	tokens = tokenizer.Feed("a\x1b[31\x00b")
	want = []TerminalToken{
		{Type: TerminalTokenText, Value: "a"},
		{Type: TerminalTokenText, Value: "\x1b[31\x00b"},
	}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("invalid csi tokens = %#v", tokens)
	}

	tokens = tokenizer.Feed("\x1bO1;")
	if len(tokens) != 0 || tokenizer.Buffer() != "\x1bO1;" {
		t.Fatalf("partial modified ss3 tokens=%#v buffer=%q", tokens, tokenizer.Buffer())
	}
	tokens = tokenizer.Feed("5D")
	want = []TerminalToken{{Type: TerminalTokenSequence, Value: "\x1bO1;5D"}}
	if !reflect.DeepEqual(tokens, want) || tokenizer.Buffer() != "" {
		t.Fatalf("completed modified ss3 tokens=%#v buffer=%q", tokens, tokenizer.Buffer())
	}

	tokens = tokenizer.Feed("\x8f1;")
	if len(tokens) != 0 || tokenizer.Buffer() != "\x8f1;" {
		t.Fatalf("partial c1 modified ss3 tokens=%#v buffer=%q", tokens, tokenizer.Buffer())
	}
	tokens = tokenizer.Feed("5D")
	want = []TerminalToken{{Type: TerminalTokenSequence, Value: "\x8f1;5D"}}
	if !reflect.DeepEqual(tokens, want) || tokenizer.Buffer() != "" {
		t.Fatalf("completed c1 modified ss3 tokens=%#v buffer=%q", tokens, tokenizer.Buffer())
	}
}

func TestTerminalTokenizerX10MouseOption(t *testing.T) {
	withoutMouse := NewTerminalTokenizer(TerminalTokenizerOptions{})
	tokens := withoutMouse.Feed("\x1b[M`rK")
	want := []TerminalToken{
		{Type: TerminalTokenSequence, Value: "\x1b[M"},
		{Type: TerminalTokenText, Value: "`rK"},
	}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("without x10 mouse tokens = %#v", tokens)
	}

	withMouse := NewTerminalTokenizer(TerminalTokenizerOptions{X10Mouse: true})
	tokens = withMouse.Feed("\x1b[M`rK")
	want = []TerminalToken{{Type: TerminalTokenSequence, Value: "\x1b[M`rK"}}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("with x10 mouse tokens = %#v", tokens)
	}

	tokens = withMouse.Feed("\x1b[M`")
	if len(tokens) != 0 || withMouse.Buffer() != "\x1b[M`" {
		t.Fatalf("incomplete x10 tokens=%#v buffer=%q", tokens, withMouse.Buffer())
	}
	tokens = withMouse.Feed("rK")
	want = []TerminalToken{{Type: TerminalTokenSequence, Value: "\x1b[M`rK"}}
	if !reflect.DeepEqual(tokens, want) || withMouse.Buffer() != "" {
		t.Fatalf("completed x10 tokens=%#v buffer=%q", tokens, withMouse.Buffer())
	}
}

func TestTerminalInputAndOutputTokenizerConstructors(t *testing.T) {
	output := NewTerminalOutputTokenizer()
	outputTokens := output.Feed("\x1b[M`rK")
	wantOutput := []TerminalToken{
		{Type: TerminalTokenSequence, Value: "\x1b[M"},
		{Type: TerminalTokenText, Value: "`rK"},
	}
	if !reflect.DeepEqual(outputTokens, wantOutput) {
		t.Fatalf("output tokenizer tokens = %#v", outputTokens)
	}

	input := NewTerminalInputTokenizer()
	inputTokens := input.Feed("\x1b[M`rK")
	wantInput := []TerminalToken{{Type: TerminalTokenSequence, Value: "\x1b[M`rK"}}
	if !reflect.DeepEqual(inputTokens, wantInput) {
		t.Fatalf("input tokenizer tokens = %#v", inputTokens)
	}
}
