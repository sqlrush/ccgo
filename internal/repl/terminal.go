package repl

import (
	"io"
	"os"

	"golang.org/x/term"
)

// Terminal abstracts the raw tty I/O the REPL needs. OSTerminal is the real
// implementation; FakeTerminal (terminal_fake.go) backs tests without a tty.
type Terminal interface {
	IsTTY() bool
	MakeRaw() (restore func() error, err error)
	Read(p []byte) (int, error)
	WriteString(s string) error
	Size() (width, height int, err error)
}

// OSTerminal drives a real terminal via golang.org/x/term.
type OSTerminal struct {
	in  *os.File
	out *os.File
}

func NewOSTerminal(in *os.File, out *os.File) *OSTerminal {
	return &OSTerminal{in: in, out: out}
}

func (t *OSTerminal) IsTTY() bool {
	return term.IsTerminal(int(t.in.Fd())) && term.IsTerminal(int(t.out.Fd()))
}

func (t *OSTerminal) MakeRaw() (func() error, error) {
	fd := int(t.in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() error { return term.Restore(fd, state) }, nil
}

func (t *OSTerminal) Read(p []byte) (int, error) { return t.in.Read(p) }

func (t *OSTerminal) WriteString(s string) error {
	_, err := io.WriteString(t.out, s)
	return err
}

func (t *OSTerminal) Size() (int, int, error) {
	w, h, err := term.GetSize(int(t.out.Fd()))
	if err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

// osPipe is a tiny seam so tests can construct OSTerminal over an os.Pipe.
func osPipe() (*os.File, *os.File, error) { return os.Pipe() }
