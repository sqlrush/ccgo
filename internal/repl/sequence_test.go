package repl

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestSegment(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		atEOF    bool
		wantSeq  string
		wantN    int
		wantDone bool
	}{
		{"ascii", "a", false, "a", 1, true},
		{"ctrl-c", "\x03", false, "\x03", 1, true},
		{"enter", "\r", false, "\r", 1, true},
		{"csi-left", "\x1b[D", false, "\x1b[D", 3, true},
		{"csi-incomplete", "\x1b[", false, "", 0, false},
		{"ss3-f1", "\x1bOP", false, "\x1bOP", 3, true},
		{"alt-key", "\x1bx", false, "\x1bx", 2, true},
		{"lone-esc-eof", "\x1b", true, "\x1b", 1, true},
		{"lone-esc-need-more", "\x1b", false, "", 0, false},
		{"utf8-2byte", "é", false, "é", 2, true}, // é
		{"utf8-split", "\xc3", false, "", 0, false},        // first byte of é, need more
		{"paste", "\x1b[200~hi\x1b[201~", false, "\x1b[200~hi\x1b[201~", 14, true}, // bracketed paste
		{"paste-incomplete", "\x1b[200~hi", false, "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seq, n, done := segment([]byte(tc.in), tc.atEOF)
			if seq != tc.wantSeq || n != tc.wantN || done != tc.wantDone {
				t.Fatalf("segment(%q,%v) = %q,%d,%v want %q,%d,%v",
					tc.in, tc.atEOF, seq, n, done, tc.wantSeq, tc.wantN, tc.wantDone)
			}
		})
	}
}

func TestSequenceScannerNext(t *testing.T) {
	// "a", left-arrow, enter, then EOF.
	sc := NewSequenceScanner(bytes.NewReader([]byte("a\x1b[D\r")))
	want := []string{"a", "\x1b[D", "\r"}
	for _, w := range want {
		got, err := sc.Next()
		if err != nil {
			t.Fatalf("Next() err: %v", err)
		}
		if got != w {
			t.Fatalf("Next() = %q want %q", got, w)
		}
	}
	if _, err := sc.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestSequenceScannerSplitReads(t *testing.T) {
	// Escape sequence split across two reads must reassemble.
	sc := NewSequenceScanner(&chunkReader{chunks: []string{"\x1b[", "D"}})
	got, err := sc.Next()
	if err != nil || got != "\x1b[D" {
		t.Fatalf("Next() = %q,%v want %q,nil", got, err, "\x1b[D")
	}
}

type chunkReader struct {
	chunks []string
	i      int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.i >= len(c.chunks) {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[c.i])
	c.i++
	return n, nil
}
