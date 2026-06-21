package repl

import (
	"io"
	"unicode/utf8"
)

const esc = 0x1b

// segment inspects buf and returns the first complete input sequence, the
// number of bytes it consumed, and whether a complete sequence was found.
// atEOF=true forces a trailing lone ESC to be emitted as KeyEsc.
func segment(buf []byte, atEOF bool) (string, int, bool) {
	if len(buf) == 0 {
		return "", 0, false
	}
	b0 := buf[0]

	if b0 == esc {
		if len(buf) == 1 {
			if atEOF {
				return "\x1b", 1, true // lone Escape
			}
			return "", 0, false // wait for the rest of the sequence
		}
		switch buf[1] {
		case '[':
			return segmentCSI(buf)
		case 'O':
			if len(buf) >= 3 {
				return string(buf[:3]), 3, true // SS3, e.g. ESC O P (F1)
			}
			return "", 0, false
		default:
			return string(buf[:2]), 2, true // Alt+<key>, ESC <byte>
		}
	}

	if b0 < utf8.RuneSelf {
		return string(buf[:1]), 1, true // ASCII / control byte
	}

	// Multi-byte UTF-8 rune.
	n := runeLen(b0)
	if n == 0 {
		// 0xFF and other invalid lead bytes can never form a valid sequence; emit immediately.
		return string(buf[:1]), 1, true // invalid lead byte; consume one
	}
	if len(buf) < n {
		return "", 0, false
	}
	return string(buf[:n]), n, true
}

// segmentCSI handles ESC [ ... sequences, including bracketed paste blocks
// (ESC[200~ ... ESC[201~) which must be consumed whole.
func segmentCSI(buf []byte) (string, int, bool) {
	const pasteStart = "\x1b[200~"
	const pasteEnd = "\x1b[201~"
	if hasPrefix(buf, pasteStart) {
		end := indexOf(buf, []byte(pasteEnd))
		if end < 0 {
			return "", 0, false // paste not finished
		}
		total := end + len(pasteEnd)
		return string(buf[:total]), total, true
	}
	// Generic CSI: ESC [ params... final byte in 0x40..0x7E.
	for i := 2; i < len(buf); i++ {
		if buf[i] >= 0x40 && buf[i] <= 0x7e {
			return string(buf[:i+1]), i + 1, true
		}
	}
	return "", 0, false
}

func runeLen(b byte) int {
	switch {
	case b&0xe0 == 0xc0:
		return 2
	case b&0xf0 == 0xe0:
		return 3
	case b&0xf8 == 0xf0:
		return 4
	default:
		return 0
	}
}

func hasPrefix(b []byte, p string) bool {
	if len(b) < len(p) {
		return false
	}
	for i := 0; i < len(p); i++ {
		if b[i] != p[i] {
			return false
		}
	}
	return true
}

func indexOf(b, sub []byte) int {
	for i := 0; i+len(sub) <= len(b); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if b[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// SequenceScanner reads raw bytes from r and yields complete input sequences.
type SequenceScanner struct {
	r       io.Reader
	buf     []byte
	eof     bool
	readBuf [1024]byte
}

func NewSequenceScanner(r io.Reader) *SequenceScanner {
	return &SequenceScanner{r: r}
}

// Next returns the next complete input sequence. It returns io.EOF only once
// the buffer is fully drained and the underlying reader is exhausted.
func (s *SequenceScanner) Next() (string, error) {
	for {
		if seq, n, ok := segment(s.buf, s.eof); ok {
			s.buf = s.buf[n:]
			return seq, nil
		}
		if s.eof {
			if len(s.buf) > 0 {
				// Undecodable trailing bytes: emit one byte to make progress.
				b := string(s.buf[:1])
				s.buf = s.buf[1:]
				return b, nil
			}
			return "", io.EOF
		}
		n, err := s.r.Read(s.readBuf[:])
		if n > 0 {
			s.buf = append(s.buf, s.readBuf[:n]...)
		}
		if err == io.EOF {
			s.eof = true
		} else if err != nil {
			return "", err
		}
	}
}
