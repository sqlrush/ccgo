package repl

import "bytes"

// FakeTerminal is a buffer-backed Terminal for tests. Read drains In; once
// empty it returns io.EOF (the loop treats EOF as a clean exit).
type FakeTerminal struct {
	In  *bytes.Buffer
	Out *bytes.Buffer
	W   int
	H   int
	Raw bool
	TTY bool
}

func NewFakeTerminal(input string, w, h int) *FakeTerminal {
	return &FakeTerminal{
		In:  bytes.NewBufferString(input),
		Out: &bytes.Buffer{},
		W:   w,
		H:   h,
		TTY: true,
	}
}

func (f *FakeTerminal) IsTTY() bool { return f.TTY }

func (f *FakeTerminal) MakeRaw() (func() error, error) {
	f.Raw = true
	return func() error { f.Raw = false; return nil }, nil
}

func (f *FakeTerminal) Read(p []byte) (int, error) { return f.In.Read(p) }

func (f *FakeTerminal) WriteString(s string) error {
	_, err := f.Out.WriteString(s)
	return err
}

func (f *FakeTerminal) Size() (int, int, error) { return f.W, f.H, nil }
