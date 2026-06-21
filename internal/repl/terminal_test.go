package repl

import "testing"

func TestFakeTerminalReadWrite(t *testing.T) {
	ft := NewFakeTerminal("ab", 80, 24)
	if !ft.IsTTY() {
		t.Fatal("FakeTerminal should report IsTTY true by default")
	}
	w, h, err := ft.Size()
	if err != nil || w != 80 || h != 24 {
		t.Fatalf("Size() = %d,%d,%v want 80,24,nil", w, h, err)
	}
	buf := make([]byte, 1)
	n, err := ft.Read(buf)
	if err != nil || n != 1 || buf[0] != 'a' {
		t.Fatalf("Read() = %d,%q,%v want 1,'a',nil", n, buf[:n], err)
	}
	if err := ft.WriteString("XY"); err != nil {
		t.Fatalf("WriteString err: %v", err)
	}
	if got := ft.Out.String(); got != "XY" {
		t.Fatalf("Out = %q want %q", got, "XY")
	}
	restore, err := ft.MakeRaw()
	if err != nil || !ft.Raw {
		t.Fatalf("MakeRaw should set Raw; err=%v", err)
	}
	if err := restore(); err != nil || ft.Raw {
		t.Fatalf("restore should clear Raw; err=%v", err)
	}
}

func TestOSTerminalIsTTYFalseForPipe(t *testing.T) {
	// os.Pipe() endpoints are never TTYs; guards against raw-mode in CI.
	r, w, err := osPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	term := NewOSTerminal(r, w)
	if term.IsTTY() {
		t.Fatal("pipe should not be a TTY")
	}
}
